import Foundation

/// Typed wrappers for the skipped ingredient endpoints
/// (`/api/v1/households/{householdId}/grocery-skips`; docs/api.md#skipped-ingredients).
/// Reading needs `household.view`; skipping and resuming need `plan.edit`. Use them through
/// `AuthSession.authorized`.
nonisolated struct GrocerySkipsAPI: Sendable {
    let client: APIClient

    /// The household's skipped ingredients, newest first.
    func list(householdID: String, accessToken: String) async throws -> [GrocerySkip] {
        let response: GrocerySkipListResponse = try await client.send(
            APIRequest.get(Self.path(householdID)).authorized(with: accessToken))
        return response.items
    }

    /// Skips an ingredient. An ingredient the household already skips has that skip replaced,
    /// so changing "this week" to "never" is one call and never leaves two skips behind.
    func skip(
        householdID: String, request body: GrocerySkipRequest, accessToken: String
    ) async throws -> GrocerySkip {
        let request = try APIRequest.post(Self.path(householdID), body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Resumes an ingredient: it is back on the next list built.
    func resume(householdID: String, skipID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID) + "/\(skipID)").authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/grocery-skips"
    }
}
