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

    // MARK: Learning

    /// What Autopilot learned from the household's feedback, strongest first.
    func learning(householdID: String, accessToken: String) async throws -> AutopilotLearning {
        try await client.send(APIRequest.get(Self.path(householdID, "/learning")).authorized(with: accessToken))
    }

    /// Clears what Autopilot learned; ratings, history, and preferences are untouched. Needs
    /// `plan.edit`.
    func resetLearning(householdID: String, accessToken: String) async throws -> AutopilotLearning {
        try await client.send(APIRequest.delete(Self.path(householdID, "/learning")).authorized(with: accessToken))
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

    /// Adds the included slots and their chosen pairings. `pairingIDs` of `nil` leaves the
    /// field out, which accepts the ones the proposal includes.
    func accept(
        householdID: String, week: ISOWeek, version: Int, excludeSlotIDs: [String], pairingIDs: [String]? = nil,
        accessToken: String
    ) async throws -> AutopilotAcceptResult {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/proposal/accept"),
            body: AutopilotAcceptRequest(
                version: version, excludeSlotIDs: excludeSlotIDs, pairingIDs: pairingIDs))
        return try await client.send(request.authorized(with: accessToken))
    }

    func reject(
        householdID: String, week: ISOWeek, version: Int, accessToken: String
    ) async throws -> AutopilotProposal {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/proposal/reject"), body: AutopilotVersionRequest(version: version))
        return try await client.send(request.authorized(with: accessToken))
    }

    // MARK: Try something similar

    /// Meals like the planned one, for "Try Something Similar". `seen` are the recipes already
    /// offered, so asking again shows different ones. Needs `plan.edit`.
    func mealAlternatives(
        householdID: String, week: ISOWeek, entryID: String, limit: Int? = nil, seen: [String] = [],
        accessToken: String
    ) async throws -> MealAlternatives {
        let path =
            Self.weekPath(householdID, week, "/entries/") + APIRequest.encodePathSegment(entryID) + "/alternatives"
        var request = APIRequest.get(path).withPercentEncodedPath()
        if let limit {
            request.queryItems.append(URLQueryItem(name: "limit", value: String(limit)))
        }
        if !seen.isEmpty {
            request.queryItems.append(URLQueryItem(name: "seen", value: seen.joined(separator: ",")))
        }
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Replaces the planned meal's recipe, keeping its day, note, and servings. Needs `plan.edit`.
    func swapPlannedMeal(
        householdID: String, week: ISOWeek, entryID: String, recipeID: String, accessToken: String
    ) async throws -> PlannedMealSwapResult {
        let path = Self.weekPath(householdID, week, "/entries/") + APIRequest.encodePathSegment(entryID) + "/swap"
        let request = try APIRequest.post(path, body: PlannedMealSwapRequest(recipeID: recipeID))
        return try await client.send(request.withPercentEncodedPath().authorized(with: accessToken))
    }

    // MARK: Pairings

    /// The week's pairings: one entry per planned main meal, plus the accepted grocery items
    /// on the week's list. `entryID` narrows it to one meal, for the "you added a pasta dish"
    /// prompt. Needs `household.view`.
    func weekPairings(
        householdID: String, week: ISOWeek, entryID: String? = nil, accessToken: String
    ) async throws -> WeekPairings {
        var request = APIRequest.get(Self.weekPath(householdID, week, "/pairings"))
        if let entryID {
            request.queryItems = [URLQueryItem(name: "entryId", value: entryID)]
        }
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Adds a pairing to the week: an add-on becomes a plan entry on the meal's day, a
    /// grocery item joins the week's list. Needs `plan.edit`.
    func acceptPairing(
        householdID: String, week: ISOWeek, entryID: String, key: String, accessToken: String
    ) async throws -> PairingAcceptResult {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/pairings/accept"),
            body: PairingActionRequest(entryID: entryID, key: key))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Hides a pairing for that meal this week and returns the week's pairings.
    func dismissPairing(
        householdID: String, week: ISOWeek, entryID: String, key: String, accessToken: String
    ) async throws -> WeekPairings {
        let request = try APIRequest.post(
            Self.weekPath(householdID, week, "/pairings/dismiss"),
            body: PairingActionRequest(entryID: entryID, key: key))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Keeps a learned pairing as a household rule for its meal category.
    func makePairingRule(
        householdID: String, week: ISOWeek, request body: MakePairingRuleRequest, accessToken: String
    ) async throws -> MakePairingRuleResult {
        let request = try APIRequest.post(Self.weekPath(householdID, week, "/pairings/rules"), body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Takes a paired grocery item off the week's list (`204`).
    func deletePairingGroceryItem(
        householdID: String, week: ISOWeek, itemID: String, accessToken: String
    ) async throws {
        let path =
            Self.weekPath(householdID, week, "/pairings/grocery-items/")
            + APIRequest.encodePathSegment(itemID)
        try await client.sendIgnoringBody(
            APIRequest.delete(path).withPercentEncodedPath().authorized(with: accessToken))
    }

    // MARK: Paths

    private static func path(_ householdID: String, _ suffix: String) -> String {
        "/api/v1/households/\(householdID)/autopilot" + suffix
    }

    private static func weekPath(_ householdID: String, _ week: ISOWeek, _ suffix: String) -> String {
        path(householdID, "/weeks/\(week.description)" + suffix)
    }
}
