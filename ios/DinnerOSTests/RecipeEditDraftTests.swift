import Foundation
import Testing

@testable import DinnerOS

/// Editing a recipe: amounts by serving size, renames, and what gets saved.
struct RecipeEditDraftTests {
    private func recipe() throws -> Recipe {
        let json = """
            {"id":"r1","householdId":"h1","source":"hellofresh","name":"Noodles","isAddon":false,"servings":[2,4],
             "cuisines":[],"tags":[],"orderWeeks":[],"timesOrdered":0,
             "createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z",
             "householdRating":{"count":0,"average":null},
             "ingredients":[
               {"ingredientId":"i-ramen","name":"Ramen Noodles","category":"pantry","pantryStaple":false,"amounts":[
                 {"servings":2,"quantity":"6","quantityValue":6,"unit":"oz","sourceUnit":"ounce","rawText":"6 ounce"},
                 {"servings":4,"quantity":"12","quantityValue":12,"unit":"oz","sourceUnit":"ounce","rawText":"12 ounce"}]},
               {"ingredientId":"i-salt","name":"Salt","category":"spices","pantryStaple":true,"amounts":[
                 {"servings":2,"quantity":null,"quantityValue":null,"unit":"","sourceUnit":"","rawText":"Salt"}]},
               {"ingredientId":"i-oil","name":"Oil","category":"pantry","pantryStaple":true,"amounts":[
                 {"servings":2,"quantity":"3/2","quantityValue":1.5,"unit":"tbsp","sourceUnit":"","rawText":""},
                 {"servings":4,"quantity":"3","quantityValue":3,"unit":"tbsp","sourceUnit":"","rawText":""}]}],
             "steps":[{"index":2,"text":"Toss with oil."},{"index":1,"text":"Boil ramen noodles, 3 minutes."}],
             "editedAt":"2026-11-10T00:00:00Z"}
            """
        return try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
    }

    @Test func loadsAmountsByServingSizeAndStepsInOrder() throws {
        let r = try recipe()
        #expect(r.editedAt != nil)
        let draft = RecipeEditDraft(recipe: r, servings: 4)
        #expect(draft.servings == 4)
        #expect(draft.steps == ["Boil ramen noodles, 3 minutes.", "Toss with oil."])
        #expect(draft.quantity(of: draft.lines[0]) == "12")
        #expect(draft.lines[2].quantities[2] == "1 1/2")
        #expect(!draft.hasChanges)
        #expect(RecipeEditDraft(recipe: r, servings: 3).servings == 2)
    }

    @Test func anAmountChangesEverySizeInProportion() throws {
        var draft = RecipeEditDraft(recipe: try recipe(), servings: 2)
        let ramen = draft.lines[0].id
        draft.setQuantity("8", for: ramen)
        #expect(draft.lines[0].quantities == [2: "8", 4: "16"])
        // A new line scales by servings.
        draft.addLine()
        let added = draft.lines[3].id
        draft.setQuantity("1/2", for: added)
        #expect(draft.lines[3].quantities[4] == "1")
        draft.setQuantity("", for: ramen)
        #expect(draft.lines[0].quantities == [2: "", 4: ""])
        #expect(draft.hasChanges)
    }

    @Test func aRenameIsASwapAndSavesWithoutTheOldIngredient() throws {
        var draft = RecipeEditDraft(recipe: try recipe(), servings: 2)
        let ramen = draft.lines[0].id
        #expect(draft.pendingSwap(for: ramen) == nil)
        draft.lines[0].name = "Lo Mein Noodles"
        #expect(draft.pendingSwap(for: ramen)?.old == "Ramen Noodles")
        #expect(draft.pendingSwap(for: ramen)?.new == "Lo Mein Noodles")
        draft.finishSwap(for: ramen)
        #expect(draft.pendingSwap(for: ramen) == nil)

        let request = draft.request
        #expect(request.ingredients[0].ingredientID == nil)
        #expect(request.ingredients[0].name == "Lo Mein Noodles")
        #expect(request.ingredients[0].amounts.map(\.unit) == ["oz", "oz"])
        // Unchanged lines keep their ingredient and their own units, salt included.
        #expect(request.ingredients[1].ingredientID == "i-salt")
        #expect(request.ingredients[1].amounts.map(\.unit) == ["", ""])
        #expect(request.ingredients[1].amounts.map(\.quantity) == ["", ""])
        #expect(request.ingredients[2].amounts.map(\.quantity) == ["1 1/2", "3"])
    }

    @Test func saysWhatStopsASave() throws {
        var draft = RecipeEditDraft(recipe: try recipe())
        #expect(draft.problem == nil)
        draft.setQuantity("a lot", for: draft.lines[0].id)
        #expect(draft.problem == "The amount for Ramen Noodles isn't a number.")
        draft.setQuantity("6", for: draft.lines[0].id)
        draft.lines[1].name = " "
        #expect(draft.problem == "Every ingredient needs a name.")
        draft.lines[1].name = "Salt"
        draft.steps = ["", "  "]
        #expect(draft.problem == "Add at least one step.")
    }

    @Test func numbersReadTheWayPeopleTypeThem() {
        #expect(RecipeEditDraft.number("1 1/2") == 1.5)
        #expect(RecipeEditDraft.number("0.25") == 0.25)
        #expect(RecipeEditDraft.number("1/0") == nil)
        #expect(RecipeEditDraft.number("two") == nil)
        #expect(RecipeEditDraft.format(2.0) == "2")
        #expect(RecipeEditDraft.format(1.333) == "1.33")
        #expect(RecipeEditDraft.format(0.5) == "0.5")
        #expect(RecipeEditDraft.display("3/2") == "1 1/2")
        #expect(RecipeEditDraft.display("1/2") == "1/2")
    }

    @Test func aSwapPreviewDecodes() throws {
        let json = """
            {"steps":[{"text":"Boil lo mein noodles, 3 minutes.","changed":true},{"text":"Toss.","changed":false}],
             "cookTime":{"lowMinutes":4,"highMinutes":6,"suggestedMinutes":6,"text":"Lo mein noodles usually cook 4 to 6 minutes."},
             "times":[{"step":0,"text":"3 minutes","minutes":3}]}
            """
        let preview = try JSONDecoder().decode(RecipeSwapPreview.self, from: Data(json.utf8))
        #expect(preview.changedCount == 1)
        #expect(preview.cookTime?.suggestedMinutes == 6)
        #expect(preview.times.first?.minutes == 3)
    }
}
