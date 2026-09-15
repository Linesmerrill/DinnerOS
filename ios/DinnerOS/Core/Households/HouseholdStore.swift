import Foundation
import Observation
import os

/// The signed-in user's households, the selected household, and invitation links.
///
/// Main-actor state, like `AuthSession`. Every request goes through
/// `AuthSession.authorized`. Permissions exposed here only decide what the UI offers;
/// the API enforces them on every request.
@Observable
final class HouseholdStore {
    enum Phase: Equatable {
        /// Signed out, or signed in and not loaded yet.
        case idle
        case loading
        /// Signed in with no household: show onboarding.
        case needsHousehold
        /// `current` holds the selected household.
        case ready
        /// The first load failed, so there's nothing to show.
        case failed(String)
    }

    enum InviteStatus: Equatable {
        /// An invitation link arrived; ask before joining.
        case awaitingConfirmation
        case accepting
        case joined(householdName: String)
        case failed(String)
    }

    private(set) var phase: Phase = .idle
    private(set) var households: [HouseholdListItem] = []
    private(set) var current: HouseholdDetail?
    /// Pending invitations for `current`; empty without `members.invite`.
    private(set) var invitations: [HouseholdInvitation] = []
    /// Set when a reload failed while content stayed on screen.
    private(set) var refreshError: String?
    private(set) var inviteStatus: InviteStatus?

    var access: HouseholdAccess? { current?.access }
    var hasPendingInvite: Bool { pendingInviteToken != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: HouseholdsAPI?
    @ObservationIgnored private let selection: any HouseholdSelectionStorage
    @ObservationIgnored private let inviteURLScheme: String
    /// Kept in memory only: a token is a secret and must not outlive the process.
    @ObservationIgnored private var pendingInviteToken: String?
    /// Incremented by every load and reset so a slow response can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "households")

    init(
        session: AuthSession,
        api: HouseholdsAPI?,
        selection: any HouseholdSelectionStorage,
        inviteURLScheme: String = InviteLink.defaultScheme
    ) {
        self.session = session
        self.api = api
        self.selection = selection
        self.inviteURLScheme = inviteURLScheme
    }

    /// A store frozen in the given state, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession,
        phase: Phase,
        current: HouseholdDetail? = nil,
        households: [HouseholdListItem] = [],
        invitations: [HouseholdInvitation] = []
    ) -> HouseholdStore {
        let store = HouseholdStore(session: session, api: nil, selection: InMemoryHouseholdSelection())
        store.phase = phase
        store.current = current
        store.households = households
        store.invitations = invitations
        return store
    }

    // MARK: - Loading

