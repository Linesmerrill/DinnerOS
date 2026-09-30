import Foundation
import Testing

@testable import DinnerOS

struct PantryQuantityTests {
    @Test(arguments: [
        ("2", "2"),
        ("0.5", "1/2"),
        (".5", "1/2"),
        ("1.25", "5/4"),
        ("1/2", "1/2"),
        ("3/6", "1/2"),
        ("4/2", "2"),
        ("1 1/2", "3/2"),
        ("  1   1/2  ", "3/2"),
        ("½", "1/2"),
        ("1½", "3/2"),
        ("1 ½", "3/2"),
        ("2¾", "11/4"),
        ("1⁄3", "1/3"),
        ("1 0.5", "3/2"),
    ])
    func parsesToTheAPIsExactForm(text: String, exact: String) throws {
        #expect(try PantryQuantity.parse(text) == exact)
    }

    @Test(arguments: ["", "   ", "\n"])
    func blankIsNoAmount(text: String) throws {
        #expect(try PantryQuantity.parse(text) == nil)
    }

    @Test(
        arguments: [
            ("-1", PantryQuantityError.negative),
            ("-1/2", .negative),
            ("1 -1/2", .negative),
            ("−2", .negative),
            ("0", .zero),
            ("0/4", .zero),
            ("0.0", .zero),
            ("abc", .invalid),
            ("1/0", .invalid),
            ("1..2", .invalid),
            ("1/2/3", .invalid),
            ("1.5/2", .invalid),
            ("1-2", .invalid),
            ("2 cups", .invalid),
            ("/", .invalid),
            ("٣", .invalid),
            ("99999999999999999999", .invalid),
            (String(repeating: "1", count: 33), .tooLong),
        ] as [(String, PantryQuantityError)])
    func rejectsInvalidAmounts(text: String, error: PantryQuantityError) {
        #expect(throws: error) {
            try PantryQuantity.parse(text)
        }
    }

    @Test func errorsHaveMessages() {
        for error in [PantryQuantityError.negative, .zero, .invalid, .tooLong] {
            #expect(error.errorDescription?.isEmpty == false)
        }
    }

    @Test(
        arguments: [(nil, ""), ("2", "2"), ("1/2", "1/2"), ("3/2", "1 1/2"), ("11/4", "2 3/4"), ("odd", "odd")]
            as [(String?, String)])
    func editingTextParsesBackToTheSameValue(exact: String?, text: String) throws {
        #expect(PantryQuantity.editingText(exact) == text)
        if let exact, exact != "odd" {
            #expect(try PantryQuantity.parse(text) == exact)
        }
    }
}

