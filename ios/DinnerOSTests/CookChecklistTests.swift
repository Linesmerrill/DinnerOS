import Foundation
import Testing

@testable import DinnerOS

/// The cooking checklist: an ingredient the steps use in shares opens into a sub-list, and
/// ticking shares, the whole ingredient, and the current step behave the way a cook expects.
@MainActor
struct CookChecklistTests {
    /// Olive oil split across both steps; garlic named once.
    private static let instructions = Data(
        #"""
        {"recipeId":"recipe-1","steps":[
          {"index":1,"text":"Heat ½ tbsp olive oil and 1 clove garlic.","segments":[
            {"kind":"text","text":"Heat "},
            {"kind":"ingredient","text":"½ tbsp olive oil","ingredientId":"ingredient-Olive Oil","name":"Olive Oil",
             "amount":{"quantity":"1/2","quantityValue":0.5,"unit":"tbsp","text":"½ tbsp"},"part":true},
            {"kind":"text","text":" and "},
            {"kind":"ingredient","text":"1 clove garlic","ingredientId":"ingredient-Garlic","name":"Garlic",
             "amount":{"quantity":"1","quantityValue":1,"unit":"clove","text":"1 clove"}},
            {"kind":"text","text":"."}],"notes":[]},
          {"index":2,"text":"Drizzle with ½ tbsp olive oil.","segments":[
            {"kind":"text","text":"Drizzle with "},
            {"kind":"ingredient","text":"½ tbsp olive oil","ingredientId":"ingredient-Olive Oil","name":"Olive Oil",
             "amount":{"quantity":"1/2","quantityValue":0.5,"unit":"tbsp","text":"½ tbsp"},"part":true},
            {"kind":"text","text":"."}],"notes":[]}]}
        """#.utf8)

    private func checklist() throws -> [CookIngredient] {
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: Self.instructions)
        return CookChecklist.make(recipe: RecipePreviewData.recipe, servings: 2, instructions: instructions)
    }

    @Test func anIngredientSplitAcrossStepsGetsASubList() throws {
        let list = try checklist()
        let oil = try #require(list.first { $0.name == "Olive Oil" })
        #expect(oil.amountText == "1 tbsp")
        #expect(oil.parts.map(\.amountText) == ["½ tbsp", "½ tbsp"])
        #expect(oil.parts.map(\.stepIndex) == [1, 2])
        // Named once, with the whole amount: no sub-list.
        #expect(list.first { $0.name == "Garlic" }?.parts.isEmpty == true)
    }

    @Test func tickingEveryShareTicksTheIngredient() throws {
        let oil = try #require(try checklist().first { $0.name == "Olive Oil" })
        let session = CookSession()
        session.toggle(oil.parts[0], of: oil, recipe: "r")
        #expect(!session.isChecked(oil.id, recipe: "r"))
        session.toggle(oil.parts[1], of: oil, recipe: "r")
        #expect(session.isChecked(oil.id, recipe: "r"))
        // Unticking the ingredient unticks its shares.
        session.toggle(oil, recipe: "r")
        #expect(!session.isChecked(oil.parts[0].id, recipe: "r"))
        #expect(!session.isChecked(oil.parts[1].id, recipe: "r"))
    }

    @Test func eachDishKeepsItsOwnProgress() throws {
        let oil = try #require(try checklist().first { $0.name == "Olive Oil" })
        let session = CookSession()
        session.toggle(oil, recipe: "main")
        session.tapStep(2, recipe: "main")
        #expect(!session.isChecked(oil.id, recipe: "side"))
        #expect(session.current(recipe: "side") == nil)
        #expect(session.current(recipe: "main") == 2)
        session.tapStep(2, recipe: "main")
        #expect(session.current(recipe: "main") == nil)
    }
}
