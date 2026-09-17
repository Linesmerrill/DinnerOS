import Foundation
import Testing

@testable import DinnerOS

struct ProductLinkTests {
    @Test(arguments: [
        (
            "Check out Test Brand Ground Beef at Walmart! https://www.walmart.com/ip/Test-Brand-Ground-Beef/100000001?classType=REGULAR",
            "https://www.walmart.com/ip/Test-Brand-Ground-Beef/100000001?classType=REGULAR"
        ),
        ("https://www.walmart.com/ip/100000002", "https://www.walmart.com/ip/100000002"),
        ("first https://walmart.com/ip/100000003 then https://example.com/other", "https://walmart.com/ip/100000003"),
        ("Saw this:\nhttps://walmrt.us/3AbCdEf", "https://walmrt.us/3AbCdEf"),
        ("www.walmart.com/ip/100000004", "http://www.walmart.com/ip/100000004"),
    ])
    func theFirstLinkIsPickedOutOfSharedText(text: String, expected: String) {
        #expect(ProductLink.firstURL(in: text) == expected)
    }

    @Test(arguments: ["ground beef", "", "email ada@example.com about it"])
    func textWithoutAWebLinkHasNone(text: String) {
        #expect(ProductLink.firstURL(in: text) == nil)
    }

    @Test func aBareItemNumberIsSentAsTheID() {
        #expect(ProductLink.reference(from: " 100000001 ") == .itemID("100000001"))
        #expect(ProductLink.reference(from: "1234") == nil)
        #expect(
            ProductLink.reference(from: "look https://www.walmart.com/ip/100000001")
                == .url("https://www.walmart.com/ip/100000001"))
    }

    @Test func itemIDsAreReadFromProductLinksOnly() {
        #expect(ProductLink.walmartItemID(inURL: "https://www.walmart.com/ip/100000001") == "100000001")
        #expect(ProductLink.walmartItemID(inURL: "https://walmart.com/ip/Test-Beef/100000001?x=1") == "100000001")
        #expect(ProductLink.walmartItemID(inURL: "https://walmrt.us/3AbCdEf") == nil)
        #expect(ProductLink.walmartItemID(inURL: "https://www.walmart.com/search?q=beef") == nil)
        #expect(ProductLink.walmartItemID(inURL: "https://example.com/ip/100000001") == nil)
    }

    /// The slug names the product, so the member doesn't retype it. Nothing is fetched, so the
    /// name is only as good as the slug: hyphens become spaces and the punctuation is lost.
    @Test func theProductNameIsReadFromTheLinksSlug() {
        #expect(
            ProductLink.walmartProductName(inURL: "https://www.walmart.com/ip/Garlic-Bulb-Fresh-Whole-Each/100000001")
                == "Garlic Bulb Fresh Whole Each")
        #expect(
            ProductLink.walmartProductName(inURL: "https://walmart.com/ip/Test-Rice-2-lb/100000002?classType=REGULAR")
                == "Test Rice 2 lb")
        #expect(
            ProductLink.walmartProductName(inURL: "https://www.walmart.com/ip/Caf%C3%A9-Test-Coffee/100000003")
                == "Café Test Coffee")
        // Nothing to read, so nothing is claimed.
        #expect(ProductLink.walmartProductName(inURL: "https://www.walmart.com/ip/100000001") == nil)
        #expect(ProductLink.walmartProductName(inURL: "https://example.com/ip/Test-Beef/100000001") == nil)
        #expect(ProductLink.walmartProductName(inURL: "https://www.walmart.com/search?q=garlic") == nil)
    }

    @Test func theSearchLinkEncodesTheIngredientName() {
        #expect(
            ProductLink.walmartSearchURL(for: " red onion & garlic ")?.absoluteString
                == "https://www.walmart.com/search?q=red%20onion%20%26%20garlic")
        #expect(ProductLink.walmartSearchURL(for: "  ") == nil)
    }
}

