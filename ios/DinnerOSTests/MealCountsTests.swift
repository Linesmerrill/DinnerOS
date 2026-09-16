import Foundation
import SwiftUI
import Testing

@testable import DinnerOS

/// Plan entries built without the API, so the counting rules can be checked directly.
private func entry(
    id: String, recipeID: String, name: String, isAddon: Bool = false, origin: PlanEntryOrigin? = .manual
) -> PlanEntry {
    PlanEntry(
        id: id,
        recipe: PlanEntryRecipe(id: recipeID, name: name, imageURLString: nil, isAddon: isAddon),
        day: nil, date: nil, servings: 2, note: "", addedBy: "user-1", addedAt: Date(timeIntervalSince1970: 0),
        origin: origin)
}

struct MealCountsTests {
    private let week = [
        entry(id: "e1", recipeID: "r1", name: "Pasta Bake", origin: .autopilot),
        entry(id: "e2", recipeID: "r2", name: "Skillet Tacos", origin: .autopilot),
        entry(id: "e3", recipeID: "r3", name: "Garden Salad"),
        entry(id: "e4", recipeID: "r4", name: "Sheet-Pan Chicken"),
        entry(id: "e5", recipeID: "r5", name: "Smoked Pork"),
        entry(id: "e6", recipeID: "addon-1", name: "Garlic Bread", isAddon: true),
    ]

    @Test func countsAddOnsApartFromMeals() {
        let counts = MealCounts.of(week) { $0.recipe.isAddon }
        #expect(counts.meals == 5)
        #expect(counts.addOns == 1)
        #expect(counts.total == 6)
    }

    @Test func anAddOnNeverCountsTowardAutopilot() {
        let fromAutopilot = [
            entry(id: "e1", recipeID: "r1", name: "Pasta Bake", origin: .autopilot),
            entry(id: "e2", recipeID: "addon-1", name: "Garlic Bread", isAddon: true, origin: .autopilot),
        ]
        let counts = MealCounts.of(fromAutopilot) { $0.recipe.isAddon }
        #expect(counts.meals == 1)
        #expect(counts.addOns == 1)
        #expect(counts.mealsFromAutopilot == 1)
    }

    @Test func aWeekWithoutAddOnsIsUnchanged() {
        let mains = Array(week.prefix(5))
        let counts = MealCounts.of(mains) { $0.recipe.isAddon }
        #expect(counts == MealCounts(meals: 5, addOns: 0, mealsFromAutopilot: 2))
        #expect(MealCounts.of([]) { $0.recipe.isAddon } == MealCounts())
    }

    @Test func theCallerDecidesWhatCountsAsAnAddOn() {
        // An older server doesn't mark entries, so the menu's card answers instead.
        let unmarked = [entry(id: "e6", recipeID: "addon-1", name: "Garlic Bread")]
        #expect(MealCounts.of(unmarked) { $0.recipe.isAddon }.addOns == 0)
        #expect(MealCounts.of(unmarked) { $0.recipe.id == "addon-1" }.addOns == 1)
    }

