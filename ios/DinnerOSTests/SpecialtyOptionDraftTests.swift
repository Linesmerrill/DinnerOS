import Foundation
import Testing

@testable import DinnerOS

struct SpecialtyOptionDraftTests {
    /// Store alternative, curated batch, household batch.
    private func options() throws -> [SpecialtyOption] {
        try JSONCoding.makeDecoder()
            .decode(SpecialtyIngredient.self, from: Data(SpecialtyFixtures.southwestJSON.utf8))
            .options
    }

    @Test func customizingABatchCopiesItAsEditableText() throws {
        let batch = try options()[1]

        let draft = SpecialtyOptionDraft(copying: batch)

        #expect(draft.type == .houseMadeBatch)
        #expect(draft.name == "Southwest spice blend (house blend) (custom)")
        #expect(draft.basedOnOptionID == "southwest-spice-blend.batch")
        #expect(draft.notes == "Keep away from heat.")
        #expect(draft.ingredients.map(\.quantityText) == ["3", "2", ""])
        #expect(draft.ingredients.last?.category == "spices")
        #expect(draft.yieldQuantityText == "12")
        #expect(draft.yieldUnit == "tbsp")
        #expect(draft.shelfLifeDays == 180)
        #expect(draft.steps.map(\.text) == ["Stir everything together in a bowl.", "Store in an airtight jar."])

        let request = try draft.request()
        #expect(request.type == .houseMadeBatch)
        #expect(request.batchYield == SpecialtyAmountInput(quantity: "12", unit: "tbsp"))
        #expect(request.shelfLifeDays == 180)
        #expect(request.per == nil)
        #expect(request.steps == ["Stir everything together in a bowl.", "Store in an airtight jar."])
        #expect(
            request.ingredients.first == SpecialtyIngredientInput(name: "Chili Powder", quantity: "3", unit: "tbsp"))
        #expect(request.ingredients.last == SpecialtyIngredientInput(name: "Salt", category: "spices"))
        #expect(request.basedOnOptionID == "southwest-spice-blend.batch")
    }

    @Test func customizingAStoreAlternativeKeepsItsRatio() throws {
        var draft = SpecialtyOptionDraft(copying: try options()[0])
        #expect(draft.perQuantityText == "1")
        #expect(draft.perUnit == "tbsp")
        #expect(draft.ingredients.map(\.quantityText) == ["1 1/2", "3/4"])

        draft.ingredients[0].quantityText = "2½"
        let request = try draft.request()

        #expect(request.per == SpecialtyAmountInput(quantity: "1", unit: "tbsp"))
        #expect(request.ingredients.map(\.quantity) == ["5/2", "3/4"])
        #expect(request.steps == nil)
        #expect(request.batchYield == nil)
        #expect(request.shelfLifeDays == nil)
    }

    @Test func editingAHouseholdOptionKeepsWhatItWasBasedOn() throws {
        let draft = SpecialtyOptionDraft(editing: try options()[2])

        #expect(draft.name == "Mild southwest blend")
        #expect(draft.basedOnOptionID == "southwest-spice-blend.batch")
        #expect(draft.ingredients.map(\.quantityText) == ["1 1/2"])
    }

    @Test func blankRowsAreIgnoredAndEmptyNotesOmitted() throws {
        var draft = SpecialtyOptionDraft(copying: try options()[1])
        draft.notes = "  "
        draft.addIngredient()
        draft.addStep()

        let request = try draft.request()

        #expect(request.notes == nil)
        #expect(request.ingredients.count == 3)
        #expect(request.steps?.count == 2)
    }

    @Test func problemsExplainWhatToFix() throws {
        let store = try options()[0]
        let batch = try options()[1]

        var draft = SpecialtyOptionDraft(copying: batch)
        draft.name = " "
        #expect(draft.problem == .nameMissing)
        #expect(!draft.isValid)

        draft = SpecialtyOptionDraft(copying: batch)
        draft.yieldQuantityText = ""
        #expect(draft.problem == .yieldMissing)

        draft = SpecialtyOptionDraft(copying: batch)
        draft.shelfLifeDays = 0
        #expect(draft.problem == .shelfLife)

        draft = SpecialtyOptionDraft(copying: batch)
        draft.ingredients[0].quantityText = "a pinch"
        #expect(draft.problem == .ingredientAmount("Chili Powder", .invalid))

        draft = SpecialtyOptionDraft(copying: batch)
        draft.ingredients[0].name = ""
        #expect(draft.problem == .ingredientNameMissing)

        draft = SpecialtyOptionDraft(copying: batch)
        draft.ingredients = []
        #expect(draft.problem == .noIngredients)

        var storeDraft = SpecialtyOptionDraft(copying: store)
        storeDraft.ingredients[1].quantityText = ""
        #expect(storeDraft.problem == .ingredientAmountMissing("Ground Cumin"))

        storeDraft = SpecialtyOptionDraft(copying: store)
        storeDraft.perQuantityText = ""
        #expect(storeDraft.problem == .perMissing)
        #expect(SpecialtyOptionProblem.perMissing.errorDescription?.isEmpty == false)
        #expect(SpecialtyOptionDraft(copying: store).isValid)
    }
}
