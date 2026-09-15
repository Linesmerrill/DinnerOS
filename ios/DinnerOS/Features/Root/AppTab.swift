import Foundation

/// Top-level destinations in the app's tab bar.
enum AppTab: String, CaseIterable, Hashable, Identifiable {
    case week
    case recipes
    case shop
    case household

    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .week: "Week"
        case .recipes: "Recipes"
        case .shop: "Shop"
        case .household: "Household"
        }
    }

    var systemImage: String {
        switch self {
        case .week: "calendar"
        case .recipes: "book.closed"
        case .shop: "cart"
        case .household: "person.2"
        }
    }
}
