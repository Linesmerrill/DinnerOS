import Foundation
import Security

/// What the home screen widgets show: this week's dinners by date, a look at next week, and
/// whether anyone is signed in. The app writes it whenever the plan changes; the widget
/// extension reads it. It lives in a keychain item both can open (a shared access group), so no
/// App Group is needed.
nonisolated struct DinnerWidgetSnapshot: Codable, Equatable, Sendable {
    struct Meal: Codable, Equatable, Sendable, Identifiable {
        let recipeID: String
        let name: String
        let imageURL: String?
        /// Cook time in minutes, when known.
        let minutes: Int?
        let isAddon: Bool
        var id: String { recipeID }
    }

    struct Day: Codable, Equatable, Sendable {
        /// `yyyy-MM-dd`, in the household's time zone.
        let date: String
        let meals: [Meal]
    }

    let updatedAt: Date
    /// This week's seven days in order, starting on the household's first day.
    let days: [Day]
    /// Meals planned this week without a day.
    let unscheduled: [Meal]
    /// Next week's meals, in day order.
    let nextWeek: [Meal]
    /// The household's time zone identifier, so "today" matches the app.
    let timeZone: String

    /// A snapshot for a signed-out app or no household: the widget asks to open the app.
    static func empty(timeZone: String = TimeZone.current.identifier) -> DinnerWidgetSnapshot {
        DinnerWidgetSnapshot(updatedAt: .now, days: [], unscheduled: [], nextWeek: [], timeZone: timeZone)
    }

    // MARK: - Reading the week

    var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: timeZone) ?? .current
        return calendar
    }

    func dateKey(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.string(from: date)
    }

    /// The main dinner on `date`, add-ons last.
    func meals(on date: Date) -> [Meal] {
        let key = dateKey(date)
        let meals = days.first { $0.date == key }?.meals ?? []
        return meals.filter { !$0.isAddon } + meals.filter(\.isAddon)
    }

    /// The days after `date` this week that have a meal.
    func upcoming(after date: Date) -> [(date: String, meal: Meal)] {
        let key = dateKey(date)
        return days.filter { $0.date > key }.compactMap { day in
            let main = day.meals.first { !$0.isAddon } ?? day.meals.first
            return main.map { (day.date, $0) }
        }
    }

    var hasWeek: Bool { days.contains { !$0.meals.isEmpty } || !unscheduled.isEmpty }
}

// MARK: - Storage

nonisolated enum DinnerWidgetStore {
    private static let service = "dinneros.widget"
    private static let account = "snapshot"

    /// The shared keychain group, from the `AppIdentifierPrefix` both Info.plists carry.
    private static var accessGroup: String? {
        guard let prefix = Bundle.main.object(forInfoDictionaryKey: "AppIdentifierPrefix") as? String,
            !prefix.isEmpty, !prefix.hasPrefix("$(")
        else { return nil }
        return prefix + "com.linesmerrill.dinneros.shared"
    }

    private static func query(group: String?) -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let group { query[kSecAttrAccessGroup as String] = group }
        return query
    }

    static func save(_ snapshot: DinnerWidgetSnapshot) {
        guard let data = try? JSONEncoder().encode(snapshot) else { return }
        for group in [accessGroup, nil] {
            let base = query(group: group)
            let update: [String: Any] = [kSecValueData as String: data]
            var status = SecItemUpdate(base as CFDictionary, update as CFDictionary)
            if status == errSecItemNotFound {
                var add = base
                add[kSecValueData as String] = data
                // Readable by the widget while the phone is locked, after the first unlock.
                add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
                status = SecItemAdd(add as CFDictionary, nil)
            }
            // Without the shared group (an unsigned simulator build) keep an app-only copy.
            if status == errSecSuccess { return }
        }
    }

    static func load() -> DinnerWidgetSnapshot? {
        for group in [accessGroup, nil] {
            var query = query(group: group)
            query[kSecReturnData as String] = true
            query[kSecMatchLimit as String] = kSecMatchLimitOne
            var result: AnyObject?
            if SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data,
                let snapshot = try? JSONDecoder().decode(DinnerWidgetSnapshot.self, from: data)
            {
                return snapshot
            }
        }
        return nil
    }

    static func clear() {
        for group in [accessGroup, nil] {
            SecItemDelete(query(group: group) as CFDictionary)
        }
    }
}

/// Links the widgets open: `dinneros://recipe?id=…&name=…`, `dinneros://autopilot`, `dinneros://shop`.
nonisolated enum DinnerWidgetLink: Equatable, Sendable {
    case recipe(id: String, name: String)
    case autopilot
    case shop
    case menu

    var url: URL {
        var components = URLComponents()
        components.scheme = "dinneros"
        switch self {
        case .recipe(let id, let name):
            components.host = "recipe"
            components.queryItems = [URLQueryItem(name: "id", value: id), URLQueryItem(name: "name", value: name)]
        case .autopilot: components.host = "autopilot"
        case .shop: components.host = "shop"
        case .menu: components.host = "menu"
        }
        return components.url ?? URL(fileURLWithPath: "/")
    }

    init?(url: URL) {
        guard url.scheme == "dinneros" else { return nil }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        switch url.host {
        case "recipe":
            guard let id = items.first(where: { $0.name == "id" })?.value, !id.isEmpty else { return nil }
            self = .recipe(id: id, name: items.first { $0.name == "name" }?.value ?? "")
        case "autopilot": self = .autopilot
        case "shop": self = .shop
        case "menu": self = .menu
        default: return nil
        }
    }
}
