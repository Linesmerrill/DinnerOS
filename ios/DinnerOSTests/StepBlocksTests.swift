import Foundation
import Testing

@testable import DinnerOS

/// A sentence that adds several things in a row reads as a list under its verb.
struct StepBlocksTests {
    private func text(_ s: String) -> InstructionSegment { InstructionSegment(kind: .text, text: s) }
    private func ing(_ s: String) -> InstructionSegment { InstructionSegment(kind: .ingredient, text: s, name: s) }

    private func joined(_ segments: [InstructionSegment]) -> String { segments.map(\.text).joined() }

    @Test func ingredientsAddedTogetherBecomeAList() throws {
        let step = [
            text("In a small pot, combine "), ing("½ cup rice"), text(", "), ing("¾ cup water"),
            text(", and a pinch of "), ing("salt"), text(". Bring to a boil, then cover. Cook until "),
            ing("rice"), text(" is tender, 15-18 minutes."),
        ]
        let blocks = StepBlocks.blocks(step)
        #expect(blocks.count == 2)
        guard case .list(let lead, let items) = blocks[0], case .prose(let rest) = blocks[1] else {
            Issue.record("blocks = \(blocks)")
            return
        }
        #expect(joined(lead) == "In a small pot, combine:")
        #expect(items.map(joined) == ["½ cup rice", "¾ cup water", "a pinch of salt"])
        #expect(joined(rest) == "Bring to a boil, then cover. Cook until rice is tender, 15-18 minutes.")
    }

    @Test func theRestOfTheSentenceJoinsTheLead() throws {
        let step = [
            text("Stir "), ing("⅔ cup coconut milk"), text(", "), ing("1 oz Sweet Thai Chili Sauce"), text(", "),
            ing("1 tsp Chicken Bouillon Base"), text(", juice from "), ing("2 lime wedges"), text(", and "),
            ing("1 tsp sugar"), text(" into pan with "), ing("chicken"), text(" mixture. Bring to a simmer."),
        ]
        let blocks = StepBlocks.blocks(step)
        guard case .list(let lead, let items) = blocks.first else {
            Issue.record("blocks = \(blocks)")
            return
        }
        #expect(joined(lead) == "Stir into pan with chicken mixture:")
        #expect(items.count == 5)
        #expect(joined(items[3]) == "juice from 2 lime wedges")
        #expect(lead.filter(\.isIngredient).map(\.text) == ["chicken"])
    }

    @Test func wordsForTheFirstNameMoveOffTheLead() throws {
        let step = [
            text("Season all over with remaining "), ing("Savory Paprika Blend"), text(", "), ing("salt"),
            text(", and "), ing("pepper"), text("."),
        ]
        guard case .list(let lead, let items) = StepBlocks.blocks(step).first else {
            Issue.record("not a list")
            return
        }
        #expect(joined(lead) == "Season all over with:")
        #expect(items.map(joined) == ["remaining Savory Paprika Blend", "salt", "pepper"])

        let share = [
            text("In a small bowl, combine ¼ of the "), ing("onion"), text(", juice from "), ing("6 lime wedges"),
            text(", and a pinch of "), ing("salt"), text("."),
        ]
        guard case .list(let shareLead, let shareItems) = StepBlocks.blocks(share).first else {
            Issue.record("not a list")
            return
        }
        #expect(joined(shareLead) == "In a small bowl, combine:")
        #expect(shareItems.map(joined).first == "¼ of the onion")
    }

    @Test func twoThingsAddedTogetherAreAListToo() throws {
        let pair = [text("Season with "), ing("salt"), text(" and "), ing("pepper"), text(".")]
        guard case .list(let lead, let items) = StepBlocks.blocks(pair).first else {
            Issue.record("not a list")
            return
        }
        #expect(joined(lead) == "Season with:")
        #expect(items.map(joined) == ["salt", "pepper"])

        let with = [
            text("In a separate small bowl, combine "), ing("¾ cup sour cream"), text(" with "),
            ing("¾ tsp Southwest Spice Blend"), text("."),
        ]
        guard case .list(let withLead, let withItems) = StepBlocks.blocks(with).first else {
            Issue.record("not a list")
            return
        }
        #expect(joined(withLead) == "In a separate small bowl, combine:")
        #expect(withItems.count == 2)

        let aside = [
            text("(You’ll use the remaining "), ing("Spice Blend"), text(" later.) Season with "), ing("salt"),
            text(" and "), ing("pepper"), text("."),
        ]
        let blocks = StepBlocks.blocks(aside)
        guard blocks.count == 2, case .list(let asideLead, _) = blocks[1] else {
            Issue.record("blocks = \(blocks)")
            return
        }
        #expect(joined(asideLead) == "Season with:")
    }

    @Test func aSubjectOrAListWithNoVerbStaysProse() {
        let subject = [
            text("Once "), ing("rice"), text(" and "), ing("beans"), text(" are done, stir together."),
        ]
        #expect(StepBlocks.blocks(subject) == [.prose(subject)])
        let goesOn = [text("Toss "), ing("carrots"), text(" and "), ing("oil"), text(", then roast.")]
        #expect(StepBlocks.blocks(goesOn) == [.prose(goesOn)])
        let one = [text("Add "), ing("chicken"), text(".")]
        #expect(StepBlocks.blocks(one) == [.prose(one)])
        let noLead = [ing("Rice"), text(", "), ing("water"), text(", and "), ing("salt"), text(" go in.")]
        #expect(StepBlocks.blocks(noLead) == [.prose(noLead)])
        // Names joined by more than a few words aren't a list.
        let spread = [text("Toss "), ing("carrots"), text(" in the oven, then top with "), ing("feta"), text(".")]
        #expect(StepBlocks.blocks(spread) == [.prose(spread)])
    }

    @Test func bulletsStartTheirOwnParagraph() {
        let step = [
            text("While "), ing("rice"), text(" cooks, wash and dry all produce. • Core and dice "),
            ing("1 bell pepper"), text(". • Pat "), ing("10 oz chicken"), text(" dry."),
        ]
        let paragraphs = StepBlocks.paragraphs(step).map(joined)
        #expect(
            paragraphs == [
                "While rice cooks, wash and dry all produce.", "Core and dice 1 bell pepper.", "Pat 10 oz chicken dry.",
            ])
    }
}
