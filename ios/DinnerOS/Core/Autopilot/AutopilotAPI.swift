import Foundation

/// Typed wrappers for the Autopilot endpoints
/// (`/api/v1/households/{householdId}/autopilot`). Use them through `AuthSession.authorized`.
nonisolated struct AutopilotAPI: Sendable {
    let client: APIClient

    /// History reads at most this many changes.
    static let maxHistoryLimit = 100

    // MARK: Profile

    /// The household's profile; the defaults with `configured: false` before it's saved.
    func profile(householdID: String, accessToken: String) async throws -> AutopilotProfile {
        try await client.send(APIRequest.get(Self.path(householdID, "/profile")).authorized(with: accessToken))
    }

    /// Replaces the whole profile (onboarding).
    func replaceProfile(
        householdID: String, settings: AutopilotSettings, accessToken: String
    ) async throws -> AutopilotProfile {
        let request = try APIRequest.put(Self.path(householdID, "/profile"), body: settings)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Replaces only the update's sections, each whole.
    func updateProfile(
        householdID: String, update: AutopilotProfileUpdate, accessToken: String
    ) async throws -> AutopilotProfile {
        let request = try APIRequest.patch(Self.path(householdID, "/profile"), body: update)
        return try await client.send(request.authorized(with: accessToken))
    }

    func vocabulary(householdID: String, accessToken: String) async throws -> AutopilotVocabulary {
        try await client.send(APIRequest.get(Self.path(householdID, "/vocabulary")).authorized(with: accessToken))
    }

    /// Preference, week context, and override changes, newest first.
    func history(householdID: String, limit: Int, accessToken: String) async throws -> [AutopilotHistoryItem] {
        var request = APIRequest.get(Self.path(householdID, "/profile/history"))
        request.queryItems = [URLQueryItem(name: "limit", value: String(min(max(limit, 1), Self.maxHistoryLimit)))]
        let response: AutopilotHistory = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    // MARK: Recipes

    func attributes(householdID: String, recipeID: String, accessToken: String) async throws
        -> AutopilotRecipeAttributes
    {
        try await client.send(
            APIRequest.get(Self.path(householdID, "/recipes/\(recipeID)/attributes")).authorized(with: accessToken))
    }

    /// Sets methods to yes (`true`), no (`false`), or automatic (`nil`); others don't change.
    func setOverride(
        householdID: String, recipeID: String, methods: [String: Bool?], accessToken: String
    ) async throws -> AutopilotRecipeAttributes {
        let request = try APIRequest.put(
            Self.path(householdID, "/recipes/\(recipeID)/override"), body: AutopilotOverrideRequest(methods: methods))
        return try await client.send(request.authorized(with: accessToken))
    }

    // MARK: Week context

    func weekContext(householdID: String, week: ISOWeek, accessToken: String) async throws -> AutopilotWeekContext {
        try await client.send(
            APIRequest.get(Self.weekPath(householdID, week, "/context")).authorized(with: accessToken))
    }

    func saveWeekContext(
        householdID: String, week: ISOWeek, draft: AutopilotWeekContextDraft, accessToken: String
    ) async throws -> AutopilotWeekContext {
        let request = try APIRequest.put(Self.weekPath(householdID, week, "/context"), body: draft)
        return try await client.send(request.authorized(with: accessToken))
    }

    func clearWeekContext(householdID: String, week: ISOWeek, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.weekPath(householdID, week, "/context")).authorized(with: accessToken))
    }

    // MARK: Proposals

    func generate(
        householdID: String, week: ISOWeek, options: AutopilotGenerateRequest = AutopilotGenerateRequest(),
        accessToken: String
    ) async throws -> AutopilotProposal {
        let request = try APIRequest.post(Self.weekPath(householdID, week, "/generate"), body: options)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// The week's latest proposal, whatever its status. `404` when there's none.
    func proposal(householdID: String, week: ISOWeek, accessToken: String) async throws -> AutopilotProposal {
        try await client.send(
            APIRequest.get(Self.weekPath(householdID, week, "/proposal")).authorized(with: accessToken))
    }

    func swap(
        householdID: String, week: ISOWeek, slotID: String, version: Int, accessToken: String
    ) async throws -> AutopilotProposal {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/proposal/slots/\(slotID)/swap"),
            body: AutopilotVersionRequest(version: version))
        return try await client.send(request.authorized(with: accessToken))
    }

    func accept(
        householdID: String, week: ISOWeek, version: Int, excludeSlotIDs: [String], accessToken: String
    ) async throws -> AutopilotAcceptResult {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/proposal/accept"),
            body: AutopilotAcceptRequest(version: version, excludeSlotIDs: excludeSlotIDs))
        return try await client.send(request.authorized(with: accessToken))
    }

    func reject(
        householdID: String, week: ISOWeek, version: Int, accessToken: String
    ) async throws -> AutopilotProposal {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/proposal/reject"), body: AutopilotVersionRequest(version: version))
        return try await client.send(request.authorized(with: accessToken))
    }

    // MARK: Paths

    private static func path(_ householdID: String, _ suffix: String) -> String {
        "/api/v1/households/\(householdID)/autopilot" + suffix
    }

    private static func weekPath(_ householdID: String, _ week: ISOWeek, _ suffix: String) -> String {
        path(householdID, "/weeks/\(week.description)" + suffix)
    }
}
