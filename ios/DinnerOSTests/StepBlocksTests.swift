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

    @Test func partWordsPrepWordsAndQualifiersStayWithTheirName() throws {
        func list(_ step: [InstructionSegment]) -> (lead: String, items: [String])? {
            guard case .list(let lead, let items) = StepBlocks.blocks(step).first else { return nil }
            return (joined(lead), items.map(joined))
        }
        // Part words finish a name, at the end or mid-list.
        let whites = try #require(
            list([
                text("Add "), ing("ginger"), text(", "), ing("garlic"), text(", and "), ing("scallion"),
                text(" whites; cook 1 minute."),
            ]))
        #expect(whites.lead == "Add:")
        #expect(whites.items == ["ginger", "garlic", "scallion whites"])
        let mid = try #require(
            list([
                text("Add "), ing("scallion"), text(" whites, "), ing("garlic"), text(", and "), ing("salt"), text("."),
            ]))
        #expect(mid.items == ["scallion whites", "garlic", "salt"])
        // Prep words before the first name go with it.
        let prep = try #require(
            list([
                text("Add sliced "), ing("onion"), text(", "), ing("poblano"), text(", and "), ing("salt"), text("."),
            ]))
        #expect(prep.lead == "Add:")
        #expect(prep.items == ["sliced onion", "poblano", "salt"])
        let plenty = try #require(
            list([text("Season with plenty of "), ing("salt"), text(" and "), ing("pepper"), text(".")]))
        #expect(plenty.lead == "Season with:")
        #expect(plenty.items == ["plenty of salt", "pepper"])
        // How much, said after the names, stays on the lead in brackets.
        let taste = try #require(
            list([text("Taste and season with "), ing("salt"), text(" and "), ing("pepper"), text(" if desired.")]))
        #expect(taste.lead == "Taste and season with (if desired):")
        // …unless it finishes "as many …".
        let asMany = try #require(
            list([
                text("Top with "), ing("sesame seeds"), text(" and as many "), ing("chili flakes"),
                text(" as you like."),
            ]))
        #expect(asMany.lead == "Top with:")
        #expect(asMany.items == ["sesame seeds", "as many chili flakes as you like"])
        // A sentence after a ";" starts the lead with a capital.
        let semi = try #require(list([text("season with "), ing("salt"), text(" and "), ing("pepper"), text(".")]))
        #expect(semi.lead == "Season with:")
    }

    @Test func aSentenceEndingAtANameStartsTheNextOne() {
        let step = [
            text("Add "), ing("tomato paste"), text(" to pan with "), ing("beef"), text(". Season with "), ing("salt"),
            text(" and "), ing("pepper"), text("."),
        ]
        #expect(StepBlocks.sentences(step).count == 2)
        let resplit = StepBlocks.sentences(StepBlocks.sentences(step).flatMap { $0 })
        #expect(resplit.count == 2)
        guard case .list(let lead, _) = StepBlocks.blocks(step).last else {
            Issue.record("no list")
            return
        }
        #expect(joined(lead) == "Season with:")
        // "half the" belongs to its name.
        let half = [
            text("In a large bowl, combine "), ing("10 oz beef"), text(", "), ing("panko"), text(", half the "),
            ing("ginger"), text(", and "), ing("salt"), text("."),
        ]
        guard case .list(_, let items) = StepBlocks.blocks(half).first else {
            Issue.record("no list")
            return
        }
        #expect(items.map(joined) == ["10 oz beef", "panko", "half the ginger", "salt"])
    }

    @Test func moreShapesFromTheLibrary() throws {
        func list(_ step: [InstructionSegment]) -> (lead: String, items: [String])? {
            guard case .list(let lead, let items) = StepBlocks.blocks(step).first else { return nil }
            return (joined(lead), items.map(joined))
        }
        // A "while" clause that closes before the list.
        let whileLead = try #require(
            list([
                text("While rice cooks, in a small bowl, combine "), ing("tomato"), text(" and "), ing("onion"),
                text("."),
            ]))
        #expect(whileLead.lead == "While rice cooks, in a small bowl, combine:")
        // A bracket after a name stays with it.
        let bracket = try #require(
            list([
                text("Fill with "), ing("pork"), text(", "), ing("pickled onion"), text(" (draining first), and "),
                ing("crema"), text("."),
            ]))
        #expect(bracket.items == ["pork", "pickled onion (draining first)", "crema"])
        // One more thing the recipe doesn't list ends the list.
        let salt = try #require(
            list([
                text("In a small pot, combine "), ing("rice"), text(", "), ing("¾ cup water"),
                text(", and a big pinch of salt. Bring to a boil."),
            ]))
        #expect(salt.lead == "In a small pot, combine:")
        #expect(salt.items == ["rice", "¾ cup water", "a big pinch of salt"])
        // …but an ending that says something else keeps the sentence as written.
        let goesOn = [
            text("Stir in "), ing("stock concentrate"), text(" and "), ing("warm water"),
            text(", scraping up any browned bits."),
        ]
        #expect(StepBlocks.blocks(goesOn) == [.prose(goesOn)])
        // With most verbs, what comes before "with" is worked on, not added.
        let season = try #require(
            list([text("Season "), ing("pasta"), text(" with "), ing("salt"), text(" and "), ing("pepper"), text(".")]))
        #expect(season.lead == "Season pasta with:")
        #expect(season.items == ["salt", "pepper"])
        let drizzle = [text("Drizzle "), ing("6 tortillas"), text(" with "), ing("1 Tbsp olive oil"), text(".")]
        #expect(StepBlocks.blocks(drizzle) == [.prose(drizzle)])
        // Where they go, after the names, joins the lead; an ending that isn't an amount isn't an item.
        let divide = try #require(
            list([text("Divide mashed "), ing("potatoes"), text(" and "), ing("chicken"), text(" between plates.")]))
        #expect(divide.lead == "Divide between plates:")
        #expect(divide.items == ["mashed potatoes", "chicken"])
        // An unlisted name followed by more words keeps the sentence as written.
        let veggies = [
            text("Divide "), ing("potatoes"), text(", "), ing("chicken"), text(", and veggies between plates."),
        ]
        #expect(StepBlocks.blocks(veggies) == [.prose(veggies)])
        let serve = [
            text("Season "), ing("pasta"), text(", "), ing("salt"), text(", and divide between bowls."),
        ]
        if case .list(_, let items) = StepBlocks.blocks(serve).first {
            #expect(!items.map(joined).contains("divide between bowls"))
        }
        // A lead that ends on unlisted names, or an ending in brackets, keeps the sentence as written.
        let unlisted = [
            text("In a bowl, combine BBQ sauce, mustard, "), ing("ketchup"), text(", and "), ing("warm water"),
            text("."),
        ]
        #expect(StepBlocks.blocks(unlisted) == [.prose(unlisted)])
        let usedNote = [
            text("Add "), ing("tomato"), text(", "), ing("garlic"), text(", and "), ing("chili flakes"),
            text(" to taste (we used ½ tsp)."),
        ]
        #expect(StepBlocks.blocks(usedNote) == [.prose(usedNote)])
        // A shorter run whose lead still lists names isn't a list.
        let turkey = [
            text("Add "), ing("turkey"), text(", "), ing("1 Tbsp spice blend"), text(", "), ing("salt"), text(", and "),
            ing("pepper"), text(" on one side of the pan."),
        ]
        if case .list(let lead, _) = StepBlocks.blocks(turkey).first {
            #expect(!joined(lead).contains("turkey,"))
        }
        let zucchini = [
            text("Toss zucchini with a drizzle of "), ing("oil"), text(", "), ing("salt"), text(", and "),
            ing("pepper"),
            text(" on opposite side of sheet."),
        ]
        #expect(StepBlocks.blocks(zucchini) == [.prose(zucchini)])
        // A verb before a name starts the list after it.
        if case .list(let lead, let items) = StepBlocks.blocks([
            text("To pot with "), ing("potatoes"), text(", add "), ing("sour cream"), text(" and "), ing("butter"),
            text("."),
        ]).first {
            #expect(joined(lead) == "To pot with potatoes, add:")
            #expect(items.map(joined) == ["sour cream", "butter"])
        } else {
            Issue.record("no list")
        }
        // "as you like" stays with "as many"; what follows still gets its own line.
        let blocks = StepBlocks.blocks([
            text("Top with "), ing("scallions"), text(" and as many "), ing("sesame seeds"),
            text(" as you like and serve."),
        ])
        if case .list(_, let items) = blocks.first {
            #expect(items.map(joined).last == "as many sesame seeds as you like")
        } else {
            Issue.record("no list")
        }
        #expect(blocks.last == .prose([text("Serve.")]))
        // A lead that would close a bracket it never opened reads as written.
        let rack = [
            text("(For 4 servings, roast "), ing("chicken"), text(" and "), ing("potatoes"), text(" on two racks.)"),
        ]
        #expect(StepBlocks.blocks(rack) == [.prose(rack)])
        // Two names with different verbs stay prose.
        let twoVerbs = [
            text("Divide "), ing("tortillas"), text(" between plates and fill with "), ing("pork"), text("."),
        ]
        #expect(StepBlocks.blocks(twoVerbs) == [.prose(twoVerbs)])
    }

    @Test func whatComesAfterTheList() throws {
        // The next thing to do gets its own line under the list.
        let serve = [
            text("Garnish with "), ing("almonds"), text(" and "), ing("scallion greens"), text(" and serve."),
        ]
        let blocks = StepBlocks.blocks(serve)
        guard blocks.count == 2, case .list(let lead, let items) = blocks[0], case .prose(let after) = blocks[1] else {
            Issue.record("blocks = \(blocks)")
            return
        }
        #expect(joined(lead) == "Garnish with:")
        #expect(items.map(joined) == ["almonds", "scallion greens"])
        #expect(joined(after) == "Serve.")
        let then = [
            text("Season "), ing("pasta"), text(" with "), ing("salt"), text(" and "), ing("pepper"),
            text(", then divide between bowls."),
        ]
        guard case .prose(let thenAfter) = StepBlocks.blocks(then).last else {
            Issue.record("no next line")
            return
        }
        #expect(joined(thenAfter) == "Divide between bowls.")
        // Any other "and…" ending keeps the sentence as written.
        let juice = [
            text("Combine "), ing("sour cream"), text(" and as much "), ing("lime"),
            text(" zest and juice as you like."),
        ]
        #expect(StepBlocks.blocks(juice) == [.prose(juice)])
        // "Top mashed potatoes with gravy and cheddar": "gravy and" isn't a lead-in to a name.
        let gravy = [text("Top "), ing("mashed potatoes"), text(" with gravy and "), ing("cheddar"), text(".")]
        #expect(StepBlocks.blocks(gravy) == [.prose(gravy)])
        // Part words before "with" still mean "with": "Sprinkle black bean filling with …".
        let sprinkle = try #require(
            {
                if case .list(let lead, let items) = StepBlocks.blocks([
                    text("Sprinkle "), ing("black bean"), text(" filling with "), ing("cheddar"), text(" and "),
                    ing("Monterey Jack"), text("."),
                ]).first {
                    return (joined(lead), items.map(joined))
                }
                return nil
            }() as (String, [String])?)
        #expect(sprinkle.0 == "Sprinkle black bean filling with:")
        #expect(sprinkle.1 == ["cheddar", "Monterey Jack"])
        // …wherever the "with" falls.
        if case .list(let lead, let items) = StepBlocks.blocks([
            text("Garnish mashed "), ing("potatoes"), text(" and "), ing("chicken"), text(" with "),
            ing("scallion greens"),
            text(" and "), ing("sesame seeds"), text("."),
        ]).first {
            #expect(joined(lead) == "Garnish mashed potatoes and chicken with:")
            #expect(items.map(joined) == ["scallion greens", "sesame seeds"])
        } else {
            Issue.record("no list")
        }
        // A name in the lead's own words isn't the start of the list.
        if case .list(let lead, let items) = StepBlocks.blocks([
            text("On opposite side of sheet from "), ing("pork"), text(", toss "), ing("green beans"),
            text(" with a drizzle of "), ing("oil"), text(", "), ing("salt"), text(", and "), ing("pepper"), text("."),
        ]).first {
            #expect(joined(lead) == "On opposite side of sheet from pork, toss green beans with:")
            #expect(items.map(joined) == ["a drizzle of oil", "salt", "pepper"])
        } else {
            Issue.record("no list")
        }
        // With a "with" lead, more words after the names keep it as written.
        let coated = [
            text("Toss "), ing("steak"), text(" with "), ing("cornstarch"), text(", "), ing("salt"), text(", and "),
            ing("pepper"), text(" until well coated."),
        ]
        #expect(StepBlocks.blocks(coated) == [.prose(coated)])
    }

    @Test func aSubjectOrAListWithNoVerbStaysProse() {
        let subject = [
            text("Once "), ing("rice"), text(" and "), ing("beans"), text(" are done, stir together."),
        ]
        #expect(StepBlocks.blocks(subject) == [.prose(subject)])
        // …though the next thing to do after the names gets its own line.
        let goesOn = [text("Toss "), ing("carrots"), text(" and "), ing("oil"), text(", then roast.")]
        #expect(StepBlocks.blocks(goesOn).last == .prose([text("Roast.")]))
        let aside = [text("Toss "), ing("carrots"), text(" and "), ing("oil"), text(", saving some for later.")]
        #expect(StepBlocks.blocks(aside) == [.prose(aside)])
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
