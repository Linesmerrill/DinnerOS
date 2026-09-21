import Foundation

/// Typed wrappers for the discovery and global-catalog endpoints
/// (`/api/v1/households/{householdId}/discover` and `.../catalog/recipes`).
/// Use them through `AuthSession.authorized`.
nonisolated struct CatalogAPI: Sendable {
    let client: APIClient

    /// "Try something else": catalog recipes the household does not already have.
    func discover(
        householdID: String, query: CatalogQuery, cursor: String?, limit: Int, accessToken: String
    ) async throws -> CatalogListPage {
        var request = APIRequest.get("/api/v1/households/\(householdID)/discover")
        // Discovery has no free-text term: searching is the other screen.
        request.queryItems = Self.queryItems(query: query, includeSearch: false, cursor: cursor, limit: limit)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Search the whole catalog. Results the household already has come back marked.
    func search(
        householdID: String, query: CatalogQuery, cursor: String?, limit: Int, accessToken: String
    ) async throws -> CatalogListPage {
        var request = APIRequest.get("/api/v1/households/\(householdID)/catalog/recipes")
        request.queryItems = Self.queryItems(query: query, includeSearch: true, cursor: cursor, limit: limit)
        return try await client.send(request.authorized(with: accessToken))
    }

    func recipe(householdID: String, id: String, accessToken: String) async throws -> CatalogRecipe {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/catalog/recipes/\(id)").authorized(with: accessToken))
    }

    /// Copies the catalog recipe into the household's library. Adding one the
    /// household already has succeeds and returns its existing recipe.
    func add(householdID: String, id: String, accessToken: String) async throws -> AddToLibraryResult {
        // The request carries no body: the path says which recipe, and the
        // caller's membership says which household.
        let request = APIRequest(
            method: .post, path: "/api/v1/households/\(householdID)/catalog/recipes/\(id)/add", body: nil,
            bearerToken: nil)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Empty values are omitted rather than sent blank.
    static func queryItems(
        query: CatalogQuery, includeSearch: Bool, cursor: String?, limit: Int
    ) -> [URLQueryItem] {
        var items: [URLQueryItem] = []
        let search = query.normalizedSearch
        if includeSearch, !search.isEmpty {
            items.append(URLQueryItem(name: "q", value: search))
        }
        if let cuisine = query.cuisine, !cuisine.isEmpty {
            items.append(URLQueryItem(name: "cuisine", value: cuisine))
        }
        if let tag = query.tag, !tag.isEmpty {
            items.append(URLQueryItem(name: "tag", value: tag))
        }
        items.append(URLQueryItem(name: "limit", value: String(limit)))
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return items
    }
}