struct PantryItemDraftTests {
    private let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    private var today: Date {
        get throws {
            try #require(
                PantryDate.calendar(timeZone: denver).date(from: DateComponents(year: 2026, month: 9, day: 15, hour: 9))
            )
        }
    }

    private func oliveOil() -> PantryItem {
        PantryFixtures.item(
            id: "item-oil", name: "Olive Oil", category: "pantry", quantity: "3/2", quantityValue: 1.5, unit: "cup",
            isStaple: true, expiresOn: "2027-03-01", note: "big tin")
    }

    @Test func unchangedDraftHasNoChanges() throws {
        let item = oliveOil()
        let draft = PantryItemDraft(item: item, today: try today, timeZone: denver)

        #expect(draft.quantityText == "1 1/2")
        #expect(draft.hasExpiry)
        #expect(try draft.changes(from: item, timeZone: denver).isEmpty)
    }

    @Test func equivalentQuantityTextIsNotAChange() throws {
        let item = oliveOil()
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        draft.quantityText = "1½"
        draft.note = " big tin "

        #expect(try draft.changes(from: item, timeZone: denver).isEmpty)
    }

    @Test func markingOutSendsOnlyTheStatus() throws {
        let item = oliveOil()
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        draft.status = .out
        draft.quantityText = "-5"

        #expect(draft.isValid)
        #expect(try draft.changes(from: item, timeZone: denver) == PantryItemChanges(status: .out))
    }

    @Test func amountUnitStapleExpiryAndNoteChanges() throws {
        let item = oliveOil()
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        draft.unit = "tbsp"
        draft.isStaple = false
        draft.hasExpiry = false
        draft.note = ""

        #expect(
            try draft.changes(from: item, timeZone: denver)
                == PantryItemChanges(
                    quantity: "3/2", unit: "tbsp", isStaple: false, expiresOn: "", note: "", storage: .pantry,
                    storedOn: "2026-09-15"))
    }

    @Test func clearingTheAmountSendsAnEmptyQuantity() throws {
        let item = oliveOil()
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        draft.quantityText = "  "

        #expect(try draft.changes(from: item, timeZone: denver) == PantryItemChanges(quantity: ""))
    }

    @Test func settingAnExpiryDateSendsTheCalendarDate() throws {
        let item = PantryFixtures.item(name: "Yogurt")
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        #expect(!draft.hasExpiry)
        draft.hasExpiry = true
        draft.expiryDate = try today.addingTimeInterval(3 * 86_400)

        #expect(try draft.changes(from: item, timeZone: denver) == PantryItemChanges(expiresOn: "2026-09-18"))
    }

    @Test func invalidQuantityBlocksSaving() throws {
        let item = oliveOil()
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        draft.quantityText = "-1"

        #expect(!draft.isValid)
        #expect(draft.quantityError == PantryQuantityError.negative.errorDescription)
        #expect(throws: PantryQuantityError.negative) {
            try draft.changes(from: item, timeZone: denver)
        }
    }

    @Test func longNotesAreInvalid() throws {
        var draft = PantryItemDraft(today: try today, timeZone: denver)
        draft.note = String(repeating: "a", count: PantryItemDraft.maxNoteLength + 1)
        #expect(draft.noteError != nil)
        #expect(!draft.isValid)
    }

    @Test func newFreeTextItemOmitsUnsetFields() throws {
        var draft = PantryItemDraft(today: try today, timeZone: denver)
        draft.unit = "cup"

        let item = try draft.newItem(name: "  Za'atar ", ingredientID: nil, timeZone: denver)

        #expect(
            item == NewPantryItem(name: "Za'atar", status: .inStock, storage: .pantry, storedOn: "2026-09-15"))
    }

    @Test func newCatalogItemSendsTheIDAndFields() throws {
        var draft = PantryItemDraft(today: try today, timeZone: denver)
        draft.status = .low
        draft.quantityText = "1½"
        draft.unit = "cup"
        draft.isStaple = true
        draft.hasExpiry = true
        draft.note = " tin "

        let item = try draft.newItem(name: "Olive Oil", ingredientID: "i-olive-oil", timeZone: denver)

        #expect(
            item
                == NewPantryItem(
                    ingredientID: "i-olive-oil", quantity: "3/2", unit: "cup", status: .low, isStaple: true,
                    expiresOn: "2026-09-15", note: "tin", storage: .pantry, storedOn: "2026-09-15"))
    }

    @Test func newFrozenItemSendsWhereAndWhenSoTheServerDatesIt() throws {
        var draft = PantryItemDraft(today: try today, timeZone: denver)
        draft.storage = .freezer
        draft.storedOn = try today.addingTimeInterval(-86_400)

        let item = try draft.newItem(name: "Ground Pork", ingredientID: nil, timeZone: denver)

        #expect(item.storage == .freezer)
        #expect(item.storedOn == "2026-09-14")
        #expect(item.expiresOn == nil)
    }

    @Test func movingAnItemSendsTheNewStorageAndDay() throws {
        var item = PantryFixtures.item(name: "Carrots", category: "produce", expiresOn: "2026-09-29")
        item.storedOn = "2026-09-15"
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        // A date the server recommended isn't the member's own.
        #expect(!draft.hasExpiry)
        #expect(try draft.changes(from: item, timeZone: denver).isEmpty)

        draft.storage = .freezer
        #expect(
            try draft.changes(from: item, timeZone: denver)
                == PantryItemChanges(storage: .freezer, storedOn: "2026-09-15"))
    }

    @Test func turningOffAnOwnDateHandsItBackToTheRecommendation() throws {
        let item = PantryFixtures.item(name: "Yogurt", expiresOn: "2026-10-01")
        var draft = PantryItemDraft(item: item, today: try today, timeZone: denver)
        #expect(draft.hasExpiry)
        draft.hasExpiry = false

        #expect(
            try draft.changes(from: item, timeZone: denver)
                == PantryItemChanges(expiresOn: "", storage: .pantry, storedOn: "2026-09-15"))
    }
}

