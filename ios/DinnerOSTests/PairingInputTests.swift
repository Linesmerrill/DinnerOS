import Foundation
import Testing

@testable import DinnerOS

/// The rules editor's validation, which mirrors what the API rejects.
struct PairingInputTests {
    private let limits = AutopilotLimits.defaults

    private func recipeRule() -> PairingRule {
        PairingRule(
            when: PairingRuleConditions(mealCategories: [.pasta]),
            add: .recipe(id: "addon-1", name: "Sample Garlic Bread"))
    }

    private func groceryRule(name: String = "Club Crackers", quantity: Double? = 1, unit: String? = "package")
        -> PairingRule
    {
        PairingRule(
            when: PairingRuleConditions(mealCategories: [.soup]),
            add: .groceryItem(PairingGroceryItem(name: name, quantity: quantity, unit: unit)))
    }

    // MARK: One rule

    @Test func aRuleWithAConditionAndAnAddIsValid() {
        #expect(PairingInput.validationMessage(for: recipeRule(), limits: limits) == nil)
        #expect(PairingInput.validationMessage(for: groceryRule(), limits: limits) == nil)
        // A grocery item needs no amount at all.
        #expect(
            PairingInput.validationMessage(for: groceryRule(quantity: nil, unit: nil), limits: limits) == nil)
    }

    @Test func aRuleNeedsAtLeastOneCondition() {
        var rule = recipeRule()
        rule.when = PairingRuleConditions()

        #expect(
            PairingInput.validationMessage(for: rule, limits: limits)
                == "Choose at least one thing this rule matches.")
    }

    /// Cuisines, tags, or proteins count on their own; the category isn't required.
    @Test func aConditionCanBeSomethingOtherThanAMealCategory() {
        var rule = recipeRule()
        rule.when = PairingRuleConditions(cuisines: ["italian"])

        #expect(PairingInput.validationMessage(for: rule, limits: limits) == nil)
        #expect(!rule.when.isEmpty)
    }

    @Test func aRuleAddsExactlyOneThing() {
        var neither = recipeRule()
        neither.add = PairingRuleTarget()
        #expect(
            PairingInput.validationMessage(for: neither, limits: limits)
                == "Choose an add-on recipe, or type a grocery item — not both.")

        var both = recipeRule()
        both.add = PairingRuleTarget(
            recipeID: "addon-1", groceryItem: PairingGroceryItem(name: "Club Crackers"))
        #expect(PairingInput.validationMessage(for: both, limits: limits) != nil)
        #expect(!both.add.isValid)
    }

    @Test func aGroceryItemNeedsAName() {
        #expect(
            PairingInput.validationMessage(for: groceryRule(name: "   "), limits: limits)
                == "Give the grocery item a name.")
        #expect(
            PairingInput.validationMessage(
                for: groceryRule(name: String(repeating: "a", count: 61)), limits: limits)
                == "Keep the name to 60 characters.")
    }

    @Test func aQuantityStaysInRangeAndAUnitNeedsOne() {
        #expect(
            PairingInput.validationMessage(for: groceryRule(quantity: 0), limits: limits)
                == "The amount must be between 0 and 99.")
        #expect(
            PairingInput.validationMessage(for: groceryRule(quantity: 100), limits: limits)
                == "The amount must be between 0 and 99.")
        #expect(
            PairingInput.validationMessage(for: groceryRule(quantity: nil, unit: "package"), limits: limits)
                == "Add an amount for the unit, or clear the unit.")
    }

    // MARK: The whole list

    @Test func theListIsCappedAndCantRepeatTheSameMealsAndItem() {
        let rules = Array(repeating: recipeRule(), count: 21)
        #expect(
            PairingInput.validationMessage(for: rules, limits: limits)
                == "You can have up to 20 pairing rules.")

        #expect(
            PairingInput.validationMessage(for: [recipeRule(), recipeRule()], limits: limits)
                == "Two rules can't add the same thing to the same meals.")
        // The same add for a different meal category is fine.
        var other = recipeRule()
        other.when = PairingRuleConditions(mealCategories: [.soup])
        #expect(PairingInput.validationMessage(for: [recipeRule(), other], limits: limits) == nil)
    }

    /// The duplicate check ignores order and case, the way the API's does.
    @Test func duplicatesAreFoundWhateverTheOrderOrCase() {
        var first = groceryRule(name: "Club Crackers")
        first.when = PairingRuleConditions(mealCategories: [.soup, .salad], cuisines: ["Italian"])
        var second = groceryRule(name: "club crackers")
        second.when = PairingRuleConditions(mealCategories: [.salad, .soup], cuisines: ["italian"])

        #expect(PairingInput.duplicateKey(first) == PairingInput.duplicateKey(second))
        #expect(PairingInput.validationMessage(for: [first, second], limits: limits) != nil)
    }

    @Test func anEmptyListIsValid() {
        #expect(PairingInput.validationMessage(for: [], limits: limits) == nil)
    }

    // MARK: Editing

    @Test func switchingWhatARuleAddsClearsTheOtherKind() {
        var rule = recipeRule()
        #expect(rule.add.recipeID == "addon-1")

        rule.setAddKind(.groceryItem)

        #expect(rule.add.recipeID == nil)
        #expect(rule.add.recipeName == nil)
        #expect(rule.add.groceryItem?.name == "")
        #expect(rule.add.kind == .groceryItem)

        rule.setAddKind(.recipe)
        #expect(rule.add.groceryItem == nil)
        // Switching to the same kind keeps what's there.
        rule.add = .recipe(id: "addon-2", name: "Another Add-On")
        rule.setAddKind(.recipe)
        #expect(rule.add.recipeID == "addon-2")
    }

    @Test func mealCategoriesKeepTheVocabularysOrder() {
        var rule = PairingRule.new()
        let order = MealCategory.known.map(\.rawValue)

        rule.setMealCategory(.soup, included: true, order: order)
        rule.setMealCategory(.pasta, included: true, order: order)

        #expect(rule.when.mealCategories == [.pasta, .soup])

        rule.setMealCategory(.pasta, included: false, order: order)
        #expect(rule.when.mealCategories == [.soup])
    }

    @Test func aNewRuleStartsAsAnEmptyGroceryItemAndIsNotValidYet() {
        let rule = PairingRule.new()

        #expect(rule.id == nil)
        #expect(rule.add.kind == .groceryItem)
        #expect(rule.frequency == .suggest)
        // A new rule needs a condition before it can be saved.
        #expect(PairingInput.validationMessage(for: rule, limits: limits) != nil)
        // Two new rules are distinct in the editor even before the server assigns ids.
        #expect(PairingRule.new().identity != PairingRule.new().identity)
    }

    @Test func namesAreTrimmedTheWayTheAPIStoresThem() {
        #expect(PairingInput.normalizedName("  Club   Crackers  ") == "Club Crackers")
        #expect(PairingInput.normalizedName("   ").isEmpty)
        // Unlike taste values, the capitalization is kept: it shows on the grocery list.
        #expect(PairingInput.normalizedName("Club Crackers") == "Club Crackers")
    }
}
