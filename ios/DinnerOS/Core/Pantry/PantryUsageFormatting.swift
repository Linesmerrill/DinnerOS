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
    /// "~31% left", for a pantry row.
    static func remainingShort(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        String(localized: "~\(percent(estimate.percentRemaining, locale: locale)) left")
    }

    /// "About 31% left", for VoiceOver.
    static func remainingSpoken(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        String(localized: "About \(percent(estimate.percentRemaining, locale: locale)) left")
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

    /// "About 1 tbsp a day", or that there isn't enough history yet.
    static func dailyRate(_ estimate: PantryEstimate, locale: Locale = .autoupdatingCurrent) -> String {
        guard let rate = estimate.dailyRate else { return String(localized: "Not enough history yet") }
        let perDay = amount(rate.quantity, value: rate.quantityValue, unit: estimate.unit, locale: locale)
        return String(localized: "About \(perDay) a day")
    }

    /// "Learned from 3 earlier periods", or `nil` without a rate.
    static func dailyRateBasis(_ estimate: PantryEstimate) -> String? {
        guard let rate = estimate.dailyRate else { return nil }
        return String(localized: "Learned from \(rate.basedOnSegments) earlier periods")
    }

    /// "1 recipe couldn't be counted", or `nil` when every recipe was.
    static func skippedRecipes(_ estimate: PantryEstimate) -> String? {
        switch estimate.skippedRecipes {
        case ...0: nil
        case 1: String(localized: "1 recipe couldn't be counted")
        default: String(localized: "\(estimate.skippedRecipes) recipes couldn't be counted")
        }
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
        default: String(localized: "Purchase")
        }
    }

    // MARK: Helpers

    /// An amount with its unit; a count shows the number alone.
    static func amount(
        _ quantity: String, value: Double?, unit: String, locale: Locale = .autoupdatingCurrent
    ) -> String {
        let number = RecipeFormat.quantity(quantity, value: value, locale: locale) ?? quantity
        let label = RecipeFormat.unitLabel(unit, sourceUnit: unit, plural: (value ?? 0) > 1)
        return label.isEmpty ? number : "\(number) \(label)"
    }

    private static func percent(_ value: Int, locale: Locale) -> String {
        (Double(value) / 100).formatted(.percent.precision(.fractionLength(0)).locale(locale))
    }
}