struct ShelfLifeSuggestionTests {
    @Test func decodesTheLibraryAnswerAndExplainsIt() throws {
        let json = Data(
            #"""
            {"bestBy":"2026-10-14","storedOn":"2026-09-30","storage":"fridge","minDays":14,"maxDays":21,"text":"2–3 weeks",
             "matched":"Carrots, parsnips","estimate":false,"source":"USDA FoodKeeper","usualStorage":"fridge"}
            """#.utf8)
        let suggestion = try JSONDecoder().decode(ShelfLifeSuggestion.self, from: json)

        #expect(suggestion.bestBy == "2026-10-14")
        #expect(suggestion.storage == .fridge)
        #expect(suggestion.usualStorage == .fridge)
        #expect(suggestion.explanation == "2–3 weeks in the fridge, from USDA FoodKeeper.")
    }

    @Test func anEstimateSaysSo() throws {
        let json = Data(
            #"""
            {"bestBy":"2026-11-30","storedOn":"2026-09-30","storage":"pantry","text":"2 months","estimate":true,
             "source":"Typical for this kind of food"}
            """#.utf8)
        let suggestion = try JSONDecoder().decode(ShelfLifeSuggestion.self, from: json)

        #expect(suggestion.matched == nil)
        #expect(suggestion.usualStorage == nil)
        #expect(suggestion.explanation == "About 2 months in the pantry, typical for foods like this.")
    }
}

struct IngredientSuggestionsTests {
    private final class Recorder {
        var queries: [String] = []
    }

    private let olive = CatalogIngredient(
        id: "i-olive-oil", key: "olive oil", name: "Olive Oil", category: "pantry", categoryConfident: true,
        imageURLString: nil)

    @Test func searchesOnceTypingPauses() async {
        let recorder = Recorder()
        let olive = olive
        let suggestions = IngredientSuggestions(debounce: .milliseconds(20)) { query in
            recorder.queries.append(query)
            return [olive]
        }

        let superseded = Task { await suggestions.update(for: "ol") }
        superseded.cancel()
        await suggestions.update(for: " oli ")
        await superseded.value

        #expect(recorder.queries == ["oli"])
        #expect(suggestions.query == "oli")
        #expect(suggestions.results == [olive])
        #expect(!suggestions.isSearching)

        // The same text doesn't search again.
        await suggestions.update(for: "oli")
        #expect(recorder.queries == ["oli"])
    }

    @Test func shortTextClearsWithoutSearching() async {
        let recorder = Recorder()
        let olive = olive
        let suggestions = IngredientSuggestions(debounce: .zero) { query in
            recorder.queries.append(query)
            return [olive]
        }
        await suggestions.update(for: "olive")

        await suggestions.update(for: " o ")

        #expect(recorder.queries == ["olive"])
        #expect(suggestions.results.isEmpty)
        #expect(suggestions.query.isEmpty)
    }

    @Test func failureShowsAMessageAndRetriesTheSameText() async {
        let recorder = Recorder()
        let suggestions = IngredientSuggestions(debounce: .zero) { query in
            recorder.queries.append(query)
            throw APIError.transport(.notConnectedToInternet)
        }

        await suggestions.update(for: "salt")
        #expect(suggestions.errorMessage?.localizedCaseInsensitiveContains("offline") == true)
        #expect(suggestions.results.isEmpty)

        await suggestions.update(for: "salt")
        #expect(recorder.queries == ["salt", "salt"])
    }
}