struct SavedProductDraftTests {
    @Test func aPastedMessageWithNameAndSizeBuildsTheRequest() {
        var draft = SavedProductDraft()
        draft.linkText = "Check this out https://www.walmart.com/ip/Test-Beef/100000001"
        draft.displayName = "  Test Brand ground beef "
        draft.hasPackageSize = true
        draft.packageQuantityText = "1 1/2"
        draft.packageUnit = "lb"

        #expect(draft.isValid)
        #expect(draft.itemID == "100000001")
        #expect(
            draft.request(ingredientName: "Ground Beef")
                == ShoppingPreferenceRequest(
                    product: .url("https://www.walmart.com/ip/Test-Beef/100000001"),
                    displayName: "Test Brand ground beef",
                    packageSize: ShoppingPackageSizeInput(quantity: "3/2", unit: "lb"), ingredientName: "Ground Beef"))
    }

    /// Pasting a link fills the name in, so the member types nothing at all.
    @Test func theNameIsFilledInFromTheLink() {
        var draft = SavedProductDraft()
        draft.linkText = "https://www.walmart.com/ip/Garlic-Bulb-Fresh-Whole-Each/100000001"
        draft.fillNameFromLink()
        #expect(draft.displayName == "Garlic Bulb Fresh Whole Each")
        #expect(draft.isValid)

        // A name the member has edited is never overwritten.
        draft.displayName = "Garlic"
        draft.fillNameFromLink()
        #expect(draft.displayName == "Garlic")

        // A link carrying no name leaves the field alone, and the API will ask for one.
        var bare = SavedProductDraft()
        bare.linkText = "https://www.walmart.com/ip/100000001"
        bare.fillNameFromLink()
        #expect(bare.displayName.isEmpty)
        #expect(!bare.isValid)
    }

    @Test func aNewProductStartsNamedAfterTheIngredientAndALinkDoesNotRenameIt() {
        var draft = SavedProductDraft(ingredientName: "Minced Garlic")
        #expect(draft.displayName == "Minced Garlic")
        draft.linkText = "https://www.walmart.com/ip/Garlic-Bulb-Fresh-Whole-Each/100000001"
        draft.fillNameFromLink()
        #expect(draft.displayName == "Minced Garlic")
        #expect(draft.isValid)
    }

    @Test func anItemNumberWithoutASizeSendsTheIDAndNoSize() {
        var draft = SavedProductDraft()
        draft.linkText = "100000002"
        draft.displayName = "Test cilantro"
        draft.packageQuantityText = "12"

        let request = draft.request(ingredientName: "  ")
        #expect(request?.product == .itemID("100000002"))
        #expect(request?.packageSize == nil)
        #expect(request?.ingredientName == nil)
    }

    @Test func invalidInputIsExplainedAndNotSent() {
        var draft = SavedProductDraft()
        #expect(!draft.isValid)
        #expect(draft.linkError == nil)

        draft.linkText = "the beef one"
        draft.displayName = "Beef"
        #expect(draft.linkError != nil)
        #expect(draft.request(ingredientName: nil) == nil)

        draft.linkText = "https://www.walmart.com/ip/100000001"
        draft.displayName = String(repeating: "x", count: 101)
        #expect(draft.nameError != nil)
        #expect(!draft.isValid)

        draft.displayName = "Beef"
        draft.hasPackageSize = true
        draft.packageQuantityText = ""
        #expect(!draft.isValid)
        draft.packageQuantityText = "a lot"
        #expect(draft.quantityError != nil)
        #expect(!draft.isValid)
        draft.packageQuantityText = "16"
        #expect(draft.isValid)
    }

    @Test func editingStartsFromTheSavedProduct() throws {
        let json = ShoppingFixtures.preferenceJSON(
            key: "i-rice", ingredientName: "Rice", productID: "100000004", displayName: "Test rice",
            size: ShoppingFixtures.amount("3/2", "cup", text: "1 ½ cup"))
        let preference = try JSONCoding.makeDecoder().decode(ShoppingPreference.self, from: Data(json.utf8))

        let draft = SavedProductDraft(preference: preference)

        #expect(draft.linkText == "https://www.walmart.com/ip/100000004")
        #expect(draft.displayName == "Test rice")
        #expect(draft.hasPackageSize)
        #expect(draft.packageQuantityText == "1 1/2")
        #expect(draft.packageUnit == "cup")
        #expect(draft.isValid)
    }

    private func preference(_ key: String, _ name: String, size: String?) throws -> ShoppingPreference {
        let json = ShoppingFixtures.preferenceJSON(
            key: key, ingredientName: name, productID: "10000\(name.count)", displayName: "Test \(name)", size: size)
        return try JSONCoding.makeDecoder().decode(ShoppingPreference.self, from: Data(json.utf8))
    }

    @Test func savedProductsListThoseMissingAPackageSizeFirst() throws {
        let preferences = [
            try preference("i-beef", "Ground Beef", size: ShoppingFixtures.amount("16", "oz")),
            try preference("i-cilantro", "Cilantro", size: nil),
            try preference("i-paprika", "Paprika", size: nil),
            try preference("i-rice", "Rice", size: ShoppingFixtures.amount("2", "lb")),
        ]

        let groups = SavedProductGroups(preferences)

        #expect(groups.needsPackageSize.map(\.ingredientName) == ["Cilantro", "Paprika"])
        #expect(groups.complete.map(\.ingredientName) == ["Ground Beef", "Rice"])
        #expect(SavedProductGroups([preferences[0]]).needsPackageSize.isEmpty)
    }

    @Test func tappingASavedProductWithoutASizeOpensItOnThePackageSize() throws {
        let missing = ProductChoice(preference: try preference("i-paprika", "Paprika", size: nil))
        #expect(missing.packageSizeFix == .add)
        #expect(missing.draft.hasPackageSize)
        #expect(missing.isSaved)

        let complete = ProductChoice(
            preference: try preference("i-beef", "Ground Beef", size: ShoppingFixtures.amount("16", "oz")))
        #expect(complete.packageSizeFix == nil)
    }
}

