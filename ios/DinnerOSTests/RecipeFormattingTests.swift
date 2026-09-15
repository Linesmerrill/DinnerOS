import Foundation
import Testing

@testable import DinnerOS

struct ISOWeekTests {
    private let locale = Locale(identifier: "en_US")

    @Test func formatsMonthAndWeek() throws {
        let week = try #require(ISOWeek("2026-W37"))
        #expect(week.monthAndYear(locale: locale) == "Sep 2026")
        #expect(week.weekOf(locale: locale) == "Week of Sep 7, 2026")
        #expect(week.description == "2026-W37")
    }

    @Test func weekOneReadsAsJanuaryEvenWhenItStartsInDecember() throws {
        let week = try #require(ISOWeek("2026-W01"))
        #expect(week.monthAndYear(locale: locale) == "Jan 2026")
        #expect(week.weekOf(locale: locale) == "Week of Dec 29, 2025")
    }

    @Test func week53OnlyInLongYears() {
        #expect(ISOWeek("2026-W53") != nil)
        #expect(ISOWeek("2025-W53") == nil)
    }

    @Test(arguments: ["", "2026-37", "26-W37", "2026-W00", "2026-W54", "2026-W5", "2026-Wxx", "abcd-W10"])
    func rejectsMalformedWeeks(_ string: String) {
        #expect(ISOWeek(string) == nil)
    }

    @Test func ordersChronologically() throws {
        #expect(try #require(ISOWeek("2025-W52")) < #require(ISOWeek("2026-W01")))
    }
}

struct QuantityFormattingTests {
    private let locale = Locale(identifier: "en_US")

    @Test(
        arguments: [
            ("1/2", 0.5, "½"),
            ("3/2", 1.5, "1½"),
            ("7/4", 1.75, "1¾"),
            ("2/4", 0.5, "½"),
            ("1/3", 0.333, "⅓"),
            ("5/8", 0.625, "⅝"),
            ("2", 2, "2"),
            ("0", 0, "0"),
            ("1000", 1000, "1,000"),
            ("1/7", 0.142857, "0.14"),
            ("23/10", 2.3, "2.3"),
        ] as [(String, Double, String)])
    func formatsExactQuantities(quantity: String, value: Double, expected: String) {
        #expect(RecipeFormat.quantity(quantity, value: value, locale: locale) == expected)
    }

    @Test func prefersTheExactStringOverTheFloat() {
        // A float that has drifted still shows the exact fraction.
        #expect(RecipeFormat.quantity("1/3", value: 0.3333333, locale: locale) == "⅓")
    }

    @Test func fallsBackToTheValueWhenTheStringIsUnusable() {
        #expect(RecipeFormat.quantity("one and a half", value: 1.5, locale: locale) == "1.5")
        #expect(RecipeFormat.quantity("1/0", value: nil, locale: locale) == nil)
        #expect(RecipeFormat.quantity(nil, value: 0.25, locale: locale) == "0.25")
        #expect(RecipeFormat.quantity(nil, value: nil, locale: locale) == nil)
    }

    @Test(
        arguments: [
            ("1/2", 0.5, "oz", "ounce", "½ oz"),
            ("1", 1, "clove", "clove", "1 clove"),
            ("2", 2, "clove", "clove", "2 cloves"),
            ("3/2", 1.5, "cup", "cup", "1½ cups"),
            ("6", 6, "count", "unit", "6"),
            ("250", 250, "ml", "milliliter", "250 mL"),
            ("2", 2, "floz", "fluid ounce", "2 fl oz"),
            ("1", 1, "", "dollop", "1 dollop"),
            ("1", 1, "", "", "1"),
        ] as [(String, Double, String, String, String)])
    func formatsAmountsWithUnits(quantity: String, value: Double, unit: String, sourceUnit: String, expected: String) {
        let amount = RecipeAmount(
            servings: 2, quantity: quantity, quantityValue: value, unit: unit, sourceUnit: sourceUnit, rawText: "")
        #expect(RecipeFormat.amount(amount, locale: locale) == expected)
    }

    @Test func noAmountWithoutAQuantity() {
        let amount = RecipeAmount(
            servings: 2, quantity: nil, quantityValue: nil, unit: "tsp", sourceUnit: "tsp", rawText: "Salt")
        #expect(RecipeFormat.amount(amount, locale: locale) == nil)
    }

    @Test func orderCountsAndDifficulty() {
        #expect(RecipeFormat.timesOrdered(0) == "Not ordered yet")
        #expect(RecipeFormat.timesOrdered(1) == "Ordered once")
        #expect(RecipeFormat.timesOrdered(3) == "Ordered 3 times")
        #expect(RecipeFormat.difficulty(2, source: "hellofresh") == "Medium")
        #expect(RecipeFormat.difficulty(7, source: "manual") == "Difficulty 7")
    }

    @Test func rowDetailsCombineStats() {
        let summary = RecipeSummary(
            id: "r", name: "Test", headline: nil, imageURLString: nil, totalMinutes: nil, timesOrdered: 3,
            lastOrderedWeek: "2026-W37", isAddon: true, tags: [])
        #expect(RecipeRow.details(for: summary, locale: locale) == "Add-on · Ordered 3 times · Last Sep 2026")
    }
}

struct ServingsTests {
    private let locale = Locale(identifier: "en_US")

    private func recipe(servings: String = "[2,4]") throws -> Recipe {
        try JSONCoding.makeDecoder().decode(Recipe.self, from: RecipeFixtures.detail(servings: servings))
    }

    @Test func switchingServingsUsesAuthoredAmountsNotScaling() throws {
        let recipe = try recipe()

        let two = recipe.ingredientLines(servings: 2, locale: locale)
        let four = recipe.ingredientLines(servings: 4, locale: locale)

        #expect(two.map(\.name) == ["Cheddar", "Garlic", "Flour Tortillas", "Salt", "Test Paste"])
        #expect(two.map(\.amount) == ["½ oz", "1 clove", "6", nil, "1½ tbsp"])
        // ¾ oz, not 1 oz: the 4-serving amount comes from the API. Test Paste has no
        // authored 4-serving amount, so none is invented.
        #expect(four.map(\.amount) == ["¾ oz", "2 cloves", "12", nil, nil])
        #expect(two.map(\.isPantryStaple) == [false, false, false, true, false])
        #expect(Set(two.map(\.id)).count == two.count)
    }

    @Test(arguments: [(nil, 2), (2, 2), (3, 4), (4, 4), (6, 4), (1, 2)] as [(Int?, Int)])
    func preferredServings(householdDefault: Int?, expected: Int) throws {
        #expect(try recipe().preferredServings(householdDefault: householdDefault) == expected)
    }

    @Test func servingOptionsFallBackToAmounts() throws {
        let recipe = try recipe(servings: "[]")
        #expect(recipe.servingOptions == [2, 4])
    }

    @Test func servingOptionsAreSortedAndUnique() throws {
        #expect(try recipe(servings: "[4,2,4]").servingOptions == [2, 4])
    }
}
