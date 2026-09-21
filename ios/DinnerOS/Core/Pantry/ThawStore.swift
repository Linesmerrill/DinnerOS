import Foundation
import Observation
import os

/// What today's planned meals need out of the freezer.
///
/// The API answers the whole question — which frozen items today's meals use, how long a
/// portion takes to thaw, and when to move it over — so this only fetches and holds it. A
/// failure is quiet: the reminder is a nudge, and an error banner about a missing nudge
/// would be worse than the nudge being missing.
@Observable
final class ThawStore {
    private(set) var householdID: String?
    private(set) var due: ThawDue?
    private(set) var isLoading = false

    /// Items the member hasn't waved away today, in the API's order.
    var items: [ThawItem] {
        (due?.items ?? []).filter { !dismissed.contains($0.itemID) }
    }

    @ObservationIgnored private var dismissed: Set<String> = []
    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PantryAPI?
    @ObservationIgnored private var userID: String?
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "thaw")

    init(session: AuthSession, api: PantryAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with the given items, for SwiftUI previews. It has no network access.
    static func preview(session: AuthSession, due: ThawDue?) -> ThawStore {
        let store = ThawStore(session: session, api: nil)
        store.householdID = "household-1"
        store.due = due
        return store
    }

    /// Shows `householdID`'s reminders, reloading when the household or the user changes.
    func activate(householdID: String) async {
        let currentUserID = session.currentUser?.id
        if self.householdID != householdID || userID != currentUserID {
            self.householdID = householdID
            userID = currentUserID
            due = nil
            dismissed = []
        }
        await load()
    }

    /// Reloads today's reminders. It runs on every pantry appearance, so it never shows a
    /// spinner of its own.
    func load() async {
        guard let api, let householdID else { return }
        generation += 1
        let started = generation
        isLoading = true
        defer { if started == generation { isLoading = false } }
        do {
            let loaded = try await session.authorized { token in
                try await api.thawDue(householdID: householdID, accessToken: token)
            }
            guard started == generation else { return }
            // A new day clears yesterday's dismissals along with yesterday's items, so a
            // waved-away reminder comes back tomorrow if the item is still needed.
            if loaded.date != due?.date {
                dismissed = dismissed.intersection(Set(loaded.items.map(\.itemID)))
            }
            due = loaded
        } catch is CancellationError {
        } catch {
            Self.logger.notice("Thaw reminders failed to load: \(String(describing: error), privacy: .public)")
        }
    }

    /// Hides one reminder for the rest of this session: the member has been told, and the
    /// server has no business remembering whether they walked to the freezer.
    func dismiss(_ item: ThawItem) {
        dismissed.insert(item.itemID)
    }

    func reset() {
        generation += 1
        householdID = nil
        userID = nil
        due = nil
        dismissed = []
        isLoading = false
    }
}
