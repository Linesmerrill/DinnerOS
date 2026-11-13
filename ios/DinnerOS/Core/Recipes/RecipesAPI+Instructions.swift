import Foundation

/// A recipe's cooking instructions, rendered by the server for one serving size.
nonisolated extension RecipesAPI {
    /// `swaps` are the meal's protein choices, so the steps name the protein being cooked.
    func instructions(
        householdID: String, recipeID: String, servings: Int?, swaps: [PlanEntryCustomization] = [],
        week: ISOWeek? = nil, accessToken: String
    ) async throws -> RecipeInstructions {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipes/\(recipeID)/instructions")
        var items: [URLQueryItem] = []
        if let servings, servings > 0 {
            items.append(URLQueryItem(name: "servings", value: String(servings)))
        }
        // The meal's week, so an ingredient skipped just this once shows as left out.
        if let week {
            items.append(URLQueryItem(name: "week", value: week.description))
        }
        for swap in swaps {
            items.append(URLQueryItem(name: "swap", value: "\(swap.ingredientKey)=\(swap.choiceID)"))
        }
        if !items.isEmpty { request.queryItems = items }
        return try await client.send(request.authorized(with: accessToken))
    }
}
