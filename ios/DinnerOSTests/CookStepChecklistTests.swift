import Foundation
import Testing

@testable import DinnerOS

/// The prep words and parts the by-step list reads out of a step's own words.
struct CookStepChecklistTests {
    @Test func prepWordsReadAsDone() {
        #expect(CookChecklist.prepWords(in: "Trim and slice ") == "sliced")
        #expect(CookChecklist.prepWords(in: "Quarter ") == "quartered")
        #expect(CookChecklist.prepWords(in: "Peel, core, and dice ") == "peeled, cored, and diced")
        #expect(CookChecklist.prepWords(in: "Halve, core, and thinly slice ") == "halved, cored, and thinly sliced")
        #expect(CookChecklist.prepWords(in: "In a medium bowl, combine ") == nil)
        // A word that belongs to something earlier in the sentence never lands here.
        #expect(CookChecklist.prepWords(in: "Stir drained rigatoni, half the Parmesan, and ") == nil)
        #expect(CookChecklist.prepWords(in: "Peel and mince or grate ") == "peeled, minced, or grated")
        #expect(CookChecklist.prepWords(in: "Add diced ") == "diced")
        #expect(CookChecklist.prepWords(in: "juice from half ") == "juiced")
        #expect(CookChecklist.prepWords(in: "Dice the onion, then add the ") == nil)
    }
}

struct CookAheadTests {
    @Test func butterAndCreamCheeseGetReadyAhead() {
        #expect(CookChecklist.aheadNote("Butter", step: 4) == "cut into pieces, for step 4")
        #expect(CookChecklist.aheadNote("Unsalted Butter", step: 3) == "cut into pieces, for step 3")
        #expect(CookChecklist.aheadNote("Peanut Butter", step: 3) == nil)
        #expect(CookChecklist.aheadNote("Cream Cheese", step: 3) == "let soften, for step 3")
        #expect(CookChecklist.aheadNote("Shallot", step: 2) == nil)
    }
}

/// The by-step list for a step that cuts something "into strips", uses "the remaining" of it
/// later, and a blend the household mixes itself.
struct CookStepGroupTests {
    private static let instructions = Data(
        #"""
        {"recipeId":"recipe-1",
         "ingredients":[
           {"index":1,"ingredientKey":"ingredient-Garlic","name":"Garlic"},
           {"index":3,"ingredientKey":"ingredient-Olive Oil","name":"Olive Oil","amountText":"1 Tbsp",
            "component":{"specialtyId":"s","specialtyName":"Olive Oil","optionName":"Mix","type":"store_alternative",
                         "parts":["2 tsp Chili Powder","1 tsp Ground Cumin"]}}],
         "steps":[
          {"index":1,"text":"Thinly slice 1 clove garlic into rounds.","segments":[
            {"kind":"text","text":"Thinly slice "},
            {"kind":"ingredient","text":"1 clove garlic","ingredientId":"ingredient-Garlic","name":"Garlic",
             "amount":{"quantity":"1","quantityValue":1,"unit":"clove","text":"1 clove"}},
            {"kind":"text","text":" into rounds."}],"notes":[]},
          {"index":2,"text":"Add remaining garlic and olive oil.","segments":[
            {"kind":"text","text":"Add remaining "},
            {"kind":"ingredient","text":"garlic","ingredientId":"ingredient-Garlic","name":"Garlic"},
            {"kind":"text","text":" and "},
            {"kind":"ingredient","text":"olive oil","ingredientId":"ingredient-Olive Oil","name":"Olive Oil"},
            {"kind":"text","text":"."}],"notes":[]}]}
        """#.utf8)

    private func groups() throws -> [CookStepGroup] {
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: Self.instructions)
        return CookChecklist.byStep(recipe: RecipePreviewData.recipe, servings: 2, instructions: instructions)
    }

    @Test func howItsCutComesAfterTheName() throws {
        let step1 = try #require(try groups().first { $0.index == 1 })
        #expect(step1.items.first { $0.name == "Garlic" }?.prep == "thinly sliced into rounds")
    }

    /// "the rest" isn't an amount: the server sends what's left; without it the row says nothing.
    @Test func remainingNeverReadsAsTheRest() throws {
        let step2 = try #require(try groups().first { $0.index == 2 })
        let garlic = try #require(step2.items.first { $0.name == "Garlic" })
        #expect(garlic.prep == nil)
    }

    @Test func aHomeMadeBlendIsMixedFirstSpiceBySpice() throws {
        let ready = try #require(try groups().first { $0.index == 0 })
        let mix = try #require(ready.items.first { $0.name == "Olive Oil" })
        #expect(mix.prep == "mix together first")
        #expect(mix.parts == ["2 tsp Chili Powder", "1 tsp Ground Cumin"])
        #expect(mix.amountText == "1 Tbsp")
    }

    @Test func trailingPrepOnlyReadsCuts() {
        #expect(CookChecklist.trailingPrep(" into strips. Halve orange.") == "into strips")
        #expect(CookChecklist.trailingPrep(" into ½-inch pieces, then chill.") == "into ½-inch pieces")
        #expect(CookChecklist.trailingPrep(" lengthwise.") == "lengthwise")
        #expect(CookChecklist.trailingPrep(" into pot with couscous.") == nil)
        #expect(CookChecklist.trailingPrep(" into a bowl.") == nil)
        #expect(CookChecklist.trailingPrep(" in damp paper towels.") == nil)
    }
}

/// Citrus in wedges reads "6 | Lime wedges", and "While the garlic cooks" doesn't list the garlic.
struct CookWedgeTests {
    private static let instructions = Data(
        #"""
        {"recipeId":"recipe-1","steps":[
          {"index":1,"text":"Juice from 6 lime wedges. While garlic cooks, warm 6 tortillas.","segments":[
            {"kind":"text","text":"Juice from "},
            {"kind":"ingredient","text":"6 lime wedges","ingredientId":"ingredient-Garlic","name":"Garlic",
             "amount":{"quantity":"6","quantityValue":6,"unit":"wedge","text":"6 wedges"},"part":true},
            {"kind":"text","text":". While "},
            {"kind":"ingredient","text":"garlic","ingredientId":"ingredient-Garlic","name":"Garlic"},
            {"kind":"text","text":" cooks, warm "},
            {"kind":"ingredient","text":"6 tortillas","ingredientId":"ingredient-Flour Tortillas","name":"Flour Tortillas",
             "amount":{"quantity":"6","quantityValue":6,"unit":"count","text":"6"}},
            {"kind":"text","text":"."}],"notes":[]}]}
        """#.utf8)

    @Test func wedgesCountTheWedgesAndWhileIsSkipped() throws {
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: Self.instructions)
        let groups = CookChecklist.byStep(recipe: RecipePreviewData.recipe, servings: 2, instructions: instructions)
        let step = try #require(groups.first { $0.index == 1 })
        // The preview recipe has no lime, so the wedge rides on garlic: the row is "Garlic wedges".
        #expect(step.items.map(\.name) == ["Garlic wedges", "Flour Tortillas"])
        #expect(step.items.first?.amountText == "6")
    }
}
