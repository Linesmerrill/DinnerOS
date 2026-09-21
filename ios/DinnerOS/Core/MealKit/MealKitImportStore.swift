import Foundation
import Observation
import os

/// The current household's meal-kit import: whether an account is linked and how the newest
/// run is going (`docs/meal-kit-import.md`).
///
/// Main-actor state that lives as long as the app, like `ImportReviewStore`. The work happens
/// on the server, so this only reads — and polls while a run is in flight, because there is
/// nothing on the device that would otherwise know it finished.
@Observable
final class MealKitImportStore {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        /// The load failed, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    private(set) var status: MealKitStatus = .unavailable
    /// Set when a reload failed while something was already on screen.
    private(set) var refreshError: String?
    /// Cleared by a `404`: this API doesn't have meal-kit import, so the screen hides
    /// instead of showing an error for something that isn't built.
    private(set) var isAvailable = true
    /// Set while linking, unlinking, or starting a run.
    private(set) var isWorking = false

    /// How often the status is re-read while a run is in flight. Slow on purpose: the work
    /// takes minutes and spans scheduled worker runs, so polling harder would only cost
    /// battery and requests.
    static let pollInterval: Duration = .seconds(15)

    @ObservationIgnored let service: MealKitService
    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: MealKitAPI?
    /// The user the status was loaded for, so another sign-in never sees it.
    @ObservationIgnored private var userID: String?
    /// Incremented whenever the household or user changes, so a slow response for an old
    /// household can't overwrite newer state.
    @ObservationIgnored private var scope = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "meal-kit-import")

    init(session: AuthSession, api: MealKitAPI?, service: MealKitService = .helloFresh) {
        self.session = session
        self.api = api
        self.service = service
    }

    /// A store frozen in a given state, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, status: MealKitStatus = .unavailable, phase: Phase = .loaded,
        householdID: String = "household-1", isAvailable: Bool = true
    ) -> MealKitImportStore {
        let store = MealKitImportStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.status = status
        store.phase = phase
        store.isAvailable = isAvailable
        return store
    }

    /// The newest run, when there is one.
    var job: MealKitImportJob? { status.latestJob }
    /// The linked account, when there is one.
    var link: MealKitLink? { status.link }
    /// Whether the server can accept a meal-kit sign-in at all.
    var isEnabled: Bool { isAvailable && status.enabled }
    /// Whether a run is going to make progress on its own.
    var isImporting: Bool { status.isWorking }

    /// Starts showing `householdID`'s import. Another household or user clears what was
    /// loaded first, including whether the route exists.
    func activate(householdID: String) {
        let currentUserID = session.currentUser?.id
        guard householdID != self.householdID || currentUserID != userID else { return }
        clear()
        self.householdID = householdID
        userID = currentUserID
    }

    /// The status, with a progress state when nothing is shown yet.
    func load() async {
        await load(showingProgress: phase != .loaded)
    }

    /// Pull to refresh: what's on screen stays until the response arrives.
    func refresh() async {
        await load(showingProgress: false)
    }

    /// Re-reads the status while a run is in flight, and stops as soon as it isn't.
    ///
    /// Held by a `.task`, so leaving the screen cancels it. Nothing here polls in the
    /// background: the push notification is what tells a closed app the import finished.
    func pollWhileImporting() async {
        while !Task.isCancelled, isImporting {
            do {
                try await Task.sleep(for: Self.pollInterval)
            } catch {
                return
            }
            await load(showingProgress: false)
        }
    }

    /// Links the meal-kit account and queues an import.
    ///
    /// The password is passed straight to the API call and is never stored on this device.
    /// Throws so the form can show the failure next to the fields.
    func link(email: String, password: String, startImport: Bool = true) async throws {
        guard let api, let householdID else { return }
        isWorking = true
        defer { isWorking = false }
        let started = scope
        let result = try await session.authorized { token in
            try await api.link(
                householdID: householdID, service: service, email: email, password: password,
                startImport: startImport, accessToken: token)
        }
        guard started == scope else { return }
        status = result
        isAvailable = true
        refreshError = nil
        phase = .loaded
    }

    /// Queues another run against the account that is already linked.
    func startImport() async throws {
        guard let api, let householdID else { return }
        isWorking = true
        defer { isWorking = false }
        let started = scope
        let job = try await session.authorized { token in
            try await api.startImport(householdID: householdID, service: service, accessToken: token)
        }
        guard started == scope else { return }
        status = MealKitStatus(enabled: status.enabled, link: status.link, latestJob: job)
    }

    /// Deletes the stored tokens and stops every run.
    func unlink() async throws {
        guard let api, let householdID else { return }
        isWorking = true
        defer { isWorking = false }
        let started = scope
        try await session.authorized { token in
            try await api.unlink(householdID: householdID, service: service, accessToken: token)
        }
        guard started == scope else { return }
        // The server cancelled the runs; read back rather than guessing what it did.
        await load(showingProgress: false)
    }

    private func load(showingProgress: Bool) async {
        guard let api, let householdID else { return }
        let started = scope
        if showingProgress {
            phase = .loading
        }
        do {
            let result = try await session.authorized { token in
                try await api.status(householdID: householdID, service: service, accessToken: token)
            }
            guard started == scope else { return }
            status = result
            isAvailable = true
            refreshError = nil
            phase = .loaded
        } catch let error as APIError where error.status == 404 {
            guard started == scope else { return }
            Self.logger.info("Meal-kit import isn't available on this API yet")
            status = .unavailable
            isAvailable = false
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == scope, phase == .loading { phase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Meal-kit import status load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        userID = nil
    }

    private func clear() {
        scope += 1
        phase = .idle
        status = .unavailable
        refreshError = nil
        isAvailable = true
        isWorking = false
    }

    /// Status and error code only; never tokens, credentials, or response bodies.
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
