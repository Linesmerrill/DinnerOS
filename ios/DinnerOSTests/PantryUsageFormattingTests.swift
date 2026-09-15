import Foundation
import Testing

@testable import DinnerOS

struct PantryUsageFormattingTests {
    private let locale = Locale(identifier: "en_US")

    @Test func remainingTextForRowsVoiceOverAndDetail() {
        let estimate = PantryFixtures.estimate()

        #expect(PantryUsageFormat.remainingShort(estimate, locale: locale) == "~31% left")
        #expect(PantryUsageFormat.remainingSpoken(estimate, locale: locale) == "About 31% left")
        #expect(PantryUsageFormat.remainingAmount(estimate, locale: locale) == "5 tbsp of 16 tbsp")
    }

    @Test func levelFollowsTheThreshold() {
        #expect(PantryUsageFormat.level(PantryFixtures.estimate()) == .plenty)
        #expect(PantryUsageFormat.level(PantryFixtures.estimate(percentRemaining: 15, belowThreshold: true)) == .low)
        #expect(PantryUsageFormat.level(PantryFixtures.estimate(percentRemaining: 0, belowThreshold: true)) == .empty)
    }

    @Test func recipeUseCountsRecipes() {
        #expect(
            PantryUsageFormat.recipeUse(PantryFixtures.estimate(recipeCount: 0), locale: locale)
                == "No cooked recipes yet")
        #expect(
            PantryUsageFormat.recipeUse(PantryFixtures.estimate(recipeCount: 1), locale: locale)
                == "1 recipe used 3 tbsp")
        #expect(
            PantryUsageFormat.recipeUse(PantryFixtures.estimate(recipeCount: 2), locale: locale)
                == "2 recipes used 6 tbsp")
    }

    @Test func dailyRateNeedsHistory() {
        let learned = PantryFixtures.estimate()
        let unlearned = PantryFixtures.estimate(dailyRate: nil)

        #expect(PantryUsageFormat.dailyRate(learned, locale: locale) == "About 1 tbsp a day")
        #expect(PantryUsageFormat.dailyRateBasis(learned) == "Learned from 3 earlier periods")
        #expect(PantryUsageFormat.dailyRate(unlearned, locale: locale) == "Not enough history yet")
        #expect(PantryUsageFormat.dailyRateBasis(unlearned) == nil)
    }

    @Test func skippedRecipesAndThresholds() {
        #expect(PantryUsageFormat.skippedRecipes(PantryFixtures.estimate()) == nil)
        #expect(
            PantryUsageFormat.skippedRecipes(PantryFixtures.estimate(skippedRecipes: 1))
                == "1 recipe couldn't be counted")
        #expect(
            PantryUsageFormat.skippedRecipes(PantryFixtures.estimate(skippedRecipes: 3))
                == "3 recipes couldn't be counted")
        #expect(PantryUsageFormat.threshold(80, locale: locale) == "80% used")
        #expect(PantryUsageFormat.thresholdSource(.household) == "Household setting")
        #expect(PantryUsageFormat.thresholdSource(.item) == "Set for this item")
    }

    @Test func estimatedStatusReadsDifferentlyFromAPersonsStatus() {
        let estimated = PantryFixtures.item(status: .low, statusSource: .estimate)
        let person = PantryFixtures.item(status: .low)
        let restocked = PantryFixtures.item(status: .inStock, statusSource: .estimate)

        #expect(PantryUsageFormat.statusTitle(estimated) == "Estimated Low")
        #expect(PantryUsageFormat.statusTitle(person) == "Low")
        #expect(PantryUsageFormat.statusTitle(restocked) == "In Stock")
    }

    @Test func purchaseAmounts() throws {
        let purchases = try JSONCoding.makeDecoder().decode(
            PantryPurchaseListResponse.self, from: PantryFixtures.purchaseHistoryJSON
        ).items

        #expect(PantryUsageFormat.purchaseAmount(purchases[0], locale: locale) == "2 packages, 8 oz each")
        #expect(PantryUsageFormat.purchaseAmount(purchases[1], locale: locale) == "No amount recorded")
        #expect(PantryUsageFormat.purchaseSource(purchases[0].source) == "Restocked")
        #expect(PantryUsageFormat.purchaseSource(purchases[1].source) == "Grocery list")
        #expect(PantryUsageFormat.amount("3/2", value: 1.5, unit: "cup", locale: locale) == "1½ cups")
        #expect(PantryUsageFormat.amount("3", value: 3, unit: "count", locale: locale) == "3")
        #expect(
            PantryUsageFormat.unitSize(
                PantryUnitSize(per: "package", quantity: "8", quantityValue: 8, unit: "oz"), locale: locale)
                == "1 package = 8 oz")
    }
}

struct PantryPurchaseDraftTests {
    private func groceryItems() throws -> [GroceryItem] {
        try JSONCoding.makeDecoder().decode(GroceryList.self, from: PlanFixtures.groceryList).allItems
    }

    private let week = ISOWeek("2026-W38")