    @Test func planEntryRecipeDefaultsToAMealWhenTheServerIsSilent() throws {
        let json = Data(#"{"id":"r1","name":"Pasta Bake","imageUrl":null}"#.utf8)
        let recipe = try JSONDecoder().decode(PlanEntryRecipe.self, from: json)
        #expect(recipe.isAddon == false)

        let marked = Data(#"{"id":"a1","name":"Garlic Bread","isAddon":true}"#.utf8)
        #expect(try JSONDecoder().decode(PlanEntryRecipe.self, from: marked).isAddon)
        // A malformed value is read as "not an add-on" rather than failing the plan.
        let wrong = Data(#"{"id":"a1","name":"Garlic Bread","isAddon":"yes"}"#.utf8)
        #expect(try JSONDecoder().decode(PlanEntryRecipe.self, from: wrong).isAddon == false)
    }

    @Test func weekSummaryReadsAddOnCountLeniently() throws {
        let withCount = Data(
            #"{"week":"2026-W38","timing":"current","plannedCount":5,"addOnCount":1,"cookedCount":0,"orderedCount":0,"status":"draft"}"#
                .utf8)
        let summary = try JSONDecoder().decode(WeekSummary.self, from: withCount)
        #expect(summary.plannedCount == 5)
        #expect(summary.addOnCount == 1)

        // An older response omits the field entirely.
        let without = Data(
            #"{"week":"2026-W38","timing":"current","plannedCount":5,"cookedCount":0,"orderedCount":0,"status":"draft"}"#
                .utf8)
        #expect(try JSONDecoder().decode(WeekSummary.self, from: without).addOnCount == 0)
    }
}

struct MealCountFormattingTests {
    @Test func theBottomBarNamesAddOnsSeparately() {
        #expect(
            MenuFormat.bottomBarTitle(counts: MealCounts(meals: 5, addOns: 1), timing: .current)
                == "5 meals · 1 add-on")
        #expect(
            MenuFormat.bottomBarTitle(counts: MealCounts(meals: 5, addOns: 2), timing: .current)
                == "5 meals · 2 add-ons")
        #expect(
            MenuFormat.bottomBarTitle(counts: MealCounts(meals: 1, addOns: 1), timing: .current)
                == "1 meal · 1 add-on")
    }

    @Test func aWeekWithNoAddOnsReadsAsBefore() {
        #expect(MenuFormat.bottomBarTitle(counts: MealCounts(meals: 5), timing: .current) == "5 meals this week")
        #expect(MenuFormat.bottomBarTitle(counts: MealCounts(meals: 1), timing: .current) == "1 meal this week")
        #expect(MenuFormat.bottomBarTitle(counts: MealCounts(meals: 3), timing: .upcoming) == "3 meals planned")
    }

    @Test func yourMealsSubtitleExcludesAddOns() {
        #expect(
            MenuFormat.yourMealsSubtitle(counts: MealCounts(meals: 5, addOns: 1, mealsFromAutopilot: 2))
                == "5 meals planned · 2 from Autopilot · 1 add-on")
        #expect(
            MenuFormat.yourMealsSubtitle(counts: MealCounts(meals: 5, mealsFromAutopilot: 2))
                == "5 meals planned · 2 from Autopilot")
        #expect(
            MenuFormat.yourMealsSubtitle(counts: MealCounts(meals: 4, addOns: 1, mealsFromAutopilot: 4))
                == "Autopilot picked 4 meals based on your preferences · 1 add-on")
        #expect(MenuFormat.yourMealsSubtitle(counts: MealCounts()) == "Nothing planned yet")
    }
}

struct MenuCardShapeTests {
    @Test func showsAtMostOneBadgeAndTheMostUsefulOne() {
        let badges = [
            MenuBadge(code: .oftenOrdered, text: "Often Ordered"),
            MenuBadge(code: .autopilotPick, text: "Autopilot Pick"),
            MenuBadge(code: .makeAgain, text: "Make Again"),
        ]
        #expect(MenuCardBadge.context(in: badges)?.code == .autopilotPick)
        #expect(MenuCardBadge.context(in: Array(badges.dropFirst(1).dropFirst()))?.code == .makeAgain)
        #expect(MenuCardBadge.context(in: [badges[0]])?.code == .oftenOrdered)
    }

    @Test func showsNoBadgeWhenNoneAreWorthTheSpace() {
        #expect(MenuCardBadge.context(in: []) == nil)
        #expect(
            MenuCardBadge.context(in: [
                MenuBadge(code: .smokerFriendly, text: "Smoker"), MenuBadge(code: .new, text: "New to You"),
            ]) == nil)
    }

    @Test func reservesTheSameNameHeightForEveryCardInARow() {
        #expect(MenuCardMetrics.titleLines(for: .large) == 2)
        #expect(MenuCardMetrics.titleLines(for: .xxxLarge) == 2)
        // Accessibility sizes get a third line — the same third line for every card.
        #expect(MenuCardMetrics.titleLines(for: .accessibility1) == 3)
        #expect(MenuCardMetrics.titleLines(for: .accessibility5) == 3)
    }
}
