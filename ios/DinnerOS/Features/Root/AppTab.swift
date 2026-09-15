import Foundation

/// Top-level destinations in the app's tab bar, in display order.
enum AppTab: String, CaseIterable, Hashable, Identifiable {
    /// Recipes and the week in one screen, with the day-by-day week behind a toggle.
    case menu
    case shop
    case pantry
    case household

    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .menu: "Menu"
        case .shop: "Shop"
        case .pantry: "Pantry"
        case .household: "Household"
        }
    }

    var systemImage: String {
        switch self {
        case .menu: "fork.knife"
        case .shop: "cart"
        case .pantry: "cabinet"
        case .household: "person.2"
        }
    }
}
