import Foundation

/// How full an estimated item is, for its indicator color.
nonisolated enum PantryEstimateLevel: Equatable, Sendable {
    case plenty
    /// At or past the threshold.
    case low
    /// Nothing left.
    case empty
}

/// Display text for usage estimates, purchases, and thresholds. The API's `summary` is
/// English; these build the short, localizable pieces from the numbers.
nonisolated enum PantryUsageFormat {
    /// "~31% left (2.5 oz)", for a pantry row: the percentage alone doesn't say how much
    /// that is, and a package count ("1 package") doesn't either.
    static func remainingShort(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        let percentText = percent(estimate.percentRemaining, locale: locale)
        guard let left = remainingGlance(estimate, locale: locale) else {
            return String(localized: "~\(percentText) left")
        }
        return String(localized: "~\(percentText) left (\(left))")
    }

    /// "About 31% left, about 2.5 oz", for VoiceOver.
    static func remainingSpoken(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        let percentText = percent(estimate.percentRemaining, locale: locale)
        guard let left = remainingGlance(estimate, locale: locale) else {
            return String(localized: "About \(percentText) left")
        }
        return String(localized: "About \(percentText) left, about \(left)")
    }

    /// The remaining amount rounded for a glance: "8 oz", "2.5 oz", "0.75 lb". An estimate
    /// isn't exact, so "5.33 oz" would claim precision it doesn't have. `nil` when there is
    /// no amount to show, or it would only repeat a bare count.
    static func remainingGlance(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String? {
        let value = estimate.remaining.quantityValue
        guard value > 0, !estimate.unit.isEmpty else { return nil }
        // Under 2, to the nearest quarter: ¾ lb is 12 oz, and rounding it to "1 lb" claimed
        // a third more than was there. Above that, to the nearest half.
        let step = value < 2 ? 4.0 : 2.0
        let rounded = (value * step).rounded() / step
        let shown = rounded > 0 ? rounded : value
        let number = shown.formatted(.number.precision(.fractionLength(0...2)).locale(locale))
        let label = RecipeFormat.unitLabel(estimate.unit, sourceUnit: estimate.unit, plural: shown != 1)
        return label.isEmpty ? nil : "\(number) \(label)"
    }

    /// "5 tbsp of 16 tbsp".
    static func remainingAmount(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        let remaining = amount(
            estimate.remaining.quantity, value: estimate.remaining.quantityValue, unit: estimate.unit)
        let start = amount(
            estimate.startAmount.quantity, value: estimate.startAmount.quantityValue, unit: estimate.unit)
        return String(localized: "\(remaining) of \(start)")
    }

    static func level(_ estimate: PantryEstimate) -> PantryEstimateLevel {
        if estimate.percentRemaining <= 0 { return .empty }
        return estimate.belowThreshold ? .low : .plenty
    }

    /// "2 recipes used 6 tbsp", or that none were counted yet.
    static func recipeUse(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        let use = estimate.recipeUse
        let used = amount(use.quantity, value: use.quantityValue, unit: estimate.unit, locale: locale)
        switch use.count {
        case 0: return String(localized: "No cooked recipes yet")
        case 1: return String(localized: "1 recipe used \(used)")
        default: return String(localized: "\(use.count) recipes used \(used)")
        }
    }

    /// "5 tbsp": non-recipe use applied so far this cycle. This is what the Other Use row
    /// counts; `dailyRate` is only the rate it was applied at.
    static func otherUse(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        amount(
            estimate.otherUse.quantity, value: estimate.otherUse.quantityValue, unit: estimate.unit, locale: locale)
    }

    /// "About 1 tbsp a day", or that there isn't enough history yet.
    static func dailyRate(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        guard let rate = estimate.dailyRate else { return String(localized: "Not enough history yet") }
        let perDay = amount(rate.quantity, value: rate.quantityValue, unit: estimate.unit, locale: locale)
        return String(localized: "About \(perDay) a day")
    }

    /// "1 recipe couldn't be counted", or `nil` when every recipe was.
    static func skippedRecipes(_ estimate: PantryEstimate) -> String? {
        switch estimate.skippedRecipes {
        case ...0: nil
        case 1: String(localized: "1 recipe couldn't be counted")
        default: String(localized: "\(estimate.skippedRecipes) recipes couldn't be counted")
        }
    }

    /// What a pantry row says about an item whose estimate had to skip a cooked recipe, so
    /// "~31% left" on an item that couldn't be counted doesn't read the same as one that was.
    /// `nil` when every recipe was counted.
    static func skippedRecipesShort(_ estimate: PantryEstimate) -> String? {
        guard estimate.skippedRecipes > 0 else { return nil }
        return String(localized: "some use not counted")
    }

    /// "80% used".
    static func threshold(_ percentUsed: Int, locale: Locale = .autoupdatingCurrent) -> String {
        String(localized: "\(percent(percentUsed, locale: locale)) used")
    }

    static func thresholdSource(_ source: PantryThresholdSource) -> String {
        source == .item ? String(localized: "Set for this item") : String(localized: "Household setting")
    }

    /// "Estimated Low" when the estimate set the status, otherwise the status title.
    static func statusTitle(_ item: PantryItem) -> String {
        item.isEstimatedLow ? String(localized: "Estimated Low") : item.status.title
    }

    /// "1 package = 8 oz".
    static func unitSize(_ size: PantryUnitSize, locale: Locale = .autoupdatingCurrent) -> String {
        let per = RecipeFormat.unitLabel(size.per, sourceUnit: size.per, plural: false)
        let one = per.isEmpty ? String(localized: "1 item") : "1 \(per)"
        return "\(one) = \(amount(size.quantity, value: size.quantityValue, unit: size.unit, locale: locale))"
    }

    // MARK: Purchases

    /// "2 packages, 8 oz each", "1½ cups", or that no amount was recorded.
    static func purchaseAmount(_ purchase: PantryPurchase, locale: Locale = .autoupdatingCurrent) -> String {
        guard let quantity = purchase.quantity, let unit = purchase.unit else {
            return String(localized: "No amount recorded")
        }
        let bought = amount(quantity, value: purchase.quantityValue, unit: unit, locale: locale)
        guard let size = purchase.unitSize else { return bought }
        let each = amount(size.quantity, value: size.quantityValue, unit: size.unit, locale: locale)
        return String(localized: "\(bought), \(each) each")
    }

    static func purchaseSource(_ source: PantryPurchaseSource) -> String {
        switch source {
        case .groceryList: String(localized: "Grocery list")
        case .manual: String(localized: "Restocked")
        case .provider: String(localized: "Online order")
        case .houseMade: String(localized: "House-made batch")
        default: String(localized: "Purchase")
        }
    }

    // MARK: Helpers

    /// An amount with its unit; a count shows the number alone.
    static func amount(
        _ quantity: String, value: Double?, unit: String, locale: Locale = .autoupdatingCurrent
    ) -> String {
        let number = RecipeFormat.quantity(quantity, value: value, locale: locale) ?? quantity
        let label = RecipeFormat.unitLabel(unit, sourceUnit: unit, plural: RecipeFormat.isPlural(value))
        return label.isEmpty ? number : "\(number) \(label)"
    }

    private static func percent(_ value: Int, locale: Locale) -> String {
        (Double(value) / 100).formatted(.percent.precision(.fractionLength(0)).locale(locale))
    }
}