    @Test func groceryLineIsPrefilledWithItsFirstAmount() throws {
        let onion = try #require(try groceryItems().first)

        var draft = PantryPurchaseDraft(groceryItem: onion, clientPurchaseID: "client-1")

        #expect(draft.quantityText == "1 1/2")
        #expect(draft.unit == "count")
        #expect(draft.isValid)
        let purchase = try draft.groceryPurchase(for: onion, week: try #require(week))
        #expect(
            purchase
                == NewPantryPurchase(
                    ingredientID: "i-onion", source: .groceryList, quantity: "3/2", unit: "count", week: "2026-W38",
                    clientPurchaseID: "client-1"))

        draft.use(onion.amounts[1])
        #expect(draft.quantityText == "8")
        #expect(draft.unit == "oz")
    }

    @Test func uncataloguedLineWithoutAnAmountIsSentByName() throws {
        let line = try JSONCoding.makeDecoder().decode(
            GroceryItem.self,
            from: Data(
                #"""
                {"ingredientKey":"name:dragon fruit","name":" Dragon Fruit ","amounts":[],"quantityText":"",
                 "unquantified":true,"status":"toBuy","recipes":[]}
                """#.utf8))

        let draft = PantryPurchaseDraft(groceryItem: line)
        let purchase = try draft.groceryPurchase(for: line, week: try #require(week))

        #expect(purchase.name == "Dragon Fruit")
        #expect(purchase.ingredientID == nil)
        #expect(purchase.quantity == nil)
        #expect(purchase.unit == nil)
        #expect(purchase.clientPurchaseID == draft.clientPurchaseID)
        #expect(!draft.clientPurchaseID.isEmpty)
    }

    @Test func invalidAmountsAreRejected() throws {
        let onion = try #require(try groceryItems().first)
        var draft = PantryPurchaseDraft(groceryItem: onion)

        draft.quantityText = "-2"

        #expect(!draft.isValid)
        #expect(draft.quantityError != nil)
        #expect(throws: PantryQuantityError.negative) {
            try draft.groceryPurchase(for: onion, week: try #require(week))
        }
    }

    @Test func packageSizeOnlyForDiscreteUnitsWithAnAmount() throws {
        var draft = PantryPurchaseDraft(clientPurchaseID: "client-2")
        draft.quantityText = "2"
        draft.unit = "package"
        draft.hasUnitSize = true
        draft.unitSizeText = "8"
        draft.unitSizeUnit = "oz"

        #expect(
            try draft.manualPurchase(itemID: "item-butter")
                == NewPantryPurchase(
                    itemID: "item-butter", source: .manual, quantity: "2", unit: "package",
                    unitSize: PantryUnitSizeInput(quantity: "8", unit: "oz"), clientPurchaseID: "client-2"))

        draft.unit = "cup"
        #expect(try draft.manualPurchase(itemID: "item-butter").unitSize == nil)

        draft.unit = "can"
        draft.quantityText = ""
        #expect(draft.unitSizeError != nil)
        #expect(!draft.isValid)

        draft.quantityText = "1"
        draft.unitSizeText = "0"
        #expect(draft.unitSizeError != nil)
    }

    @Test func restockStartsInTheItemsUnitAndSize() {
        let packaged = PantryFixtures.item(
            quantity: "2", quantityValue: 2, unit: "oz",
            unitSize: PantryUnitSize(per: "package", quantity: "1/2", quantityValue: 0.5, unit: "cup"))
        let loose = PantryFixtures.item(quantity: "1", quantityValue: 1, unit: "lb")

        let packagedDraft = PantryPurchaseDraft(item: packaged)
        let looseDraft = PantryPurchaseDraft(item: loose)

        #expect(packagedDraft.unit == "package")
        #expect(packagedDraft.hasUnitSize)
        #expect(packagedDraft.unitSizeText == "1/2")
        #expect(packagedDraft.unitSizeUnit == "cup")
        #expect(looseDraft.unit == "lb")
        #expect(!looseDraft.hasUnitSize)
        #expect(looseDraft.quantityText.isEmpty)
    }

    @Test func itemThresholdEditsSendOnlyRealChanges() throws {
        let inherits = PantryFixtures.item(estimate: PantryFixtures.estimate(lowThresholdPercent: 75))
        var draft = PantryItemDraft(item: inherits)
        #expect(draft.usesHouseholdThreshold)
        #expect(draft.lowThresholdPercent == 75)
        #expect(try draft.changes(from: inherits).lowThresholdPercent == nil)

        draft.usesHouseholdThreshold = false
        draft.lowThresholdPercent = 65
        #expect(try draft.changes(from: inherits).lowThresholdPercent == .percent(65))

        let own = PantryFixtures.item(lowThresholdPercent: 65)
        var ownDraft = PantryItemDraft(item: own)
        #expect(!ownDraft.usesHouseholdThreshold)
        #expect(try ownDraft.changes(from: own).isEmpty)

        ownDraft.usesHouseholdThreshold = true
        #expect(try ownDraft.changes(from: own) == PantryItemChanges(lowThresholdPercent: .household))
    }
}
