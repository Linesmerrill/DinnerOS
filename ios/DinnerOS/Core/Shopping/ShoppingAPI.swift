import Foundation

/// Typed wrappers for the shopping endpoints (Phase 8a; docs/api.md#shopping). Use them
/// through `AuthSession.authorized`.
nonisolated struct ShoppingAPI: Sendable {
    let client: APIClient

    /// The enabled providers. Needs only a signed-in user.
    func providers(accessToken: String) async throws -> [ShoppingProvider] {
        let response: ShoppingProviderList = try await client.send(
            APIRequest.get("/api/v1/shopping/providers").authorized(with: accessToken))
        return response.items
    }

    func settings(householdID: String, accessToken: String) async throws -> ShoppingSettings {
        try await client.send(APIRequest.get(Self.path(householdID) + "/settings").authorized(with: accessToken))
    }

    func updateSettings(
        householdID: String, settings: UpdateShoppingSettingsRequest, accessToken: String
    ) async throws -> ShoppingSettings {
        try await client.send(
            try APIRequest.put(Self.path(householdID) + "/settings", body: settings).authorized(with: accessToken))
    }

    /// Saved products, by ingredient name.
    func preferences(householdID: String, provider: String, accessToken: String) async throws -> [ShoppingPreference] {
        let response: ShoppingPreferenceList = try await client.send(
            APIRequest.get(Self.path(householdID) + "/\(provider)/preferences").authorized(with: accessToken))
        return response.items
    }

    /// Saves (`201`) or replaces (`200`) the product for a grocery line's `ingredientKey`.
    func savePreference(
        householdID: String, provider: String, ingredientKey: String, preference: ShoppingPreferenceRequest,
        accessToken: String
    ) async throws -> ShoppingPreference {
        let request = try APIRequest.put(
            Self.preferencePath(householdID: householdID, provider: provider, ingredientKey: ingredientKey),
            body: preference)
        return try await client.send(request.withPercentEncodedPath().authorized(with: accessToken))
    }

    func deletePreference(householdID: String, provider: String, ingredientKey: String, accessToken: String)
        async throws
    {
        let request = APIRequest.delete(
            Self.preferencePath(householdID: householdID, provider: provider, ingredientKey: ingredientKey))
        try await client.sendIgnoringBody(request.withPercentEncodedPath().authorized(with: accessToken))
    }

    /// Matches the week's list to saved products. Nothing is stored.
    func match(
        householdID: String, week: ISOWeek, provider: String, request: ShoppingMatchRequest, accessToken: String
    ) async throws -> ShoppingProposal {
        try await client.send(
            try APIRequest.post(Self.weekPath(householdID, week, provider) + "/match", body: request)
                .authorized(with: accessToken))
    }

    /// Matches like `match` and stores the result as a handoff (`201`).
    func createHandoff(
        householdID: String, week: ISOWeek, provider: String, request: ShoppingMatchRequest, accessToken: String
    ) async throws -> ShoppingHandoff {
        try await client.send(
            try APIRequest.post(Self.weekPath(householdID, week, provider) + "/handoffs", body: request)
                .authorized(with: accessToken))
    }

    /// Handoffs, newest first.
    func handoffs(
        householdID: String, week: ISOWeek? = nil, status: ShoppingHandoffStatus? = nil, limit: Int? = nil,
        accessToken: String
    ) async throws -> [ShoppingHandoff] {
        var request = APIRequest.get(Self.path(householdID) + "/handoffs")
        request.queryItems = [
            week.map { URLQueryItem(name: "week", value: $0.description) },
            status.map { URLQueryItem(name: "status", value: $0.rawValue) },
            limit.map { URLQueryItem(name: "limit", value: String($0)) },
        ].compactMap { $0 }
        let response: ShoppingHandoffList = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    func handoff(householdID: String, handoffID: String, accessToken: String) async throws -> ShoppingHandoff {
        try await client.send(
            APIRequest.get(Self.path(householdID) + "/handoffs/\(handoffID)").authorized(with: accessToken))
    }

    /// Records what a member says was ordered. Idempotent per line.
    func confirm(
        householdID: String, handoffID: String, request: ConfirmShoppingOrderRequest, accessToken: String
    ) async throws -> ConfirmShoppingOrderResponse {
        try await client.send(
            try APIRequest.post(Self.path(householdID) + "/handoffs/\(handoffID)/confirm", body: request)
                .authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/shopping"
    }

    private static func weekPath(_ householdID: String, _ week: ISOWeek, _ provider: String) -> String {
        "/api/v1/households/\(householdID)/plans/\(week.description)/shopping/\(provider)"
    }

    /// Percent-encoded: an `ingredientKey` such as `name:red onion` or `name:half & half` is
    /// one path segment.
    static func preferencePath(householdID: String, provider: String, ingredientKey: String) -> String {
        let segments = [householdID, provider].map(APIRequest.encodePathSegment)
        return "/api/v1/households/\(segments[0])/shopping/\(segments[1])/preferences/"
            + APIRequest.encodePathSegment(ingredientKey)
    }
}
