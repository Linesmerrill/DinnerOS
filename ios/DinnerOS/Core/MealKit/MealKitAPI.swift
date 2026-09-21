import Foundation

/// Typed wrappers for the meal-kit import endpoints
/// (`/api/v1/households/{householdId}/meal-kit/{source}`). Use them through
/// `AuthSession.authorized`.
///
/// Nothing here logs a request: the link body carries the member's meal-kit password.
nonisolated struct MealKitAPI: Sendable {
    let client: APIClient

    private func path(_ householdID: String, _ service: MealKitService, _ suffix: String = "") -> String {
        "/api/v1/households/\(householdID)/meal-kit/\(service.rawValue)\(suffix)"
    }

    /// The household's link and its newest run.
    ///
    /// A server without the route answers `404`, which the store reads as "this API doesn't
    /// have meal-kit import yet" and hides the screen, as the import review store does.
    func status(householdID: String, service: MealKitService, accessToken: String) async throws -> MealKitStatus {
        try await client.send(APIRequest.get(path(householdID, service)).authorized(with: accessToken))
    }

    /// Links the account and, by default, queues an import.
    ///
    /// The password is used by the server for one token exchange and then discarded; it is
    /// never written to the Keychain, `UserDefaults`, or a log on this device either.
    func link(
        householdID: String, service: MealKitService, email: String, password: String,
        startImport: Bool = true, accessToken: String
    ) async throws -> MealKitStatus {
        let request = try APIRequest.put(
            path(householdID, service, "/link"),
            body: MealKitLinkRequest(email: email, password: password, startImport: startImport))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Deletes the stored tokens and stops every run for that service.
    func unlink(householdID: String, service: MealKitService, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(path(householdID, service, "/link")).authorized(with: accessToken))
    }

    /// Queues a run. A run already in flight is returned instead of a second one.
    func startImport(
        householdID: String, service: MealKitService, accessToken: String
    ) async throws -> MealKitImportJob {
        let request = APIRequest(
            method: .post, path: path(householdID, service, "/imports"), body: nil, bearerToken: nil)
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
}
