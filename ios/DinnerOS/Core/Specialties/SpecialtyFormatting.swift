import Foundation

/// What "Use Suggested for All" did, for the summary shown afterwards.
nonisolated struct SpecialtyDefaultsOutcome: Equatable, Sendable {
    /// Specialty ingredients that got their suggested option, in response order.
    let chosenNames: [String]
    /// Specialty ingredients that already had a choice.
    let skipped: Int

    var summary: String {
        let chosen: String
        switch chosenNames.count {
        case 0 where skipped == 0:
            return String(localized: "There were no specialty ingredients to set up.")
        case 0:
            return String(localized: "Every specialty ingredient already had a choice, so nothing changed.")
        case 1:
            chosen = String(localized: "Chose the suggested option for \(chosenNames[0]).")
        default:
            chosen = String(localized: "Chose the suggested option for \(chosenNames.count) specialty ingredients.")
        }
        switch skipped {
        case 0:
            return chosen
        case 1:
            return chosen + " " + String(localized: "1 already had a choice and wasn't changed.")
        default:
            return chosen + " " + String(localized: "\(skipped) already had a choice and weren't changed.")
        }
    }
}

/// Display text for specialty ingredients, shared by the setup screens and grocery lists.
nonisolated enum SpecialtyFormat {
    static func recipeCount(_ count: Int) -> String {
        switch count {
        case 0: String(localized: "Not in this household's recipes")
        case 1: String(localized: "Used in 1 recipe")
        default: String(localized: "Used in \(count) recipes")
        }
    }

    /// The strategy summary row's second line: the strategy in force, an honest failure when it
    /// couldn't be loaded, and "Loading…" only while it really is loading.
    static func strategySummaryDetail(_ settings: SpecialtySettings?, error: String? = nil) -> String {
        if let settings {
            return settings.currentOption?.label ?? settings.strategy.rawValue
        }
        if error != nil {
            return String(localized: "Couldn't load — tap to try again")
        }
        return String(localized: "Loading…")
    }

    /// A short label for the choice: "Not Set", "Keep as Is", "Store Alternative", or
    /// "House-Made Batch".
    static func choiceKind(_ ingredient: SpecialtyIngredient) -> String {
        guard let choice = ingredient.choice else { return String(localized: "Not Set") }
        switch choice.type {
        case .asIs: return String(localized: "Keep as Is")
        default: return SpecialtyOptionType(rawValue: choice.type.rawValue).title
        }
    }

    /// The chosen option's name, when an option (not "keep as is") is chosen.
    static func choiceOptionName(_ ingredient: SpecialtyIngredient) -> String? {
        guard let choice = ingredient.choice, choice.type != .asIs else { return nil }
        return ingredient.chosenOption?.name ?? choice.optionName
    }

    /// The badge naming the current plan, marked "· Default" when the household's standing
    /// strategy picked it rather than a member.
    static func choiceBadge(_ ingredient: SpecialtyIngredient) -> String {
        let kind = choiceKind(ingredient)
        guard ingredient.isResolvedByStrategy else { return kind }
        return String(localized: "\(kind) · Default")
    }

    /// "Chosen by Ada Lovelace · Sep 15, 2026".
    ///
    /// `nil` unless a member chose it: a strategy's pick has no chooser and no time, so it is
    /// never attributed to anyone.
    static func choiceAttribution(
        _ ingredient: SpecialtyIngredient, members: [HouseholdMember]?, currentUserID: String?,
        locale: Locale = .autoupdatingCurrent, timeZone: TimeZone = .autoupdatingCurrent
    ) -> String? {
        guard ingredient.hasHouseholdChoice, let choice = ingredient.choice, let userID = choice.chosenBy
        else { return nil }
        let name = AutopilotFormat.memberName(userID, members: members, currentUserID: currentUserID)
        guard let chosenAt = choice.chosenAt else { return String(localized: "Chosen by \(name)") }
        let date = chosenAt.formatted(
            Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone))
        return String(localized: "Chosen by \(name) · \(date)")
    }

    /// Why an ingredient nobody chose for still has a plan; `nil` when a member chose it.
    static func strategyNote(_ ingredient: SpecialtyIngredient) -> String? {
        guard ingredient.isResolvedByStrategy else { return nil }
        return String(localized: "Nobody chose this — your household default picked it.")
    }

    /// "Changed by Ada Lovelace · Sep 15, 2026"; `nil` when nobody has ever set the strategy.
    static func strategyAttribution(
        _ settings: SpecialtySettings?, members: [HouseholdMember]?, currentUserID: String?,
        locale: Locale = .autoupdatingCurrent, timeZone: TimeZone = .autoupdatingCurrent
    ) -> String? {
        guard let settings, let userID = settings.updatedBy else { return nil }
        let name = AutopilotFormat.memberName(userID, members: members, currentUserID: currentUserID)
        guard let updatedAt = settings.updatedAt else { return String(localized: "Changed by \(name)") }
        let date = updatedAt.formatted(
            Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone))
        return String(localized: "Changed by \(name) · \(date)")
    }

    /// The house-made batch in the pantry, when a batch is chosen or one exists.
    static func batchStatus(_ ingredient: SpecialtyIngredient) -> String? {
        guard let stock = ingredient.batch else {
            return ingredient.hasBatchChoice ? String(localized: "No batch in the pantry yet") : nil
        }
        switch stock.status {
        case .inStock:
            return stock.remaining.map { String(localized: "In pantry, about \($0.text) left") }
                ?? String(localized: "In pantry")
        case .low:
            return stock.remaining.map { String(localized: "Running low, about \($0.text) left") }
                ?? String(localized: "Running low")
        case .out:
            return String(localized: "Out, time to make another batch")
        }
    }

    /// "Replaces 1 tbsp Tex-Mex Paste" above a store alternative's ingredients.
    static func replaces(_ option: SpecialtyOption, specialtyName: String) -> String? {
        guard let per = option.per else { return nil }
        return String(localized: "Replaces \(per.text) \(specialtyName)")
    }

    /// "1 day" or "180 days", for how long a batch keeps.
    static func days(_ days: Int) -> String {
        days == 1 ? String(localized: "1 day") : String(localized: "\(days) days")
    }

    /// "Needed for Chili Bowls and Smoky Pork Tacos", or `nil` without recipes.
    static func neededFor(_ recipes: [GroceryRecipe], locale: Locale = .autoupdatingCurrent) -> String? {
        guard !recipes.isEmpty else { return nil }
        let names = recipes.map(\.name).formatted(.list(type: .and).locale(locale))
        return String(localized: "Needed for \(names)")
    }

    /// Why the list asks to make a batch, with the week's amounts when known.
    static func batchDetail(_ batch: GroceryBatch) -> String {
        var parts: [String] = []
        switch batch.reason {
        case .missing: parts.append(String(localized: "Not in the pantry yet."))
        case .out: parts.append(String(localized: "The pantry batch is out."))
        case .low: parts.append(String(localized: "The pantry batch is running low."))
        case .notEnough: parts.append(String(localized: "Not enough left for this week."))
        case .enough: parts.append(String(localized: "The pantry batch covers this week."))
        case .inStock: parts.append(String(localized: "In the pantry."))
        default: break
        }
        if let needed = batch.needed {
            parts.append(String(localized: "This week needs \(needed.text)."))
        }
        if let remaining = batch.remaining {
            parts.append(String(localized: "About \(remaining.text) left."))
        }
        return parts.joined(separator: " ")
    }

    /// "1 batch (12 tbsp)" or "2 batches (12 tbsp each)", for confirming "Made It".
    static func batchCount(_ count: Int, yield amount: GroceryAmount?) -> String {
        switch (count, amount) {
        case (1, let amount?): String(localized: "1 batch (\(amount.text))")
        case (1, nil): String(localized: "1 batch")
        case (_, let amount?): String(localized: "\(count) batches (\(amount.text) each)")
        case (_, nil): String(localized: "\(count) batches")
        }
    }
}
