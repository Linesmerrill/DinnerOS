import Foundation

/// Typed wrappers for the meal-kit import endpoints
/// (`/api/v1/households/{householdId}/meal-kit/{source}`). Use them through
/// `AuthSession.authorized`.
///
/// There is no link call, because there is nothing to link: an import carries the order history
/// the member's own browser session just read, and the server keeps no credential.
nonisolated struct MealKitAPI: Sendable {
    let client: APIClient

    private func path(_ householdID: String, _ service: MealKitService, _ suffix: String = "") -> String {
        "/api/v1/households/\(householdID)/meal-kit/\(service.rawValue)\(suffix)"
    }

    /// The household's newest run.
    ///
    /// A server without the route answers `404`, which the store reads as "this API doesn't
    /// have meal-kit import yet" and hides the screen, as the import review store does.
    func status(householdID: String, service: MealKitService, accessToken: String) async throws -> MealKitStatus {
        try await client.send(APIRequest.get(path(householdID, service)).authorized(with: accessToken))
    }

    /// Queues a run for the harvested order history. A run already in flight is returned instead
    /// of a second one.
    ///
    /// `accessToken` is our own API's. The meal kit's session is not here and never will be.
    func startImport(
        householdID: String, service: MealKitService, harvest: MealKitHarvest, accessToken: String
    ) async throws -> MealKitImportJob {
        let request = try APIRequest.post(
            path(householdID, service, "/imports"), body: MealKitStartImportRequest(harvest: harvest))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// The household's runs for that service, newest first.
    func imports(
        householdID: String, service: MealKitService, accessToken: String
    ) async throws -> [MealKitImportJob] {
        let response: MealKitImportJobList = try await client.send(
            APIRequest.get(path(householdID, service, "/imports")).authorized(with: accessToken))
        return response.items
    }

    /// Stops every run in flight. Recipes already imported stay in the library.
    func stopImports(householdID: String, service: MealKitService, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(path(householdID, service, "/imports")).authorized(with: accessToken))
    }
}
