import Foundation

/// A member's private note on a recipe: what they changed, what to try next time. Nobody else
/// in the household sees it.
nonisolated struct RecipeNote: Codable, Equatable, Sendable {
    let recipeID: String
    let text: String
    /// `nil` when there is no note.
    let updatedAt: Date?

    private enum CodingKeys: String, CodingKey {
        case text, updatedAt
        case recipeID = "recipeId"
    }
}

nonisolated struct RecipeNoteRequest: Encodable, Sendable {
    let text: String
}

extension RecipesAPI {
    /// The caller's own note, empty when there is none.
    func note(householdID: String, recipeID: String, accessToken: String) async throws -> RecipeNote {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/recipes/\(recipeID)/note").authorized(with: accessToken))
    }

    /// Replaces the caller's note; blank text deletes it.
    func saveNote(householdID: String, recipeID: String, text: String, accessToken: String) async throws -> RecipeNote {
        let request = try APIRequest.put(
            "/api/v1/households/\(householdID)/recipes/\(recipeID)/note", body: RecipeNoteRequest(text: text))
        return try await client.send(request.authorized(with: accessToken))
    }
}
