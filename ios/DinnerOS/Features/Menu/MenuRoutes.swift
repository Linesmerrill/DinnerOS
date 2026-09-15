import SwiftUI

/// Opens the All Meals list filtered by a section's "Show More" query.
struct AllMealsRoute: Hashable {
    let title: String
    let query: MenuRecipeQuery
}

/// Opens the list of past weeks.
struct PastWeeksRoute: Hashable {}

/// Which half of the Menu tab is showing: the menu, or the day-by-day week.
enum MenuMode: String, CaseIterable, Identifiable {
    case menu
    case byDay

    var id: String { rawValue }

    var title: LocalizedStringKey {
        switch self {
        case .menu: "Menu"
        case .byDay: "By Day"
        }
    }
}