struct OrderConfirmationDraftTests {
    /// Beef (3 packages, pending), cilantro (confirmed), limes (1 package, pending or skipped).
    private func handoff(limeStatus: String = "pending") throws -> ShoppingHandoff {
        let fields = ShoppingFixtures.proposalFields(
            lines: [
                ShoppingFixtures.lineJSON(
                    id: "l1", key: "i-beef", name: "Ground Beef", productID: "100000001", displayName: "Test beef",
                    size: nil, computed: 3, confirmation: ShoppingFixtures.confirmationJSON(status: "pending")),
                ShoppingFixtures.lineJSON(
                    id: "l2", key: "i-cilantro", name: "Cilantro", productID: "100000002", displayName: "Test cilantro",
                    size: nil, computed: 1,
                    confirmation: ShoppingFixtures.confirmationJSON(status: "confirmed", packages: 1)),
                ShoppingFixtures.lineJSON(
                    id: "l3", key: "i-lime", name: "Lime", productID: "100000003", displayName: "Test limes",
                    size: nil, computed: 1, confirmation: ShoppingFixtures.confirmationJSON(status: limeStatus)),
            ],
            excluded: [], links: [])
        let json = ShoppingFixtures.handoffJSON(id: "handoff-1", status: "open", fields: fields)
        return try JSONCoding.makeDecoder().decode(ShoppingHandoff.self, from: Data(json.utf8))
    }

