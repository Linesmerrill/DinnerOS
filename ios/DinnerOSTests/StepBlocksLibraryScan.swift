import Foundation
import Testing

@testable import DinnerOS

/// Lays out every step of a local library copy and writes each list it made, and each sentence with
/// several ingredients it left as prose, for review. The steps come from the API's library scan
/// (`TestLibraryScan` writes SCAN_LIB.steps.json); the copy is local and never committed.
///
///     TEST_RUNNER_SCAN_STEPS=/tmp/scan.txt.steps.json xcodebuild test … \
///       -only-testing:DinnerOSTests/StepBlocksLibraryScan
struct StepBlocksLibraryScan {
    private struct Recipe: Decodable {
        let name: String
        let steps: [InstructionStep]
    }

    @Test func layOutTheLibrary() throws {
        guard let path = ProcessInfo.processInfo.environment["SCAN_STEPS"] else { return }
        let recipes = try JSONDecoder().decode([Recipe].self, from: Data(contentsOf: URL(fileURLWithPath: path)))
        var out = ""
        for recipe in recipes {
            for step in recipe.steps {
                for paragraph in StepBlocks.paragraphs(step.segments) {
                    for block in StepBlocks.blocks(paragraph) {
                        switch block {
                        case .list(let lead, let items):
                            let names = items.map { $0.map(\.text).joined() }.joined(separator: " | ")
                            out += "LIST\t\(recipe.name)\t\(step.index)\t\(lead.map(\.text).joined())\t\(names)\n"
                        case .prose(let segments):
                            for sentence in StepBlocks.sentences(segments)
                            where sentence.filter(\.isIngredient).count >= 2 {
                                out += "PROSE\t\(recipe.name)\t\(step.index)\t\(sentence.map(\.text).joined())\n"
                            }
                        }
                    }
                }
            }
        }
        try out.write(toFile: path + ".blocks", atomically: true, encoding: .utf8)
    }
}
