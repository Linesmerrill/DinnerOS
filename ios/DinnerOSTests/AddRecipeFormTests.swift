import Foundation
import Testing

@testable import DinnerOS

/// The Add a Recipe pass: ingredient suggestions, the ingredient sheet's rows, prep and cook
/// times, nutrition, reordering, the no-photo picture, and what an edit sends.
struct AddRecipeFormTests {
    // MARK: Suggestions

    @Test func householdNamesComeFirstThenCommonOnes() {
        let index = IngredientNameIndex(
            household: ["Garlic", "Ground Beef", "Ground Turkey", "Olive Oil"],
            common: ["Garlic Powder", "Ginger", "Green Bell Pepper", "Garlic", "Basil"])
        #expect(
            index.suggestions(for: "g")
                == ["Garlic", "Ground Beef", "Ground Turkey", "Garlic Powder", "Ginger", "Green Bell Pepper"])
        #expect(index.suggestions(for: "GAR") == ["Garlic", "Garlic Powder"])
        // A word inside the name counts too, after names that start with it.
        #expect(index.suggestions(for: "oil") == ["Olive Oil"])
        #expect(index.suggestions(for: "") == [])
    }

    @Test func caseAndPluralsDontMakeTwoSuggestions() {
        let index = IngredientNameIndex(
            household: ["Tomatoes", "egg"], common: ["Tomato", "Eggs", "Berries", "Berry"])
        #expect(index.suggestions(for: "tom") == ["Tomatoes"])
        #expect(index.suggestions(for: "e") == ["egg"])
        #expect(index.suggestions(for: "ber") == ["Berries"])
        // What's typed already isn't offered back.
        #expect(index.suggestions(for: "tomato") == [])
        #expect(IngredientNameIndex.key("Green Bell Peppers") == IngredientNameIndex.key("green bell pepper"))
        #expect(IngredientNameIndex.key("Glass") == "glass")
    }

    @Test func suggestionsDecodeFromTheServer() throws {
        let json = #"{"household":[{"name":"Garlic","count":41}],"common":["Ginger"]}"#
        let list = try JSONDecoder().decode(IngredientNameList.self, from: Data(json.utf8))
        #expect(list.index.suggestions(for: "g") == ["Garlic", "Ginger"])
    }

    // MARK: Rows

    @Test func rowsShowAmountThenNote() {
        #expect(IngredientFields.detail(quantity: "2", unit: "count", note: "chopped") == "2, chopped")
        #expect(IngredientFields.detail(quantity: "1 1/2", unit: "tbsp", note: "") == "1 ½ Tbsp")
        #expect(IngredientFields.detail(quantity: "", unit: "count", note: "minced") == "minced")
        #expect(IngredientFields.detail(quantity: "", unit: "cup", note: " ") == "")
    }

    @Test func theSheetFillsAndSavesADraftLine() {
        var line = RecipeDraftIngredient(
            fields: IngredientFields(quantity: "2", unit: "tbsp", name: "Garlic", note: "minced"))
        #expect(line.name == "Garlic")
        #expect(line.quantity == "2")
        #expect(line.unit == "tbsp")
        #expect(line.note == "minced")
        line.apply(IngredientFields(quantity: "", unit: "count", name: "Garlic", note: ""))
        #expect(line.quantity == nil)
        #expect(line.note == nil)
        #expect(line.fields == IngredientFields(quantity: "", unit: "count", name: "Garlic", note: ""))
    }

    // MARK: Reorder & Delete

    @Test func reorderKeepsOnlyWhatsLeftInItsNewOrder() {
        #expect(["a", "b", "c", "d"].reordered(by: [2, 0, 3]) == ["c", "a", "d"])
        #expect(["a", "b"].reordered(by: [5, 1]) == ["b"])
        var list = ["a", "b", "c"]
        list.move(at: 0, by: 1)
        #expect(list == ["b", "a", "c"])
        list.move(at: 0, by: -1)
        #expect(list == ["b", "a", "c"])
    }

    // MARK: Times

    @Test func prepAndCookAreSavedAsPrepAndTotal() {
        var draft = RecipeDraft(name: "Brisket")
        #expect(draft.prepTime == 0 && draft.cookTime == 0)
        draft.cookTime = 14 * 60
        #expect(draft.totalMinutes == 840 && draft.prepMinutes == nil)
        draft.prepTime = 30
        // Prep doesn't eat into the cook time.
        #expect(draft.cookTime == 840)
        #expect(draft.totalMinutes == 870)
        #expect(draft.totalTime == 870)
        draft.cookTime = 0
        #expect(draft.totalMinutes == 30)
        draft.prepTime = 0
        #expect(draft.totalMinutes == nil && draft.prepMinutes == nil)
    }

    @Test func aRecipeWithOneTimeShowsItAsCook() {
        let draft = RecipeDraft(name: "Chili", totalMinutes: 40)
        #expect(draft.cookTime == 40)
        #expect(draft.prepTime == 0)
    }

    @Test func thePickerGoesToADay() {
        #expect(RecipeTimes.minutes(hours: 4, minutes: 30) == 270)
        #expect(RecipeTimes.minutes(hours: 24, minutes: 30) == 24 * 60)
        #expect(RecipeTimes.nearestStep(7) == 5)
        #expect(RecipeTimes.nearestStep(58) == 55)
        #expect(RecipeTimes.presets == [15, 30, 60, 120, 240])
        #expect(RecipeTimes.short(15) == "15m")
        #expect(RecipeTimes.short(120) == "2h")
    }

    @Test func timesReadLikeAPersonSaysThem() {
        #expect(RecipeFormat.minutes(30) == "30 min")
        #expect(RecipeFormat.minutes(120) == "2 hr")
        #expect(RecipeFormat.minutes(270) == "4 hr 30 min")
    }

    // MARK: Nutrition

    @Test func nutritionSendsOnlyWhatsFilledIn() {
        var values = NutritionValues()
        #expect(values.isEmpty)
        values[.calories] = "540"
        values[.protein] = "30.5"
        values[.sodium] = ""
        #expect(
            values.entries == [
                RecipeNutrientEntry(name: "Calories", amount: 540, unit: "kcal"),
                RecipeNutrientEntry(name: "Protein", amount: 30.5, unit: "g"),
            ])
        values[.fat] = "lots"
        #expect(values.problem == "Enter a number for fat.")
    }

    @Test func nutritionReadsImportSpellings() {
        let values = NutritionValues([
            RecipeNutrient(name: "Carbohydrate", amount: 61, unit: "g"),
            RecipeNutrient(name: "Dietary Fiber", amount: 4.25, unit: "g"),
            RecipeNutrient(name: "Vitamin C", amount: 3, unit: "mg"),
        ])
        #expect(values[.carbohydrate] == "61")
        #expect(values[.fiber] == "4.2")
        #expect(values.entries.count == 2)
    }

    // MARK: What an edit sends

    @Test func aDetailsOnlyEditLeavesTheIngredientsAlone() throws {
        let photo = try json(RecipeEditRequest.photo("https://api.test/p/a"))
        #expect(photo["imageUrl"] as? String == "https://api.test/p/a")
        #expect(photo["ingredients"] == nil)
        #expect(photo["steps"] == nil)
        let nutrition = try json(RecipeEditRequest.nutrition([]))
        #expect((nutrition["nutritionPerServing"] as? [Any])?.isEmpty == true)
        #expect(nutrition["ingredients"] == nil)
    }

    @Test func editSendsNotesTimesAndNutritionOnlyWhenChanged() throws {
        var draft = RecipeEditDraft(recipe: try Self.recipe(), servings: 2)
        let untouched = try json(draft.request)
        #expect(untouched["prepMinutes"] == nil && untouched["nutritionPerServing"] == nil)
        #expect(!draft.hasChanges)
        let id = draft.lines[0].id
        draft.apply(IngredientFields(quantity: "2", unit: "can", name: "Beans", note: "rinsed"), to: id)
        draft.prepTime = 15
        draft.nutrition[.calories] = "400"
        #expect(draft.hasChanges)
        let body = try json(draft.request)
        let lines = try #require(body["ingredients"] as? [[String: Any]])
        #expect(lines[0]["note"] as? String == "rinsed")
        #expect(body["prepMinutes"] as? Int == 15)
        #expect(body["totalMinutes"] as? Int == 55)
        let nutrients = try #require(body["nutritionPerServing"] as? [[String: Any]])
        #expect(nutrients.first?["name"] as? String == "Calories")
    }

    @Test func editRenamesAndSaysWhatsMissing() throws {
        var draft = RecipeEditDraft(recipe: try Self.recipe(), servings: 2)
        #expect(try json(draft.request)["name"] == nil)
        draft.name = "  Grandma's Chili "
        #expect(draft.hasChanges)
        #expect(try json(draft.request)["name"] as? String == "Grandma's Chili")
        draft.name = " "
        #expect(draft.problem == "Add a name.")
        draft.name = "Chili"
        draft.lines.removeAll()
        #expect(draft.problem == "Add at least one ingredient.")
    }

    @Test func editAddsALineFromTheSheet() throws {
        var draft = RecipeEditDraft(recipe: try Self.recipe(), servings: 2)
        let id = draft.addLine(IngredientFields(quantity: "1", unit: "tbsp", name: "Cumin", note: "ground"))
        #expect(draft.fields(of: id) == IngredientFields(quantity: "1", unit: "tbsp", name: "Cumin", note: "ground"))
        #expect(draft.lines.count == 3)
    }

    @Test func theRecipePageShowsTheNoteAfterTheName() throws {
        let lines = try Self.recipe().ingredientLines(servings: 2)
        #expect(lines[1].nameWithNote == "Salt, to taste")
        #expect(lines[0].nameWithNote == "Beans")
    }

    // MARK: The no-photo picture

    @Test func theNoPhotoPictureFitsTheDish() {
        #expect(RecipePlaceholderStyle.symbol(for: "No Bake Cookies") == "birthday.cake")
        #expect(RecipePlaceholderStyle.symbol(for: "Grandma's Chili") == "takeoutbag.and.cup.and.straw")
        #expect(RecipePlaceholderStyle.symbol(for: "Grilled Salmon") == "fish")
        #expect(RecipePlaceholderStyle.symbol(for: "Mystery Dish") == "fork.knife")
        #expect(RecipePlaceholderStyle.fade(top: 300) == 1)
        #expect(RecipePlaceholderStyle.fade(top: 150) == 0.5)
        #expect(RecipePlaceholderStyle.fade(top: 40) == 0)
        // The same name always gets the same colors.
        #expect(RecipePlaceholderStyle.palette(for: "Tacos") == RecipePlaceholderStyle.palette(for: "Tacos"))
    }

    // MARK: The recipe page header

    @Test func theBarDoesntFlickerAtTheLine() {
        // Header 400 tall: the bar shows past 290 and hides again only below 250.
        #expect(!RecipeDetailHeader.showsBar(offset: 280, heroHeight: 400, showing: false))
        #expect(RecipeDetailHeader.showsBar(offset: 291, heroHeight: 400, showing: false))
        #expect(RecipeDetailHeader.showsBar(offset: 260, heroHeight: 400, showing: true))
        #expect(!RecipeDetailHeader.showsBar(offset: 249, heroHeight: 400, showing: true))
    }

    @Test func theTabPickerStopsUnderTheBar() {
        // Bar 116 tall: the picker rides along until it reaches it, then stays there.
        #expect(RecipeDetailHeader.stickyOffset(minY: 500, top: 116) == 0)
        #expect(RecipeDetailHeader.stickyOffset(minY: 116, top: 116) == 0)
        #expect(RecipeDetailHeader.stickyOffset(minY: -200, top: 116) == 316)
    }

    // MARK: Deleting

    @Test func aDeletionSaysWhichWeeksItLeft() throws {
        let result = try JSONDecoder().decode(
            RecipeDeletion.self, from: Data(#"{"removedFromWeeks":["2026-11-16"],"undoUntil":"x"}"#.utf8))
        #expect(result.removedFromWeeks == ["2026-11-16"])
    }

    private func json(_ request: RecipeEditRequest) throws -> [String: Any] {
        try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any])
    }

    static func recipe() throws -> Recipe {
        let json = """
            {"id":"r1","householdId":"h1","source":"manual","name":"Chili","isAddon":false,"servings":[2],
             "totalMinutes":40,
             "cuisines":[],"tags":[],"orderWeeks":[],"timesOrdered":0,
             "createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z",
             "householdRating":{"count":0,"average":null},
             "ingredients":[
               {"ingredientId":"i-beans","name":"Beans","category":"pantry","pantryStaple":false,"amounts":[
                 {"servings":2,"quantity":"1","quantityValue":1,"unit":"can","sourceUnit":"","rawText":""}]},
               {"ingredientId":"i-salt","name":"Salt","category":"spices","pantryStaple":true,"note":"to taste",
                "amounts":[{"servings":2,"quantity":null,"quantityValue":null,"unit":"","sourceUnit":"","rawText":""}]}],
             "steps":[{"index":1,"text":"Cook it."}]}
            """
        return try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
    }
}
