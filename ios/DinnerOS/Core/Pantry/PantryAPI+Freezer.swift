import Foundation

/// The freezer and today's thaw reminders (docs/pantry-usage.md#the-freezer).
nonisolated extension PantryAPI {
    /// The household's freezer, newest reads first in aisle order like the rest of the pantry.
    func freezerItems(householdID: String, accessToken: String) async throws -> [PantryItem] {
        var request = APIRequest.get(Self.path(householdID))
        request.queryItems = [URLQueryItem(name: "storage", value: PantryStorage.freezer.rawValue)]
        let response: PantryItemListResponse = try await client.send(request.authorized(with: accessToken))
        return response.items
    }

    /// Records a bulk pack's remainder in the freezer. Sending the same handoff line again
    /// answers `alreadyFrozen` and changes nothing. Requires `pantry.edit`.
    func freeze(
        householdID: String, request: FreezePantryItemRequest, accessToken: String
    ) async throws -> FreezePantryItemResponse {
        try await client.send(
            try APIRequest.post(Self.path(householdID) + "/freezer", body: request).authorized(with: accessToken))
    }

    /// What today's planned meals need out of the freezer, with when to move each one over.
    func thawDue(householdID: String, accessToken: String) async throws -> ThawDue {
        try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/thaw").authorized(with: accessToken))
    }
}
