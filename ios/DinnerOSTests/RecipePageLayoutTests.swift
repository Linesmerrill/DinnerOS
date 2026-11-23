import Foundation
import Testing

@testable import DinnerOS

/// Build 121's recipe page and cooking screen: one bottom-bar layout, the photo's height on a
/// wide screen, stats that wrap instead of breaking words, and one way to write units.
struct RecipePageLayoutTests {
    // MARK: The bottom bar

    private func bar(
        planned: Bool = false, canEdit: Bool = true, draft: Bool = true, past: Bool = false, cook: Bool = true,
        inBar: Bool = false, stacks: Bool = false
    ) -> RecipePlanBarLayout? {
        RecipePlanBarLayout.make(
            isPlanned: planned, canEdit: canEdit, isDraft: draft, isPastWeek: past, hasStartCooking: cook,
            startCookingInBar: inBar, stacksStartCooking: stacks)
    }

    /// Every recipe, imported or typed by hand, gets the same row: Pick a Day and Add to Week.
    @Test func anIPhoneOffersPickADayAndAddToWeekFilled() {
        #expect(bar()?.row == [.day, .add(primary: true)])
        #expect(bar()?.cookRowOnTop == false)
    }

    @Test func theDayMenuSaysPickADayUntilThereIsOne() {
        #expect(RecipePlanBarLayout.dayLabel(nil) == "Pick a Day")
        #expect(RecipePlanBarLayout.dayLabel("Tuesday") == "Tuesday")
    }

    /// Start Cooking is the filled button wherever the bar has it; Add to Week steps back.
    @Test func onIPadStartCookingIsTheFilledButton() {
        #expect(bar(inBar: true)?.row == [.day, .add(primary: false), .startCooking])
        #expect(bar(planned: true, inBar: true)?.row == [.day, .stepper, .startCooking])
    }

