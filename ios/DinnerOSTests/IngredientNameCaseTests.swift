import Foundation
import Testing

@testable import DinnerOS

/// Build 122, Hark's review item 16: ingredient names read in Title Case everywhere, the same
/// rule as the server's copytext.IngredientName, so a recipe saved before the server sent Title
/// Case reads the same.
struct IngredientNameCaseTests {
    @Test func namesReadInTitleCase() {
        #expect(RecipeFormat.displayName("bow tie pasta") == "Bow Tie Pasta")
        #expect(RecipeFormat.displayName("Bow tie pasta") == "Bow Tie Pasta")
        #expect(RecipeFormat.displayName("Jasmine Rice") == "Jasmine Rice")
        #expect(RecipeFormat.displayName("sun-dried tomatoes") == "Sun-Dried Tomatoes")
        #expect(RecipeFormat.displayName("cream of mushroom soup") == "Cream of Mushroom Soup")
        #expect(RecipeFormat.displayName("and more") == "And More")
        #expect(RecipeFormat.displayName("BBQ sauce") == "BBQ Sauce")
        #expect(RecipeFormat.displayName("iPhone case") == "iPhone Case")
        #expect(RecipeFormat.displayName("lemon (zested)") == "Lemon (Zested)")
    }

    @Test func aNoteAfterTheNameStaysAsWritten() {
        #expect(RecipeFormat.displayName("lemon wedge, for serving") == "Lemon Wedge, for serving")
    }

    @Test func instructionsMatchARecipeSavedBeforeTitleCase() throws {
        let json = Data(
            #"""
            {"recipeId":"r1","steps":[],"leftOutApplied":true,"ingredients":[
              {"index":0,"ingredientKey":"name:cilantro","name":"Cilantro",
               "leftOut":{"skipId":"s-cilantro","scope":"recipe"},"component":null}]}
            """#.utf8)
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: json)
        let saved = IngredientLine(id: "0-x", name: "cilantro", amount: "1", isPantryStaple: false, category: "produce")
        #expect(RecipeIngredientStates.make(lines: [saved], instructions: instructions).first?.isLeftOut == true)
        let other = IngredientLine(id: "0-x", name: "parsley", amount: "1", isPantryStaple: false, category: "produce")
        #expect(RecipeIngredientStates.make(lines: [other], instructions: instructions).first?.isLeftOut == false)
    }
}
