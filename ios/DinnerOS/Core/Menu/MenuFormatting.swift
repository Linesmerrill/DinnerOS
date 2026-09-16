import Foundation

/// Display text for the Menu screen and the recipe screen's facts. Numbers come from the API
/// and are only formatted here.
nonisolated enum MenuFormat {
    // MARK: Facts

    /// Spoken facts, for example `["30 minutes", "690 calories", "36 grams of protein"]`.
    static func spokenFacts(minutes: Int?, calories: Int?, proteinGrams: Int?) -> [String] {
        var parts: [String] = []
        if let minutes, minutes > 0 {
            parts.append(
                Duration.seconds(minutes * 60).formatted(.units(allowed: [.hours, .minutes], width: .wide)))
        }
        if let calories, calories > 0 {
            parts.append(String(localized: "\(calories) calories"))
        }
        if let proteinGrams, proteinGrams > 0 {
            parts.append(String(localized: "\(proteinGrams) grams of protein"))
        }
        return parts
    }

    /// For example "One-Pan Santa Fe Pork Tacos, 20 minutes, quick, often ordered, in your week".
    ///
    /// A card shows only its name and the badges over the photo, so everything those badges
    /// say has to be spoken here instead.
    static func cardAccessibilityLabel(
        name: String, minutes: Int?, isQuick: Bool = false, badge: String? = nil, isAddOn: Bool = false,
        inPlan: Bool
    ) -> String {
        var parts = [name]
        parts += spokenFacts(minutes: minutes, calories: nil, proteinGrams: nil)
        if isQuick {
            parts.append(String(localized: "quick"))
        }
        if let badge, !badge.isEmpty {
            parts.append(badge.lowercased())
        }
        if isAddOn {
            parts.append(String(localized: "add-on"))
        }
        if inPlan {
            parts.append(String(localized: "in your week"))
        }
        return parts.joined(separator: ", ")
    }

    // MARK: Your Meals

    /// "Autopilot picked 4 meals based on your preferences" when every meal came from Autopilot,
    /// "5 meals planned · 3 from Autopilot" when some did, else "5 meals planned".
    static func yourMealsSubtitle(total: Int, fromAutopilot: Int) -> String {
        if total > 0, fromAutopilot == total {
            return fromAutopilot == 1
                ? String(localized: "Autopilot picked 1 meal based on your preferences")
                : String(localized: "Autopilot picked \(fromAutopilot) meals based on your preferences")
        }
        let planned = mealsPlanned(total)
        guard fromAutopilot > 0 else { return planned }
        return planned + " · " + String(localized: "\(fromAutopilot) from Autopilot")
    }

    static func mealsPlanned(_ count: Int) -> String {
        switch count {
        case 0: String(localized: "Nothing planned yet")
        case 1: String(localized: "1 meal planned")
        default: String(localized: "\(count) meals planned")
        }
    }

    /// "1 add-on" or "3 add-ons".
    static func addOns(_ count: Int) -> String {
        count == 1 ? String(localized: "1 add-on") : String(localized: "\(count) add-ons")
    }

    /// Your Meals' subtitle, counting main meals only and naming the week's add-ons separately:
    /// "5 meals planned · 2 from Autopilot · 1 add-on".
    static func yourMealsSubtitle(counts: MealCounts) -> String {
        let base = yourMealsSubtitle(total: counts.meals, fromAutopilot: counts.mealsFromAutopilot)
        guard counts.addOns > 0 else { return base }
        return base + " · " + addOns(counts.addOns)
    }

    /// The bottom bar's title, counting main meals only: "5 meals this week", or "5 meals ·
    /// 1 add-on" when add-ons are planned alongside them.
    static func bottomBarTitle(counts: MealCounts, timing: WeekTiming) -> String {
        guard counts.addOns > 0 else { return bottomBarTitle(count: counts.meals, timing: timing) }
        let meals =
            counts.meals == 1 ? String(localized: "1 meal") : String(localized: "\(counts.meals) meals")
        return meals + " · " + addOns(counts.addOns)
    }

    /// The bottom bar's title: "4 meals this week" for this week, "4 meals planned" otherwise.
    static func bottomBarTitle(count: Int, timing: WeekTiming) -> String {
        guard timing == .current else { return mealsPlanned(count) }
        return count == 1 ? String(localized: "1 meal this week") : String(localized: "\(count) meals this week")
    }

    /// For example "1 in your week (2 servings)".
    static func planCount(_ count: Int, servings: Int) -> String {
        let servingsText =
            servings == 1 ? String(localized: "1 serving") : String(localized: "\(servings) servings")
        return String(localized: "\(count) in your week (\(servingsText))")
    }

    static func servings(_ count: Int) -> String {
        count == 1 ? String(localized: "1 serving") : String(localized: "\(count) servings")
    }

    // MARK: Weeks

    /// "This Week", "Next Week", or "Last Week"; `nil` for other weeks.
    static func relativeWeekName(_ week: ISOWeek, current: ISOWeek) -> String? {
        switch week {
        case current: String(localized: "This Week")
        case current.next: String(localized: "Next Week")
        case current.previous: String(localized: "Last Week")
        default: nil
        }
    }

    /// A week pill's small line: cooked meals for a past week, planned meals otherwise. `nil`
    /// when there's nothing to say.
    static func weekPillDetail(_ summary: WeekSummary?, timing: WeekTiming) -> String? {
        guard let summary else { return nil }
        if timing == .past, summary.cookedCount > 0 {
            return String(localized: "\(summary.cookedCount) cooked")
        }
        switch summary.plannedCount {
        case 0: return timing == .past && summary.orderedCount > 0 ? String(localized: "Ordered") : nil
        case 1: return String(localized: "1 meal")
        default: return String(localized: "\(summary.plannedCount) meals")
        }
    }

    /// For the Past Weeks list, for example "4 planned · 3 cooked · Ordered".
    static func weekHistoryDetail(_ summary: WeekSummary?) -> String {
        guard let summary else { return String(localized: "No details") }
        var parts: [String] = []
        parts.append(
            summary.plannedCount == 0
                ? String(localized: "Nothing planned") : String(localized: "\(summary.plannedCount) planned"))
        if summary.cookedCount > 0 {
            parts.append(String(localized: "\(summary.cookedCount) cooked"))
        }
        if summary.orderedCount > 0 {
            parts.append(
                summary.orderedCount == 1
                    ? String(localized: "1 delivery") : String(localized: "\(summary.orderedCount) deliveries"))
        }
        return parts.joined(separator: " · ")
    }

    // MARK: Recipe screen

    /// A column of the recipe screen's stats row.
    struct Stat: Hashable, Sendable, Identifiable {
        let id: String
        let title: String
        let systemImage: String
        let value: String
        let accessibilityValue: String
    }

    /// Total Time, Calories, Protein, and Difficulty, leaving out unknown values.
    static func stats(
        minutes: Int?, calories: Int?, proteinGrams: Int?, difficulty: String?
    ) -> [Stat] {
        var stats: [Stat] = []
        if let minutes, minutes > 0 {
            stats.append(
                Stat(
                    id: "time", title: String(localized: "Total Time"), systemImage: "clock",
                    value: RecipeFormat.minutes(minutes),
                    accessibilityValue: spokenFacts(minutes: minutes, calories: nil, proteinGrams: nil).first ?? ""))
        }
        if let calories, calories > 0 {
            stats.append(
                Stat(
                    id: "calories", title: String(localized: "Calories"), systemImage: "flame",
                    value: String(localized: "\(calories) Cal"),
                    accessibilityValue: String(localized: "\(calories) calories")))
        }
        if let proteinGrams, proteinGrams > 0 {
            stats.append(
                Stat(
                    id: "protein", title: String(localized: "Protein"), systemImage: "dumbbell",
                    value: String(localized: "\(proteinGrams)g"),
                    accessibilityValue: String(localized: "\(proteinGrams) grams")))
        }
        if let difficulty, !difficulty.isEmpty {
            stats.append(
                Stat(
                    id: "difficulty", title: String(localized: "Difficulty"), systemImage: "chart.bar",
                    value: difficulty, accessibilityValue: difficulty))
        }
        return stats
    }

    /// Nutrients in label order (Calories, Fat, Saturated Fat, Carbohydrate, Sugar, Dietary Fiber,
    /// Protein, Cholesterol, Sodium), then any others in the order the recipe lists them.
    static func orderedNutrition(_ nutrients: [RecipeNutrient]) -> [RecipeNutrient] {
        let order = [
            "calories", "energy", "fat", "saturated fat", "carbohydrate", "carbohydrates", "sugar", "dietary fiber",
            "fiber", "protein", "cholesterol", "sodium",
        ]
        return nutrients.enumerated()
            .sorted { lhs, rhs in
                let left = order.firstIndex(of: lhs.element.name.lowercased()) ?? order.count
                let right = order.firstIndex(of: rhs.element.name.lowercased()) ?? order.count
                return left == right ? lhs.offset < rhs.offset : left < right
            }
            .map(\.element)
    }

    /// For example "36 g" or "1,250 mg".
    static func nutrientAmount(_ nutrient: RecipeNutrient, locale: Locale = .autoupdatingCurrent) -> String {
        let amount = nutrient.amount.formatted(.number.precision(.fractionLength(0...1)).locale(locale))
        return nutrient.unit.isEmpty ? amount : "\(amount) \(nutrient.unit)"
    }
}

nonisolated extension Recipe {
    /// Calories per serving, rounded; `nil` when the recipe doesn't list them.
    var calories: Int? {
        nutrient(named: ["calories", "energy"]).map { Int($0.amount.rounded()) }
    }

    /// Grams of protein per serving, rounded; `nil` when the recipe doesn't list it.
    var proteinGrams: Int? {
        guard let protein = nutrient(named: ["protein"]), ["g", ""].contains(protein.unit.lowercased()) else {
            return nil
        }
        return Int(protein.amount.rounded())
    }

    private func nutrient(named names: Set<String>) -> RecipeNutrient? {
        nutritionValues.first { names.contains($0.name.lowercased()) && $0.amount > 0 }
    }
}

/// Serving sizes a stepper moves between.
nonisolated enum ServingSizes {
    /// The next supported size after `current` in the direction of `delta`, or `nil` at the end.
    static func step(from current: Int, options: [Int], by delta: Int) -> Int? {
        let sizes = Array(Set(options.filter { $0 > 0 })).sorted()
        if delta > 0 {
            return sizes.first { $0 > current }
        }
        if delta < 0 {
            return sizes.last { $0 < current }
        }
        return nil
    }
}
