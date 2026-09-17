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

    /// An invitation link's progress: checked with the API, confirmed, then accepted.
    enum InviteStatus: Equatable {
        /// An invitation link arrived; the API is describing it.
        case loadingPreview
        /// Ask before joining, naming the household and who sent the invitation.
        case awaitingConfirmation(InvitationPreview)
        /// The invitation is unknown, expired, revoked, or already used.
        case invalid
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

    /// The day the current household's weeks start on; Sunday until one loads.
    var weekStartsOn: PlanDay { current?.household.weekStartsOn ?? PlanDay.defaultWeekStart }
    var hasPendingInvite: Bool { pendingInviteToken != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: HouseholdsAPI?
    @ObservationIgnored private let selection: any HouseholdSelectionStorage
    @ObservationIgnored private let inviteURLScheme: String
    @ObservationIgnored private let inviteLinkHost: String
    /// Kept in memory only: a token is a secret and must not outlive the process.
    @ObservationIgnored private var pendingInviteToken: String?
    /// Incremented by every load and reset so a slow response can't overwrite newer state.
    @ObservationIgnored private var generation = 0
    /// Incremented whenever the invitation-link flow restarts or is abandoned, so a slow
    /// preview or accept can't overwrite newer state.
    @ObservationIgnored private var inviteGeneration = 0
    /// The latest invitation-link preview or accept. Tests await it.
    @ObservationIgnored private(set) var inviteTask: Task<Void, Never>?

    private static let logger = Logger(subsystem: "DinnerOS", category: "households")

    init(
        session: AuthSession,
        api: HouseholdsAPI?,
        selection: any HouseholdSelectionStorage,
        inviteURLScheme: String = InviteLink.defaultScheme,
        inviteLinkHost: String = InviteLink.defaultWebHost
    ) {
        self.session = session
        self.api = api
        self.selection = selection
        self.inviteURLScheme = inviteURLScheme
        self.inviteLinkHost = inviteLinkHost
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
    /// can be previewed and accepted after the next sign-in; the stored selection is kept
    /// per user.
    func reset() {
        generation += 1
        phase = .idle
        households = []
        current = nil
        invitations = []
        refreshError = nil
        inviteGeneration += 1
        inviteStatus = nil
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

    /// Describes the invitation for a typed code without accepting it, so the user can
    /// confirm which household they're joining. Needs no sign-in.
    func previewInvitation(code: String) async throws -> InvitationPreview {
        let api = try requireAPI()
        return try await api.previewInvitation(.code(InviteCode.normalize(code)))
    }

    /// Whether `error` means the invitation is unknown, expired, revoked, or already used.
    static func isInvalidInvitation(_ error: any Error) -> Bool {
        (error as? APIError)?.code == "invitation_invalid"
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

    /// Handles a URL opened by the system: a universal link or a custom-scheme link.
    /// Returns `false` when it isn't an invitation link. When signed out, the token waits
    /// for the next sign-in.
    @discardableResult
    func handleOpenURL(_ url: URL) -> Bool {
        guard let link = InviteLink(url: url, scheme: inviteURLScheme, webHost: inviteLinkHost) else {
            return false
        }
        switch link {
        case .token(let token):
            Self.logger.info("Invitation link received")
            pendingInviteToken = token
            if inviteStatus != .accepting {
                inviteGeneration += 1
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

    /// Accepts the pending invitation link after the user confirms its preview. Returns
    /// the running task, or `nil` when nothing is awaiting confirmation. State changes to
    /// `.accepting` before this returns, so a confirmation alert can dismiss cleanly.
    @discardableResult
    func confirmPendingInvite() -> Task<Void, Never>? {
        guard let token = pendingInviteToken, case .awaitingConfirmation = inviteStatus else { return nil }
        pendingInviteToken = nil
        inviteGeneration += 1
        let started = inviteGeneration
        inviteStatus = .accepting
        let task = Task {
            do {
                let household = try await accept(.token(token))
                guard started == inviteGeneration else { return }
                inviteStatus = .joined(householdName: household.name)
            } catch is CancellationError {
                if started == inviteGeneration { inviteStatus = nil }
            } catch {
                guard started == inviteGeneration else { return }
                Self.logger.notice("Invitation link rejected: \(Self.describe(error), privacy: .public)")
                inviteStatus = Self.isInvalidInvitation(error) ? .invalid : .failed(Self.message(for: error))
            }
        }
        inviteTask = task
        return task
    }

    /// Discards the pending link, including one whose preview is still loading.
    func declinePendingInvite() {
        pendingInviteToken = nil
        switch inviteStatus {
        case .loadingPreview, .awaitingConfirmation:
            inviteGeneration += 1
            inviteStatus = nil
        default:
            break
        }
    }

    /// Dismisses a finished `.invalid`, `.joined`, or `.failed` status, then previews any
    /// link that arrived in the meantime.
    func dismissInviteStatus() {
        switch inviteStatus {
        case .invalid, .joined, .failed:
            inviteStatus = nil
            promptForPendingInvite()
        default:
            break
        }
    }

    /// Previews the pending link, then asks before joining. It waits for a signed-in user
    /// with households loaded. Preview needs no access token, but joining does, and the
    /// prompt shouldn't cover sign-in.
    private func promptForPendingInvite() {
        guard
            let token = pendingInviteToken,
            let api,
            session.currentUser != nil,
            phase == .ready || phase == .needsHousehold,
            inviteStatus == nil
        else { return }
        inviteGeneration += 1
        let started = inviteGeneration
        inviteStatus = .loadingPreview
        inviteTask = Task {
            do {
                let preview = try await api.previewInvitation(.token(token))
                guard started == inviteGeneration else { return }
                inviteStatus = .awaitingConfirmation(preview)
            } catch is CancellationError {
                if started == inviteGeneration { inviteStatus = nil }
            } catch {
                guard started == inviteGeneration else { return }
                Self.logger.notice("Invitation preview failed: \(Self.describe(error), privacy: .public)")
                pendingInviteToken = nil
                inviteStatus = Self.isInvalidInvitation(error) ? .invalid : .failed(Self.message(for: error))
            }
        }
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
