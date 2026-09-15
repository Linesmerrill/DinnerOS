import Foundation

/// Typed wrappers for the week plan endpoints
/// (`/api/v1/households/{householdId}/plans`). Use them through `AuthSession.authorized`.
nonisolated struct PlansAPI: Sendable {
    let client: APIClient

    /// Every week from `from` through `to`, including unplanned ones. The API allows at
    /// most `PlanLimits.maxListWeeks` weeks.
    func listPlans(householdID: String, from: ISOWeek, to: ISOWeek, accessToken: String) async throws -> [PlanSummary] {
        var request = APIRequest.get(Self.plansPath(householdID))
        request.queryItems = [
            URLQueryItem(name: "from", value: from.description), URLQueryItem(name: "to", value: to.description),
        ]
        let response: PlanListResponse = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    /// The week's plan; an empty draft when nobody has planned it.
    func plan(householdID: String, week: ISOWeek, accessToken: String) async throws -> Plan {
        try await client.send(APIRequest.get(Self.weekPath(householdID, week)).authorized(with: accessToken))
    }

    func addEntry(
        householdID: String, week: ISOWeek, entry: NewPlanEntry, accessToken: String
    ) async throws -> AddPlanEntryResponse {
        let request = try APIRequest.post(Self.weekPath(householdID, week) + "/entries", body: entry)
        return try await client.send(request.authorized(with: accessToken))
    }

    func updateEntry(
        householdID: String, week: ISOWeek, entryID: String, changes: PlanEntryChanges, accessToken: String
    ) async throws -> Plan {
        let request = try APIRequest.patch(Self.weekPath(householdID, week) + "/entries/\(entryID)", body: changes)
        return try await client.send(request.authorized(with: accessToken))
    }

    func deleteEntry(householdID: String, week: ISOWeek, entryID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.weekPath(householdID, week) + "/entries/\(entryID)").authorized(with: accessToken))
    }

    /// Replaces an entry's ingredient choices and returns the plan.
    func setCustomization(
        householdID: String, week: ISOWeek, entryID: String, request body: PlanCustomizationRequest,
        accessToken: String
    ) async throws -> Plan {
        let request = try APIRequest.put(
            Self.weekPath(householdID, week) + "/entries/\(entryID)/customization", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    func setStatus(householdID: String, week: ISOWeek, status: PlanStatus, accessToken: String) async throws -> Plan {
        let request = try APIRequest.put(
            Self.weekPath(householdID, week) + "/status", body: ["status": status.rawValue])
        return try await client.send(request.authorized(with: accessToken))
    }

    func groceryList(householdID: String, week: ISOWeek, accessToken: String) async throws -> GroceryList {
        try await client.send(
            APIRequest.get(Self.weekPath(householdID, week) + "/grocery").authorized(with: accessToken))
    }

    private static func plansPath(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/plans"
    }

    private static func weekPath(_ householdID: String, _ week: ISOWeek) -> String {
        plansPath(householdID) + "/\(week.description)"
    }
}
