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

    /// Sends a recipe photo the member took or picked, and returns its link for the recipe.
    func uploadPhoto(householdID: String, jpeg: Data, accessToken: String) async throws -> String {
        let request = APIRequest.post(
            "/api/v1/households/\(householdID)/recipe-photos", data: jpeg, contentType: "image/jpeg")
        let uploaded: RecipePhotoUpload = try await client.send(request.authorized(with: accessToken))
        return uploaded.imageURL
    }

    /// Deletes a recipe from the household's library. It can be put back for a while.
    func deleteRecipe(householdID: String, recipeID: String, accessToken: String) async throws -> RecipeDeletion {
        try await client.send(
            APIRequest.delete("/api/v1/households/\(householdID)/recipes/\(recipeID)").authorized(with: accessToken))
    }

    /// Puts a deleted recipe back.
    func restoreRecipe(householdID: String, recipeID: String, accessToken: String) async throws -> Recipe {
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/recipes/\(recipeID)/restore", body: [String: String]())
        return try await client.send(request.authorized(with: accessToken))
    }

    /// The names to offer while an ingredient is typed.
    func ingredientNames(householdID: String, accessToken: String) async throws -> IngredientNameList {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/ingredient-names").authorized(with: accessToken))
    }

    /// Photos on the web for a search, a page at a time.
    func searchPhotos(
        householdID: String, query: String, page: Int, accessToken: String
    ) async throws -> RecipePhotoSearchPage {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipe-photos/search")
        request.queryItems = [URLQueryItem(name: "q", value: query), URLQueryItem(name: "page", value: String(page))]
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Whether photo search is on. Off when the server has no photo provider.
    func photoSearchStatus(householdID: String, accessToken: String) async throws -> RecipePhotoSearchStatus {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/recipe-photos/search-status")
                .authorized(with: accessToken))
    }

    /// Picks a photo found with photo search: returns where to show it from and who to credit.
    func useFoundPhoto(householdID: String, photoID: String, accessToken: String) async throws -> RecipePhotoPick {
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/recipe-photos/from-search", body: ["id": photoID])
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
