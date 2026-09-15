import Foundation

/// A synthetic week plan and grocery list for SwiftUI previews. Not DEBUG-only because
/// `#Preview` bodies are type-checked in Release builds too.
enum PlanPreviewData {
    static let week = ISOWeek("2026-W38") ?? .current(in: .gmt)

    static let plan = Plan(
        householdID: HouseholdPreviewData.household.id, week: "2026-W38", startDate: "2026-09-14",
        endDate: "2026-09-20", status: .draft,
        entries: [
            entry(
                "entry-1", recipe: "recipe-1", name: "Skillet Test Tacos", day: .mon, servings: 4, note: "Extra lime"),
            entry("entry-2", recipe: "recipe-3", name: "Placeholder Pasta Bake", day: .wed, servings: 2),
            entry("entry-3", recipe: "recipe-2", name: "Sample Garden Salad", day: nil, servings: 2),
        ],
        createdAt: .now, updatedAt: .now)

    static let emptyPlan = Plan(
        householdID: HouseholdPreviewData.household.id, week: "2026-W38", startDate: "2026-09-14",
        endDate: "2026-09-20", status: .draft, entries: [], createdAt: nil, updatedAt: nil)

    static let groceryList = GroceryList(
        week: "2026-W38", status: .draft, pantryApplied: false,
        categories: [
            GroceryCategory(
                category: "produce",
                items: [
                    item(
                        "onion", "Yellow Onion", quantity: "1 ½ + 8 oz",
                        recipes: ["Skillet Test Tacos", "Placeholder Pasta Bake"]),
                    item("garlic", "Garlic", quantity: "3 cloves", recipes: ["Skillet Test Tacos"]),
                ]),
            GroceryCategory(
                category: "spices",
                items: [
                    item(
                        "salt", "Salt", quantity: "", unquantified: true, status: .pantryHint,
                        recipes: ["Skillet Test Tacos"])
                ]),
        ],
        skipped: [
            GrocerySkippedEntry(
                entryID: "entry-3", recipeID: "recipe-2", recipeName: "Sample Garden Salad",
                reason: .servingsUnavailable)
        ])

    static func store(session: AuthSession, plan: Plan = plan) -> PlanStore {
        .preview(session: session, plan: plan)
    }

    private static func entry(
        _ id: String, recipe: String, name: String, day: PlanDay?, servings: Int, note: String = ""
    ) -> PlanEntry {
        PlanEntry(
            id: id, recipe: PlanEntryRecipe(id: recipe, name: name, imageURLString: nil), day: day,
            date: nil, servings: servings, note: note, addedBy: HouseholdPreviewData.user.id, addedAt: .now)
    }

    private static func item(
        _ key: String, _ name: String, quantity: String, unquantified: Bool = false,
        status: GroceryItemStatus = .toBuy, recipes: [String]
    ) -> GroceryItem {
        GroceryItem(
            ingredientKey: key, name: name, amounts: [], quantityText: quantity, unquantified: unquantified,
            status: status, recipes: recipes.map { GroceryRecipe(id: "recipe-\($0)", name: $0) })
    }
}
