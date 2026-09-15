import Foundation

/// A recipe's customizations and pairings.
nonisolated extension RecipesAPI {
    func customizations(householdID: String, recipeID: String, accessToken: String) async throws
        -> RecipeCustomizations
    {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/recipes/\(recipeID)/customizations")
                .authorized(with: accessToken))
    }

    /// Pairings for `recipeID`; `week` sets each pairing's `inPlan`.
    func pairings(householdID: String, recipeID: String, week: ISOWeek?, accessToken: String) async throws
        -> RecipePairings
    {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipes/\(recipeID)/pairings")
        if let week {
            request.queryItems = [URLQueryItem(name: "week", value: week.description)]
        }
        return try await client.send(request.authorized(with: accessToken))
    }
}
