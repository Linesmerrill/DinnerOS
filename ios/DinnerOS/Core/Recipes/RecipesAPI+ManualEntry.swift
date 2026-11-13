import Foundation

/// Manual recipe entry: parse, review, save, and the global-catalog opt-in.
///
/// Parsing happens on the server, including fetching a URL. Only when a site turns the
/// server away does the phone load the page itself (`RecipePageLoader`) and send its HTML
/// for the server to read.
nonisolated extension RecipesAPI {
    /// Parses pasted text into a draft. Nothing is stored.
    func parseRecipe(householdID: String, text: String, accessToken: String) async throws -> RecipeDraft {
        try await parse(householdID: householdID, body: ParseRecipeRequest(text: text), accessToken: accessToken)
    }

    /// Reads a recipe page into a draft. Nothing is stored. With `html`, the server reads the
    /// page this phone loaded instead of fetching it.
    func parseRecipe(
        householdID: String, url: String, html: String? = nil, accessToken: String
    ) async throws -> RecipeDraft {
        try await parse(
            householdID: householdID, body: ParseRecipeRequest(url: url, html: html), accessToken: accessToken)
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
