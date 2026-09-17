import Foundation

/// Week arithmetic and labels for the planner.
///
/// `ISOWeek` dates are midnight UTC, so labels format in UTC. Which week is "now"
/// depends on the household's time zone: late Sunday in Denver is already Monday in UTC.
///
/// A week key (`YYYY-Www`) is always the ISO week, but the seven days it covers follow the
/// household's `weekStartsOn`: key K starts on K's ISO Monday moved by
/// `PlanDay.weekStartShift`. Anything that turns a day into a date, or lists a week's days in
/// order, takes the start day explicitly.
nonisolated extension ISOWeek {
    /// A week from components that are already known to be valid.
    private init(validYear year: Int, week: Int) {
        self.year = year
        self.week = week
    }

    /// The week containing `date` as seen in `timeZone`, for a household whose weeks start on
    /// `weekStartsOn`: the ISO week of the local calendar date moved back by the start day's shift.
    /// With Sunday weeks, Sunday, September 20, 2026 is already in 2026-W39.
    static func containing(_ date: Date, in timeZone: TimeZone, weekStartsOn: PlanDay) -> ISOWeek {
        var local = Calendar(identifier: .gregorian)
        local.timeZone = timeZone
        let parts = local.dateComponents([.year, .month, .day], from: date)
        var utc = Calendar(identifier: .gregorian)
        utc.timeZone = .gmt
        guard let midnight = utc.date(from: parts) else { return isoContaining(date) }
        return isoContaining(midnight.addingTimeInterval(TimeInterval(-weekStartsOn.weekStartShift) * 86_400))
    }

    /// This week in `timeZone`, for a household whose weeks start on `weekStartsOn`.
    static func current(in timeZone: TimeZone, weekStartsOn: PlanDay, now: Date = .now) -> ISOWeek {
        containing(now, in: timeZone, weekStartsOn: weekStartsOn)
    }

    /// The plain ISO week of a UTC instant.
    private static func isoContaining(_ date: Date) -> ISOWeek {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = .gmt
        let components = calendar.dateComponents([.yearForWeekOfYear, .weekOfYear], from: date)
        return ISOWeek(validYear: components.yearForWeekOfYear ?? 1970, week: components.weekOfYear ?? 1)
    }

    /// The week `count` weeks later (earlier when negative). Handles 52- and 53-week years.
    func adding(weeks count: Int) -> ISOWeek {
        guard let start = startDate else { return self }
        return Self.isoContaining(start.addingTimeInterval(TimeInterval(count * 7) * 86_400))
    }

    var next: ISOWeek { adding(weeks: 1) }
    var previous: ISOWeek { adding(weeks: -1) }

    /// Midnight UTC on the first day of the week as a household starting on `weekStartsOn` lays
    /// it out: the ISO Monday moved by `PlanDay.weekStartShift`. With Sunday weeks, 2026-W38
    /// starts on Sunday, September 13.
    func startDate(weekStartsOn: PlanDay) -> Date? {
        startDate.map { $0.addingTimeInterval(TimeInterval(weekStartsOn.weekStartShift) * 86_400) }
    }

    /// Midnight UTC on the week's last day, six days after `startDate(weekStartsOn:)`.
    func endDate(weekStartsOn: PlanDay) -> Date? {
        startDate(weekStartsOn: weekStartsOn).map { $0.addingTimeInterval(6 * 86_400) }
    }

    /// First day to last, for example "Sep 13 – 19", "Sep 27 – Oct 3", or
    /// "Dec 27, 2026 – Jan 2, 2027" when the week spans two years.
    func rangeLabel(weekStartsOn: PlanDay, locale: Locale = .autoupdatingCurrent) -> String {
        guard let start = startDate(weekStartsOn: weekStartsOn), let end = endDate(weekStartsOn: weekStartsOn) else {
            return description
        }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .gmt
        var style = Date.IntervalFormatStyle(locale: locale, calendar: calendar, timeZone: .gmt)
            .month(.abbreviated).day()
        if calendar.component(.year, from: start) != calendar.component(.year, from: end) {
            style = style.year()
        }
        return (start..<end).formatted(style)
    }

    /// For example "Week of Sep 13, 2026", from the week's first day.
    func weekOf(weekStartsOn: PlanDay, locale: Locale = .autoupdatingCurrent) -> String {
        guard let start = startDate(weekStartsOn: weekStartsOn) else { return description }
        let date = start.formatted(
            Date.FormatStyle(date: .omitted, time: .omitted, locale: locale, timeZone: .gmt)
                .month(.abbreviated).day().year())
        return String(localized: "Week of \(date)")
    }
}

