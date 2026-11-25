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
        #expect(oil.amountText == "1 Tbsp")
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

    /// Cilantro skipped for this dish: a saved answer from an older server still lists it struck
    /// through and still says to save some. The cooking screen shows neither (build 123).
    @Test func aSkippedIngredientIsNotListedOrSaved() throws {
        let json = Data(
            #"""
            {"recipeId":"r1","steps":[],"leftOutApplied":true,
             "ingredients":[
               {"index":0,"ingredientKey":"rice","name":"Rice"},
               {"index":1,"ingredientKey":"cilantro","name":"Cilantro","leftOut":{"skipId":"s","scope":"recipe","once":true}}],
             "checklist":{"all":[
               {"id":"0-rice","index":0,"name":"Rice","ingredientKey":"rice","parts":[],"components":[]},
               {"id":"1-cilantro","index":1,"name":"Cilantro","leftOut":true,"ingredientKey":"cilantro","parts":[],"components":[]}],
              "byStep":[
               {"index":1,"items":[
                 {"id":"a","ingredientIndex":0,"name":"Rice","ingredientKey":"rice","parts":[]},
                 {"id":"b","ingredientIndex":1,"name":"Cilantro","prep":"roughly chopped","leftOut":true,"ingredientKey":"cilantro","parts":[]}]},
               {"index":2,"items":[
                 {"id":"c","ingredientIndex":1,"name":"Cilantro","leftOut":true,"ingredientKey":"cilantro","parts":[]}]}]},
             "saveForLater":[
               {"ingredientIndex":1,"ingredientKey":"cilantro","name":"Cilantro","text":"Use ¼ oz. Save ¼ oz for Pork Bowls.","short":"Save ¼ oz for Pork Bowls"},
               {"ingredientIndex":0,"ingredientKey":"rice","name":"Rice","text":"Use 1 cup. Save 1 cup for Pork Bowls.","short":"Save 1 cup for Pork Bowls"}]}
            """#.utf8)
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: json)
        let dish = CookDish(recipe: RecipePreviewData.recipe, servings: 2, instructions: instructions)
        #expect(dish.ingredients.map(\.name) == ["Rice"])
        #expect(dish.stepGroups.map(\.index) == [1])
        #expect(dish.stepGroups.flatMap(\.items).map(\.name) == ["Rice"])
        #expect(dish.saveForLater.map(\.name) == ["Rice"])
    }
}
