import Foundation

nonisolated extension RecipesAPI {
    /// The most review items one request asks for, and the most one resolve call sends. The
    /// household's whole backlog is small (93 items for 431 recipes), so the screen reads it in
    /// one page.
    static let importReviewLimit = 500

    /// Things the importer could not map confidently, oldest first (`recipes.import`).
    ///
    /// A server without the route answers `404`, which the store reads as "this API doesn't
    /// have import reviews yet" and hides the screen, as the store catalog does.
    func importReviews(
        householdID: String, limit: Int = RecipesAPI.importReviewLimit, accessToken: String
    ) async throws -> [ImportReview] {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipes/import-reviews")
        request.queryItems = [
            URLQueryItem(name: "status", value: ImportReview.openStatus),
            URLQueryItem(name: "limit", value: String(limit)),
        ]
        let response: ImportReviewListResponse = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    /// Records a decision on up to `importReviewLimit` items and returns how many open items it
    /// resolved (`recipes.import`). `recipeID` links a recipe added for a delivered variant, and
    /// goes only with `.differentRecipe`.
    func resolveImportReviews(
        householdID: String, ids: [String], resolution: ImportReviewResolution, recipeID: String? = nil,
        accessToken: String
    ) async throws -> Int {
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/recipes/import-reviews/resolve",
            body: ResolveImportReviewsRequest(ids: ids, resolution: resolution.rawValue, recipeId: recipeID))
        let response: ResolveImportReviewsResponse = try await client.send(request.authorized(with: accessToken))
        return response.resolved
    }
}
