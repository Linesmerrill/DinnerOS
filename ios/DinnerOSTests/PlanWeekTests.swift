import Foundation
import Testing

@testable import DinnerOS

struct PlanWeekTests {
    private let locale = Locale(identifier: "en_US")
    private let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    private func date(_ string: String) throws -> Date {
        try #require(JSONCoding.parseDate(string))
    }

    private func week(_ string: String) throws -> ISOWeek {
        try #require(ISOWeek(string))
    }

    /// `YYYY-MM-DD` of a UTC-midnight date.
    private func day(_ date: Date?) -> String? {
        date?.formatted(Date.ISO8601FormatStyle(timeZone: .gmt).year().month().day())
    }

    /// ICU separates interval parts with thin spaces; compare with plain spaces.
    private func plain(_ string: String) -> String {
        string.replacing("\u{2009}", with: " ").replacing("\u{202F}", with: " ")
    }

    // MARK: - Monday weeks (ISO)

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
        #expect(ISOWeek.containing(try date(instant), in: .gmt, weekStartsOn: .mon).description == expected)
    }

    @Test func currentWeekFollowsTheHouseholdTimeZone() throws {
        // Sunday 9 pm in Denver is already Monday in UTC.
        let sundayNight = try date("2026-09-21T03:00:00Z")
        #expect(ISOWeek.current(in: denver, weekStartsOn: .mon, now: sundayNight).description == "2026-W38")
        #expect(ISOWeek.current(in: .gmt, weekStartsOn: .mon, now: sundayNight).description == "2026-W39")
        #expect(PlanDay.containing(sundayNight, in: denver) == .sun)
        #expect(PlanDay.containing(sundayNight, in: .gmt) == .mon)

        // Monday 1 am in Auckland is still Sunday in UTC.
        let auckland = try #require(TimeZone(identifier: "Pacific/Auckland"))
        let mondayMorning = try date("2026-09-20T13:00:00Z")
        #expect(ISOWeek.current(in: auckland, weekStartsOn: .mon, now: mondayMorning).description == "2026-W39")
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
        #expect(PlanDay.sun.date(in: plan, weekStartsOn: .mon) == plan.endDate(weekStartsOn: .mon))
        #expect(PlanDay.mon.date(in: plan, weekStartsOn: .mon) == plan.startDate(weekStartsOn: .mon))
        #expect(plan.startDate(weekStartsOn: .mon) == plan.startDate)
        #expect(PlanDay.allCases.map(\.offset) == [0, 1, 2, 3, 4, 5, 6])
    }

    @Test(
        arguments: [
            ("2026-W38", "Sep 14 – 20"),
            ("2026-W40", "Sep 28 – Oct 4"),
            ("2026-W53", "Dec 28, 2026 – Jan 3, 2027"),
        ])
    func rangeLabels(weekString: String, expected: String) throws {
        #expect(plain(try week(weekString).rangeLabel(weekStartsOn: .mon, locale: locale)) == expected)
    }

    @Test func dayTitles() throws {
        #expect(PlanDay.tue.title(in: try week("2026-W38"), weekStartsOn: .mon, locale: locale) == "Tuesday, Sep 15")
        #expect(PlanDay.fri.title(in: try week("2026-W53"), weekStartsOn: .mon, locale: locale) == "Friday, Jan 1")
        #expect(PlanDay.allCases.map { $0.name(locale: locale) }.first == "Monday")
        #expect(PlanDay.allCases.map { $0.name(locale: locale) }.last == "Sunday")
    }

    // MARK: - Start days

    @Test func shiftsFromTheISOMonday() {
        #expect(PlanDay.allCases.map(\.weekStartShift) == [0, 1, 2, 3, -3, -2, -1])
    }

    @Test func daysInWeekOrder() {
        #expect(PlanDay.week(startingOn: .sun) == [.sun, .mon, .tue, .wed, .thu, .fri, .sat])
        #expect(PlanDay.week(startingOn: .mon) == [.mon, .tue, .wed, .thu, .fri, .sat, .sun])
        #expect(PlanDay.week(startingOn: .sat) == [.sat, .sun, .mon, .tue, .wed, .thu, .fri])
        #expect(PlanDay.sun.position(inWeekStartingOn: .sun) == 0)
        #expect(PlanDay.sat.position(inWeekStartingOn: .sun) == 6)
        #expect(PlanDay.fri.position(inWeekStartingOn: .sat) == 6)
    }

    /// With Sunday weeks, 2026-W38 is Sunday, September 13 to Saturday, September 19.
    @Test func sundayWeeks() throws {
        let w38 = try week("2026-W38")
        #expect(day(w38.startDate(weekStartsOn: .sun)) == "2026-09-13")
        #expect(day(w38.endDate(weekStartsOn: .sun)) == "2026-09-19")
        #expect(
            PlanDay.week(startingOn: .sun).map { day($0.date(in: w38, weekStartsOn: .sun)) } == [
                "2026-09-13", "2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17", "2026-09-18", "2026-09-19",
            ])
        #expect(plain(w38.rangeLabel(weekStartsOn: .sun, locale: locale)) == "Sep 13 – 19")
        #expect(PlanDay.sun.title(in: w38, weekStartsOn: .sun, locale: locale) == "Sunday, Sep 13")
        #expect(PlanFixtures.dates(for: "2026-W38", weekStartsOn: .sun) == ("2026-09-13", "2026-09-19"))
    }

    /// With Saturday weeks, 2026-W38 is Saturday, September 12 to Friday, September 18.
    @Test func saturdayWeeks() throws {
        let w38 = try week("2026-W38")
        #expect(day(w38.startDate(weekStartsOn: .sat)) == "2026-09-12")
        #expect(day(w38.endDate(weekStartsOn: .sat)) == "2026-09-18")
        #expect(day(PlanDay.sat.date(in: w38, weekStartsOn: .sat)) == "2026-09-12")
        #expect(day(PlanDay.sun.date(in: w38, weekStartsOn: .sat)) == "2026-09-13")
        #expect(day(PlanDay.fri.date(in: w38, weekStartsOn: .sat)) == "2026-09-18")
        #expect(plain(w38.rangeLabel(weekStartsOn: .sat, locale: locale)) == "Sep 12 – 18")
    }

    @Test(
        arguments: [
            // Sunday weeks: Thursday and Saturday are in W38; Sunday the 20th starts W39.
            ("2026-09-13T12:00:00Z", PlanDay.sun, "2026-W38"),
            ("2026-09-17T12:00:00Z", PlanDay.sun, "2026-W38"),
            ("2026-09-19T12:00:00Z", PlanDay.sun, "2026-W38"),
            ("2026-09-20T12:00:00Z", PlanDay.sun, "2026-W39"),
            ("2026-09-12T12:00:00Z", PlanDay.sun, "2026-W37"),
            // Saturday weeks: Saturday the 12th starts W38, Saturday the 19th starts W39.
            ("2026-09-11T12:00:00Z", PlanDay.sat, "2026-W37"),
            ("2026-09-12T12:00:00Z", PlanDay.sat, "2026-W38"),
            ("2026-09-18T12:00:00Z", PlanDay.sat, "2026-W38"),
            ("2026-09-19T12:00:00Z", PlanDay.sat, "2026-W39"),
            // Thursday weeks: shift +3.
            ("2026-09-16T12:00:00Z", PlanDay.thu, "2026-W37"),
            ("2026-09-17T12:00:00Z", PlanDay.thu, "2026-W38"),
            // Year boundaries. 2026-W53 is Mon Dec 28, 2026 – Sun Jan 3, 2027; with Sunday weeks,
            // Sun Dec 27 – Sat Jan 2, so Sunday, January 3, 2027 starts 2027-W01.
            ("2026-12-27T12:00:00Z", PlanDay.sun, "2026-W53"),
            ("2027-01-02T12:00:00Z", PlanDay.sun, "2026-W53"),
            ("2027-01-03T12:00:00Z", PlanDay.sun, "2027-W01"),
            ("2026-12-26T12:00:00Z", PlanDay.sun, "2026-W52"),
            ("2027-01-03T12:00:00Z", PlanDay.mon, "2026-W53"),
            ("2027-01-02T12:00:00Z", PlanDay.sat, "2027-W01"),
            // 2021-W01 is Mon Jan 4 – Sun Jan 10, 2021; with Sunday weeks, Sun Jan 3 – Sat Jan 9.
            ("2021-01-03T12:00:00Z", PlanDay.sun, "2021-W01"),
            ("2021-01-02T12:00:00Z", PlanDay.sun, "2020-W53"),
            ("2021-01-09T12:00:00Z", PlanDay.sun, "2021-W01"),
            ("2021-01-10T12:00:00Z", PlanDay.sun, "2021-W02"),
            ("2021-01-03T12:00:00Z", PlanDay.mon, "2020-W53"),
        ])
    func weekContainingADateForAStartDay(instant: String, start: PlanDay, expected: String) throws {
        #expect(ISOWeek.containing(try date(instant), in: .gmt, weekStartsOn: start).description == expected)
    }

    @Test func yearBoundaryRangesForSundayWeeks() throws {
        #expect(
            plain(try week("2026-W53").rangeLabel(weekStartsOn: .sun, locale: locale)) == "Dec 27, 2026 – Jan 2, 2027")
        #expect(plain(try week("2027-W01").rangeLabel(weekStartsOn: .sun, locale: locale)) == "Jan 3 – 9")
        #expect(plain(try week("2021-W01").rangeLabel(weekStartsOn: .sun, locale: locale)) == "Jan 3 – 9")
        #expect(plain(try week("2021-W01").rangeLabel(weekStartsOn: .sat, locale: locale)) == "Jan 2 – 8")
        #expect(
            plain(try week("2020-W53").rangeLabel(weekStartsOn: .sun, locale: locale)) == "Dec 27, 2020 – Jan 2, 2021")
        #expect(PlanDay.sat.title(in: try week("2026-W53"), weekStartsOn: .sun, locale: locale) == "Saturday, Jan 2")
    }

    /// Thursday, September 17, 2026 in Denver: the week chip reads "Sep 13 – 19" for a Sunday
    /// household and "Sep 14 – 20" for a Monday one.
    @Test func currentWeekForTheAcceptanceExample() throws {
        let thursday = try date("2026-09-17T18:00:00Z")
        let sunday = ISOWeek.current(in: denver, weekStartsOn: .sun, now: thursday)
        #expect(sunday.description == "2026-W38")
        #expect(plain(sunday.rangeLabel(weekStartsOn: .sun, locale: locale)) == "Sep 13 – 19")
        let monday = ISOWeek.current(in: denver, weekStartsOn: .mon, now: thursday)
        #expect(plain(monday.rangeLabel(weekStartsOn: .mon, locale: locale)) == "Sep 14 – 20")

        // Saturday 9 pm in Denver is Sunday in UTC, but still this week for a Sunday household.
        let saturdayNight = try date("2026-09-20T03:00:00Z")
        #expect(ISOWeek.current(in: denver, weekStartsOn: .sun, now: saturdayNight).description == "2026-W38")
        #expect(ISOWeek.current(in: .gmt, weekStartsOn: .sun, now: saturdayNight).description == "2026-W39")
    }

    /// Every date a week lays out is in that week again, for every start day.
    @Test(arguments: PlanDay.allCases)
    func everyDayOfAWeekIsInThatWeek(start: PlanDay) throws {
        for key in ["2020-W53", "2021-W01", "2026-W01", "2026-W38", "2026-W53", "2027-W01"] {
            let parsed = try week(key)
            for planDay in PlanDay.allCases {
                let date = try #require(planDay.date(in: parsed, weekStartsOn: start))
                #expect(ISOWeek.containing(date, in: .gmt, weekStartsOn: start) == parsed)
                #expect(PlanDay.containing(date, in: .gmt) == planDay)
            }
        }
    }

    @Test func entriesInDayOrderFollowTheStartDay() {
        func entry(_ id: String, _ day: PlanDay?) -> PlanEntry {
            PlanEntry(
                id: id, recipe: PlanEntryRecipe(id: "r-\(id)", name: id, imageURLString: nil), day: day, date: nil,
                servings: 2, note: "", addedBy: "u", addedAt: .distantPast)
        }
        let plan = Plan(
            householdID: "h", week: "2026-W38", startDate: "2026-09-13", endDate: "2026-09-19", status: .draft,
            entries: [entry("a", .mon), entry("b", nil), entry("c", .sat), entry("d", .sun), entry("e", .mon)],
            createdAt: nil, updatedAt: nil)
        #expect(plan.entriesInDayOrder(weekStartsOn: .sun).map(\.id) == ["d", "a", "e", "c", "b"])
        #expect(plan.entriesInDayOrder(weekStartsOn: .mon).map(\.id) == ["a", "e", "c", "d", "b"])
        #expect(plan.entriesInDayOrder(weekStartsOn: .sat).map(\.id) == ["c", "d", "a", "e", "b"])
    }

    @Test func settingsOfferSundayFirstThenMonday() {
        #expect(WeekStartChoices.days == [.sun, .mon, .tue, .wed, .thu, .fri, .sat])
        #expect(PlanDay.defaultWeekStart == .sun)
        #expect(PlanDay.isoWeekStart == .mon)
    }

    @Test func unknownHouseholdTimeZoneFallsBackToTheDevice() {
        let household = Household(
            id: "h", name: "Test", defaultServings: 2, timeZone: "Not/AZone", orderDay: nil, createdBy: "u",
            createdAt: .now, updatedAt: .now)
        #expect(household.planningTimeZone == .autoupdatingCurrent)
        #expect(household.weekStartsOn == .mon)
        #expect(household.weekScope == HouseholdWeekScope(householdID: "h", weekStartsOn: .mon))
    }
}
