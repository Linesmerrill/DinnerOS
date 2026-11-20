import Foundation
import Testing

@testable import DinnerOS

/// Build 121's recipe page and cooking screen: one bottom-bar layout, the photo's height on a
/// wide screen, stats that wrap instead of breaking words, and one way to write units.
struct RecipePageLayoutTests {
    // MARK: The bottom bar

    private func bar(
        planned: Bool = false, canEdit: Bool = true, draft: Bool = true, past: Bool = false, cook: Bool = true,
        inBar: Bool = false, stacks: Bool = false, choices: Bool = true
    ) -> RecipePlanBarLayout? {
        RecipePlanBarLayout.make(
            isPlanned: planned, canEdit: canEdit, isDraft: draft, isPastWeek: past, hasStartCooking: cook,
            startCookingInBar: inBar, stacksStartCooking: stacks, hasServingChoices: choices)
    }

    @Test func anIPhoneOffersServingsAndAddToWeekFilled() {
        #expect(bar()?.row == [.servings, .add(primary: true)])
        #expect(bar()?.cookRowOnTop == false)
    }

    /// A recipe typed by hand has one size: Add to Week takes the whole row, no empty half.
    @Test func oneServingSizeLeavesAddToWeekAlone() {
        #expect(bar(choices: false)?.row == [.add(primary: true)])
    }

    /// Start Cooking is the filled button wherever the bar has it; Add to Week steps back.
    @Test func onIPadStartCookingIsTheFilledButton() {
        #expect(bar(inBar: true)?.row == [.servings, .add(primary: false), .startCooking])
        #expect(bar(planned: true, inBar: true)?.row == [.day, .stepper, .startCooking])
    }

    /// The Duo's cover screen keeps Start Cooking in its own row on top (build 119).
    @Test func theDuoStacksStartCookingOnTop() {
        let layout = bar(inBar: true, stacks: true)
        #expect(layout?.cookRowOnTop == true)
        #expect(layout?.row == [.servings, .add(primary: false)])
    }

    @Test func aPlannedMealShowsItsDayAndServings() {
        #expect(bar(planned: true)?.row == [.day, .stepper])
    }

    /// Once the week is set, the bar is for cooking only.
    @Test func aSetWeekLeavesOnlyStartCooking() {
        let layout = bar(planned: true, draft: false)
        #expect(layout?.row == [.startCooking])
        #expect(layout?.showsFinalizedNote == false)
    }

    @Test func aSetWeekWithoutStepsSaysWhy() {
        let layout = bar(planned: true, draft: false, cook: false)
        #expect(layout?.row == [.day, .stepper])
        #expect(layout?.showsFinalizedNote == true)
    }

    @Test func someoneWhoCantPlanSeesOnlyStartCookingOrNothing() {
        #expect(bar(canEdit: false, inBar: true)?.row == [.startCooking])
        #expect(bar(canEdit: false) == nil)
        #expect(bar(canEdit: false, cook: false, inBar: true) == nil)
    }

    // MARK: The header

    @Test func thePhotoStopsAtTheHeroCapOnWideScreens() {
        #expect(RecipeDetailHeader.heroHeight(width: 1_180, isWide: true) == DS.Size.heroMaxHeight)
        #expect(RecipeDetailHeader.heroHeight(width: 820, isWide: true) == DS.Size.heroMaxHeight)
        // A phone held upright keeps its taller photo.
        #expect(abs(RecipeDetailHeader.heroHeight(width: 375, isWide: false) - 412.5) < 0.01)
        #expect(RecipeDetailHeader.heroHeight(width: 440, isWide: false) == 420)
        #expect(RecipeDetailHeader.heroHeight(width: 200, isWide: false) == 240)
    }

    @Test func statsWrapToTwoEvenRows() {
        #expect(RecipeStatsLayout.fallbackColumns(count: 4) == 2)
        #expect(RecipeStatsLayout.fallbackColumns(count: 3) == 2)
        #expect(RecipeStatsLayout.fallbackColumns(count: 1) == 1)
    }

    // MARK: Units

