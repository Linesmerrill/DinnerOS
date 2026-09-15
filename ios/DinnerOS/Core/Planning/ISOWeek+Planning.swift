import Foundation

/// Week arithmetic and labels for the planner.
///
/// `ISOWeek` dates are midnight UTC, so labels format in UTC. Which week is "now"
/// depends on the household's time zone: late Sunday in Denver is already Monday in UTC.
nonisolated extension ISOWeek {
    /// A week from components that are already known to be valid.
    private init(validYear year: Int, week: Int) {
        self.year = year
        self.week = week
    }

    /// The ISO week containing `date` as seen in `timeZone`.
    static func containing(_ date: Date, in timeZone: TimeZone) -> ISOWeek {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = timeZone
        let components = calendar.dateComponents([.yearForWeekOfYear, .weekOfYear], from: date)
        return ISOWeek(validYear: components.yearForWeekOfYear ?? 1970, week: components.weekOfYear ?? 1)
    }

    /// This week in `timeZone`.
    static func current(in timeZone: TimeZone, now: Date = .now) -> ISOWeek {
        containing(now, in: timeZone)
    }

    /// The week `count` weeks later (earlier when negative). Handles 52- and 53-week years.
    func adding(weeks count: Int) -> ISOWeek {
        guard let start = startDate else { return self }
        return .containing(start.addingTimeInterval(TimeInterval(count * 7) * 86_400), in: .gmt)
    }

    var next: ISOWeek { adding(weeks: 1) }
    var previous: ISOWeek { adding(weeks: -1) }

    /// Midnight UTC on the week's Sunday.
    var endDate: Date? {
        startDate.map { $0.addingTimeInterval(6 * 86_400) }
    }

    /// Monday to Sunday, for example "Sep 14 – 20", "Sep 28 – Oct 4", or
    /// "Dec 28, 2026 – Jan 3, 2027" when the week spans two years.
    func rangeLabel(locale: Locale = .autoupdatingCurrent) -> String {
        guard let start = startDate, let end = endDate else { return description }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .gmt
        var style = Date.IntervalFormatStyle(locale: locale, calendar: calendar, timeZone: .gmt)
            .month(.abbreviated).day()
        if calendar.component(.year, from: start) != calendar.component(.year, from: end) {
            style = style.year()
        }
        return (start..<end).formatted(style)
    }
}

nonisolated extension PlanDay {
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
    func title(in week: ISOWeek, locale: Locale = .autoupdatingCurrent) -> String {
        guard let date = date(in: week) else { return name(locale: locale) }
        return date.formatted(Date.FormatStyle(locale: locale, timeZone: .gmt).weekday(.wide).month(.abbreviated).day())
    }
}

extension Household {
    /// The household's time zone, or the device's when the stored name is unknown.
    nonisolated var planningTimeZone: TimeZone {
        TimeZone(identifier: timeZone) ?? .autoupdatingCurrent
    }
}
