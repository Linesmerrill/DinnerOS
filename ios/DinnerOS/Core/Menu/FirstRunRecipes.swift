import Foundation

/// When the Menu tab shows its get-started card to a household whose library is still
/// empty (#530): a new household lands there with nothing to plan, so the first thing it is
/// shown is how to get recipes in, by browsing shared recipes or adding one.
///
/// Pure: the rule for *when* to show lives here, tested in words, while `GetStartedSection`
/// only draws it.
nonisolated enum FirstRunRecipes {
    /// Everything the rule reads. Assembled by the view from the library and this member's
    /// own dismissal.
    struct Input: Equatable, Sendable {
        /// The library's first page has come back, so "no recipes" is a fact rather than
        /// a screen that hasn't loaded.
        var libraryLoaded = false
        var hasRecipes = false
        /// A search or filter is narrowing the library, so an empty list says nothing about
        /// whether the household has recipes.
        var isFiltered = false
        /// This member dismissed the card for this household.
        var isDismissed = false
    }

    /// Whether the card shows for `input`.
    ///
    /// A household with recipes never sees it, whatever else is true: the point of the
    /// card is an empty library, and a household that has one has moved on.
    static func shows(_ input: Input) -> Bool {
        input.libraryLoaded && !input.hasRecipes && !input.isFiltered && !input.isDismissed
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