    @Test func sourceUnitsReadInOneShortForm() {
        #expect(RecipeFormat.abbreviatedUnit("ounce") == "oz")
        #expect(RecipeFormat.abbreviatedUnit("Ounces") == "oz")
        #expect(RecipeFormat.abbreviatedUnit("tablespoons") == "Tbsp")
        #expect(RecipeFormat.abbreviatedUnit("tsp.") == "tsp")
        #expect(RecipeFormat.abbreviatedUnit("pounds") == "lb")
        #expect(RecipeFormat.abbreviatedUnit("grams") == "g")
        #expect(RecipeFormat.abbreviatedUnit("kcal") == "Cal")
        #expect(RecipeFormat.abbreviatedUnit("handful") == "handful")
        #expect(RecipeFormat.abbreviatedUnit("") == "")
    }

    @Test func anUnknownUnitCodeFallsBackToTheShortForm() {
        #expect(RecipeFormat.unitLabel("", sourceUnit: "ounce", plural: true) == "oz")
        let amount = RecipeAmount(
            servings: 2, quantity: "10", quantityValue: 10, unit: "", sourceUnit: "ounce", rawText: "10 ounce")
        #expect(RecipeFormat.amount(amount, locale: Locale(identifier: "en_US")) == "10 oz")
    }

    @Test func caloriesReadAsCalNeverKcal() {
        let locale = Locale(identifier: "en_US")
        let calories = RecipeNutrient(name: "Calories", amount: 880, unit: "kcal")
        #expect(MenuFormat.nutrientAmount(calories, locale: locale) == "880 Cal")
        let protein = RecipeNutrient(name: "Protein", amount: 39, unit: "g")
        #expect(MenuFormat.nutrientAmount(protein, locale: locale) == "39 g")
        let stats = MenuFormat.stats(minutes: nil, calories: 880, proteinGrams: 39, difficulty: nil)
        #expect(stats.map(\.value) == ["880 Cal", "39 g"])
    }

    @Test func theServersNutritionTextWins() {
        let locale = Locale(identifier: "en_US")
        let fromServer = RecipeNutrient(name: "Calories", amount: 640, unit: "kcal", text: "640 Cal")
        #expect(MenuFormat.nutrientAmount(fromServer, locale: locale) == "640 Cal")
        // An older server, or a saved response from before: the same words, made here.
        let older = RecipeNutrient(name: "Calories", amount: 640, unit: "kcal")
        #expect(MenuFormat.nutrientAmount(older, locale: locale) == "640 Cal")
        let blank = RecipeNutrient(name: "Protein", amount: 39, unit: "g", text: " ")
        #expect(MenuFormat.nutrientAmount(blank, locale: locale) == "39 g")
    }

    @Test func theNutritionFormShowsCalNotKcal() {
        #expect(NutritionField.calories.unit == "kcal")
        #expect(NutritionField.calories.displayUnit == "Cal")
        #expect(NutritionField.protein.displayUnit == "g")
        #expect(NutritionField.sodium.displayUnit == "mg")
    }

    @Test func nutritionDecodesTheServersText() throws {
        let json = Data(#"{"name":"Protein","amount":39,"unit":"g","text":"39 g"}"#.utf8)
        #expect(try JSONDecoder().decode(RecipeNutrient.self, from: json).text == "39 g")
        let old = Data(#"{"name":"Protein","amount":39,"unit":"g"}"#.utf8)
        #expect(try JSONDecoder().decode(RecipeNutrient.self, from: old).text == nil)
    }

    // MARK: Cooking

    /// "¾ cup water" isn't on the recipe's list, but its amount still reads like the others'.
    @Test func aStepsOwnAmountIsFoundForTheList() {
        #expect(InstructionStepText.leadingAmount("¾ cup water") == "¾ cup")
        #expect(InstructionStepText.leadingAmount("2 Tbsp butter") == "2 Tbsp")
        #expect(InstructionStepText.leadingAmount("1½ cups stock") == "1½ cups")
        #expect(InstructionStepText.leadingAmount("3 eggs") == "3")
        #expect(InstructionStepText.leadingAmount("a big pinch of salt") == nil)
        #expect(InstructionStepText.leadingAmount("pepper") == nil)
    }

    @Test func stepGroupsAreNamedForWhatToDo() {
        #expect(CookStepGroupTitle.text(0) == "Have Ready")
        #expect(CookStepGroupTitle.text(2) == "Step 2")
    }
}
