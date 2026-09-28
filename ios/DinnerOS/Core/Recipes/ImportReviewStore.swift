import Foundation
import Observation
import os

/// The current household's import review backlog: what the importer flagged, and the member's
/// decisions on it.
///
/// Main-actor state that lives as long as the app, like `SpecialtyStore`. The backlog is small
/// and only changes when someone runs an import or resolves an item, so it loads in one page
/// when the Household tab opens. `resolve` removes an item only after the server recorded the
/// decision, so nothing leaves the list that would come back on the next load.
@Observable
final class ImportReviewStore {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        /// The load failed, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    /// Oldest first, as the API returns them.
    private(set) var items: [ImportReview] = []
    /// Set when a reload failed while items stayed on screen.
    private(set) var refreshError: String?
    /// Cleared by a `404`: this API doesn't have import reviews yet, so the row hides instead
    /// of showing an error for something that isn't built (#421, the store catalog).
    private(set) var isAvailable = true
    /// True while a decision is being sent, so the screen can't send a second one on top.
    private(set) var isResolving = false

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: RecipesAPI?
    /// The user the backlog was loaded for, so another sign-in never sees it.
    @ObservationIgnored private var userID: String?
    /// Incremented whenever the household or user changes, so a slow response for an old
    /// household can't overwrite newer state.
    @ObservationIgnored private var scope = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "import-reviews")

    init(session: AuthSession, api: RecipesAPI?) {
        self.session = session
        self.api = api
    }

    /// The backlog sorted into real differences, spelling-only ones, and other gaps.
    var digest: ImportReviewDigest { ImportReviewDigest(items: items) }

    /// A store frozen with the given items, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, items: [ImportReview] = [], phase: Phase = .loaded,
        householdID: String = "household-1", isAvailable: Bool = true
    ) -> ImportReviewStore {
        let store = ImportReviewStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.items = items
        store.phase = phase
        store.isAvailable = isAvailable
        return store
    }

    /// Starts showing `householdID`'s backlog. Another household or user clears what was
    /// loaded first, including whether the route exists.
    func activate(householdID: String) {
        let currentUserID = session.currentUser?.id
        guard householdID != self.householdID || currentUserID != userID else { return }
        clear()
        self.householdID = householdID
        userID = currentUserID
    }

    /// The backlog, with a progress state when nothing is shown yet.
    func load() async {
        await load(showingProgress: phase != .loaded)
    }

    /// Pull to refresh: items stay on screen until the response arrives.
    func refresh() async {
        await load(showingProgress: false)
    }

    private func load(showingProgress: Bool) async {
        guard let api, let householdID else { return }
        let started = scope
        if showingProgress {
            items = []
            phase = .loading
        }
        do {
            let reviews = try await session.authorized { token in
                try await api.importReviews(householdID: householdID, accessToken: token)
            }
            guard started == scope else { return }
            items = reviews
            isAvailable = true
            refreshError = nil
            phase = .loaded
        } catch let error as APIError where error.status == 404 {
            guard started == scope else { return }
            Self.logger.info("Import reviews aren't available on this API yet")
            items = []
            isAvailable = false
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == scope, phase == .loading { phase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Import reviews load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Records `resolution` for `items` and takes them off the list once the server has it.
    ///
    /// Items the resolution does not apply to (a "same recipe" on a missing-steps item) are not
    /// sent and stay on the list, because the server would leave them open too. More than one
    /// page of ids goes out in several calls; a failure part-way keeps what already landed off
    /// the list and throws, so a retry sends only the rest. Throws for the screen to show.
    func resolve(
        _ items: [ImportReview], as resolution: ImportReviewResolution, linkedRecipeID: String? = nil
    ) async throws {
        guard let api, let householdID, !isResolving else { return }
        let targets = items.filter(resolution.applies(to:))
        guard !targets.isEmpty else { return }
        let started = scope
        isResolving = true
        defer { isResolving = false }

        var ids = targets.map(\.id)
        while !ids.isEmpty {
            let chunk = Array(ids.prefix(RecipesAPI.importReviewLimit))
            do {
                _ = try await session.authorized { token in
                    try await api.resolveImportReviews(
                        householdID: householdID, ids: chunk, resolution: resolution, recipeID: linkedRecipeID,
                        accessToken: token)
                }
            } catch {
                Self.logger.notice("Import review resolve failed: \(Self.describe(error), privacy: .public)")
                throw error
            }
            guard started == scope else { return }
            // Resolved already (by someone else) counts as gone too: either way it isn't open.
            let done = Set(chunk)
            self.items.removeAll { done.contains($0.id) }
            ids.removeFirst(chunk.count)
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
        items = []
        refreshError = nil
        isAvailable = true
    }

    /// Status and error code only; never tokens or response bodies.
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
