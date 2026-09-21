import Foundation

/// Manual recipe entry: parse, review, save, and the global-catalog opt-in.
///
/// Parsing happens on the server, including fetching a URL, so the phone never
/// loads a page the member pasted and never has to decide whether an address is
/// safe to open.
nonisolated extension RecipesAPI {
    /// Parses pasted text into a draft. Nothing is stored.
    func parseRecipe(householdID: String, text: String, accessToken: String) async throws -> RecipeDraft {
        try await parse(householdID: householdID, body: ParseRecipeRequest(text: text), accessToken: accessToken)
    }

    /// Reads a recipe page into a draft. Nothing is stored.
    func parseRecipe(householdID: String, url: String, accessToken: String) async throws -> RecipeDraft {
        try await parse(householdID: householdID, body: ParseRecipeRequest(url: url), accessToken: accessToken)
    }

    /// Saves a reviewed draft as one of the household's own recipes. It stays
    /// private: a typed or pasted recipe never enters the global catalog.
    func createRecipe(householdID: String, draft: RecipeDraft, accessToken: String) async throws -> Recipe {
        let request = try APIRequest.post("/api/v1/households/\(householdID)/recipes", body: draft)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Turns the global-catalog opt-in on or off for one of the household's recipes.
    func setSharing(
        householdID: String, recipeID: String, sharedToCatalog: Bool, accessToken: String
    ) async throws -> RecipeSharing {
        let request = try APIRequest.put(
            "/api/v1/households/\(householdID)/recipes/\(recipeID)/sharing",
            body: RecipeSharingRequest(sharedToCatalog: sharedToCatalog))
        return try await client.send(request.authorized(with: accessToken))
    }

    private func parse(
        householdID: String, body: ParseRecipeRequest, accessToken: String
    ) async throws -> RecipeDraft {
        let request = try APIRequest.post("/api/v1/households/\(householdID)/recipes/parse", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }
}