    /// The Duo's cover screen keeps Start Cooking in its own row on top (build 119).
    @Test func theDuoStacksStartCookingOnTop() {
        let layout = bar(inBar: true, stacks: true)
        #expect(layout?.cookRowOnTop == true)
        #expect(layout?.row == [.day, .add(primary: false)])
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

    private func item(_ name: String, key: String? = nil) -> CookStepItem {
        CookStepItem(
            id: name, name: name, amountText: nil, prep: nil, parts: [], isLeftOut: false, ingredientKey: key)
    }

    /// Sip Fudge (Hark's review item 16): sugar and butter go in at Step 1, so they're listed
    /// there and not again under Have Ready, which goes away when nothing is left in it.
    @Test func whatAStepUsesIsntListedUnderHaveReady() {
        let groups = [
            CookStepGroup(index: 0, items: [item("sugar"), item("butter")]),
            CookStepGroup(index: 1, items: [item("Butter"), item("Sugar")]),
            CookStepGroup(index: 2, items: [item("sugar")]),
        ]
        let shown = CookStepGroups.shown(groups)
        #expect(shown.map(\.index) == [1, 2])
        #expect(shown[0].items.map(\.name) == ["Butter", "Sugar"])
    }

    /// Salt no step names stays under Have Ready; the rice Step 1 uses moves to Step 1 alone.
    @Test func haveReadyKeepsWhatNoStepNames() {
        let groups = [
            CookStepGroup(index: 0, items: [item("Rice", key: "rice"), item("Salt", key: "salt")]),
            CookStepGroup(index: 1, items: [item("Rice", key: "rice")]),
        ]
        let shown = CookStepGroups.shown(groups)
        #expect(shown.map(\.index) == [0, 1])
        #expect(shown[0].items.map(\.name) == ["Salt"])
        #expect(shown[1].items.map(\.name) == ["Rice"])
    }

    /// Getting something ready ahead is a job of its own: butter cut up for Step 2 and a blend
    /// to mix first stay under Have Ready even though a step uses them.
    @Test func readyAheadAndBlendsStayUnderHaveReady() {
        let butter = CookStepItem(
            id: "b@ahead", name: "Butter", amountText: "2 Tbsp", prep: "cut into pieces, for step 2", parts: [],
            isLeftOut: false, ingredientKey: "butter")
        let blend = CookStepItem(
            id: "s@0", name: "Spice Blend", amountText: "1 Tbsp", prep: nil, parts: ["1 tsp Cumin"],
            isLeftOut: false, ingredientKey: "spice")
        let groups = [
            CookStepGroup(index: 0, items: [butter, blend]),
            CookStepGroup(index: 1, items: [item("Spice Blend", key: "spice")]),
            CookStepGroup(index: 2, items: [item("Butter", key: "butter")]),
        ]
        #expect(CookStepGroups.shown(groups) == groups)
    }

    /// "Save ½ Lemon for Pesto" shows once, on the row where the lemon first goes in: the
    /// chicken bowls showed it on Step 2 and again on Step 3 (build 122, Hark's review item 4).
    @Test func saveReminderShowsOnceWhereItFirstGoesIn() {
        let lemon = { (id: String) in
            CookStepItem(
                id: id, name: "Lemon", amountText: nil, prep: nil, parts: [], isLeftOut: false, ingredientKey: "lemon")
        }
        let groups = [
            CookStepGroup(index: 2, items: [lemon("lemon@2"), item("Scallions", key: "scallion")]),
            CookStepGroup(index: 3, items: [lemon("lemon@3")]),
        ]
        let notes = [
            InstructionSaveForLater(
                ingredientIndex: 4, ingredientKey: "lemon", name: "Lemon",
                text: "Use 1 Lemon. Save ½ Lemon for Pesto Bow Tie Pasta.",
                short: "Save ½ Lemon for Pesto Bow Tie Pasta")
        ]
        #expect(CookStepGroups.saveReminderRows(groups, notes: notes) == ["lemon@2"])
        #expect(CookStepGroups.saveReminderRows(groups, notes: []).isEmpty)
    }

    /// A recipe without a Have Ready group is shown as it is.
    @Test func noHaveReadyIsUnchanged() {
        let groups = [CookStepGroup(index: 1, items: [item("Rice", key: "rice")])]
        #expect(CookStepGroups.shown(groups) == groups)
    }

    @Test func amountsReadInTheKitchenForm() {
        #expect(RecipeFormat.kitchenFractions("1 1/2 cups") == "1 ½ cups")
        #expect(RecipeFormat.kitchenFractions("1½ Tbsp") == "1 ½ Tbsp")
        #expect(RecipeFormat.kitchenFractions("1/2 tsp") == "½ tsp")
        #expect(RecipeFormat.kitchenFractions("2 1/3 cups") == "2 ⅓ cups")
        #expect(RecipeFormat.kitchenFractions("10/12 oz") == "10/12 oz")
        #expect(RecipeFormat.kitchenFractions("2 Tbsp") == "2 Tbsp")
        #expect(RecipeFormat.quantity("3/2", value: 1.5, locale: Locale(identifier: "en_US")) == "1 ½")
    }

    /// Hark's review, build 122 round 5: "¼ / cup" and "½- / inch" split across lines on the iPad
    /// cooking screen. An amount stays on one line and a size keeps its hyphen.
    @Test func amountsAndSizesNeverSplitAcrossLines() {
        #expect(RecipeFormat.unbrokenAmount("1/4 cup") == "¼\u{00A0}cup")
        #expect(RecipeFormat.unbrokenAmount("1 1/2 Tbsp") == "1\u{00A0}½\u{00A0}Tbsp")
        #expect(RecipeFormat.unbroken("Dice 1 tomato into ½-inch pieces") == "Dice 1 tomato into ½\u{2011}inch pieces")
        #expect(RecipeFormat.unbroken("cut into 1-inch cubes") == "cut into 1\u{2011}inch cubes")
        // Ranges and ordinary hyphens stay as written.
        #expect(RecipeFormat.unbroken("4-6 minutes") == "4-6 minutes")
        #expect(RecipeFormat.unbroken("Street Cart-Style") == "Street Cart-Style")
    }

    @Test func theServersAmountTextWins() {
        let fromServer = RecipeAmount(
            servings: 2, quantity: "3/2", quantityValue: 1.5, unit: "cup", sourceUnit: "cup", rawText: "1 1/2 cups",
            text: "1 ½ cups")
        #expect(RecipeFormat.amount(fromServer) == "1 ½ cups")
        let older = RecipeAmount(
            servings: 2, quantity: "3/2", quantityValue: 1.5, unit: "cup", sourceUnit: "cup", rawText: "1 1/2 cups")
        #expect(RecipeFormat.amount(older, locale: Locale(identifier: "en_US")) == "1 ½ cups")
    }

    @Test func typedNamesStartWithACapital() {
        #expect(RecipeFormat.displayName("sugar") == "Sugar")
        #expect(RecipeFormat.displayName("Butter") == "Butter")
        #expect(RecipeFormat.displayName("") == "")
        #expect(RecipeFormat.displayName("1% milk") == "1% Milk")
    }

    // MARK: The tab picker

    @Test func everyTabHasAnIdentifierForTests() {
        #expect(RecipeDetailTab.description.accessibilityID == "recipeTab.description")
        #expect(RecipeDetailTab.ingredients.accessibilityID == "recipeTab.ingredients")
        #expect(RecipeDetailTab.nutrition.accessibilityID == "recipeTab.nutrition")
        #expect(RecipeDetailTab.customization.accessibilityID == "recipeTab.customize")
    }

    /// The shared recipe page: the bar's title only once the large one is under the bar.
    @Test func theStarterPageShowsOneTitleAtATime() {
        #expect(!CatalogRecipeHeader.showsBarTitle(titleBottom: 180, barBottom: 100))
        #expect(CatalogRecipeHeader.showsBarTitle(titleBottom: 90, barBottom: 100))
    }

    @Test func stepGroupsAreNamedForWhatToDo() {
        #expect(CookStepGroupTitle.text(0) == "Have Ready")
        #expect(CookStepGroupTitle.text(2) == "Step 2")
    }
}
