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

    /// Hands the server the push token of the run's Live Activity, so it can update the activity
    /// while the app is closed. Called again whenever ActivityKit rotates the token. The token
    /// goes in the body, never the path, so it stays out of request logs.
    func registerLiveActivity(
        householdID: String, service: String, jobID: String, token: String, environment: PushEnvironment,
        accessToken: String
    ) async throws {
        try await client.sendIgnoringBody(
            try APIRequest.put(
                liveActivityPath(householdID, service, jobID),
                body: MealKitLiveActivityRegistration(token: token, environment: environment)
            ).authorized(with: accessToken))
    }

    /// Forgets the run's Live Activity token, for a member who swiped the activity away.
    func unregisterLiveActivity(householdID: String, service: String, jobID: String, accessToken: String)
        async throws
    {
        try await client.sendIgnoringBody(
            APIRequest.delete(liveActivityPath(householdID, service, jobID)).authorized(with: accessToken))
    }

    private func liveActivityPath(_ householdID: String, _ service: String, _ jobID: String) -> String {
        "/api/v1/households/\(householdID)/meal-kit/\(service)/imports/\(jobID)/live-activity"
    }
}

/// Body of `PUT .../imports/{jobId}/live-activity`.
nonisolated struct MealKitLiveActivityRegistration: Encodable, Equatable, Sendable {
    /// The activity's push token as lowercase hex — not the device token.
    let token: String
    let environment: PushEnvironment
}
