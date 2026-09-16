import Foundation

/// Input rules the API validates for pairing rules, applied as the member edits so a save
/// doesn't fail.
nonisolated enum PairingInput {
    /// Free text the way the API stores a grocery item's name: trimmed, with runs of
    /// whitespace collapsed. Unlike taste values, the name keeps its capitalization — it is
    /// shown on the grocery list as typed.
    static func normalizedName(_ value: String) -> String {
        value.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// Why the API would reject one rule, or `nil` when it's valid.
    static func validationMessage(for rule: PairingRule, limits: AutopilotLimits) -> String? {
        if rule.when.isEmpty {
            return String(localized: "Choose at least one thing this rule matches.")
        }
        guard rule.add.isValid else {
            return String(localized: "Choose an add-on recipe, or type a grocery item — not both.")
        }
        if let item = rule.add.groceryItem {
            let name = normalizedName(item.name)
            if name.isEmpty {
                return String(localized: "Give the grocery item a name.")
            }
            if name.count > limits.maxGroceryItemNameLength {
                return String(localized: "Keep the name to \(limits.maxGroceryItemNameLength) characters.")
            }
            if let quantity = item.quantity {
                if quantity <= 0 || quantity > Double(limits.maxGroceryItemQuantity) {
                    return String(localized: "The amount must be between 0 and \(limits.maxGroceryItemQuantity).")
                }
            } else if let unit = item.unit, !unit.isEmpty {
                return String(localized: "Add an amount for the unit, or clear the unit.")
            }
        }
        if rule.label.count > limits.maxLabelLength {
            return String(localized: "Keep the name to \(limits.maxLabelLength) characters.")
        }
        return nil
    }

    /// Why the API would reject the whole list, or `nil` when it's valid. Checks each rule,
    /// the limit, and the "two rules can't repeat the same meals and item" constraint.
    static func validationMessage(for rules: [PairingRule], limits: AutopilotLimits) -> String? {
        if rules.count > limits.maxPairingRules {
            return String(localized: "You can have up to \(limits.maxPairingRules) pairing rules.")
        }
        for rule in rules {
            if let message = validationMessage(for: rule, limits: limits) {
                return message
            }
        }
        var seen = Set<String>()
        for rule in rules where !seen.insert(duplicateKey(rule)).inserted {
            return String(localized: "Two rules can't add the same thing to the same meals.")
        }
        return nil
    }

    /// What makes two rules duplicates: the same conditions and the same add.
    static func duplicateKey(_ rule: PairingRule) -> String {
        let when = [
            rule.when.mealCategories.map(\.rawValue).sorted().joined(separator: ","),
            rule.when.cuisines.map { $0.lowercased() }.sorted().joined(separator: ","),
            rule.when.tags.map { $0.lowercased() }.sorted().joined(separator: ","),
            rule.when.proteins.map { $0.lowercased() }.sorted().joined(separator: ","),
        ]
        .joined(separator: "|")
        let add = rule.add.recipeID ?? "item:" + normalizedName(rule.add.groceryItem?.name ?? "").lowercased()
        return when + "→" + add
    }
}

nonisolated extension PairingRule {
    /// A new rule, ready to edit.
    static func new() -> PairingRule {
        PairingRule(add: PairingRuleTarget(kind: .groceryItem, groceryItem: PairingGroceryItem(name: "")))
    }

    /// Switches what the rule adds, keeping nothing from the other kind: the API takes
    /// exactly one of the two.
    mutating func setAddKind(_ kind: PairingKind) {
        guard add.kind != kind else { return }
        switch kind {
        case .recipe:
            add = PairingRuleTarget(kind: .recipe, recipeID: nil, recipeName: nil)
        case .groceryItem:
            add = PairingRuleTarget(kind: .groceryItem, groceryItem: PairingGroceryItem(name: ""))
        }
    }

    /// Includes or removes a meal category, keeping the vocabulary's order.
    mutating func setMealCategory(_ category: MealCategory, included: Bool, order: [String]) {
        var values = when.mealCategories.map(\.rawValue)
        AutopilotInput.set(category.rawValue, included: included, in: &values, order: order)
        when.mealCategories = values.map { MealCategory(rawValue: $0) }
    }
}
