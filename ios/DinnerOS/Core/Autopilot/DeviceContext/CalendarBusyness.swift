import Foundation

/// A span of time the member's calendar marks as taken, stripped of everything but its
/// times. Titles, attendees, locations, and notes are never read into it.
nonisolated struct CalendarBusyInterval: Hashable, Sendable {
    let start: Date
    let end: Date
    /// All-day events (birthdays, holidays, "Out of office") don't block an evening.
    var isAllDay = false
    /// The event's availability is free, or the member declined it.
    var isFree = false
}

/// One evening's busyness, before anything is sent.
nonisolated struct EveningBusyness: Hashable, Sendable {
    /// Minutes of the evening window not covered by any busy event (0 when fully booked).
    let freeMinutes: Int
    let band: AutopilotBusyness

    /// `eveningFreeMinutes` must be 1–1440, so a fully booked evening sends 1.
    var requestFreeMinutes: Int { min(max(freeMinutes, 1), 1440) }
}

/// Works out how busy each dinner evening is from calendar intervals, entirely on the device.
///
/// The evening is 4 pm to 8 pm in the household's time zone: the stretch that decides whether
/// there's time to cook. Overlapping events are merged so a double-booked hour counts once,
/// and each event is clipped to the window.
///
/// Bands, by minutes of the 240-minute window left free:
/// - **free**: at least 210 (at most 30 minutes taken)
/// - **busy**: under 90 (more than 2½ hours taken)
/// - **some**: anything between
nonisolated enum CalendarBusyness {
    static let eveningStartHour = 16
    static let eveningEndHour = 20
    static let freeThresholdMinutes = 210
    static let busyThresholdMinutes = 90

    static func band(freeMinutes: Int) -> AutopilotBusyness {
        if freeMinutes >= freeThresholdMinutes { return .free }
        if freeMinutes < busyThresholdMinutes { return .busy }
        return .some
    }

    /// The evening window of `day` in `week`, in `timeZone`. Handles daylight-saving days,
    /// since the window is built from wall-clock hours rather than a fixed offset.
    static func window(day: PlanDay, week: ISOWeek, timeZone: TimeZone) -> DateInterval? {
        guard let utcDate = day.date(in: week) else { return nil }
        var utc = Calendar(identifier: .gregorian)
        utc.timeZone = .gmt
        let parts = utc.dateComponents([.year, .month, .day], from: utcDate)
        var local = Calendar(identifier: .gregorian)
        local.timeZone = timeZone
        var start = parts
        start.hour = eveningStartHour
        var end = parts
        end.hour = eveningEndHour
        guard let startDate = local.date(from: start), let endDate = local.date(from: end), endDate > startDate
        else { return nil }
        return DateInterval(start: startDate, end: endDate)
    }

    /// The span that covers every evening of `week`, to fetch events once.
    static func weekSpan(week: ISOWeek, timeZone: TimeZone) -> DateInterval? {
        guard let first = window(day: .mon, week: week, timeZone: timeZone),
            let last = window(day: .sun, week: week, timeZone: timeZone)
        else { return nil }
        return DateInterval(start: first.start, end: last.end)
    }

    /// One evening's free minutes and band.
    static func evening(_ window: DateInterval, intervals: [CalendarBusyInterval]) -> EveningBusyness {
        let clipped =
            intervals
            .filter { !$0.isAllDay && !$0.isFree && $0.end > $0.start }
            .compactMap { interval -> (Date, Date)? in
                let start = max(interval.start, window.start)
                let end = min(interval.end, window.end)
                return end > start ? (start, end) : nil
            }
            .sorted { $0.0 < $1.0 }
        var taken: TimeInterval = 0
        var current: (Date, Date)?
        for span in clipped {
            if let open = current, span.0 <= open.1 {
                current = (open.0, max(open.1, span.1))
            } else {
                if let open = current { taken += open.1.timeIntervalSince(open.0) }
                current = span
            }
        }
        if let open = current { taken += open.1.timeIntervalSince(open.0) }
        let free = Int(((window.duration - taken) / 60).rounded(.down))
        let freeMinutes = max(free, 0)
        return EveningBusyness(freeMinutes: freeMinutes, band: band(freeMinutes: freeMinutes))
    }

    /// Every evening of `week` from `onOrAfter` on; earlier evenings are left out, since there's
    /// nothing left to plan for them.
    static func week(
        _ week: ISOWeek, timeZone: TimeZone, intervals: [CalendarBusyInterval], onOrAfter now: Date? = nil
    ) -> [PlanDay: EveningBusyness] {
        var result: [PlanDay: EveningBusyness] = [:]
        for day in PlanDay.allCases {
            guard let window = window(day: day, week: week, timeZone: timeZone) else { continue }
            if let now, window.end <= now { continue }
            result[day] = evening(window, intervals: intervals)
        }
        return result
    }
}
