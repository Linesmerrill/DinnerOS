import Foundation

/// The weekday a household means to place its grocery order. The API stores `mon`…`sun`,
/// or an empty string meaning "no reminder".
nonisolated enum OrderDay {
    /// The API's codes, Monday first, in the same order as `PlanDay`.
    static let codes = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]

    /// For example "Thursday". Falls back to the raw code for one this build doesn't know.
    static func name(_ code: String, locale: Locale = .autoupdatingCurrent) -> String {
        guard let index = codes.firstIndex(of: code), index < PlanDay.allCases.count else { return code }
        return PlanDay.allCases[index].name(locale: locale)
    }
}

/// A week's grocery-order state (`OrderReminder`).
///
/// Every field but `ordered` is derived by the API when it's read, from the household's
/// order day and today's date in its time zone. There is no scheduled reminder and nothing
/// stored to drift: the only thing recorded is that a member said they ordered.
nonisolated struct OrderReminder: Decodable, Hashable, Sendable {
    /// The ISO week this is about.
    let week: String
    /// `nil` until the household picks an order day, which is what turns reminders off.
    let orderDay: String?
    /// The order day's date in this week (`YYYY-MM-DD`).
    let dueOn: String?
    /// The order day has arrived and the week hasn't ended.
    let due: Bool
    /// Due and not yet ordered: the one state that shows a reminder. It goes quiet the
    /// moment the week is marked, and comes straight back if that's undone.
    let remind: Bool
    let ordered: Bool
    let orderedBy: String?
    let orderedAt: Date?

    /// For example "Thursday", or `nil` when no order day is set.
    var orderDayName: String? { orderDay.map { OrderDay.name($0) } }
}

/// The body of `PUT .../shopping/weeks/{week}/order`. `false` takes the mark back.
nonisolated struct SetWeekOrderedRequest: Encodable, Equatable, Sendable {
    var ordered: Bool
}
