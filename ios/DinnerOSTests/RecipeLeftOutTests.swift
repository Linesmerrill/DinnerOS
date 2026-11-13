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
            "notes":[{"kind":"left_out","text":"You skip the Cilantro."}]},
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
        #expect(first.spokenText == "Simmer 1 cup coconut milk and top with cilantro (skipped).")
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
        #expect(states[1].leftOutText == "Skipped in this dish")
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
            coconut.leaveOutRequest(.thisDish, recipeID: "r1")
                == .leaveOut(ingredientKey: "i-coconut", name: "Coconut Milk", recipeID: "r1"))
        #expect(
            coconut.leaveOutRequest(.everyDish, recipeID: "r1")
                == .always(ingredientKey: "i-coconut", name: "Coconut Milk"))
        // Just this once needs the week the meal is planned in.
        #expect(coconut.leaveOutRequest(.thisOnce, recipeID: "r1") == nil)
        let week = try #require(ISOWeek("2026-W46"))
        let once = try #require(coconut.leaveOutRequest(.thisOnce, recipeID: "r1", week: week))
        #expect(once.scope == .recipe && once.recipeID == "r1" && once.week == "2026-W46")
    }

    @Test func aSkipForThisWeekReadsAsThisTime() throws {
        let json = Data(#"{"skipId":"s1","scope":"recipe","once":true}"#.utf8)
        let leftOut = try JSONDecoder().decode(InstructionLeftOut.self, from: json)
        #expect(leftOut.isOnce)
        let state = RecipeIngredientState(
            line: line(0, "Sour Cream"), ingredientKey: "k", leftOut: leftOut, component: nil)
        #expect(state.leftOutText == "Skipped this time")
        let lasting = RecipeIngredientState(
            line: line(0, "Sour Cream"), ingredientKey: "k", leftOut: InstructionLeftOut(skipID: "s1", scope: .recipe),
            component: nil)
        #expect(lasting.leftOutText == "Skipped in this dish")
    }
}