    /// Loads the user's households and the selected one. Call after sign-in and to refresh.
    ///
    /// The selection is the stored household ID when the user still belongs to it,
    /// otherwise the first household in the list.
    func load() async {
        guard let api, let userID = session.currentUser?.id else { return }
        generation += 1
        let started = generation
        if phase != .ready && phase != .needsHousehold {
            phase = .loading
        }

        do {
            let items = try await session.authorized { token in try await api.listHouseholds(accessToken: token) }
            guard started == generation else { return }
            households = items

            let preferred = selection.selectedHouseholdID(for: userID)
            guard let chosen = items.first(where: { $0.id == preferred }) ?? items.first else {
                selection.setSelectedHouseholdID(nil, for: userID)
                current = nil
                invitations = []
                refreshError = nil
                phase = .needsHousehold
                promptForPendingInvite()
                return
            }
            selection.setSelectedHouseholdID(chosen.id, for: userID)

            let detail = try await session.authorized { token in
                try await api.household(id: chosen.id, accessToken: token)
            }
            var pending: [HouseholdInvitation] = []
            if detail.access.can(.membersInvite) {
                pending = try await session.authorized { token in
                    try await api.pendingInvitations(householdID: chosen.id, accessToken: token)
                }
            }
            guard started == generation else { return }
            current = detail
            invitations = pending
            refreshError = nil
            phase = .ready
        } catch is CancellationError {
            if started == generation, phase == .loading { phase = .idle }
            return
        } catch {
            guard started == generation, session.currentUser != nil else { return }
            Self.logger.notice("Household load failed: \(Self.describe(error), privacy: .public)")
            let message = Self.message(for: error)
            if phase == .ready || phase == .needsHousehold {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
        promptForPendingInvite()
    }

    /// Switches to another of the user's households.
    func selectHousehold(id: String) async {
        guard let userID = session.currentUser?.id, id != current?.household.id else { return }
        selection.setSelectedHouseholdID(id, for: userID)
        await load()
    }

    /// Clears in-memory state after sign-out. A pending invitation link is kept so it
    /// can be accepted after the next sign-in; the stored selection is kept per user.
    func reset() {
        generation += 1
        phase = .idle
        households = []
        current = nil
        invitations = []
        refreshError = nil
        if inviteStatus != .awaitingConfirmation {
            inviteStatus = nil
        }
    }

    // MARK: - Creating and joining

    /// Creates a household with the caller as admin and selects it.
    func createHousehold(name: String, timeZone: String) async throws {
        let api = try requireAPI()
        let response = try await session.authorized { token in
            try await api.createHousehold(name: name, timeZone: timeZone, defaultServings: nil, accessToken: token)
        }
        Self.logger.info("Household created")
        select(response.household.id)
        await load()
    }

    /// Joins with a typed invite code and selects that household.
    @discardableResult
    func acceptInvitation(code: String) async throws -> Household {
        try await accept(.code(InviteCode.normalize(code)))
    }

    private func accept(_ secret: InvitationSecret) async throws -> Household {
        let api = try requireAPI()
        let response = try await session.authorized { token in
            try await api.acceptInvitation(secret, accessToken: token)
        }
        Self.logger.info("Invitation accepted")
        select(response.household.id)
        await load()
        return response.household
    }

    // MARK: - Invitation links

    /// Handles a URL opened by the system. Returns `false` when it isn't an invitation
    /// link. When signed out, the token waits for the next sign-in.
    @discardableResult
    func handleOpenURL(_ url: URL) -> Bool {
        guard let link = InviteLink(url: url, scheme: inviteURLScheme) else { return false }
        switch link {
        case .token(let token):
            Self.logger.info("Invitation link received")
            pendingInviteToken = token
            if inviteStatus != .accepting {
                inviteStatus = nil
            }
            promptForPendingInvite()
        case .missingToken:
            Self.logger.notice("Invitation link without a token")
            inviteStatus = .failed(
                String(localized: "This invitation link is incomplete. Ask for a new invitation, or enter the code."))
        }
        return true
    }

    /// Accepts the pending invitation link after the user confirms. Returns the running
    /// task, or `nil` when there's nothing to accept. State changes to `.accepting`
    /// before this returns, so a confirmation alert can dismiss cleanly.
    @discardableResult
    func confirmPendingInvite() -> Task<Void, Never>? {
        guard let token = pendingInviteToken, inviteStatus != .accepting else { return nil }
        pendingInviteToken = nil
        inviteStatus = .accepting
        return Task {
            do {
                let household = try await accept(.token(token))
                inviteStatus = .joined(householdName: household.name)
            } catch is CancellationError {
                inviteStatus = nil
            } catch {
                Self.logger.notice("Invitation link rejected: \(Self.describe(error), privacy: .public)")
                inviteStatus = .failed(Self.message(for: error))
            }
        }
    }

    func declinePendingInvite() {
        pendingInviteToken = nil
        if inviteStatus == .awaitingConfirmation {
            inviteStatus = nil
        }
    }

    /// Dismisses a finished `.joined` or `.failed` status.
    func dismissInviteStatus() {
        switch inviteStatus {
        case .joined, .failed: inviteStatus = nil
        default: break
        }
    }

    private func promptForPendingInvite() {
        guard
            pendingInviteToken != nil,
            session.currentUser != nil,
            phase == .ready || phase == .needsHousehold,
            inviteStatus != .accepting
        else { return }
        inviteStatus = .awaitingConfirmation
    }

    // MARK: - Managing the current household

    func updateHousehold(_ changes: HouseholdChanges) async throws {
        guard !changes.isEmpty, let householdID = current?.household.id else { return }
        try await mutate { api, token in
            _ = try await api.updateHousehold(id: householdID, changes: changes, accessToken: token)
        }
    }

    func changeRole(of member: HouseholdMember, to role: HouseholdRole) async throws {
        guard let householdID = current?.household.id else { return }
        try await mutate { api, token in
            _ = try await api.changeRole(
                householdID: householdID, userID: member.userID, to: role, accessToken: token)
        }
    }

    func removeMember(_ member: HouseholdMember) async throws {
        guard let householdID = current?.household.id else { return }
        try await mutate { api, token in
            try await api.removeMember(householdID: householdID, userID: member.userID, accessToken: token)
        }
    }

    /// Leaves the current household and clears the selection. The API refuses with
    /// `409 last_admin` when the caller is its only admin.
    func leaveHousehold() async throws {
        guard let householdID = current?.household.id, let userID = session.currentUser?.id else { return }
        try await mutate { api, token in
            try await api.removeMember(householdID: householdID, userID: userID, accessToken: token)
            selection.setSelectedHouseholdID(nil, for: userID)
        }
        Self.logger.info("Left household")
    }

    /// Invites `email` with `role`. The response's `code` is shown once; never log it.
    func createInvitation(email: String, role: HouseholdRole) async throws -> CreateInvitationResponse {
        guard let householdID = current?.household.id else { throw AuthSessionError.signedOut }
        return try await mutate { api, token in
            try await api.createInvitation(householdID: householdID, email: email, role: role, accessToken: token)
        }
    }

    func revokeInvitation(_ invitation: HouseholdInvitation) async throws {
        guard let householdID = current?.household.id else { return }
        try await mutate { api, token in
            try await api.revokeInvitation(householdID: householdID, invitationID: invitation.id, accessToken: token)
        }
    }

    /// The current user is the household's only admin, so the API will refuse to let them
    /// leave. A hint for the UI; `nil` when the member list isn't visible.
    var isOnlyAdmin: Bool? {
        guard let current, current.role == .admin, let members = current.members else { return nil }
        return members.filter { $0.role == .admin }.count <= 1
    }

    // MARK: - Helpers

    /// Runs a change, then reloads so the UI shows the server's state. A rejected change
    /// (403, 404, 409) also reloads, because it usually means the local state is stale.
    private func mutate<Result: Sendable>(
        _ operation: (HouseholdsAPI, String) async throws -> Result
    ) async throws -> Result {
        let api = try requireAPI()
        do {
            let result = try await session.authorized { token in try await operation(api, token) }
            await load()
            return result
        } catch let error as APIError where [403, 404, 409].contains(error.status) {
            Self.logger.notice("Household change rejected: \(Self.describe(error), privacy: .public)")
            await load()
            throw error
        }
    }

    private func select(_ householdID: String) {
        guard let userID = session.currentUser?.id else { return }
        selection.setSelectedHouseholdID(householdID, for: userID)
    }

    private func requireAPI() throws -> HouseholdsAPI {
        guard let api else { throw AuthSessionError.notConfigured }
        return api
    }

    static func message(for error: any Error) -> String {
        (error as? LocalizedError)?.errorDescription ?? String(localized: "Something went wrong. Try again.")
    }

    /// Status and error code only; never tokens, codes, or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}
