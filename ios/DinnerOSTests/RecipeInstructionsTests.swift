import Foundation
import Testing

@testable import DinnerOS

/// The server renders the steps; the app only has to read them faithfully. These cover the
/// contract the cooking screen depends on: segments join to the step, spicy ingredients are
/// flagged, substitutions are named, and an older or newer server still decodes.
private nonisolated enum InstructionFixtures {
    /// A step that names a spicy ingredient, a substituted specialty ingredient, and the same
    /// ingredient twice (the amount is on the first mention only).
    static let instructions = Data(
        #"""
        {"recipeId":"r1","recipeName":"Gochujang Bowl","servings":4,"servingOptions":[2,4],
         "specialtiesApplied":true,
         "steps":[
           {"index":1,
            "text":"Whisk 2 tbsp gochujang into 2 tbsp Tomato Paste, then taste the gochujang.",
            "originalText":"Whisk the gochujang into the Tex-Mex Paste, then taste the gochujang.",
            "imageUrl":"https://img.example.com/step-1.jpg",
            "segments":[
              {"kind":"text","text":"Whisk "},
              {"kind":"ingredient","text":"2 tbsp gochujang","ingredientId":"ing-g","name":"Gochujang",
               "amount":{"quantity":"2","quantityValue":2,"unit":"tbsp","text":"2 tbsp"},"spicy":true},
              {"kind":"text","text":" into "},
              {"kind":"ingredient","text":"2 tbsp Tomato Paste","name":"Tomato Paste","substituted":true,
               "amount":{"quantity":"2","quantityValue":2,"unit":"tbsp","text":"2 tbsp"},
               "specialtyId":"tex-mex-paste","specialtyName":"Tex-Mex Paste"},
              {"kind":"text","text":", then taste the "},
              {"kind":"ingredient","text":"gochujang","ingredientId":"ing-g","name":"Gochujang","spicy":true},
              {"kind":"text","text":"."}],
            "notes":[{"kind":"substitution","specialtyId":"tex-mex-paste",
                      "text":"Instead of 2 tbsp Tex-Mex Paste, use 4 tsp Tomato Paste."}]}],
         "substitutions":[
           {"specialtyId":"tex-mex-paste","specialtyKey":"tex mex paste","specialtyName":"Tex-Mex Paste",
            "optionId":"opt","optionName":"Chipotle tomato base","type":"store_alternative","source":"strategy",
            "text":"Tex-Mex Paste → 4 tsp Tomato Paste"}],
         "unchosenSpecialties":[{"id":"ponzu-sauce","key":"ponzu sauce","name":"Ponzu Sauce"}]}
        """#.utf8)

    /// An older server: only the fields the first version sent.
    static let bare = Data(
        #"""
        {"recipeId":"r1","steps":[{"index":1,"text":"Cook everything."}]}
        """#.utf8)

    /// A newer server: a segment kind this build doesn't know.
    static let futureKind = Data(
        #"""
        {"recipeId":"r1","recipeName":"Later","servings":2,"servingOptions":[2],"specialtiesApplied":true,
         "steps":[{"index":1,"text":"Rest 5 minutes.","segments":[
           {"kind":"timer","text":"Rest 5 minutes."}],"notes":[]}],
         "substitutions":[],"unchosenSpecialties":[]}
        """#.utf8)

    static func decode(_ data: Data) throws -> RecipeInstructions {
        try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: data)
    }
}

struct RecipeInstructionsDecodingTests {
    @Test func decodesSegmentsAmountsAndSubstitutions() throws {
        let instructions = try InstructionFixtures.decode(InstructionFixtures.instructions)

        #expect(instructions.recipeID == "r1")
        #expect(instructions.servings == 4)
        #expect(instructions.servingOptions == [2, 4])
        #expect(instructions.specialtiesApplied)

        let step = try #require(instructions.steps.first)
        #expect(step.originalText?.contains("Tex-Mex Paste") == true)
        #expect(step.imageURL?.lastPathComponent == "step-1.jpg")

        let named = step.ingredientSegments
        #expect(named.count == 3)
        #expect(named[0].amount?.text == "2 tbsp")
        #expect(named[0].spicy)
        #expect(named[1].substituted)
        #expect(named[1].specialtyName == "Tex-Mex Paste")
        // The same ingredient twice: the amount belongs to the first mention.
        #expect(named[2].name == "Gochujang")
        #expect(named[2].amount == nil)
        #expect(named[2].spicy)
    }

    /// The whole contract that lets the app render without character offsets.
    @Test func segmentsJoinToTheStepText() throws {
        let step = try #require(try InstructionFixtures.decode(InstructionFixtures.instructions).steps.first)
        #expect(step.segments.map(\.text).joined() == step.text)
    }

    @Test func spokenTextAnnouncesHeatWithoutColour() throws {
        let step = try #require(try InstructionFixtures.decode(InstructionFixtures.instructions).steps.first)
        #expect(step.spokenText.hasPrefix("Whisk spicy 2 tbsp gochujang"))
        #expect(step.spokenText.hasSuffix("taste the spicy gochujang."))
    }

    @Test func notesAndUnchosenSpecialtiesAreReadable() throws {
        let instructions = try InstructionFixtures.decode(InstructionFixtures.instructions)
        let note = try #require(instructions.steps.first?.notes.first)
        #expect(note.kind == "substitution")
        #expect(note.text.contains("4 tsp Tomato Paste"))

        let substitution = try #require(instructions.substitutions.first)
        #expect(substitution.id == "tex-mex-paste")
        #expect(substitution.isDefaultChoice)
        #expect(instructions.unchosenSpecialties.map(\.name) == ["Ponzu Sauce"])
    }

    @Test func readsAServerThatSendsOnlyTheSteps() throws {
        let instructions = try InstructionFixtures.decode(InstructionFixtures.bare)
        #expect(!instructions.specialtiesApplied)
        #expect(instructions.substitutions.isEmpty)
        #expect(instructions.unchosenSpecialties.isEmpty)
        let step = try #require(instructions.steps.first)
        #expect(step.text == "Cook everything.")
        #expect(step.segments.isEmpty)
        // With no segments the step still reads and still speaks.
        #expect(step.spokenText == "Cook everything.")
        #expect(step.ingredientSegments.isEmpty)
    }

    @Test func anUnknownSegmentKindReadsAsPlainText() throws {
        let step = try #require(try InstructionFixtures.decode(InstructionFixtures.futureKind).steps.first)
        #expect(step.segments.map(\.text).joined() == step.text)
        #expect(step.ingredientSegments.isEmpty)
    }
}
