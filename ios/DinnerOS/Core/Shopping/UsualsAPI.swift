import Foundation

/// Add Item and "Your usuals" (docs/api.md#your-usuals).
extension ShoppingAPI {
    /// Adds items to a week's list. Adding one the week has sets its count.
    func addItems(
        householdID: String, week: String, request: AddGroceryItemsRequest, accessToken: String
    ) async throws -> [AddedGroceryItem] {
        let response: AddedGroceryItemList = try await client.send(
            try APIRequest.post(Self.path(householdID) + "/weeks/\(week)/items", body: request)
                .authorized(with: accessToken))
        return response.items
    }

    /// Takes an added item off its week (`204`).
    func removeAddedItem(householdID: String, week: String, itemID: String, accessToken: String) async throws {
        let request = APIRequest.delete(
            Self.path(householdID) + "/weeks/\(week)/items/" + APIRequest.encodePathSegment(itemID))
        try await client.sendIgnoringBody(request.withPercentEncodedPath().authorized(with: accessToken))
    }

    /// Takes the week's added item for an ingredient off (`204`).
    func removeAddedItem(householdID: String, week: String, ingredientKey: String, accessToken: String) async throws {
        var request = APIRequest.delete(Self.path(householdID) + "/weeks/\(week)/items")
        request.queryItems = [URLQueryItem(name: "ingredientKey", value: ingredientKey)]
        try await client.sendIgnoringBody(request.authorized(with: accessToken))
    }

    /// The household's usuals for a week, and whether to ask "Add your usuals?".
    func usuals(householdID: String, week: String, accessToken: String) async throws -> WeekUsuals {
        try await client.send(
            APIRequest.get(Self.path(householdID) + "/weeks/\(week)/usuals").authorized(with: accessToken))
    }

    /// Answers the week's prompt with Not Now (`204`).
    func dismissUsuals(householdID: String, week: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest(
                method: .post, path: Self.path(householdID) + "/weeks/\(week)/usuals/dismiss", body: nil,
                bearerToken: nil
            )
            .authorized(with: accessToken))
    }

    /// Pins an item, marks it "not a usual", or puts it back.
    func setUsual(householdID: String, request: SetUsualRequest, accessToken: String) async throws -> UsualPref {
        let response: UsualPrefEnvelope = try await client.send(
            try APIRequest.put(Self.path(householdID) + "/usuals", body: request).authorized(with: accessToken))
        return response.usual
    }
}
