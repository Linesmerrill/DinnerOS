import Foundation

/// An ISO 8601 week such as `2026-W37`, the unit the API uses for order history.
nonisolated struct ISOWeek: Hashable, Sendable, Comparable {
    let year: Int
    let week: Int

    /// `nil` unless `string` is `YYYY-Www` naming a week that exists in that year.
    init?(_ string: String) {
        let parts = string.split(separator: "-W", omittingEmptySubsequences: false)
        guard
            parts.count == 2, parts[0].count == 4, parts[1].count == 2,
            let year = Int(parts[0]), let week = Int(parts[1]), (1...53).contains(week)
        else { return nil }
        self.year = year
        self.week = week
        // Week 53 only exists in some years.
        guard let monday = startDate, Self.calendar.component(.weekOfYear, from: monday) == week else { return nil }
    }

    /// Midnight UTC on the Monday that starts the week.
    var startDate: Date? {
        Self.calendar.date(
            from: DateComponents(weekday: 2, weekOfYear: week, yearForWeekOfYear: year))
    }

    /// Short month and year, for example "Sep 2026". Uses the week's Thursday, which
    /// always falls in the week's ISO year, so week 1 never reads as December.
    func monthAndYear(locale: Locale = .autoupdatingCurrent) -> String {
        guard let thursday = startDate.map({ $0.addingTimeInterval(3 * 86_400) }) else { return description }
        return thursday.formatted(Self.style(locale: locale).month(.abbreviated).year())
    }

    /// For example "Week of Sep 7, 2026".
    func weekOf(locale: Locale = .autoupdatingCurrent) -> String {
        guard let monday = startDate else { return description }
        let date = monday.formatted(Self.style(locale: locale).month(.abbreviated).day().year())
        return String(localized: "Week of \(date)")
    }

    static func < (lhs: ISOWeek, rhs: ISOWeek) -> Bool {
        (lhs.year, lhs.week) < (rhs.year, rhs.week)
    }

    var description: String {
        "\(year)-W" + (week < 10 ? "0\(week)" : "\(week)")
    }

    private static let calendar: Calendar = {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = .gmt
        return calendar
    }()

    private static func style(locale: Locale) -> Date.FormatStyle {
        // Dates are UTC midnights, so format in UTC to keep the day.
        Date.FormatStyle(date: .omitted, time: .omitted, locale: locale, timeZone: .gmt)
    }
}

