import Foundation
import Testing

@testable import DinnerOS

struct PantryFormattingTests {
    private let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    private func date(_ year: Int, _ month: Int, _ day: Int, hour: Int = 12) throws -> Date {
        try #require(
            PantryDate.calendar(timeZone: denver).date(
                from: DateComponents(year: year, month: month, day: day, hour: hour)))
    }

    // MARK: - Grouping and ordering

    @Test func sectionsFollowAisleOrderThenName() {
        let items = [
            PantryFixtures.item(id: "1", name: "sugar", category: "pantry"),
            PantryFixtures.item(id: "2", name: "Mystery", category: "future-aisle"),
            PantryFixtures.item(id: "3", name: "Basil", category: "produce"),
            PantryFixtures.item(id: "4", name: "Almond Flour", category: "pantry"),
            PantryFixtures.item(id: "5", name: "Apples", category: "produce"),
            PantryFixtures.item(id: "6", name: "Ice", category: "other"),
            PantryFixtures.item(id: "7", name: "Butter", category: "dairy-eggs"),
        ]

        let sections = PantryList.sections(items)

        #expect(sections.map(\.category) == ["produce", "dairy-eggs", "pantry", "other", "future-aisle"])
        #expect(sections.map(\.title) == ["Produce", "Dairy & Eggs", "Pantry", "Other", "Future-Aisle"])
        #expect(sections[0].items.map(\.displayName) == ["Apples", "Basil"])
        #expect(sections[2].items.map(\.displayName) == ["Almond Flour", "sugar"])
    }

    @Test func sameNamesSortByID() {
        let items = [
            PantryFixtures.item(id: "b", name: "Salt", category: "spices"),
            PantryFixtures.item(id: "a", name: "salt", category: "spices"),
        ]
        #expect(PantryList.sorted(items).map(\.id) == ["a", "b"])
    }

    @Test func statusFilterAndSearchNarrowTheSections() {
        let items = [
            PantryFixtures.item(id: "1", name: "Jalapeño", category: "produce", status: .low),
            PantryFixtures.item(id: "2", name: "Carrots", category: "produce", status: .out),
            PantryFixtures.item(id: "3", name: "Salt", category: "spices", status: .low),
        ]

        #expect(PantryList.sections(items, status: .low).flatMap { $0.items.map(\.id) } == ["1", "3"])
        #expect(PantryList.sections(items, status: .out).map(\.category) == ["produce"])
        #expect(PantryList.sections(items, search: " jalapeno ").flatMap { $0.items.map(\.id) } == ["1"])
        #expect(PantryList.sections(items, search: "SAL", status: .low).flatMap { $0.items.map(\.id) } == ["3"])
        #expect(PantryList.sections(items, search: "salt", status: .out).isEmpty)
        #expect(PantryList.sections(items, search: "   ").count == 2)
    }

    // MARK: - Expiry

    @Test(arguments: [
        ("2026-09-14", -1, "Expired"),
        ("2026-09-15", 0, "Expires today"),
        ("2026-09-16", 1, "Expires tomorrow"),
        ("2026-09-18", 3, "Expires in 3 days"),
        ("2026-10-15", 30, "Expires in 30 days"),
    ])
    func expiryText(expiresOn: String, days: Int, text: String) throws {
        let expiry = try #require(
            PantryExpiry(expiresOn: expiresOn, today: try date(2026, 9, 15, hour: 23), timeZone: denver))
        #expect(expiry.daysRemaining == days)
        #expect(expiry.text(locale: Locale(identifier: "en_US")) == text)
        #expect(expiry.isExpired == (days < 0))
        #expect(expiry.isSoon == (0...3).contains(days))
    }

    @Test func farExpiryShowsTheDate() throws {
        let expiry = try #require(PantryExpiry(expiresOn: "2027-03-01", today: try date(2026, 9, 15), timeZone: denver))
        #expect(expiry.text(locale: Locale(identifier: "en_US")) == "Expires Mar 1, 2027")
        #expect(!expiry.isSoon)
    }

    @Test func expiryCountsCalendarDaysAcrossDaylightSavingTime() throws {
        let today = try date(2026, 10, 31)
        let expiry = try #require(PantryExpiry(expiresOn: "2026-11-02", today: today, timeZone: denver))
        #expect(expiry.daysRemaining == 2)
    }

    @Test(arguments: [nil, "", "2026-02-30", "2026-9-15", "15/09/2026", "2026-13-01", "+026-09-15"])
    func invalidExpiryDatesAreIgnored(expiresOn: String?) throws {
        #expect(PantryExpiry(expiresOn: expiresOn, today: try date(2026, 9, 15), timeZone: denver) == nil)
    }

    @Test func datesRoundTripInTheGivenTimeZone() throws {
        let parsed = try #require(PantryDate.date(from: "2026-12-31", timeZone: denver))
        #expect(PantryDate.string(from: parsed, timeZone: denver) == "2026-12-31")
        #expect(PantryDate.string(from: try date(2026, 1, 2, hour: 23), timeZone: denver) == "2026-01-02")
    }

    // MARK: - Amounts

    @Test func amountText() {
        let english = Locale(identifier: "en_US")
        let cups = PantryFixtures.item(quantity: "3/2", quantityValue: 1.5, unit: "cup")
        let count = PantryFixtures.item(quantity: "2", quantityValue: 2, unit: "count")
        let clove = PantryFixtures.item(quantity: "1", quantityValue: 1, unit: "clove")
        let unknown = PantryFixtures.item(quantity: "2", quantityValue: 2, unit: "sprig")
        let none = PantryFixtures.item()

        #expect(PantryFormat.amount(cups, locale: english) == "1½ cups")
        #expect(PantryFormat.amount(count, locale: english) == "2")
        #expect(PantryFormat.amount(clove, locale: english) == "1 clove")
        #expect(PantryFormat.amount(unknown, locale: english) == "2 sprig")
        #expect(PantryFormat.amount(none, locale: english) == nil)
    }

    @Test func unitPickerKeepsUnknownCodes() {
        #expect(PantryUnit.options(including: "cup") == PantryUnit.codes)
        #expect(PantryUnit.options(including: "sprig").last == "sprig")
        #expect(PantryUnit.pickerLabel("count") == "Count")
        #expect(PantryUnit.pickerLabel("floz") == "fl oz")
    }

    // MARK: - Bulk summary

    @Test func bulkSummaryNamesMissingItems() {
        let one = PantryBulkOutcome(status: .low, updatedCount: 1, missingCount: 0, missingNames: [])
        let named = PantryBulkOutcome(status: .out, updatedCount: 3, missingCount: 2, missingNames: ["Salt", "Flour"])
        let unnamed = PantryBulkOutcome(status: .inStock, updatedCount: 0, missingCount: 2, missingNames: ["Salt"])

        #expect(one.summary == "Marked 1 item as low.")
        #expect(
            named.summary
                == "Marked 3 items as out. Salt and Flour were no longer in the pantry. Someone may have removed them.")
        #expect(
            unnamed.summary
                == "Marked 0 items as in stock. 2 items were no longer in the pantry. Someone may have removed them.")
    }
}
