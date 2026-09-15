import Foundation
import Testing

@testable import DinnerOS

struct PlanWeekTests {
    private let locale = Locale(identifier: "en_US")

    private func date(_ string: String) throws -> Date {
        try #require(JSONCoding.parseDate(string))
    }

    private func week(_ string: String) throws -> ISOWeek {
        try #require(ISOWeek(string))
    }

    /// ICU separates interval parts with thin spaces; compare with plain spaces.
    private func plain(_ string: String) -> String {
        string.replacing("\u{2009}", with: " ").replacing("\u{202F}", with: " ")
    }

    @Test(
        arguments: [
            ("2026-09-16T12:00:00Z", "2026-W38"),
            // Friday, January 1, 2027 belongs to the 53rd week of 2026.
            ("2027-01-01T12:00:00Z", "2026-W53"),
            ("2027-01-04T00:00:00Z", "2027-W01"),
            // Monday, December 29, 2025 starts 2026's first week.
            ("2025-12-29T00:00:00Z", "2026-W01"),
            ("2025-12-28T23:59:59Z", "2025-W52"),
            ("2024-12-30T08:00:00Z", "2025-W01"),
        ])
    func weekContainingADate(instant: String, expected: String) throws {
        #expect(ISOWeek.containing(try date(instant), in: .gmt).description == expected)
    }

    @Test func currentWeekFollowsTheHouseholdTimeZone() throws {
        // Sunday 9 pm in Denver is already Monday in UTC.
        let sundayNight = try date("2026-09-21T03:00:00Z")
        let denver = try #require(TimeZone(identifier: "America/Denver"))
        #expect(ISOWeek.current(in: denver, now: sundayNight).description == "2026-W38")
        #expect(ISOWeek.current(in: .gmt, now: sundayNight).description == "2026-W39")
        #expect(PlanDay.containing(sundayNight, in: denver) == .sun)
        #expect(PlanDay.containing(sundayNight, in: .gmt) == .mon)

        // Monday 1 am in Auckland is still Sunday in UTC.
        let auckland = try #require(TimeZone(identifier: "Pacific/Auckland"))
        let mondayMorning = try date("2026-09-20T13:00:00Z")
        #expect(ISOWeek.current(in: auckland, now: mondayMorning).description == "2026-W39")
        #expect(PlanDay.containing(mondayMorning, in: auckland) == .mon)
    }

    @Test func nextAndPreviousCrossYearBoundaries() throws {
        #expect(try week("2026-W38").next.description == "2026-W39")
        #expect(try week("2026-W52").next.description == "2026-W53")
        #expect(try week("2026-W53").next.description == "2027-W01")
        #expect(try week("2027-W01").previous.description == "2026-W53")
        #expect(try week("2025-W52").next.description == "2026-W01")
        #expect(try week("2026-W01").previous.description == "2025-W52")
        #expect(try week("2026-W01").adding(weeks: 52).description == "2026-W53")
        // Back through 2026's 53 weeks and 2025's 52.
        #expect(try week("2027-W01").adding(weeks: -105).description == "2025-W01")
        #expect(try week("2026-W38").adding(weeks: 0).description == "2026-W38")
    }

    @Test func weekStartsMondayAndEndsSunday() throws {
        let plan = try week("2026-W38")
        #expect(PlanFixtures.dates(for: "2026-W38") == ("2026-09-14", "2026-09-20"))
        #expect(PlanDay.sun.date(in: plan) == plan.endDate)
        #expect(PlanDay.mon.date(in: plan) == plan.startDate)
        #expect(PlanDay.allCases.map(\.offset) == [0, 1, 2, 3, 4, 5, 6])
    }

    @Test(
        arguments: [
            ("2026-W38", "Sep 14 – 20"),
            ("2026-W40", "Sep 28 – Oct 4"),
            ("2026-W53", "Dec 28, 2026 – Jan 3, 2027"),
        ])
    func rangeLabels(weekString: String, expected: String) throws {
        #expect(plain(try week(weekString).rangeLabel(locale: locale)) == expected)
    }

    @Test func dayTitles() throws {
        #expect(PlanDay.tue.title(in: try week("2026-W38"), locale: locale) == "Tuesday, Sep 15")
        #expect(PlanDay.fri.title(in: try week("2026-W53"), locale: locale) == "Friday, Jan 1")
        #expect(PlanDay.allCases.map { $0.name(locale: locale) }.first == "Monday")
        #expect(PlanDay.allCases.map { $0.name(locale: locale) }.last == "Sunday")
    }

    @Test func unknownHouseholdTimeZoneFallsBackToTheDevice() {
        let household = Household(
            id: "h", name: "Test", defaultServings: 2, timeZone: "Not/AZone", createdBy: "u", createdAt: .now,
            updatedAt: .now)
        #expect(household.planningTimeZone == .autoupdatingCurrent)
    }
}
