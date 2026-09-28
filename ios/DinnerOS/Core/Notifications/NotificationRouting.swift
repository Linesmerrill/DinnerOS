import Foundation

/// The screen a tapped notification opens. A bell row and a tapped push both get theirs from
/// `NotificationRouting.destination(type:subject:)`, so the two can't lead different places.
nonisolated enum NotificationDestination: Hashable, Sendable {
    /// Pantry tab, with the item open.
    case pantryItem(id: String)
    /// Pantry tab at the top, where Take Out to Thaw is the first section.
    case pantryThaw(itemID: String)
    /// The Pantry tab: where a pantry notification lands once its item is gone.
    case pantry
    /// Shop tab, showing the week.
    case shopWeek(ISOWeek)
    /// Shop tab as it is: an order reminder whose week can't be read.
    case shop
    /// Household tab, with Recipe Import open.
    case recipeImport
    /// The Household tab: where an import notification lands for someone who can't import.
    case household
    /// A type this build doesn't know: a push opens the bell, a bell row stays put.
    case notifications

    /// The destination's name in `api/internal/notifications/testdata/routes.json`, which the
    /// API's tests check against every notification it can create.
    var fixtureName: String {
        switch self {
        case .pantryItem: "pantry_item"
        case .pantryThaw: "pantry_thaw"
        case .pantry: "pantry"
        case .shopWeek: "shop_week"
        case .shop: "shop"
        case .recipeImport: "recipe_import"
        case .household: "household"
        case .notifications: "notifications"
        }
    }

    /// What VoiceOver says a row does when tapped.
    var accessibilityHint: String {
        switch self {
        case .pantryItem: String(localized: "Opens the item in Pantry.")
        case .pantryThaw: String(localized: "Opens Take Out to Thaw in Pantry.")
        case .pantry: String(localized: "Opens Pantry.")
        case .shopWeek: String(localized: "Opens the week in Shop.")
        case .shop: String(localized: "Opens Shop.")
        case .recipeImport: String(localized: "Opens Recipe Import in Household.")
        case .household: String(localized: "Opens Household.")
        case .notifications: String(localized: "Marks it read.")
        }
    }
}

/// What the router needs to know about the household to avoid opening a screen for something
/// that's gone. Closures, so tests can answer without stores.
struct NotificationSubjectCheck {
    /// Whether the pantry item still exists. `true` when that can't be told (the pantry failed
    /// to load): the item screen shows the load error with Try Again.
    var pantryItemExists: (String) async -> Bool
    /// Whether this member may open Recipe Import (`recipes.import`, and an API with the route).
    var canOpenRecipeImport: () -> Bool
}

/// Where each notification opens (`api/internal/notifications/testdata/routes.json`).
enum NotificationRouting {
    /// The screen for a notification, from its type first and then its subject, so a new
    /// type about a known kind of record still opens something sensible.
    nonisolated static func destination(
        type: AppNotificationType, subject: AppNotificationSubject?
    ) -> NotificationDestination {
        switch type {
        case .pantryLow:
            return subject?.pantryItemID.map { .pantryItem(id: $0) } ?? .pantry
        case .pantryThaw:
            return subject?.pantryItemID.map { .pantryThaw(itemID: $0) } ?? .pantry
        case .shoppingOrderDue:
            return shop(subject)
        case .recipeImportFinished, .recipeImportAttention:
            return .recipeImport
        default:
            break
        }
        switch subject?.kind {
        case AppNotificationSubject.pantryItemKind:
            return subject.map { .pantryItem(id: $0.id) } ?? .pantry
        case AppNotificationSubject.shoppingWeekKind:
            return shop(subject)
        case AppNotificationSubject.recipeImportKind:
            return .recipeImport
        default:
            return .notifications
        }
    }

    nonisolated static func destination(for notification: AppNotification) -> NotificationDestination {
        destination(type: notification.type, subject: notification.subject)
    }

    nonisolated static func destination(for route: PushRoute) -> NotificationDestination {
        destination(type: route.type, subject: route.subject)
    }

    /// The nearest screen that exists: an item that's gone opens the Pantry tab, and Recipe
    /// Import for someone who can't use it opens the Household tab. Never an empty screen.
    static func resolve(
        _ destination: NotificationDestination, check: NotificationSubjectCheck
    ) async -> NotificationDestination {
        switch destination {
        case .pantryItem(let id):
            return await check.pantryItemExists(id) ? destination : .pantry
        case .recipeImport:
            return check.canOpenRecipeImport() ? destination : .household
        default:
            return destination
        }
    }

    private nonisolated static func shop(_ subject: AppNotificationSubject?) -> NotificationDestination {
        subject?.shoppingWeek.flatMap(ISOWeek.init).map { .shopWeek($0) } ?? .shop
    }
}
