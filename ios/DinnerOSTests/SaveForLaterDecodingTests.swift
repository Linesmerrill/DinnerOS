import Foundation
import Testing

@testable import DinnerOS

/// What to keep for the week's later meals decodes and is found by the checklist row's key; an
/// older server without it still works.
struct SaveForLaterDecodingTests {
    @Test func notesDecodeAndMatchTheirRow() throws {
        let json = """
            {"recipeId":"r1","steps":[],"saveForLater":[
              {"ingredientIndex":2,"ingredientKey":"ing-mushrooms","name":"Mushrooms",
               "text":"Use 4 oz. Save 4 oz for Beef Stir Fry on Thursday.","short":"Save 4 oz for Beef Stir Fry"},
              {"name":"Coconut Milk","text":"Use 6.8 fl oz. Save 6.8 fl oz for Curry.","short":"Save 6.8 fl oz for Curry"}]}
            """
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: Data(json.utf8))
        #expect(instructions.saveForLater.count == 2)
        let mushrooms = try #require(instructions.saveForLater(ingredientKey: "ing-mushrooms"))
        #expect(mushrooms.short == "Save 4 oz for Beef Stir Fry")
        #expect(mushrooms.text == "Use 4 oz. Save 4 oz for Beef Stir Fry on Thursday.")
        // A note the recipe doesn't list by name shows only on the card.
        #expect(instructions.saveForLater.last?.ingredientKey == nil)
        #expect(instructions.saveForLater(ingredientKey: nil) == nil)
        #expect(instructions.saveForLater(ingredientKey: "") == nil)
        #expect(Set(instructions.saveForLater.map(\.id)).count == 2)
    }

    @Test func anOlderServerHasNone() throws {
        let instructions = try JSONDecoder().decode(
            RecipeInstructions.self, from: Data(#"{"recipeId":"r1","steps":[]}"#.utf8))
        #expect(instructions.saveForLater.isEmpty)
    }
}
