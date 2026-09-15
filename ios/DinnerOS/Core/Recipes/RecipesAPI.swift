import Foundation

/// Typed wrappers for the recipe read endpoints (`/api/v1/households/{householdId}/recipes`).
/// Use them through `AuthSession.authorized`.
nonisolated struct RecipesAPI: Sendable {
    let client: APIClient

    /// One page of summaries. Pass the previous page's `nextCursor` with the same filters.
    func listRecipes(
        householdID: String, filters: RecipeListFilters, cursor: String?, limit: Int, accessToken: String
    ) async throws -> RecipeListPage {
        var request = APIRequest.get("/api/v1/households/\(householdID)/recipes")
        request.queryItems = Self.queryItems(filters: filters, cursor: cursor, limit: limit)
        return try await client.send(request.authorized(with: accessToken))
    }

    func recipe(householdID: String, id: String, accessToken: String) async throws -> Recipe {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/recipes/\(id)").authorized(with: accessToken))
    }

    /// Empty values are omitted rather than sent blank.
    static func queryItems(filters: RecipeListFilters, cursor: String?, limit: Int) -> [URLQueryItem] {
        var items = [URLQueryItem(name: "sort", value: filters.sort.rawValue)]
        let search = filters.normalizedSearch
        if !search.isEmpty {
            items.append(URLQueryItem(name: "q", value: search))
        }
        if let addons = filters.kind.addonsParameter {
            items.append(URLQueryItem(name: "addons", value: addons ? "true" : "false"))
        }
        if let tag = filters.tag, !tag.isEmpty {
            items.append(URLQueryItem(name: "tag", value: tag))
        }
        if let cuisine = filters.cuisine, !cuisine.isEmpty {
            items.append(URLQueryItem(name: "cuisine", value: cuisine))
        }
        items.append(URLQueryItem(name: "limit", value: String(limit)))
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return items
    }
}
