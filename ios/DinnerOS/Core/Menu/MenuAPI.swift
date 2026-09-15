import Foundation

/// Typed wrappers for the Menu screen's read endpoints (`/api/v1/households/{householdId}/menu`
/// and `/weeks`). Use them through `AuthSession.authorized`.
nonisolated struct MenuAPI: Sendable {
    let client: APIClient

    /// The week's plan, pending proposal, and recipe sections.
    func menu(householdID: String, week: ISOWeek, accessToken: String) async throws -> WeekMenu {
        var request = APIRequest.get(Self.householdPath(householdID) + "/menu")
        request.queryItems = [URLQueryItem(name: "week", value: week.description)]
        return try await client.send(request.authorized(with: accessToken))
    }

    /// One page of All Meals. Pass the previous page's `nextCursor` with the same query and week.
    func recipes(
        householdID: String, query: MenuRecipeQuery, week: ISOWeek?, cursor: String?, limit: Int,
        accessToken: String
    ) async throws -> MenuRecipePage {
        var request = APIRequest.get(Self.householdPath(householdID) + "/menu/recipes")
        request.queryItems = Self.queryItems(query: query, week: week, cursor: cursor, limit: limit)
        return try await client.send(request.authorized(with: accessToken))
    }

    func filters(householdID: String, accessToken: String) async throws -> MenuFilterOptions {
        try await client.send(
            APIRequest.get(Self.householdPath(householdID) + "/menu/filters").authorized(with: accessToken))
    }

    /// Week summaries from `before` weeks before `around` through `after` weeks after it.
    func weeks(
        householdID: String, around: ISOWeek, before: Int, after: Int, accessToken: String
    ) async throws -> WeekListResponse {
        var request = APIRequest.get(Self.householdPath(householdID) + "/weeks")
        request.queryItems = [
            URLQueryItem(name: "around", value: around.description),
            URLQueryItem(name: "before", value: String(max(before, 0))),
            URLQueryItem(name: "after", value: String(max(after, 0))),
        ]
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Empty values are omitted rather than sent blank.
    static func queryItems(query: MenuRecipeQuery, week: ISOWeek?, cursor: String?, limit: Int) -> [URLQueryItem] {
        var items = [URLQueryItem(name: "sort", value: query.sort.rawValue)]
        let search = query.normalizedSearch
        if !search.isEmpty {
            items.append(URLQueryItem(name: "q", value: search))
        }
        if let protein = query.protein {
            items.append(URLQueryItem(name: "protein", value: protein))
        }
        if let cuisine = query.cuisine {
            items.append(URLQueryItem(name: "cuisine", value: cuisine))
        }
        if let maxMinutes = query.maxMinutes {
            items.append(URLQueryItem(name: "maxMinutes", value: String(maxMinutes)))
        }
        if let tag = query.tag {
            items.append(URLQueryItem(name: "tag", value: tag))
        }
        if let addons = query.addons {
            items.append(URLQueryItem(name: "addons", value: addons ? "true" : "false"))
        }
        if let week {
            items.append(URLQueryItem(name: "week", value: week.description))
        }
        items.append(URLQueryItem(name: "limit", value: String(limit)))
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return items
    }

    private static func householdPath(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)"
    }
}