    @Test func everyUnconfirmedLineStartsChecked() throws {
        let draft = OrderConfirmationDraft(handoff: try handoff())

        #expect(draft.lines.map(\.id) == ["l1", "l3"])
        #expect(draft.selectedCount == 2)
        #expect(draft.lines.allSatisfy(draft.isSelected))
        #expect(draft.packages(for: draft.lines[0]) == 3)
        #expect(draft.everythingRequest == .all)
        #expect(
            draft.selectedRequest
                == .lines([ConfirmedOrderLine(lineID: "l1"), ConfirmedOrderLine(lineID: "l3")], skipRest: true))
    }

    @Test func changedCountsAndUncheckedLinesAreSent() throws {
        var draft = OrderConfirmationDraft(handoff: try handoff())
        let (beef, limes) = (draft.lines[0], draft.lines[1])

        draft.setPackages(5, for: beef)
        draft.toggle(limes)

        #expect(!draft.isSelected(limes))
        #expect(
            draft.everythingRequest
                == .lines(
                    [ConfirmedOrderLine(lineID: "l1", packages: 5), ConfirmedOrderLine(lineID: "l3")], skipRest: true))
        #expect(draft.selectedRequest == .lines([ConfirmedOrderLine(lineID: "l1", packages: 5)], skipRest: true))

        draft.setPackages(0, for: beef)
        #expect(draft.packages(for: beef) == 1)
        draft.setPackages(500, for: beef)
        #expect(draft.packages(for: beef) == 99)

        draft.toggle(beef)
        #expect(draft.selectedCount == 0)
        #expect(draft.selectedRequest == nil)
    }

    @Test func aLineSkippedEarlierIsNamedBecauseAllWouldLeaveItOut() throws {
        let draft = OrderConfirmationDraft(handoff: try handoff(limeStatus: "skipped"))

        #expect(draft.lines.map(\.id) == ["l1", "l3"])
        #expect(
            draft.everythingRequest
                == .lines([ConfirmedOrderLine(lineID: "l1"), ConfirmedOrderLine(lineID: "l3")], skipRest: true))
    }

    @Test func theOneConfirmButtonSaysWhatItConfirms() throws {
        var draft = OrderConfirmationDraft(handoff: try handoff())
        #expect(draft.isAllSelected)
        #expect(draft.confirmTitle == "Ordered All 2")
        #expect(draft.confirmRequest == .all)

        draft.toggle(draft.lines[1])
        #expect(draft.confirmTitle == "Ordered 1 of 2")
        #expect(draft.confirmRequest == .lines([ConfirmedOrderLine(lineID: "l1")], skipRest: true))

        draft.toggle(draft.lines[0])
        #expect(draft.confirmTitle == "Ordered 0 of 2")
        #expect(draft.confirmRequest == nil)
    }
}

struct OrderConfirmationGroupsTests {
    private static let names = ["r-tacos": "Tacos", "r-pasta": "Pasta", "r-bread": "Garlic Bread"]

    private func line(_ id: String, _ name: String, recipes: [String]) throws -> ShoppingHandoffLine {
        let json = ShoppingFixtures.lineJSON(
            id: id, key: "i-\(id)", name: name, productID: "10000000\(id.count)", displayName: "Test \(name)",
            size: nil, computed: 1)
        var line = try JSONCoding.makeDecoder().decode(ShoppingHandoffLine.self, from: Data(json.utf8))
        line.recipeRefs = recipes.map { GroceryRecipe(id: $0, name: Self.names[$0] ?? $0) }
        return line
    }

    private func entry(_ recipeID: String, day: PlanDay?, isAddon: Bool = false) -> PlanEntry {
        PlanEntry(
            id: "e-\(recipeID)",
            recipe: PlanEntryRecipe(
                id: recipeID, name: Self.names[recipeID] ?? recipeID,
                imageURLString: "https://images.example.com/\(recipeID).jpg", isAddon: isAddon),
            day: day, date: nil, servings: 2, note: "", addedBy: "user-1", addedAt: .distantPast)
    }