/// Display text for recipe values. Amounts come from the API per serving size and are
/// only formatted here, never scaled.
nonisolated enum RecipeFormat {
    // MARK: Quantities

    /// Kitchen-friendly text for an exact quantity: "2", "½", "1¾", or a short decimal
    /// when the fraction isn't a common one. Falls back to `value` when `quantity` can't
    /// be parsed. `nil` when there's no amount.
    static func quantity(_ quantity: String?, value: Double?, locale: Locale = .autoupdatingCurrent) -> String? {
        if let quantity, let fraction = Fraction(quantity) {
            return format(fraction, locale: locale)
        }
        return value.map { decimal($0, locale: locale) }
    }

    private static let glyphs: [Fraction: String] = [
        Fraction(1, 2): "½", Fraction(1, 3): "⅓", Fraction(2, 3): "⅔", Fraction(1, 4): "¼", Fraction(3, 4): "¾",
        Fraction(1, 5): "⅕", Fraction(2, 5): "⅖", Fraction(3, 5): "⅗", Fraction(4, 5): "⅘", Fraction(1, 6): "⅙",
        Fraction(5, 6): "⅚", Fraction(1, 8): "⅛", Fraction(3, 8): "⅜", Fraction(5, 8): "⅝", Fraction(7, 8): "⅞",
    ]

    private static func format(_ fraction: Fraction, locale: Locale) -> String {
        let whole = fraction.numerator / fraction.denominator
        let remainder = Fraction(fraction.numerator % fraction.denominator, fraction.denominator)
        if remainder.numerator == 0 {
            return whole.formatted(.number.locale(locale))
        }
        guard let glyph = glyphs[remainder] else {
            return decimal(Double(fraction.numerator) / Double(fraction.denominator), locale: locale)
        }
        return whole == 0 ? glyph : whole.formatted(.number.locale(locale)) + glyph
    }

    private static func decimal(_ value: Double, locale: Locale) -> String {
        value.formatted(.number.precision(.fractionLength(0...2)).locale(locale))
    }

    /// A reduced, non-negative fraction parsed from `"n"` or `"n/d"`.
    private struct Fraction: Hashable {
        let numerator: Int
        let denominator: Int

        init(_ numerator: Int, _ denominator: Int) {
            let divisor = Self.gcd(numerator, denominator)
            self.numerator = divisor == 0 ? numerator : numerator / divisor
            self.denominator = divisor == 0 ? denominator : denominator / divisor
        }

        init?(_ string: String) {
            let parts = string.trimmingCharacters(in: .whitespaces).split(
                separator: "/", omittingEmptySubsequences: false)
            guard (1...2).contains(parts.count), let numerator = Int(parts[0]), numerator >= 0 else { return nil }
            let denominator = parts.count == 2 ? Int(parts[1]) : 1
            guard let denominator, denominator > 0 else { return nil }
            self.init(numerator, denominator)
        }

        private static func gcd(_ a: Int, _ b: Int) -> Int {
            b == 0 ? a : gcd(b, a % b)
        }
    }

    // MARK: Units

    /// Whether a unit label should be plural for `value`.
    ///
    /// Only exactly one is singular: "0 cups", "½ cup", "1 cup", "1½ cups". A `> 1` test
    /// reads zero as singular and renders "0 cup". `nil` keeps the singular label, because
    /// an amount with no number to speak of has nothing to agree with.
    static func isPlural(_ value: Double?) -> Bool {
        guard let value else { return false }
        return value == 0 || value > 1
    }

    /// A label for a DinnerOS unit code. `count` has no label ("2 onions"). An unknown
    /// or empty code shows `sourceUnit` as written.
    static func unitLabel(_ unit: String, sourceUnit: String = "", plural: Bool) -> String {
        switch unit {
        case "count": ""
        case "clove": plural ? String(localized: "cloves") : String(localized: "clove")
        case "can": plural ? String(localized: "cans") : String(localized: "can")
        case "package": plural ? String(localized: "packages") : String(localized: "package")
        case "slice": plural ? String(localized: "slices") : String(localized: "slice")
        case "bunch": plural ? String(localized: "bunches") : String(localized: "bunch")
        case "pinch": plural ? String(localized: "pinches") : String(localized: "pinch")
        case "thumb": plural ? String(localized: "thumbs") : String(localized: "thumb")
        case "cup": plural ? String(localized: "cups") : String(localized: "cup")
        case "tsp": String(localized: "tsp")
        case "tbsp": String(localized: "tbsp")
        case "floz": String(localized: "fl oz")
        case "oz": String(localized: "oz")
        case "lb": String(localized: "lb")
        case "g": String(localized: "g")
        case "kg": String(localized: "kg")
        case "ml": String(localized: "mL")
        case "l": String(localized: "L")
        default: sourceUnit
        }
    }

    /// Quantity and unit together, for example "½ oz" or "2 cloves". `nil` when the
    /// source gave no amount ("salt, to taste").
    static func amount(_ amount: RecipeAmount, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let quantity = quantity(amount.quantity, value: amount.quantityValue, locale: locale) else {
            return nil
        }
        let unit = unitLabel(amount.unit, sourceUnit: amount.sourceUnit, plural: isPlural(amount.quantityValue))
        return unit.isEmpty ? quantity : "\(quantity) \(unit)"
    }

    // MARK: Other values

    /// The minutes to show for a recipe. The API's `cookMinutes` wins because sources report
    /// total time inconsistently (often below prep). Without it, the larger of prep and total
    /// time, which is how the API derives it. `nil` when nothing positive is known.
    static func displayMinutes(cook: Int?, prep: Int?, total: Int?) -> Int? {
        if let cook, cook > 0 { return cook }
        let known = [prep, total].compactMap { $0 }.filter { $0 > 0 }
        return known.max()
    }

    /// For example "30 min" or "1 hr, 15 min".
    static func minutes(_ minutes: Int) -> String {
        Duration.seconds(minutes * 60).formatted(.units(allowed: [.hours, .minutes], width: .abbreviated))
    }

    static func timesOrdered(_ count: Int) -> String {
        switch count {
        case 0: String(localized: "Not ordered yet")
        case 1: String(localized: "Ordered once")
        default: String(localized: "Ordered \(count) times")
        }
    }

    /// HelloFresh rates difficulty 1–3. Other sources show the raw level.
    static func difficulty(_ level: Int, source: String) -> String {
        switch (source, level) {
        case ("hellofresh", 1): String(localized: "Easy")
        case ("hellofresh", 2): String(localized: "Medium")
        case ("hellofresh", 3): String(localized: "Hard")
        default: String(localized: "Difficulty \(level)")
        }
    }
}

// MARK: - Servings

/// One ingredient as shown for a chosen serving size.
nonisolated struct IngredientLine: Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    /// `nil` when the recipe has no amount for this ingredient at this size.
    let amount: String?
    let isPantryStaple: Bool
    let category: String
    var imageURL: URL? = nil
    /// For example `["Soy", "Wheat"]`; empty when unknown.
    var allergens: [String] = []
}

extension Recipe {
    /// Serving sizes the recipe was authored for, ascending. Falls back to the sizes
    /// found in ingredient amounts when `servings` is empty.
    nonisolated var servingOptions: [Int] {
        let sizes = servings.isEmpty ? ingredients.flatMap { $0.amounts.map(\.servings) } : servings
        return Array(Set(sizes.filter { $0 > 0 })).sorted()
    }

    /// The household's default when the recipe offers it, else the smallest larger
    /// size, else the largest. `nil` when the recipe has no sizes.
    nonisolated func preferredServings(householdDefault: Int?) -> Int? {
        let options = servingOptions
        guard let target = householdDefault else { return options.first }
        return options.first { $0 >= target } ?? options.last
    }

    /// Ingredients with the amounts authored for `servings`. Amounts are never derived
    /// from another size: a size without an authored amount shows none.
    nonisolated func ingredientLines(servings: Int, locale: Locale = .autoupdatingCurrent) -> [IngredientLine] {
        ingredients.enumerated().map { index, ingredient in
            IngredientLine(
                id: "\(index)-\(ingredient.ingredientID)",
                name: ingredient.name,
                amount: ingredient.amounts.first { $0.servings == servings }
                    .flatMap { RecipeFormat.amount($0, locale: locale) },
                isPantryStaple: ingredient.pantryStaple,
                category: ingredient.category,
                imageURL: ingredient.imageURL,
                allergens: ingredient.allergens)
        }
    }
}
