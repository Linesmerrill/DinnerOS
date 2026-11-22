import Foundation
import Testing

@testable import DinnerOS

/// The Shop tab's meal-by-meal arrangement: everything comes from the API's meals and shares,
/// and a shared ingredient is shown under every meal but bought in one place.
struct ShopMealLayoutTests {
    /// Adds fields to one JSON object from the shopping fixtures.
    private func with(_ json: String, _ fields: String) -> String {
        String(json.dropLast()) + "," + fields + "}"
    }

    private func share(_ recipeID: String, _ name: String, _ quantity: String, component: String? = nil) -> String {
        let component = component.map {
            #"{"specialtyId":"crema","specialtyKey":"smoky red pepper crema","specialtyName":"\#($0)","ingredientKey":"i-crema"}"#
        }
        return #"""
            {"recipeId":"\#(recipeID)","recipeName":"\#(name)","amounts":[],"quantityText":"\#(quantity)",
             "unquantified":false,"combined":false,"extra":false,"component":\#(component ?? "null")}
            """#
    }

    private let meals = #"""
        "meals":[{"recipeId":"r-tacos","recipeName":"Beef Tacos","imageUrl":null,"isAddon":false,"day":"mon"},
                 {"recipeId":"r-chili","recipeName":"Chili","imageUrl":null,"isAddon":false,"day":"thu"}]
        """#

    private func proposal(lines: [String], excluded: [String] = []) throws -> ShoppingProposal {
        let json =
            "{" + ShoppingFixtures.proposalFields(lines: lines, excluded: excluded, links: []) + "," + meals + "}"
        return try JSONDecoder().decode(ShoppingProposal.self, from: Data(json.utf8))
    }

    private func beefLine(shares: [String]) -> String {
        with(
            ShoppingFixtures.lineJSON(
                id: "l1", key: "i-beef", name: "Ground Beef", category: "meat-seafood", quantityText: "24 oz",
                productID: "100000001", displayName: "Beef 16 oz", size: nil, computed: 2),
            #""recipes":[{"id":"r-chili","name":"Chili"},{"id":"r-tacos","name":"Beef Tacos"}],"shares":[\#(shares.joined(separator: ","))]"#
        )
    }

    @Test func aSharedLineIsBoughtUnderTheFirstMealAndPointedToFromTheOthers() throws {
        // The API lists shares by recipe name (Chili first); the plan puts Tacos first.
        let layout = ShopMealLayout(
            proposal: try proposal(lines: [
                beefLine(shares: [share("r-chili", "Chili", "12 oz"), share("r-tacos", "Beef Tacos", "12 oz")])
            ]))

        #expect(layout.groups.map(\.title) == ["Beef Tacos", "Chili"])
        let tacos = try #require(layout.groups.first?.rows.first)
        let chili = try #require(layout.groups.last?.rows.first)
        #expect(tacos.isPrimary)
        #expect(tacos.alsoIn == ["Chili"])
        #expect(tacos.quantityText == "12 oz")
        #expect(!chili.isPrimary)
        #expect(chili.boughtWith == "Beef Tacos")
        #expect(chili.recipeID == "r-chili")
        // One purchase: exactly one row carries the product and the stepper.
        let primaries = layout.groups.flatMap(\.rows).filter(\.isPrimary)
        #expect(primaries.count == 1)
        #expect(ShopMealText.note(for: tacos) == "12 oz for this meal · Also in Chili")
        #expect(ShopMealText.boughtWith(chili, packages: 2) == "Bought with Beef Tacos · 2 packages")
    }

    @Test func aMealsLeftOutShareIsStruckThroughUnderThatMealOnly() throws {
        let leftOut = with(
            ShoppingFixtures.excludedJSON(
                key: "i-beef", name: "Ground Beef", reason: "skipped", text: "Skipped in Chili",
                groceryStatus: "skipped"),
            #""skipScope":"recipe","recipes":[{"id":"r-chili","name":"Chili"}],"shares":[\#(share("r-chili", "Chili", "12 oz"))]"#
        )
        let proposal = try proposal(
            lines: [beefLine(shares: [share("r-tacos", "Beef Tacos", "12 oz")])], excluded: [leftOut])
        let layout = ShopMealLayout(proposal: proposal)

        let tacos = try #require(layout.groups.first { $0.title == "Beef Tacos" }?.rows.first)
        let chili = try #require(layout.groups.first { $0.title == "Chili" }?.rows.first)
        #expect(tacos.isPrimary && tacos.alsoIn.isEmpty)
        #expect(chili.isLeftOut)
        #expect(chili.quantityText == "12 oz")
        // Not also listed as "not included": it's shown where it was left out.
        #expect(proposal.notIncluded.isEmpty)
        #expect(proposal.leftOut.count == 1)
        #expect(proposal.leftOut.first?.skipScope == .recipe)
    }

    @Test func aComponentsIngredientsAreClumpedUnderItsName() throws {
        let sourCream = with(
            ShoppingFixtures.lineJSON(
                id: "l1", key: "i-sour-cream", name: "Sour Cream", category: "dairy-eggs", quantityText: "7 tsp",
                productID: "100000002", displayName: "Sour Cream 16 oz", size: nil, computed: 1),
            #""shares":[\#(share("r-tacos", "Beef Tacos", "1 tbsp")),\#(share("r-tacos", "Beef Tacos", "4 tsp", component: "Smoky Red Pepper Crema"))]"#
        )
        let peppers = with(
            ShoppingFixtures.excludedJSON(
                key: "i-peppers", name: "Roasted Red Peppers", reason: "no_product", text: "Choose a Walmart product"),
            #""shares":[\#(share("r-tacos", "Beef Tacos", "2 tsp", component: "Smoky Red Pepper Crema"))]"#
        )
        let layout = ShopMealLayout(proposal: try proposal(lines: [sourCream], excluded: [peppers]))

        let tacos = try #require(layout.groups.first)
        // The meal's own sour cream stays a plain row; the crema's parts are clumped.
        #expect(tacos.rows.map(\.name) == ["Sour Cream"])
        let crema = try #require(tacos.components.first)
        #expect(crema.name == "Smoky Red Pepper Crema")
        #expect(crema.rows.map(\.name) == ["Sour Cream", "Roasted Red Peppers"])
        #expect(!crema.isNotMade(skips: [], recipeID: "r-tacos"))
        #expect(!crema.hasNothingToBuy)
        // One sour cream purchase: the plain row holds it, the crema's row points to it.
        #expect(tacos.rows.first?.isPrimary == true)
        #expect(crema.rows.first?.isPrimary == false)
    }

    private func skip(_ key: String, _ name: String, scope: GrocerySkipScope, recipeID: String? = nil) -> GrocerySkip {
        GrocerySkip(
            id: "skip-\(key)-\(recipeID ?? "")", ingredientKey: key, key: name.lowercased(), name: name, scope: scope,
            week: scope == .week ? "2026-W48" : nil, text: "Skipped", recipeID: recipeID)
    }

    /// "Don’t Make This" is a skip of the component itself: only that reads as not made.
    @Test func aComponentIsNotMadeOnlyWhenItsOwnSkipSaysSo() throws {
        let leftOut = with(
            ShoppingFixtures.excludedJSON(
                key: "i-peppers", name: "Roasted Red Peppers", reason: "skipped", text: "Skipped in Beef Tacos",
                groceryStatus: "skipped"),
            #""skipScope":"recipe","shares":[\#(share("r-tacos", "Beef Tacos", "2 tsp", component: "Smoky Red Pepper Crema"))]"#
        )
        let layout = ShopMealLayout(proposal: try proposal(lines: [], excluded: [leftOut]))
        let crema = try #require(layout.groups.first?.components.first)
        let forTacos = skip("i-crema", "Smoky Red Pepper Crema", scope: .recipe, recipeID: "r-tacos")
        #expect(crema.isNotMade(skips: [forTacos], recipeID: "r-tacos"))
        #expect(crema.notMadeSkips(in: [forTacos], recipeID: "r-tacos") == [forTacos])
        let never = skip("i-crema", "Smoky Red Pepper Crema", scope: .always)
        #expect(crema.isNotMade(skips: [never], recipeID: "r-tacos"))
        // Another dish's skip doesn't count.
        let forChili = skip("i-crema", "Smoky Red Pepper Crema", scope: .recipe, recipeID: "r-chili")
        #expect(!crema.isNotMade(skips: [forChili], recipeID: "r-tacos"))
    }

    /// Merrill's Cream Sauce Base (Nov 22): Milk skipped because they have it, and every part
    /// is skipped, but the sauce is still made. There's just nothing to buy.
    @Test func skippingEveryPartNeverMeansNotMakingIt() throws {
        let milk = with(
            ShoppingFixtures.excludedJSON(
                key: "i-milk", name: "Milk", reason: "skipped", text: "You have it this week", groceryStatus: "skipped"),
            #""skipScope":"week","shares":[\#(share("r-tacos", "Beef Tacos", "1 cup", component: "Cream Sauce Base"))]"#
        )
        let layout = ShopMealLayout(proposal: try proposal(lines: [], excluded: [milk]))
        let sauce = try #require(layout.groups.first?.components.first)
        #expect(sauce.rows.allSatisfy { $0.isLeftOut })
        #expect(sauce.hasNothingToBuy)
        let milkSkip = skip("i-milk", "Milk", scope: .week)
        #expect(!sauce.isNotMade(skips: [milkSkip], recipeID: "r-tacos"))
        // Milk left out of the dish on purpose is still about Milk, not the sauce.
        let milkForTacos = skip("i-milk", "Milk", scope: .recipe, recipeID: "r-tacos")
        #expect(!sauce.isNotMade(skips: [milkSkip, milkForTacos], recipeID: "r-tacos"))
        #expect(sauce.notMadeSkips(in: [milkSkip, milkForTacos], recipeID: "r-tacos").isEmpty)
    }

    @Test func onePartOfAComponentCanBeSkippedOnItsOwn() throws {
        let sourCream = with(
            ShoppingFixtures.lineJSON(
                id: "l1", key: "i-sour-cream", name: "Sour Cream", category: "dairy-eggs", quantityText: "4 tsp",
                productID: "100000002", displayName: "Sour Cream 16 oz", size: nil, computed: 1),
            #""shares":[\#(share("r-tacos", "Beef Tacos", "4 tsp", component: "Smoky Red Pepper Crema"))]"#
        )
        let peppers = with(
            ShoppingFixtures.excludedJSON(
                key: "i-peppers", name: "Roasted Red Peppers", reason: "skipped", text: "You have it this week",
                groceryStatus: "skipped"),
            #""skipScope":"week","shares":[\#(share("r-tacos", "Beef Tacos", "2 tsp", component: "Smoky Red Pepper Crema"))]"#
        )
        let layout = ShopMealLayout(proposal: try proposal(lines: [sourCream], excluded: [peppers]))

        let crema = try #require(layout.groups.first?.components.first)
        // Still made: only the peppers are struck through, and each part knows its meal.
        #expect(!crema.isNotMade(skips: [skip("i-peppers", "Roasted Red Peppers", scope: .week)], recipeID: "r-tacos"))
        #expect(!crema.hasNothingToBuy)
        #expect(crema.rows.map(\.isLeftOut) == [false, true])
        #expect(crema.rows.allSatisfy { $0.recipeID == "r-tacos" })
    }

    @Test func linesWithoutSharesAndPairingsHaveTheirOwnGroups() throws {
        let old = ShoppingFixtures.lineJSON(
            id: "l1", key: "i-rice", name: "Rice", productID: "100000003", displayName: "Rice", size: nil, computed: 1)
        let pairing = with(
            ShoppingFixtures.lineJSON(
                id: "l2", key: "i-crackers", name: "Club Crackers", productID: "100000004", displayName: "Crackers",
                size: nil, computed: 1),
            #""shares":[{"recipeId":"r-chili","recipeName":"Chili","amounts":[],"quantityText":"","unquantified":true,"combined":false,"extra":true,"component":null}]"#
        )
        let layout = ShopMealLayout(proposal: try proposal(lines: [old, pairing]))
        #expect(layout.groups.map(\.title) == ["Add-Ons", "Other Items"])
        // A pairing is removed from the week, not left out of a dish.
        #expect(layout.groups.first?.rows.first?.recipeID == nil)
    }

    @Test func aProposalFromAnOlderServerStillDecodes() throws {
        let json =
            "{"
            + ShoppingFixtures.proposalFields(
                lines: [
                    ShoppingFixtures.lineJSON(
                        id: "l1", key: "i-rice", name: "Rice", productID: "1", displayName: "Rice", size: nil,
                        computed: 1)
                ], excluded: [], links: []) + "}"
        let proposal = try JSONDecoder().decode(ShoppingProposal.self, from: Data(json.utf8))
        #expect(proposal.meals.isEmpty)
        #expect(proposal.lines.first?.shares.isEmpty == true)
        #expect(ShopMealLayout(proposal: proposal).groups.map(\.title) == ["Other Items"])
    }

    @Test func putBackFindsTheSkipsHoldingALineBack() throws {
        let line = try JSONDecoder().decode(
            ShoppingExcludedLine.self,
            from: Data(
                with(
                    ShoppingFixtures.excludedJSON(
                        key: "i-peppers", name: "Roasted Red Peppers", reason: "skipped", text: "Skipped",
                        groceryStatus: "skipped"),
                    #""skipScope":"recipe","shares":[\#(share("r-tacos", "Beef Tacos", "2 tsp", component: "Smoky Red Pepper Crema"))]"#
                ).utf8))
        let skips = [
            GrocerySkip(
                id: "s-crema", ingredientKey: "i-crema", key: "smoky red pepper crema",
                name: "Smoky Red Pepper Crema", scope: .recipe, week: nil, text: "", recipeID: "r-tacos"),
            GrocerySkip(
                id: "s-other", ingredientKey: "i-crema", key: "smoky red pepper crema",
                name: "Smoky Red Pepper Crema", scope: .recipe, week: nil, text: "", recipeID: "r-chili"),
        ]
        #expect(line.holdingSkips(in: skips).map(\.id) == ["s-crema"])
    }
}

/// "Always Have It" adds a pantry staple: in stock, by name, so the line leaves the list without
/// leaving the recipe.
struct ShopAlwaysHaveTests {
    @Test func aLineBecomesAStapleByName() {
        let item = ShopRemoveOptions.staple(name: "Dried Thyme")
        #expect(item.ingredientID == nil)
        #expect(item.name == "Dried Thyme")
        #expect(item.isStaple == true)
        #expect(item.status == .inStock)
    }
}
