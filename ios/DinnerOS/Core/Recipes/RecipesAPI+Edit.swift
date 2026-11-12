import Foundation

/// Editing a recipe: saving the household's version, and previewing an ingredient swap.
nonisolated extension RecipesAPI {
    func editRecipe(
        householdID: String, recipeID: String, request body: RecipeEditRequest, accessToken: String
    ) async throws -> Recipe {
        let request = try APIRequest.put("/api/v1/households/\(householdID)/recipes/\(recipeID)/edits", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// The steps with one ingredient renamed, and its cook time. Nothing is stored.
    func swapPreview(
        householdID: String, request body: RecipeSwapRequest, accessToken: String
    ) async throws -> RecipeSwapPreview {
        let request = try APIRequest.post("/api/v1/households/\(householdID)/recipes/swap-preview", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }
}
