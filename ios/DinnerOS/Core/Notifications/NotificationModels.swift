import Foundation

/// A notification's stable type (`notifications.Type` in the API). Unknown types show their
/// title and body. Where each one opens is `NotificationRouting`.
nonisolated struct AppNotificationType: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let pantryLow = AppNotificationType(rawValue: "pantry.low")
    /// A frozen item is needed for one of today's meals.
    static let pantryThaw = AppNotificationType(rawValue: "pantry.thaw")
    /// The household's order day has arrived and the week isn't marked ordered.
    static let shoppingOrderDue = AppNotificationType(rawValue: "shopping.order_due")
    /// A meal-kit recipe import finished.
    static let recipeImportFinished = AppNotificationType(rawValue: "recipe_import.finished")
    /// A meal-kit recipe import needs a person.
    static let recipeImportAttention = AppNotificationType(rawValue: "recipe_import.attention")
}

/// What a notification is about, and so what tapping it opens.
nonisolated struct AppNotificationSubject: Codable, Hashable, Sendable {
    static let pantryItemKind = "pantry_item"
    static let shoppingWeekKind = "shopping_week"
    /// A meal-kit import job. The app opens Recipe Import, which shows the latest run.
    static let recipeImportKind = "recipe_import_job"

    let kind: String
    let id: String

    /// The pantry item to open, when the subject is one.
    var pantryItemID: String? {
        kind == Self.pantryItemKind ? id : nil
    }

    /// The ISO week to open in Shop, when the subject is one.
    var shoppingWeek: String? {
        kind == Self.shoppingWeekKind ? id : nil
    }
}

/// A household notification as the signed-in member sees it (`Notification`). Named to
/// stay clear of Foundation's `Notification`.
nonisolated struct AppNotification: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let householdID: String
    let type: AppNotificationType
    let title: String
    let body: String
    let subject: AppNotificationSubject
    /// Whether this member has read it.
    var read: Bool
    let createdAt: Date

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case type, title, body, subject, read, createdAt
    }
}

/// Response to `GET .../notifications`.
nonisolated struct NotificationListResponse: Decodable, Equatable, Sendable {
    let items: [AppNotification]
    /// Pass as `before` for the next page; `nil` on the last page.
    let nextCursor: String?
}

/// Response to `GET .../notifications/unread-count` and `POST .../notifications/read`.
nonisolated struct NotificationUnreadCount: Decodable, Equatable, Sendable {
    let unreadCount: Int
}

/// The body of `POST .../notifications/read`: `ids`, or `all`.
nonisolated struct MarkNotificationsReadRequest: Encodable, Equatable, Sendable {
    var ids: [String]?
    var all: Bool?
}
