import Foundation

/// Top-level destinations in the app's tab bar, in display order.
enum AppTab: String, CaseIterable, Hashable, Identifiable {
    case recipes
    case week
    case shop
    case pantry
    case household

    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .week: "Week"
        case .recipes: "Recipes"
        case .shop: "Shop"
        case .pantry: "Pantry"
        case .household: "Household"
        }
    }

    var systemImage: String {
        switch self {
        case .week: "calendar"
        case .recipes: "book.closed"
        case .shop: "cart"
        case .pantry: "cabinet"
        case .household: "person.2"
        }
    }
}
