import Foundation

/// A recipe's cooking instructions, rendered by the server for one serving size.
nonisolated extension RecipesAPI {
    func instructions(householdID: String, recipeID: String, servings: Int?, accessToken: String) async throws
        -> RecipeInstructions
    {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipes/\(recipeID)/instructions")
        if let servings, servings > 0 {
            request.queryItems = [URLQueryItem(name: "servings", value: String(servings))]
        }
        return try await client.send(request.authorized(with: accessToken))
    }
}
