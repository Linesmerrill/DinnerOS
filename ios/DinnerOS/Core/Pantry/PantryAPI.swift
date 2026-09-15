import Foundation

/// Typed wrappers for the pantry endpoints (`/api/v1/households/{householdId}/pantry`).
/// Use them through `AuthSession.authorized`.
nonisolated struct PantryAPI: Sendable {
    /// The API accepts at most this many statuses per bulk request.
    static let maxBulkUpdates = 200

    let client: APIClient

    /// Every item, in aisle order and then by name. Pantries aren't paginated.
    func listItems(householdID: String, accessToken: String) async throws -> [PantryItem] {
        let response: PantryItemListResponse = try await client.send(
            APIRequest.get(Self.path(householdID)).authorized(with: accessToken))
        return response.items
    }

    /// Adds an item, or merges into the item that already has the ingredient (`201` or `200`).
    func addItem(householdID: String, item: NewPantryItem, accessToken: String) async throws -> PantryItem {
        try await client.send(try APIRequest.post(Self.path(householdID), body: item).authorized(with: accessToken))
    }

    func updateItem(
        householdID: String, itemID: String, changes: PantryItemChanges, accessToken: String
    ) async throws -> PantryItem {
        try await client.send(
            try APIRequest.patch(Self.path(householdID) + "/\(itemID)", body: changes).authorized(with: accessToken))
    }

    func deleteItem(householdID: String, itemID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID) + "/\(itemID)").authorized(with: accessToken))
    }

    /// Sets up to `maxBulkUpdates` statuses. IDs no longer in the pantry come back in `missing`.
    func setStatuses(
        householdID: String, updates: [PantryStatusUpdate], accessToken: String
    ) async throws -> PantryBulkStatusResponse {
        try await client.send(
            try APIRequest.post(Self.path(householdID) + "/bulk", body: PantryBulkStatusRequest(items: updates))
                .authorized(with: accessToken))
    }

    /// Adds the default staples the pantry doesn't have yet. Safe to repeat.
    func addDefaultStaples(householdID: String, accessToken: String) async throws -> PantryDefaultStaplesResponse {
        let request = APIRequest(
            method: .post, path: Self.path(householdID) + "/staples/defaults", body: nil, bearerToken: nil)
        return try await client.send(request.authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/pantry"
    }
}

/// Search in the global ingredient catalog (`GET /api/v1/ingredients`). Needs only a
/// signed-in user.
nonisolated struct IngredientsAPI: Sendable {
    static let maxQueryLength = 100
    static let maxLimit = 50

    let client: APIClient

    func search(query: String, limit: Int, accessToken: String) async throws -> [CatalogIngredient] {
        var request = APIRequest.get("/api/v1/ingredients")
        request.queryItems = Self.queryItems(query: query, limit: limit)
        let response: IngredientSearchResponse = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    /// The query trimmed and cut to the API's limit; `limit` kept within 1...50.
    static func queryItems(query: String, limit: Int) -> [URLQueryItem] {
        let trimmed = String(query.trimmingCharacters(in: .whitespacesAndNewlines).prefix(maxQueryLength))
        return [
            URLQueryItem(name: "q", value: trimmed),
            URLQueryItem(name: "limit", value: String(min(max(limit, 1), maxLimit))),
        ]
    }
}