nonisolated extension PlanDay {
    /// The start day to use before a household has loaded. New households start on Sunday.
    static let defaultWeekStart: PlanDay = .sun

    /// The start day of a household that never chose one, or from a server that doesn't send
    /// `weekStartsOn`: ISO weeks, Monday to Sunday, which is how their meals are stored.
    static let isoWeekStart: PlanDay = .mon

    /// Days from a week key's ISO Monday to the first day of a week that starts on this day:
    /// Monday 0, Tuesday +1, Wednesday +2, Thursday +3, Friday −3, Saturday −2, Sunday −1. A week
    /// always keeps its ISO Thursday, so a key's year and month read the same for every start day.
    var weekStartShift: Int {
        offset <= 3 ? offset : offset - 7
    }

    /// This day's place, 0 to 6, in a week that starts on `start`.
    func position(inWeekStartingOn start: PlanDay) -> Int {
        (offset - start.offset + 7) % 7
    }

    /// The seven days in the order a week that starts on `start` lists them.
    static func week(startingOn start: PlanDay) -> [PlanDay] {
        allCases.sorted { $0.position(inWeekStartingOn: start) < $1.position(inWeekStartingOn: start) }
    }

    /// The day's calendar date in `week` laid out from `weekStartsOn`, as midnight UTC.
    func date(in week: ISOWeek, weekStartsOn: PlanDay) -> Date? {
        week.startDate(weekStartsOn: weekStartsOn).map {
            $0.addingTimeInterval(TimeInterval(position(inWeekStartingOn: weekStartsOn)) * 86_400)
        }
    }

    /// The day `date` falls on in `timeZone`.
    static func containing(_ date: Date, in timeZone: TimeZone) -> PlanDay {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = timeZone
        // `weekday` is 1 for Sunday through 7 for Saturday.
        let weekday = calendar.component(.weekday, from: date)
        return PlanDay.allCases[(weekday + 5) % 7]
    }

    /// For example "Monday".
    func name(locale: Locale = .autoupdatingCurrent) -> String {
        let monday = ISOWeek("2026-W01")?.startDate ?? .distantPast
        return monday.addingTimeInterval(TimeInterval(offset) * 86_400)
            .formatted(Date.FormatStyle(locale: locale, timeZone: .gmt).weekday(.wide))
    }

    /// For example "Monday, Sep 14".
    func title(in week: ISOWeek, weekStartsOn: PlanDay, locale: Locale = .autoupdatingCurrent) -> String {
        guard let date = date(in: week, weekStartsOn: weekStartsOn) else { return name(locale: locale) }
        return date.formatted(Date.FormatStyle(locale: locale, timeZone: .gmt).weekday(.wide).month(.abbreviated).day())
    }
}

nonisolated extension Plan {
    /// Entries by day in the order a week starting on `weekStartsOn` lists them, then the
    /// unscheduled ones; entries on the same day keep the order they were added.
    func entriesInDayOrder(weekStartsOn: PlanDay) -> [PlanEntry] {
        entries.enumerated()
            .sorted { lhs, rhs in
                let left = lhs.element.day?.position(inWeekStartingOn: weekStartsOn) ?? 7
                let right = rhs.element.day?.position(inWeekStartingOn: weekStartsOn) ?? 7
                return left == right ? lhs.offset < rhs.offset : left < right
            }
            .map(\.element)
    }
}

extension Household {
    /// The household's time zone, or the device's when the stored name is unknown.
    nonisolated var planningTimeZone: TimeZone {
        TimeZone(identifier: timeZone) ?? .autoupdatingCurrent
    }

    /// What the week-based stores are activated with, for a `.task(id:)` that re-activates them
    /// when the household or its week start day changes.
    nonisolated var weekScope: HouseholdWeekScope {
        HouseholdWeekScope(householdID: id, weekStartsOn: weekStartsOn)
    }
}

/// The days a household can start its weeks on, in the order Household Settings offers them:
/// Sunday, the default, then Monday, then the rest of the week.
nonisolated enum WeekStartChoices {
    static let days: [PlanDay] = [.sun, .mon, .tue, .wed, .thu, .fri, .sat]
}

/// A household and the day its weeks start on. Changing either re-activates the week-based
/// stores, which reload what the change moved.
nonisolated struct HouseholdWeekScope: Hashable, Sendable {
    let householdID: String
    let weekStartsOn: PlanDay
}
