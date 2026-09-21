import Foundation

/// What the Menu tab offers a household whose library is still empty (#530).
///
/// A new household lands on the Menu tab with nothing to plan, so the first thing it is
/// shown is how to get recipes in — importing a meal-kit order history above all, because
/// that is the one action that fills a library in a single sign-in.
///
/// Pure: the rule for *what* to show lives here, tested in words, while `GetStartedSection`
/// only draws it.
nonisolated enum FirstRunRecipes {
    /// Everything the rule reads. Assembled by the view from the library, the household's
    /// access, the meal-kit status, and this member's own dismissal.
    struct Input: Equatable, Sendable {
        /// The library's first page has come back, so "no recipes" is a fact rather than
        /// a screen that hasn't loaded.
        var libraryLoaded = false
        var hasRecipes = false
        /// A search or filter is narrowing the library, so an empty list says nothing about
        /// whether the household has recipes.
        var isFiltered = false
        /// This member dismissed the surface for this household.
        var isDismissed = false
        /// `recipes.import`. Without it the import endpoints answer `403`, so the surface
        /// neither offers the sign-in nor reads the status.
        var canImport = false
        /// The server can accept a meal-kit sign-in at all (`MealKitImportStore.isEnabled`).
        var importEnabled = false
        var job: MealKitImportJob?
    }

    /// Why the import isn't on offer, when it isn't.
    enum Unavailable: Equatable, Sendable {
        /// This member's role doesn't carry `recipes.import`.
        case noPermission
        /// This API has no meal-kit import, or no encryption key for one.
        case notOnThisServer
    }

    /// What the surface shows. Every case still offers the catalog and typing a recipe;
    /// they differ in what they say about the import.
    enum State: Equatable, Sendable {
        /// Nothing to show.
        case hidden
        /// Nothing imported yet: the sign-in is the main action.
        case offer
        /// A run is queued or going — anyone's in this household, not only this member's.
        case importing(MealKitImportJob)
        /// The last run couldn't read the account; signing in again is the fix.
        case needsSignIn(detail: String)
        /// A run gave up.
        case stopped(detail: String)
        /// A run finished and added recipes this screen hasn't listed yet.
        case imported(count: Int)
        /// A run finished having found nothing to import.
        case foundNothing
        /// The import isn't this member's to start.
        case unavailable(Unavailable)
    }

    /// The surface for `input`.
    ///
    /// A household with recipes never sees it, whatever else is true: the point of the
    /// surface is an empty library, and a household that has one has moved on.
    static func state(for input: Input) -> State {
        guard input.libraryLoaded, !input.hasRecipes, !input.isFiltered, !input.isDismissed else {
            return .hidden
        }
        guard input.canImport else { return .unavailable(.noPermission) }
        guard input.importEnabled else { return .unavailable(.notOnThisServer) }
        // Nothing is stored about the meal-kit account any more (#533): the only
        // history is the last run, and without one there is nothing to offer but the
        // sign-in itself.
        guard let job = input.job else { return .offer }
        switch job.state {
        case .queued, .running:
            return .importing(job)
        case .failed:
            return .stopped(detail: job.lastError?.message ?? stoppedDetail)
        case .finished:
            return job.recipesAdded > 0 ? .imported(count: job.recipesAdded) : .foundNothing
        case .canceled:
            return .offer
        }
    }

    /// Whether the status is worth reading at all for this member. Without `recipes.import`
    /// the request is a guaranteed `403`, so it is never made.
    static func readsImportStatus(canImport: Bool, hasRecipes: Bool) -> Bool {
        canImport && !hasRecipes
    }

    private static var expiredDetail: String {
        String(localized: "Your meal-kit sign-in expired. Sign in again to carry on importing.")
    }

    private static var stoppedDetail: String {
        String(localized: "The import stopped before it finished. Your recipes weren't changed.")
    }
}

/// Remembers which households this member has waved the Menu tab's get-started surface away
/// for, so dismissing it means dismissing it.
///
/// Keyed by user and household, like `AutopilotPromptStorage`: one member closing it is not
/// the household saying it, and the same phone may hold two households at different stages.
/// It is a preference about a screen, not household data, so it lives in `UserDefaults` and
/// not on the server — there is nothing here another device needs, and syncing it would mean
/// a write path (and a migration) for one boolean whose only cost of being wrong is one card
/// on one phone.
protocol FirstRunDismissalStorage: AnyObject {
    func isDismissed(userID: String, householdID: String) -> Bool
    func dismiss(userID: String, householdID: String)
}

final class UserDefaultsFirstRunDismissals: FirstRunDismissalStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func isDismissed(userID: String, householdID: String) -> Bool {
        defaults.bool(forKey: Self.key(userID: userID, householdID: householdID))
    }

    func dismiss(userID: String, householdID: String) {
        defaults.set(true, forKey: Self.key(userID: userID, householdID: householdID))
    }

    static func key(userID: String, householdID: String) -> String {
        "menuGetStartedDismissed.\(userID).\(householdID)"
    }
}

/// A non-persistent record for tests and previews.
final class InMemoryFirstRunDismissals: FirstRunDismissalStorage {
    private(set) var dismissed: Set<String> = []

    init(dismissed: Set<String> = []) {
        self.dismissed = dismissed
    }

    func isDismissed(userID: String, householdID: String) -> Bool {
        dismissed.contains("\(userID).\(householdID)")
    }

    func dismiss(userID: String, householdID: String) {
        dismissed.insert("\(userID).\(householdID)")
    }
}
