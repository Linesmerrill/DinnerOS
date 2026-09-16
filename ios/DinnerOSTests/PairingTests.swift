import Foundation
import Testing

@testable import DinnerOS

/// Decoding for every add-on pairing shape, including the ones a newer server could send.
struct PairingTests {
    private func decode<Value: Decodable>(_ type: Value.Type, _ json: String) throws -> Value {
        try JSONCoding.makeDecoder().decode(Value.self, from: Data(json.utf8))
    }

    // MARK: Pairings

    @Test func decodesALearnedRecipePairing() throws {
        let pairing = try decode(Pairing.self, PairingFixtures.pairing(inPlan: true))

        #expect(pairing.key == "recipe:addon-1")
        #expect(pairing.id == pairing.key)
        #expect(pairing.name == "Sample Garlic Bread")
        #expect(pairing.servings == 2)
        #expect(pairing.source == .learned)
        #expect(pairing.frequency == .suggest)
        #expect(pairing.mealCategory == .pasta)
        #expect(pairing.confidence == 0.95)
        #expect(pairing.ruleID == nil)
        #expect(pairing.inPlan)
        #expect(pairing.canMakeRule)
        #expect(pairing.offersRule)
        let learned = try #require(pairing.learned)
        #expect(learned.weeksTogether == 186)
        #expect(learned.mealCategoryWeeks == 195)
        #expect(learned.otherWeeksRate == 0.49)
        let recipe = try #require(pairing.recipe)
        #expect(recipe.cookMinutes == 15)
        #expect(recipe.isAddon)
        #expect(recipe.summary.id == "addon-1")
        #expect(pairing.groceryItem == nil)
    }

    @Test func decodesARuleGroceryPairing() throws {
        let pairing = try decode(Pairing.self, PairingFixtures.crackersPairing())

        #expect(pairing.key == "grocery:club crackers")
        #expect(pairing.source == .rule)
        #expect(pairing.mealCategory == .soup)
        #expect(pairing.ruleID == "rule-2")
        #expect(pairing.servings == nil)
        #expect(pairing.confidence == nil)
        #expect(pairing.learned == nil)
        #expect(!pairing.canMakeRule)
        #expect(!pairing.offersRule)
        // The quantity is a number here, unlike the exact-fraction strings elsewhere.
        #expect(pairing.groceryItem == PairingGroceryItem(name: "Club Crackers", quantity: 1, unit: "package"))
        #expect(pairing.groceryItem?.amountText == "1 package")
    }