    private func lines() throws -> [ShoppingHandoffLine] {
        [
            try line("l1", "Garlic", recipes: ["r-pasta", "r-tacos"]),
            try line("l2", "Tortillas", recipes: ["r-tacos"]),
            try line("l3", "Cavatappi", recipes: ["r-pasta"]),
            try line("l4", "Baguette", recipes: ["r-bread"]),
            try line("l5", "Crackers", recipes: []),
            try line("l6", "Ground Beef", recipes: ["r-tacos"]),
        ]
    }

    @Test func itemsGroupUnderTheirMealInPlanOrderWithEachItemOnce() throws {
        // Pasta was added first, but tacos is on Monday.
        let entries = [
            entry("r-pasta", day: .wed), entry("r-bread", day: .wed, isAddon: true), entry("r-tacos", day: .mon),
        ]

        let groups = OrderConfirmationGroups(lines: try lines(), entries: entries, weekStartsOn: .mon)

        #expect(groups.meals.map(\.name) == ["Tacos", "Pasta"])
        #expect(groups.meals.map { $0.lines.map(\.name) } == [["Tortillas", "Ground Beef"], ["Cavatappi"]])
        #expect(groups.meals.map(\.day) == [.mon, .wed])
        #expect(groups.meals.first?.imageURL?.absoluteString == "https://images.example.com/r-tacos.jpg")
        // Garlic is for both meals: listed once, naming them in plan order.
        #expect(groups.shared.map(\.line.name) == ["Garlic"])
        #expect(groups.shared.first?.mealNames == ["Tacos", "Pasta"])
        // The add-on's items and the item tied to no recipe are extras.
        #expect(groups.extras.map(\.name) == ["Baguette", "Crackers"])

        let all = groups.meals.flatMap(\.lines) + groups.shared.map(\.line) + groups.extras
        #expect(all.map(\.id).sorted() == ["l1", "l2", "l3", "l4", "l5", "l6"])
    }

    @Test func mealsFollowTheHouseholdsWeekStartDay() throws {
        let entries = [
            entry("r-tacos", day: .mon), entry("r-bread", day: .mon, isAddon: true), entry("r-pasta", day: .sun),
        ]

        let sunday = OrderConfirmationGroups(lines: try lines(), entries: entries, weekStartsOn: .sun)
        let monday = OrderConfirmationGroups(lines: try lines(), entries: entries, weekStartsOn: .mon)

        #expect(sunday.meals.map(\.day) == [.sun, .mon])
        #expect(monday.meals.map(\.day) == [.mon, .sun])
    }

    @Test func withoutThePlanMealsComeFromTheLinesThemselves() throws {
        let groups = OrderConfirmationGroups(lines: try lines(), entries: nil, weekStartsOn: .mon)

        // In the order the lines first name them; add-ons can't be told apart without the plan.
        #expect(groups.meals.map(\.name) == ["Pasta", "Tacos", "Garlic Bread"])
        #expect(groups.meals.allSatisfy { $0.imageURL == nil && $0.day == nil })
        #expect(groups.shared.map(\.line.name) == ["Garlic"])
        #expect(groups.extras.map(\.name) == ["Crackers"])
    }

    @Test func aLineDecodesItsRecipesAndOlderHandoffsHaveNone() throws {
        let json = ShoppingFixtures.lineJSON(
            id: "l1", key: "i-beef", name: "Ground Beef", productID: "100000001", displayName: "Test beef",
            size: nil, computed: 1)
        let decoder = JSONCoding.makeDecoder()

        let old = try decoder.decode(ShoppingHandoffLine.self, from: Data(json.utf8))
        #expect(old.recipes.isEmpty)

        let withRecipes = json.replacingOccurrences(
            of: #""cart":null}"#, with: #""cart":null,"recipes":[{"id":"r-tacos","name":"Tacos"}]}"#)
        let line = try decoder.decode(ShoppingHandoffLine.self, from: Data(withRecipes.utf8))
        #expect(line.recipes == [GroceryRecipe(id: "r-tacos", name: "Tacos")])
    }
}
