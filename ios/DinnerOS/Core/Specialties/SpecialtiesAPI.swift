import Foundation

/// Typed wrappers for the specialty ingredient endpoints
/// (`/api/v1/households/{householdId}/specialty-ingredients`; docs/specialty-ingredients.md).
/// Reads need `household.view`; every change needs `pantry.edit`. Use them through
/// `AuthSession.authorized`.
nonisolated struct SpecialtiesAPI: Sendable {
    /// The API keeps at most this many household options per specialty ingredient.
    static let maxHouseholdOptions = 10
    /// The most batches one request records.
    static let maxBatches = 10

    let client: APIClient

    /// The specialty ingredients the household's recipes use, most used first. `all` includes
    /// every curated one.
    func list(householdID: String, all: Bool = false, accessToken: String) async throws -> [SpecialtyIngredient] {
        var request = APIRequest.get(Self.path(householdID))
        if all {
            request.queryItems = [URLQueryItem(name: "all", value: "true")]
        }
        let response: SpecialtyIngredientList = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    func ingredient(householdID: String, specialtyID: String, accessToken: String) async throws -> SpecialtyIngredient {
        try await client.send(
            APIRequest.get(Self.path(householdID, specialtyID)).authorized(with: accessToken))
    }

    /// The household's standing answer for the specialty ingredients nobody has chosen for,
    /// with the server's words for every strategy (`household.view`).
    func settings(householdID: String, accessToken: String) async throws -> SpecialtySettings {
        try await client.send(
            APIRequest.get(Self.path(householdID) + "/settings").authorized(with: accessToken))
    }

    /// Sets the standing answer (`pantry.edit`). Nothing is written to the household's choices:
    /// the strategy is resolved whenever a grocery list is built.
    func setSettings(
        householdID: String, strategy: SpecialtyStrategy, accessToken: String
    ) async throws -> SpecialtySettings {
        let request = try APIRequest.put(
            Self.path(householdID) + "/settings", body: SpecialtySettingsUpdate(strategy: strategy))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Chooses `defaultOptionId` for every used specialty ingredient without a choice.
    func applyDefaults(householdID: String, accessToken: String) async throws -> SpecialtyDefaultsResponse {
        let request = APIRequest(
            method: .post, path: Self.path(householdID) + "/choices/defaults", body: nil, bearerToken: nil)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Chooses an option, or `SpecialtyChoice.asIsOptionID`.
    func setChoice(
        householdID: String, specialtyID: String, optionID: String, accessToken: String
    ) async throws -> SpecialtyIngredient {
        let request = try APIRequest.put(
            Self.path(householdID, specialtyID) + "/choice", body: SpecialtyChoiceRequest(optionID: optionID))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Clears the choice. Clearing twice is fine.
    func clearChoice(householdID: String, specialtyID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID, specialtyID) + "/choice").authorized(with: accessToken))
    }

    func createOption(
        householdID: String, specialtyID: String, option: SpecialtyOptionRequest, accessToken: String
    ) async throws -> SpecialtyOption {
        let request = try APIRequest.post(Self.path(householdID, specialtyID) + "/options", body: option)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Replaces a household option's content. Curated options can't be edited.
    func updateOption(
        householdID: String, specialtyID: String, optionID: String, option: SpecialtyOptionRequest,
        accessToken: String
    ) async throws -> SpecialtyOption {
        let request = try APIRequest.put(
            Self.path(householdID, specialtyID) + "/options/\(optionID)", body: option)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Deletes a household option and clears any choice of it.
    func deleteOption(householdID: String, specialtyID: String, optionID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID, specialtyID) + "/options/\(optionID)")
                .authorized(with: accessToken))
    }

    /// Records a batch made: a `house_made` pantry purchase. A repeated `clientPurchaseID`
    /// answers `200` with the first purchase and changes nothing.
    func recordBatch(
        householdID: String, specialtyID: String, request body: RecordSpecialtyBatchRequest, accessToken: String
    ) async throws -> RecordSpecialtyBatchResponse {
        let request = try APIRequest.post(Self.path(householdID, specialtyID) + "/batches", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/specialty-ingredients"
    }

    static func path(_ householdID: String, _ specialtyID: String) -> String {
        path(householdID) + "/\(specialtyID)"
    }
}
