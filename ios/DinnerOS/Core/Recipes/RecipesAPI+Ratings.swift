import Foundation

/// The rating endpoints (`/api/v1/households/{householdId}/recipes/{recipeId}/rating(s)`).
/// Managing your own rating needs only `household.view`.
nonisolated extension RecipesAPI {
    /// Creates or replaces the caller's rating and returns it as saved.
    func rate(
        householdID: String, recipeID: String, request body: RateRecipeRequest, accessToken: String
    ) async throws -> RecipeRating {
        let request = try APIRequest.put(Self.recipePath(householdID, recipeID) + "/rating", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Removes the caller's rating. Succeeds when there was none.
    func removeRating(householdID: String, recipeID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.recipePath(householdID, recipeID) + "/rating").authorized(with: accessToken))
    }

    /// Every member's rating with display names, and the household aggregate.
    func ratings(householdID: String, recipeID: String, accessToken: String) async throws -> RatingListResponse {
        try await client.send(
            APIRequest.get(Self.recipePath(householdID, recipeID) + "/ratings").authorized(with: accessToken))
    }

    private static func recipePath(_ householdID: String, _ recipeID: String) -> String {
        "/api/v1/households/\(householdID)/recipes/\(recipeID)"
    }
}
