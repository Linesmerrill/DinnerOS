import Foundation
import Testing

@testable import DinnerOS

/// "Same as Another Item…" on the Shop tab: what can be picked, what a merged line says, and
/// what the app sends.
struct SameItemsTests {
    private func with(_ json: String, _ fields: String) -> String {
        String(json.dropLast()) + "," + fields + "}"
    }

    @Test func aMergedLineSaysWhatElseItsCalled() throws {
        let line = with(
            ShoppingFixtures.lineJSON(
                id: "l1", key: "i-ccp", name: "Chipotle Chili Powder", category: "spices",
                productID: "100000001", displayName: "Chipotle Chili Powder 2 oz", size: nil, computed: 1),
            #""alsoCalled":[{"linkId":"link-1","name":"Chipotle Powder"}]"#)
        let excluded = with(
            ShoppingFixtures.excludedJSON(
                key: "i-gp", name: "Garlic Powder", reason: "no_product", text: "Choose a Walmart product"),
            #""alsoCalled":[{"linkId":"link-2","name":"Fine Garlic Powder"},{"linkId":"link-3","name":"Granulated Garlic"}]"#
        )
        let json = "{" + ShoppingFixtures.proposalFields(lines: [line], excluded: [excluded], links: []) + "}"
        let proposal = try JSONDecoder().decode(ShoppingProposal.self, from: Data(json.utf8))

        let chipotle = try #require(proposal.lines.first)
        #expect(chipotle.alsoCalled == [SameItemName(linkID: "link-1", name: "Chipotle Powder")])
        #expect(ShopMealText.alsoCalled(chipotle.alsoCalled) == "Also called Chipotle Powder")
        let garlic = try #require(proposal.excluded.first)
        #expect(ShopMealText.alsoCalled(garlic.alsoCalled) == "Also called Fine Garlic Powder and Granulated Garlic")
        #expect(ShopMealText.alsoCalled([]) == nil)

        // Every row under a meal carries them, so its menu can undo each one.
        let row = try #require(
            ShopMealLayout(proposal: proposal).groups.flatMap(\.rows).first { $0.name == "Garlic Powder" })
        #expect(row.alsoCalled.map(\.linkID) == ["link-2", "link-3"])
    }

    @Test func theGroceryListSaysWhatElseAnItemIsCalled() throws {
        let json = #"""
            {"ingredientKey":"i-ccp","name":"Chipotle Chili Powder","amounts":[],"quantityText":"3 tsp",
             "unquantified":false,"status":"toBuy","recipes":[],
             "alsoCalled":[{"linkId":"link-1","name":"Chipotle Powder"}]}
            """#
        let item = try JSONDecoder().decode(GroceryItem.self, from: Data(json.utf8))
        #expect(item.alsoCalled == [SameItemName(linkID: "link-1", name: "Chipotle Powder")])
        #expect(item.alsoCalledText == "Also called Chipotle Powder")

        // An older server sends none.
        let old = #"""
            {"ingredientKey":"i-ccp","name":"Chipotle Chili Powder","amounts":[],"quantityText":"",
             "unquantified":false,"status":"toBuy","recipes":[]}
            """#
        let plain = try JSONDecoder().decode(GroceryItem.self, from: Data(old.utf8))
        #expect(plain.alsoCalled.isEmpty && plain.alsoCalledText == nil)
    }

    @Test func aSkippedMergedRowStillKnowsItsOtherNames() throws {
        let skipped = with(
            ShoppingFixtures.excludedJSON(
                key: "i-ccp", name: "Chipotle Chili Powder", reason: "skipped", text: "You have it this week",
                groceryStatus: "skipped"),
            #""skipScope":"week","alsoCalled":[{"linkId":"link-1","name":"Chipotle Powder"}]"#)
        let json = "{" + ShoppingFixtures.proposalFields(lines: [], excluded: [skipped], links: []) + "}"
        let proposal = try JSONDecoder().decode(ShoppingProposal.self, from: Data(json.utf8))
        let row = try #require(ShopMealLayout(proposal: proposal).groups.flatMap(\.rows).first)
        // Its menu offers "Chipotle Powder Isn't the Same" from this.
        #expect(row.isLeftOut)
        #expect(row.alsoCalled.map(\.name) == ["Chipotle Powder"])
    }

    @Test func anOlderServerHasNoOtherNames() throws {
        let line = ShoppingFixtures.lineJSON(
            id: "l1", key: "i-ccp", name: "Chipotle Chili Powder", productID: "1", displayName: "Jar", size: nil,
            computed: 1)
        let json = "{" + ShoppingFixtures.proposalFields(lines: [line], excluded: [], links: []) + "}"
        let proposal = try JSONDecoder().decode(ShoppingProposal.self, from: Data(json.utf8))
        #expect(proposal.lines.first?.alsoCalled.isEmpty == true)
    }

    @Test func choicesAreTheListThenThePantryEachNameOnce() {
        let choices = SameItemChoice.choices(
            for: "name:chipotle powder", name: "Chipotle Powder", alsoCalled: ["Smoked Chili Powder"],
            listItems: [
                (key: "i-onion", name: "Onion"),
                (key: "name:chipotle powder", name: "Chipotle Powder"),  // itself
                (key: "i-ccp", name: "Chipotle Chili Powder"),
                (key: "", name: ""),  // a line no meal names
                (key: "name:smoked chili powder", name: "Smoked Chili Powder"),  // already the same
            ],
            pantryItems: [
                (key: "i-ccp", name: "Chipotle Chili Powder"),  // on the list too
                (key: "name:chipotle chili powder", name: "chipotle chili powder"),  // same name, other key
                (key: "i-cumin", name: "Cumin"),
            ])
        #expect(choices.map(\.name) == ["Chipotle Chili Powder", "Onion", "Cumin"])
        #expect(choices.map(\.detail) == ["On this week's list", "On this week's list", "In your pantry"])
        #expect(SameItemChoice.filter(choices, by: " chipotle ").map(\.key) == ["i-ccp"])
        #expect(SameItemChoice.filter(choices, by: "").count == 3)
    }

    @Test func linkingSendsTheItemAndWhatItsTheSameAs() throws {
        let body =
            try JSONSerialization.jsonObject(
                with: JSONEncoder().encode(
                    SameItemRequest(
                        variantKey: "name:chipotle powder", variantName: "Chipotle Powder", keptKey: "i-ccp",
                        keptName: "Chipotle Chili Powder"))) as? [String: String]
        #expect(
            body == [
                "variantKey": "name:chipotle powder", "variantName": "Chipotle Powder", "keptKey": "i-ccp",
                "keptName": "Chipotle Chili Powder",
            ])
        #expect(SameItemsAPI.path("h1") == "/api/v1/households/h1/same-items")
    }
}
