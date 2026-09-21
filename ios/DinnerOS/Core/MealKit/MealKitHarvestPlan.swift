import Foundation

/// One stretch of a household's order history to walk, oldest-ward
/// (`docs/meal-kit-import.md`).
///
/// The walk always moves backwards a page at a time, and a page covers roughly four to five
/// delivered weeks — measured, not assumed: a real account returned 160 weeks in 40 pages. So
/// one sitting cannot read four years of history, and a resumed harvest is expressed as
/// segments rather than "start again from today".
nonisolated struct MealKitHarvestSegment: Equatable, Sendable {
    /// The ISO week to start walking back from. Empty means this week.
    let from: String
    /// Stop as soon as a page reaches this week or earlier — it and everything below it have
    /// already been read. Empty means walk until the history runs out.
    let floor: String

    init(from: String = "", floor: String = "") {
        self.from = from
        self.floor = floor
    }

    /// What the harvest script reads.
    var arguments: [String: String] { ["from": from, "floor": floor] }
}

/// Where the next harvest should walk, given what the server remembers.
///
/// Pure, so the resume behaviour is tested here rather than guessed at from a simulator run.
nonisolated enum MealKitHarvestPlan {
    /// The segments to walk for a household whose history the server has read this far.
    ///
    /// - A household that has never imported walks back from today until the history runs out.
    /// - A household part-way through gets two segments: a short catch-up from today down to the
    ///   newest week already seen, which is how a *new* delivery is picked up, and then the
    ///   resume proper, starting the week before the oldest week already seen.
    /// - A household whose history is finished gets the catch-up alone. There is nothing older
    ///   left to read, and re-reading four years to find one new week would be rude to the
    ///   service and slow for the member.
    static func segments(for history: MealKitImportHistory?) -> [MealKitHarvestSegment] {
        guard let history else { return [MealKitHarvestSegment()] }
        var plan: [MealKitHarvestSegment] = []
        if !history.latestWeek.isEmpty {
            plan.append(MealKitHarvestSegment(floor: history.latestWeek))
        }
        if let resume = history.resumeWeek, let start = weekBefore(resume) {
            plan.append(MealKitHarvestSegment(from: start))
        }
        return plan.isEmpty ? [MealKitHarvestSegment()] : plan
    }

    /// Whether a harvest for this history would only be looking for new deliveries.
    static func isCatchUpOnly(_ history: MealKitImportHistory?) -> Bool {
        guard let history else { return false }
        return segments(for: history).allSatisfy { !$0.floor.isEmpty }
    }

    /// The ISO week before `week`, or `nil` when `week` is not one.
    ///
    /// Weeks are compared as strings everywhere else (they are zero-padded, so that works), but
    /// stepping back over a year boundary is real calendar arithmetic: the week before
    /// `2024-W01` is `2023-W52`, and some years have 53.
    static func weekBefore(_ week: String) -> String? {
        guard let monday = mondayOf(week) else { return nil }
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
        guard let earlier = calendar.date(byAdding: .weekOfYear, value: -1, to: monday) else { return nil }
        return isoWeek(of: earlier, calendar: calendar)
    }

    /// The Monday of an ISO week such as `2026-W38`, or `nil` when it is not one.
    static func mondayOf(_ week: String) -> Date? {
        let parts = week.split(separator: "W", omittingEmptySubsequences: false)
        guard parts.count == 2, parts[0].count == 5, parts[0].hasSuffix("-"),
            let year = Int(parts[0].dropLast()), let number = Int(parts[1]),
            parts[1].count == 2, (1...53).contains(number)
        else { return nil }
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
        var components = DateComponents()
        components.yearForWeekOfYear = year
        components.weekOfYear = number
        components.weekday = calendar.firstWeekday
        return calendar.date(from: components)
    }

    private static func isoWeek(of date: Date, calendar: Calendar) -> String? {
        let parts = calendar.dateComponents([.yearForWeekOfYear, .weekOfYear], from: date)
        guard let year = parts.yearForWeekOfYear, let week = parts.weekOfYear else { return nil }
        return String(format: "%04d-W%02d", year, week)
    }
}
