import SwiftUI

/// Pushes Recipe Import on the Household tab.
struct RecipeImportRoute: Hashable {}

/// The tab shell's selection and the navigation stacks a notification can open, in the
/// environment of everything the shell shows, including sheets presented over it (the bell).
/// `nil` outside the tab shell, where there are no tabs to switch.
///
/// One object instead of an action per screen, so a new screen or sheet gets tab switching
/// by being inside the shell, not by being wired to it.
@Observable
final class TabRouter {
    var selection: AppTab = .menu
    var menuPath = NavigationPath()
    var pantryPath = NavigationPath()
    var householdPath = NavigationPath()
    /// A tapped push of a type this build doesn't know opens the bell.
    var showsNotifications = false

    /// Set by the tab shell (`MainTabView.configureRouter()`).
    @ObservationIgnored var check = NotificationSubjectCheck(
        pantryItemExists: { _ in true }, canOpenRecipeImport: { true })
    /// Shows a week on the Shop tab. Set by the tab shell, which owns the shopping store.
    @ObservationIgnored var showWeek: (ISOWeek) -> Void = { _ in }

    /// Switches to Shop and shows `week`, for example from a grocery list.
    func openShop(_ week: ISOWeek) {
        selection = .shop
        showWeek(week)
    }

    /// Opens a notification's screen, or the nearest one when its subject is gone, and
    /// returns what it opened. The bell and tapped pushes both come here.
    @discardableResult
    func open(_ destination: NotificationDestination) async -> NotificationDestination {
        let resolved = await NotificationRouting.resolve(destination, check: check)
        show(resolved)
        return resolved
    }

    /// Switches to a destination as it is, without checking its subject.
    func show(_ destination: NotificationDestination) {
        switch destination {
        case .pantryItem(let id):
            selection = .pantry
            pantryPath = NavigationPath([PantryItemRoute(itemID: id)])
        case .pantryThaw, .pantry:
            selection = .pantry
            pantryPath = NavigationPath()
        case .shopWeek(let week):
            openShop(week)
        case .shop:
            selection = .shop
        case .recipeImport:
            selection = .household
            householdPath = NavigationPath([RecipeImportRoute()])
        case .household:
            selection = .household
            householdPath = NavigationPath()
        case .notifications:
            showsNotifications = true
        }
    }
}