    @Test func anUnknownSourceAndAMissingLearnedStillDecode() throws {
        let pairing = try decode(
            Pairing.self,
            #"""
            {"key":"recipe:addon-9","target":\#(PairingFixtures.recipeTarget(id: "addon-9", name: "Future Bread")),
             "servings":null,"source":"inferred","frequency":"whenever","mealCategory":"ramen",
             "confidence":null,"reason":"Because","inPlan":false,"canMakeRule":false}
            """#)

        // Unknown values are kept as-is rather than failing the response.
        #expect(pairing.source == PairingSource(rawValue: "inferred"))
        #expect(pairing.frequency == PairingFrequency(rawValue: "whenever"))
        #expect(pairing.mealCategory == MealCategory(rawValue: "ramen"))
        #expect(pairing.mealCategory?.fallbackTitle == "Ramen")
        #expect(pairing.learned == nil)
        #expect(pairing.ruleID == nil)
        #expect(pairing.servings == nil)
        #expect(pairing.reason == "Because")
    }

    @Test func aQuantitySentAsAStringStillDecodes() throws {
        let pairing = try decode(
            Pairing.self,
            PairingFixtures.pairing(
                key: "grocery:crackers", name: "Crackers", isRecipe: false, quantityOverride: #""2.5""#))

        #expect(pairing.groceryItem?.quantity == 2.5)
    }

    @Test func aTargetOfAnUnknownKindIsLeftOut() throws {
        let response = try decode(
            RecipePairings.self,
            #"""
            {"recipeId":"recipe-1","week":null,"entryId":null,"mealCategories":[],
             "items":[\#(PairingFixtures.pairing()),
                      {"key":"video:1","target":{"kind":"video","url":"https://example.test"},"inPlan":false}]}
            """#)

        // There is nothing useful to show for an add this build can't describe.
        #expect(response.items.map(\.key) == ["recipe:addon-1"])
        #expect(response.week == nil)
        #expect(response.entryID == nil)
    }

    // MARK: Week pairings

    @Test func decodesTheWeeksPairingsAndItsGroceryItems() throws {
        let week = try decode(
            WeekPairings.self,
            PairingFixtures.weekPairings(
                meals: [
                    PairingFixtures.meal(pairings: [PairingFixtures.pairing()]),
                    PairingFixtures.meal(
                        entryID: "entry-2", recipeID: "recipe-2", name: "Sample Soup", day: "null",
                        mealCategories: #"["soup"]"#, pairings: [PairingFixtures.crackersPairing()]),
                ],
                groceryItems: [PairingFixtures.groceryLine()]))

        #expect(week.week == "2026-W38")
        #expect(week.meals.map(\.entryID) == ["entry-1", "entry-2"])
        #expect(week.meals.first?.day == .tue)
        #expect(week.meals.first?.mealCategories == [.pasta])
        // An unscheduled meal has no day.
        #expect(week.meals.last?.day == nil)
        #expect(week.openSuggestions.count == 2)
        #expect(!week.isEmpty)
        #expect(week.meal(entryID: "entry-2")?.recipe.name == "Sample Soup")

        let line = try #require(week.groceryItems.first)
        #expect(line.id == "item-1")
        #expect(line.key == "grocery:club crackers")
        #expect(line.entryID == "entry-2")
        #expect(line.text == "Club Crackers for Sample Soup")
        #expect(line.groceryItem.quantity == 1)
    }

    @Test func aWeekWithNoPlanDecodesEmpty() throws {
        let week = try decode(WeekPairings.self, PairingFixtures.weekPairings(week: "2026-W50"))

        #expect(week.meals.isEmpty)
        #expect(week.groceryItems.isEmpty)
        #expect(week.isEmpty)
    }

    // MARK: Proposal pairings

    @Test func decodesASlotsPairingsWithTheirIDsAndDefaults() throws {
        let proposal = try decode(
            AutopilotProposal.self,
            AutopilotFixtures.proposal(slots: [
                AutopilotFixtures.Slot(
                    day: "mon", recipeID: "recipe-1", name: "Placeholder Pasta Bake",
                    pairings: [
                        .init(key: "recipe:addon-1", name: "Sample Garlic Bread", included: true),
                        .init(key: "grocery:club crackers", name: "Club Crackers", included: false, isRecipe: false),
                    ])
            ]))

        let slot = try #require(proposal.slots.first)
        #expect(slot.pairings.map(\.id) == ["mon/recipe:addon-1", "mon/grocery:club crackers"])
        #expect(slot.pairings.map(\.included) == [true, false])
        #expect(slot.pairings.first?.name == "Sample Garlic Bread")
        #expect(slot.pairings.first?.pairing.key == "recipe:addon-1")
    }

    @Test func slotsWithoutPairingsStillDecode() throws {
        let proposal = try decode(AutopilotProposal.self, AutopilotFixtures.proposal())

        #expect(proposal.slots.allSatisfy { $0.pairings.isEmpty })
    }

    // MARK: Accept and rules

    @Test func decodesAnAcceptResult() throws {
        let result = try decode(
            PairingAcceptResult.self,
            #"""
            {"status":"alreadyAdded","pairing":\#(PairingFixtures.pairing(inPlan: true)),
             "entry":null,"groceryItem":\#(PairingFixtures.groceryLine()),
             "plan":\#(PlanFixtures.plan())}
            """#)

        #expect(result.status == .alreadyAdded)
        #expect(result.entry == nil)
        #expect(result.groceryItem?.id == "item-1")
        #expect(result.plan.week == "2026-W38")
    }

    @Test func decodesAnAcceptedProposalsPairings() throws {
        let result = try decode(
            AutopilotAcceptResult.self,
            #"""
            {"proposal":\#(AutopilotFixtures.proposal(status: "accepted")),"plan":\#(PlanFixtures.plan()),
             "added":[],"skipped":[],
             "pairingsAdded":[{"id":"mon/recipe:addon-1","slotId":"mon","pairing":\#(PairingFixtures.pairing()),
                               "entry":null,"groceryItem":null}],
             "pairingsSkipped":[{"id":"wed/grocery:club crackers","slotId":"wed","key":"grocery:club crackers",
                                 "reason":"alreadyPlanned"}]}
            """#)

        #expect(result.pairingsAdded.map(\.name) == ["Sample Garlic Bread"])
        #expect(result.pairingsSkipped.map(\.reason) == ["alreadyPlanned"])
        let summary = try #require(PairingFormat.acceptSummary(result))
        #expect(summary.contains("Sample Garlic Bread"))
        #expect(summary.contains("1 add-on wasn't added."))
    }

    @Test func anAcceptResultWithoutPairingFieldsStillDecodes() throws {
        let result = try decode(
            AutopilotAcceptResult.self,
            #"""
            {"proposal":\#(AutopilotFixtures.proposal(status: "accepted")),"plan":\#(PlanFixtures.plan()),
             "added":[],"skipped":[]}
            """#)

        #expect(result.pairingsAdded.isEmpty)
        #expect(result.pairingsSkipped.isEmpty)
        #expect(PairingFormat.acceptSummary(result) == nil)
    }

    // MARK: Rules in the profile

    @Test func decodesTheProfilesPairingRules() throws {
        let profile = try decode(AutopilotProfile.self, AutopilotFixtures.profile())

        #expect(profile.pairings.map(\.id) == ["rule-1", "rule-2"])
        let pasta = try #require(profile.pairings.first)
        #expect(pasta.when.mealCategories == [.pasta])
        #expect(pasta.add.kind == .recipe)
        #expect(pasta.add.recipeID == "addon-1")
        #expect(pasta.add.recipeName == "Sample Garlic Bread")
        #expect(pasta.frequency == .always)
        let soup = try #require(profile.pairings.last)
        #expect(soup.add.kind == .groceryItem)
        #expect(soup.add.groceryItem == PairingGroceryItem(name: "Club Crackers", quantity: 1, unit: "package"))
        #expect(soup.frequency == .suggest)
        #expect(profile.sections[.pairings]?.updatedBy == Fixtures.user.id)
        #expect(profile.settings.pairings == profile.pairings)
    }

    @Test func aProfileWithoutPairingsDecodesNone() throws {
        var json = String(decoding: Data(AutopilotFixtures.profile().utf8), as: UTF8.self)
        json = json.replacingOccurrences(of: #""pairings":["#, with: #""unusedPairings":["#)

        let profile = try decode(AutopilotProfile.self, json)

        #expect(profile.pairings.isEmpty)
    }

    /// A rule is sent back with its `id` so the server keeps it, and never with the
    /// read-only `kind`/`recipeName`.
    @Test func encodingARuleSendsTheWritableHalf() throws {
        let rule = PairingRule(
            id: "rule-1", label: "Pasta night",
            when: PairingRuleConditions(mealCategories: [.pasta], proteins: ["chicken"]),
            add: .recipe(id: "addon-1", name: "Sample Garlic Bread"), frequency: .always)

        let data = try JSONCoding.makeEncoder().encode(rule)
        let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])

        #expect(object["id"] as? String == "rule-1")
        #expect(object["label"] as? String == "Pasta night")
        #expect(object["frequency"] as? String == "always")
        let when = try #require(object["when"] as? [String: Any])
        #expect(when["mealCategories"] as? [String] == ["pasta"])
        #expect(when["proteins"] as? [String] == ["chicken"])
        let add = try #require(object["add"] as? [String: Any])
        #expect(add["recipeId"] as? String == "addon-1")
        #expect(add["groceryItem"] is NSNull)
        #expect(add["kind"] == nil)
        #expect(add["recipeName"] == nil)
    }

    @Test func encodingAGroceryRuleSendsTheQuantityAsANumber() throws {
        let rule = PairingRule(
            when: PairingRuleConditions(mealCategories: [.soup]),
            add: .groceryItem(PairingGroceryItem(name: "Club Crackers", quantity: 1, unit: "package")))

        let data = try JSONCoding.makeEncoder().encode(rule)
        let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let add = try #require(object["add"] as? [String: Any])
        let item = try #require(add["groceryItem"] as? [String: Any])

        #expect(item["name"] as? String == "Club Crackers")
        // A number, not an exact-fraction string.
        #expect(item["quantity"] as? Double == 1)
        #expect(item["unit"] as? String == "package")
        #expect(add["recipeId"] is NSNull)
        // A new rule has no id, so the server creates it.
        #expect(object["id"] == nil)
    }

    // MARK: The vocabulary

    @Test func readsMealCategoriesAndFrequenciesFromTheVocabulary() throws {
        let vocabulary = try JSONCoding.makeDecoder().decode(
            AutopilotVocabulary.self, from: AutopilotFixtures.vocabulary)

        #expect(vocabulary.mealCategoryOptions.map(\.value).prefix(2) == ["pasta", "soup"])
        #expect(vocabulary.pairingFrequencyOptions.map(\.value) == ["always", "suggest"])
        #expect(vocabulary.limits.maxPairingRules == 20)
        #expect(vocabulary.limits.maxGroceryItemNameLength == 60)
        #expect(vocabulary.limits.maxGroceryItemQuantity == 99)
    }

    /// A server without pairings still fills the pickers, from the closed list.
    @Test func aVocabularyWithoutPairingListsFallsBackToTheKnownOnes() throws {
        var json = String(decoding: AutopilotFixtures.vocabulary, as: UTF8.self)
        json = json.replacingOccurrences(of: #""mealCategories":"#, with: #""unusedCategories":"#)
        json = json.replacingOccurrences(of: #""pairingFrequencies":"#, with: #""unusedFrequencies":"#)

        let vocabulary = try decode(AutopilotVocabulary.self, json)

        #expect(vocabulary.mealCategories.isEmpty)
        #expect(vocabulary.mealCategoryOptions.map(\.value) == MealCategory.known.map(\.rawValue))
        #expect(vocabulary.pairingFrequencyOptions.map(\.value) == ["always", "suggest"])
    }

    // MARK: Grocery extras

    @Test func groceryItemsCarryTheirExtras() throws {
        let list = try JSONCoding.makeDecoder().decode(
            GroceryList.self, from: PairingFixtures.groceryListWithExtra)

        let crackers = try #require(list.allItems.first)
        // The unreadable third extra is skipped; the unknown origin is kept.
        #expect(crackers.extras.map(\.id) == ["item-1", "item-2"])
        #expect(crackers.extras.first?.origin == .pairing)
        #expect(crackers.extras.first?.text == "Club Crackers for Sample Soup")
        #expect(crackers.extras.last?.origin == GroceryExtra.Origin(rawValue: "future_origin"))
        // Only a pairing extra can be taken off the week.
        #expect(crackers.pairingExtras.map(\.id) == ["item-1"])

        let salt = try #require(list.allItems.last)
        #expect(salt.extras.isEmpty)
        #expect(salt.pairingExtras.isEmpty)
    }

    /// The plain-text export reads an extra the same way it reads `via`.
    @Test func theSharedListIncludesExtras() throws {
        let list = try JSONCoding.makeDecoder().decode(
            GroceryList.self, from: PairingFixtures.groceryListWithExtra)
        let item = try #require(list.allItems.first)

        let line = GroceryListText.line(for: item, checked: false)

        #expect(line.contains("- [ ] Club Crackers, 1 package"))
        #expect(line.contains("\n    Club Crackers for Sample Soup"))
    }

    // MARK: Formatting

    @Test func ruleTitlesReadAsPastaToGarlicBread() throws {
        let profile = try decode(AutopilotProfile.self, AutopilotFixtures.profile())
        let vocabulary = try JSONCoding.makeDecoder().decode(
            AutopilotVocabulary.self, from: AutopilotFixtures.vocabulary)

        #expect(
            PairingFormat.ruleTitle(profile.pairings[0], vocabulary: vocabulary) == "Pasta → Sample Garlic Bread")
        #expect(
            PairingFormat.ruleTitle(profile.pairings[1], vocabulary: vocabulary)
                == "Soup, stew & chili → Club Crackers")
        #expect(PairingFormat.ruleSummary(profile.pairings[0], vocabulary: vocabulary).hasPrefix("Always"))
        #expect(PairingFormat.ruleSummary(profile.pairings[1], vocabulary: vocabulary).contains("1 package"))
        #expect(PairingFormat.sectionSummary([], vocabulary: vocabulary) == "No rules")
    }

    /// The server's own `reason` is shown as written, never rebuilt.
    @Test func theServersReasonIsUsedAsIs() throws {
        let pairing = try decode(Pairing.self, PairingFixtures.pairing())

        #expect(
            PairingFormat.reason(pairing, vocabulary: nil)
                == "You usually have Sample Garlic Bread with pasta (95% of pasta weeks)")
        #expect(PairingFormat.addPrompt(pairing) == "Add Sample Garlic Bread?")

        let crackers = try decode(Pairing.self, PairingFixtures.crackersPairing())
        #expect(PairingFormat.reason(crackers, vocabulary: nil) == "Your rule: soup → Club Crackers")
        #expect(PairingFormat.addPrompt(crackers) == "Add Club Crackers to the list?")
    }
}
