import Foundation
import Testing

@testable import DinnerOS

struct MenuFormattingTests {
    private let locale = Locale(identifier: "en_US")

    @Test func factsRowJoinsKnownValues() {
        #expect(
            MenuFormat.factsText(minutes: 30, calories: 690, proteinGrams: 36, locale: locale)
                == "30 min · 690 cal · 36g protein")
        #expect(MenuFormat.factsText(minutes: nil, calories: 1_250, proteinGrams: nil, locale: locale) == "1,250 cal")
        #expect(MenuFormat.factsText(minutes: 75, calories: 0, proteinGrams: nil, locale: locale) == "1 hr, 15 min")
        #expect(MenuFormat.factsText(minutes: nil, calories: nil, proteinGrams: nil, locale: locale).isEmpty)
    }

    @Test func factsRowUsesTheSummarysFields() {
        var summary = RecipePreviewData.summaries[0]
        summary.cookMinutes = 25
        summary.calories = 540
        summary.proteinGrams = 31
        #expect(MenuFormat.factsText(for: summary) == "25 min · 540 cal · 31g protein")
    }

    @Test func cardAccessibilityLabelReadsNaturally() {
        #expect(
            MenuFormat.cardAccessibilityLabel(name: "Creamy Dill Pork Filet", minutes: 30, calories: 690, inPlan: true)
                == "Creamy Dill Pork Filet, 30 minutes, 690 calories, in your week")
        #expect(
            MenuFormat.cardAccessibilityLabel(name: "Toast", minutes: nil, calories: nil, inPlan: false) == "Toast")
    }

    @Test func yourMealsSubtitle() {
        #expect(
            MenuFormat.yourMealsSubtitle(total: 4, fromAutopilot: 4)
                == "Autopilot picked 4 meals based on your preferences")
        #expect(
            MenuFormat.yourMealsSubtitle(total: 1, fromAutopilot: 1)
                == "Autopilot picked 1 meal based on your preferences")
        #expect(MenuFormat.yourMealsSubtitle(total: 5, fromAutopilot: 3) == "5 meals planned · 3 from Autopilot")
        #expect(MenuFormat.yourMealsSubtitle(total: 2, fromAutopilot: 0) == "2 meals planned")
        #expect(MenuFormat.yourMealsSubtitle(total: 0, fromAutopilot: 0) == "Nothing planned yet")
    }

    @Test func bottomBarAndPlanCounts() {
        #expect(MenuFormat.bottomBarTitle(count: 4, timing: .current) == "4 meals this week")
        #expect(MenuFormat.bottomBarTitle(count: 1, timing: .current) == "1 meal this week")
        #expect(MenuFormat.bottomBarTitle(count: 3, timing: .upcoming) == "3 meals planned")
        #expect(MenuFormat.planCount(1, servings: 2) == "1 in your week (2 servings)")
        #expect(MenuFormat.planCount(2, servings: 1) == "2 in your week (1 serving)")
    }

    @Test func weekPills() throws {
        let current = try #require(ISOWeek("2026-W38"))
        #expect(MenuFormat.relativeWeekName(current, current: current) == "This Week")
        #expect(MenuFormat.relativeWeekName(current.next, current: current) == "Next Week")
        #expect(MenuFormat.relativeWeekName(current.previous, current: current) == "Last Week")
        #expect(MenuFormat.relativeWeekName(current.adding(weeks: 2), current: current) == nil)

        let past = WeekSummary(week: "2026-W37", timing: .past, plannedCount: 4, cookedCount: 3, orderedCount: 1)
        #expect(MenuFormat.weekPillDetail(past, timing: .past) == "3 cooked")
        let upcoming = WeekSummary(week: "2026-W39", timing: .upcoming, plannedCount: 1)
        #expect(MenuFormat.weekPillDetail(upcoming, timing: .upcoming) == "1 meal")
        let ordered = WeekSummary(week: "2026-W30", timing: .past, orderedCount: 1)
        #expect(MenuFormat.weekPillDetail(ordered, timing: .past) == "Ordered")
        #expect(MenuFormat.weekPillDetail(WeekSummary(week: "2026-W40", timing: .upcoming), timing: .upcoming) == nil)
        #expect(MenuFormat.weekPillDetail(nil, timing: .current) == nil)
        #expect(MenuFormat.weekHistoryDetail(past) == "4 planned · 3 cooked · 1 delivery")
    }

    @Test func servingSteps() {
        #expect(ServingSizes.step(from: 2, options: [4, 2, 6], by: 1) == 4)
        #expect(ServingSizes.step(from: 6, options: [2, 4, 6], by: 1) == nil)
        #expect(ServingSizes.step(from: 4, options: [2, 4], by: -1) == 2)
        #expect(ServingSizes.step(from: 2, options: [2, 4], by: -1) == nil)
        // A size the recipe no longer offers moves to the nearest one in that direction.
        #expect(ServingSizes.step(from: 3, options: [2, 4], by: 1) == 4)
        #expect(ServingSizes.step(from: 3, options: [2, 4], by: -1) == 2)
        #expect(ServingSizes.step(from: 2, options: [2, 4], by: 0) == nil)
    }

    @Test func recipeStatsLeaveOutUnknownValues() {
        let stats = MenuFormat.stats(minutes: 40, calories: 820, proteinGrams: 43, difficulty: "Easy")
        #expect(stats.map(\.title) == ["Total Time", "Calories", "Protein", "Difficulty"])
        #expect(stats.map(\.value) == ["40 min", "820 Cal", "43g", "Easy"])
        #expect(stats.first?.accessibilityValue == "40 minutes")

        let partial = MenuFormat.stats(minutes: nil, calories: 0, proteinGrams: 12, difficulty: nil)
        #expect(partial.map(\.id) == ["protein"])
    }

    @Test func recipeNutritionComesFromTheListedValues() {
        var recipe = RecipePreviewData.recipe
        #expect(recipe.calories == 640)
        #expect(recipe.proteinGrams == 33)

        recipe.nutrition = [RecipeNutrient(name: "Protein", amount: 1_200, unit: "mg")]
        #expect(recipe.nutritionValues.count == 1)
        #expect(recipe.calories == nil)
        // Protein in milligrams isn't read as grams.
        #expect(recipe.proteinGrams == nil)
    }

    @Test func nutritionFollowsLabelOrder() {
        let nutrients = [
            RecipeNutrient(name: "Sodium", amount: 900, unit: "mg"),
            RecipeNutrient(name: "Iron", amount: 2, unit: "mg"),
            RecipeNutrient(name: "Protein", amount: 36, unit: "g"),
            RecipeNutrient(name: "Calories", amount: 690, unit: "kcal"),
            RecipeNutrient(name: "Potassium", amount: 700, unit: "mg"),
            RecipeNutrient(name: "Saturated Fat", amount: 8.5, unit: "g"),
            RecipeNutrient(name: "Fat", amount: 30, unit: "g"),
        ]
        #expect(
            MenuFormat.orderedNutrition(nutrients).map(\.name)
                == ["Calories", "Fat", "Saturated Fat", "Protein", "Sodium", "Iron", "Potassium"])
        #expect(MenuFormat.nutrientAmount(nutrients[5], locale: locale) == "8.5 g")
        #expect(
            MenuFormat.nutrientAmount(RecipeNutrient(name: "Sodium", amount: 1_250, unit: "mg"), locale: locale)
                == "1,250 mg")
    }
}
