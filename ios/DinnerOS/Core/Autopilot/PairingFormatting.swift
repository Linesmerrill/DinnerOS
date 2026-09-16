import Foundation

/// Display text for add-on pairings and the rules behind them.
nonisolated enum PairingFormat {
    // MARK: Rules

    /// "Pasta → Garlic Bread", the one line a rule shows in the list. Falls back to the
    /// rule's label, then to what it adds, when it matches on something else.
    static func ruleTitle(_ rule: PairingRule, vocabulary: AutopilotVocabulary?) -> String {
        let add = rule.add.name
        guard let when = conditionSummary(rule.when, vocabulary: vocabulary) else {
            let label = rule.label.trimmingCharacters(in: .whitespacesAndNewlines)
            return label.isEmpty ? add : "\(label) → \(add)"
        }
        return add.isEmpty ? when : "\(when) → \(add)"
    }

    /// "Always · Adds a grocery item", the second line under a rule.
    static func ruleSummary(_ rule: PairingRule, vocabulary: AutopilotVocabulary?) -> String {
        var parts = [frequencyLabel(rule.frequency, vocabulary: vocabulary)]
        switch rule.add.kind {
        case .recipe:
            parts.append(String(localized: "Add-on recipe"))
        case .groceryItem:
            if let amount = rule.add.groceryItem?.amountText {
                parts.append(String(localized: "Grocery item · \(amount)"))
            } else {
                parts.append(String(localized: "Grocery item"))
            }
        case nil:
            break
        }
        let label = rule.label.trimmingCharacters(in: .whitespacesAndNewlines)
        if !label.isEmpty {
            parts.append(label)
        }
        return parts.joined(separator: " · ")
    }

    /// What a rule matches on, joined with " · " across groups and "or" within one:
    /// "Pasta", "Pasta or Soup · Chicken". `nil` when the rule has no conditions.
    static func conditionSummary(_ when: PairingRuleConditions, vocabulary: AutopilotVocabulary?) -> String? {
        var groups: [String] = []
        if !when.mealCategories.isEmpty {
            let options = vocabulary?.mealCategoryOptions ?? []
            groups.append(
                when.mealCategories
                    .map { category in
                        options.first { $0.value == category.rawValue }?.label ?? category.fallbackTitle
                    }
                    .formatted(.list(type: .or)))
        }
        if !when.cuisines.isEmpty {
            groups.append(
                when.cuisines.map { AutopilotFormat.label($0, vocabulary?.cuisines ?? []) }
                    .formatted(.list(type: .or)))
        }
        if !when.tags.isEmpty {
            groups.append(
                when.tags.map { AutopilotFormat.label($0, vocabulary?.tags ?? []) }.formatted(.list(type: .or)))
        }
        if !when.proteins.isEmpty {
            groups.append(
                when.proteins.map { AutopilotFormat.label($0, vocabulary?.proteins ?? []) }
                    .formatted(.list(type: .or)))
        }
        return groups.isEmpty ? nil : groups.joined(separator: " · ")
    }

    static func frequencyLabel(_ frequency: PairingFrequency, vocabulary: AutopilotVocabulary?) -> String {
        if let option = vocabulary?.pairingFrequencies.first(where: { $0.value == frequency.rawValue }) {
            return option.label
        }
        return frequency.title
    }

    static func mealCategoryLabel(_ category: MealCategory, vocabulary: AutopilotVocabulary?) -> String {
        vocabulary?.mealCategoryOptions.first { $0.value == category.rawValue }?.label ?? category.fallbackTitle
    }

    /// What the Pairings row says in the preferences list.
    static func sectionSummary(_ rules: [PairingRule], vocabulary: AutopilotVocabulary?) -> String {
        guard !rules.isEmpty else { return String(localized: "No rules") }
        return rules.prefix(3).map { ruleTitle($0, vocabulary: vocabulary) }.joined(separator: ", ")
    }

    // MARK: Suggestions

    /// The caption under a suggestion. The server's `reason` is shown as written and never
    /// parsed; this only stands in when a server sends none.
    static func reason(_ pairing: Pairing, vocabulary: AutopilotVocabulary?) -> String? {
        if let reason = pairing.reason, !reason.isEmpty {
            return reason
        }
        guard let category = pairing.mealCategory else { return nil }
        let label = mealCategoryLabel(category, vocabulary: vocabulary).lowercased()
        switch pairing.source {
        case .rule:
            return String(localized: "Your rule: \(label) → \(pairing.name)")
        case .learned:
            guard let confidence = pairing.confidence else {
                return String(localized: "You usually have \(pairing.name) with \(label)")
            }
            let share = confidence.formatted(.percent.precision(.fractionLength(0)))
            return String(localized: "You usually have \(pairing.name) with \(label) (\(share) of \(label) weeks)")
        default:
            return nil
        }
    }

    /// "Add Garlic Bread?" / "Add Club Crackers to the list?" — what the prompt asks.
    static func addPrompt(_ pairing: Pairing) -> String {
        switch pairing.target {
        case .recipe: String(localized: "Add \(pairing.name)?")
        case .groceryItem: String(localized: "Add \(pairing.name) to the list?")
        }
    }

    /// What accepting the proposal did with its pairings, for the summary. `nil` when it
    /// added and skipped none.
    static func acceptSummary(_ result: AutopilotAcceptResult) -> String? {
        var parts: [String] = []
        let added = result.pairingsAdded.map(\.name).filter { !$0.isEmpty }
        if !added.isEmpty {
            parts.append(String(localized: "Also added \(added.formatted(.list(type: .and)))."))
        }
        if !result.pairingsSkipped.isEmpty {
            let count = result.pairingsSkipped.count
            parts.append(
                count == 1
                    ? String(localized: "1 add-on wasn't added.")
                    : String(localized: "\(count) add-ons weren't added."))
        }
        return parts.isEmpty ? nil : parts.joined(separator: " ")
    }
}
