import Foundation

/// Purchases and the low-stock setting (docs/pantry-usage.md).
nonisolated extension PantryAPI {
    /// Records a purchase. A repeated `clientPurchaseID` answers `200` with the first
    /// purchase and changes nothing. Requires `pantry.edit`.
    func recordPurchase(
        householdID: String, purchase: NewPantryPurchase, accessToken: String
    ) async throws -> PantryPurchaseResponse {
        try await client.send(
            try APIRequest.post(Self.path(householdID) + "/purchases", body: purchase).authorized(with: accessToken))
    }

    /// The item's 20 most recent purchases, newest first.
    func purchases(householdID: String, itemID: String, accessToken: String) async throws -> [PantryPurchase] {
        let response: PantryPurchaseListResponse = try await client.send(
            APIRequest.get(Self.path(householdID) + "/\(itemID)/purchases").authorized(with: accessToken))
        return response.items
    }

    func settings(householdID: String, accessToken: String) async throws -> PantrySettings {
        try await client.send(APIRequest.get(Self.path(householdID) + "/settings").authorized(with: accessToken))
    }

    /// Sets the household's threshold. Estimates use it from the next read. Requires `pantry.edit`.
    func updateSettings(
        householdID: String, lowThresholdPercent: Int, accessToken: String
    ) async throws -> PantrySettings {
        try await client.send(
            try APIRequest.put(
                Self.path(householdID) + "/settings",
                body: PantrySettingsUpdate(lowThresholdPercent: lowThresholdPercent)
            )
            .authorized(with: accessToken))
    }
}
