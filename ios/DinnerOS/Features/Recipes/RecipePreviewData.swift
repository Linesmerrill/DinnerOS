import Foundation

/// Synthetic recipes for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are
/// type-checked in Release builds too.
enum RecipePreviewData {
    static let summaries = [
        RecipeSummary(
            id: "recipe-1", name: "Skillet Test Tacos", headline: "with Lime Crema", imageURLString: nil,
            totalMinutes: 30, timesOrdered: 3, lastOrderedWeek: "2026-W37", isAddon: false, tags: ["Quick"]),
        RecipeSummary(
            id: "recipe-2", name: "Sample Garden Salad", headline: nil, imageURLString: nil,
            totalMinutes: 15, timesOrdered: 1, lastOrderedWeek: "2026-W12", isAddon: true, tags: []),
        RecipeSummary(
            id: "recipe-3", name: "Placeholder Pasta Bake", headline: "with Roasted Peppers", imageURLString: nil,
            totalMinutes: 45, timesOrdered: 0, lastOrderedWeek: nil, isAddon: false, tags: []),
    ]

    static let recipe = Recipe(
        id: "recipe-1", householdID: HouseholdPreviewData.household.id, source: "hellofresh",
        name: "Skillet Test Tacos", headline: "with Lime Crema",
        description: "A synthetic recipe for previews.", imageURLString: nil, isAddon: false, servings: [2, 4],
        prepMinutes: 10, totalMinutes: 30, difficulty: 1, cuisines: ["Mexican"], tags: ["Quick"],
        utensils: ["Large skillet", "Small bowl"], allergens: ["Milk", "Wheat"],
        nutritionPerServing: [
            RecipeNutrient(name: "Calories", amount: 640, unit: "kcal"),
            RecipeNutrient(name: "Protein", amount: 32.5, unit: "g"),
        ],
        ingredients: [
            ingredient("Cheddar Cheese", category: "dairy-eggs", two: ("1/2", 0.5, "oz"), four: ("1", 1, "oz")),
            ingredient("Garlic", category: "produce", two: ("1", 1, "clove"), four: ("2", 2, "clove")),
            ingredient("Flour Tortillas", category: "bakery", two: ("6", 6, "count"), four: ("12", 12, "count")),
            ingredient("Olive Oil", category: "pantry", pantry: true, two: ("1", 1, "tbsp"), four: ("2", 2, "tbsp")),
            ingredient("Salt", category: "spices", pantry: true, two: nil, four: nil),
        ],
        steps: [
            RecipeStep(index: 1, text: "Warm the tortillas in a dry skillet.", imageURLString: nil),
            RecipeStep(index: 2, text: "• Fill the tortillas.\n• Top with crema and serve.", imageURLString: nil),
        ],
        orderWeeks: ["2026-W12", "2026-W30", "2026-W37"], timesOrdered: 3, lastOrderedWeek: "2026-W37",
        createdAt: .now, updatedAt: .now)

    static func library(session: AuthSession) -> RecipeLibrary {
        .preview(session: session, items: summaries, recipes: [recipe])
    }

    private static func ingredient(
        _ name: String, category: String, pantry: Bool = false,
        two: (String, Double, String)?, four: (String, Double, String)?
    ) -> RecipeIngredient {
        func amount(_ servings: Int, _ value: (String, Double, String)?) -> RecipeAmount {
            RecipeAmount(
                servings: servings, quantity: value?.0, quantityValue: value?.1, unit: value?.2 ?? "",
                sourceUnit: value?.2 ?? "", rawText: name)
        }
        return RecipeIngredient(
            ingredientID: "ingredient-\(name)", name: name, category: category, pantryStaple: pantry,
            amounts: [amount(2, two), amount(4, four)])
    }
}
