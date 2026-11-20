import Foundation

/// Notifications that say the same thing, shown as one row (build 121).
///
/// Ten "Your invite counted" rows in a row said nothing the first one didn't. Notifications of
/// the same type with the same title fold into the newest of them, which shows how many more
/// there are. Grouping is display only: the list still loads, pages, and marks read one
/// notification at a time, and the server may group too (agent D's side); this copes with both.
nonisolated struct NotificationGroup: Identifiable, Hashable, Sendable {
    /// Newest first. Never empty.
    let items: [AppNotification]

    /// The newest notification's id, so a row keeps its identity as older ones join it.
    var id: String { latest.id }
    /// The row shows this one's title, body, and age, and a tap opens it.
    var latest: AppNotification { items[0] }
    /// How many older notifications this row stands for.
    var moreCount: Int { items.count - 1 }
    /// Unread while any notification in it is.
    var isRead: Bool { items.allSatisfy(\.read) }
    var unread: [AppNotification] { items.filter { !$0.read } }
}

nonisolated enum NotificationGrouping {
    /// Types whose notifications repeat with only a name changing, so they fold together.
    /// A low pantry item or an order reminder is about one thing each and stays its own row.
    static let groupedTypes: Set<AppNotificationType> = [.plusReferral]

    /// `items` (newest first) as rows: each repeated notification joins the row of the newest
    /// one like it, which keeps its place in the list.
    static func groups(_ items: [AppNotification]) -> [NotificationGroup] {
        var order: [String] = []
        var members: [String: [AppNotification]] = [:]
        for item in items {
            let key = groupKey(item)
            if members[key] == nil { order.append(key) }
            members[key, default: []].append(item)
        }
        return order.compactMap { key in
            members[key].map { NotificationGroup(items: $0.sorted { $0.createdAt > $1.createdAt }) }
        }
    }

    /// "12 more like this", or `nil` for a single notification.
    static func moreText(_ count: Int) -> String? {
        switch count {
        case ..<1: nil
        case 1: String(localized: "1 more like this")
        default: String(localized: "\(count) more like this")
        }
    }

    private static func groupKey(_ item: AppNotification) -> String {
        guard groupedTypes.contains(item.type) else { return "id:\(item.id)" }
        return "type:\(item.type.rawValue)|\(item.title)"
    }
}
