import Foundation
import Testing

@testable import DinnerOS

/// A recipe's ingredient list and steps with what the household leaves out: struck through,
/// never removed, and put back from where they're shown.
struct RecipeLeftOutTests {
    private let instructionsJSON = Data(
        #"""
        {"recipeId":"r1","recipeName":"Thai Coconut Curry Chicken","servings":2,"servingOptions":[2,4],
         "specialtiesApplied":true,"substitutions":[],"unchosenSpecialties":[],"leftOutApplied":true,
         "steps":[
           {"index":1,"text":"Simmer 1 cup coconut milk and top with cilantro.","segments":[
             {"kind":"text","text":"Simmer "},
             {"kind":"ingredient","text":"1 cup coconut milk","name":"Coconut Milk"},
             {"kind":"text","text":" and top with "},
             {"kind":"ingredient","text":"cilantro","name":"Cilantro","leftOut":true},
             {"kind":"text","text":"."}],
            "notes":[{"kind":"left_out","text":"You leave out the Cilantro."}]},
           {"index":2,"text":"Drizzle with the crema.","leftOut":true,"segments":[
             {"kind":"text","text":"Drizzle with the "},
             {"kind":"ingredient","text":"crema","name":"Smoky Red Pepper Crema","leftOut":true},
             {"kind":"text","text":"."}],"notes":[]}],
         "ingredients":[
           {"index":0,"ingredientKey":"i-coconut","name":"Coconut Milk","leftOut":null,"component":null},
           {"index":1,"ingredientKey":"name:cilantro","name":"Cilantro",
            "leftOut":{"skipId":"s-cilantro","scope":"recipe"},"component":null},
           {"index":2,"ingredientKey":"i-crema","name":"Smoky Red Pepper Crema",
            "leftOut":{"skipId":"s-crema","scope":"always"},
            "component":{"specialtyId":"crema","specialtyName":"Smoky Red Pepper Crema",
                         "optionName":"Sour cream with roasted peppers","type":"store_alternative",
                         "parts":["4 tsp Sour Cream","2 tsp Roasted Red Peppers"]}}]}
        """#.utf8)

    private func line(_ index: Int, _ name: String) -> IngredientLine {
        IngredientLine(id: "\(index)-x", name: name, amount: "1", isPantryStaple: false, category: "produce")
    }

    @Test func instructionsDecodeWhatIsLeftOut() throws {
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: instructionsJSON)

        #expect(instructions.leftOutApplied)
        let first = try #require(instructions.steps.first)
        #expect(first.segments.filter(\.leftOut).map(\.text) == ["cilantro"])
        #expect(first.notes.first?.isLeftOut == true)
        #expect(!first.leftOut)
        #expect(instructions.steps.last?.leftOut == true)
        // VoiceOver hears it, rather than relying on a strikethrough.
        #expect(first.spokenText == "Simmer 1 cup coconut milk and top with cilantro (left out).")
        #expect(instructions.ingredient(at: 1)?.leftOut == InstructionLeftOut(skipID: "s-cilantro", scope: .recipe))
        #expect(instructions.ingredient(at: 2)?.component?.parts.count == 2)
    }

    @Test func olderInstructionsLeaveNothingOut() throws {
        let json = Data(#"{"recipeId":"r1","steps":[{"index":1,"text":"Cook.","segments":[],"notes":[]}]}"#.utf8)
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: json)
        #expect(!instructions.leftOutApplied)
        #expect(instructions.ingredients.isEmpty)
        let states = RecipeIngredientStates.make(lines: [line(0, "Rice")], instructions: instructions)
        #expect(states.first?.canLeaveOut == false)
    }

    @Test func statesJoinTheIngredientListByPositionAndName() throws {
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: instructionsJSON)
        let states = RecipeIngredientStates.make(
            lines: [line(0, "Coconut Milk"), line(1, "Cilantro"), line(2, "Smoky Red Pepper Crema")],
            instructions: instructions)

        #expect(states.map(\.isLeftOut) == [false, true, true])
        #expect(states[1].leftOutText == "Left out of this dish")
        #expect(states[2].isComponent)
        #expect(states[2].leftOutText == "Never making this")
        // A line whose name doesn't match (instructions for an older copy) isn't touched.
        let stale = RecipeIngredientStates.make(lines: [line(0, "Rice")], instructions: instructions)
        #expect(stale.first?.isLeftOut == false)
        #expect(stale.first?.canLeaveOut == false)
    }

    @Test func leavingOutJustThisDishOrAlwaysBuildsTheRightSkip() throws {
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: instructionsJSON)
        let coconut = try #require(
            RecipeIngredientStates.make(lines: [line(0, "Coconut Milk")], instructions: instructions).first)

        #expect(
            coconut.leaveOutRequest(scope: .recipe, recipeID: "r1")
                == .leaveOut(ingredientKey: "i-coconut", name: "Coconut Milk", recipeID: "r1"))
        #expect(
            coconut.leaveOutRequest(scope: .always, recipeID: "r1")
                == .always(ingredientKey: "i-coconut", name: "Coconut Milk"))
        #expect(coconut.leaveOutRequest(scope: .week, recipeID: "r1") == nil)
    }
}
