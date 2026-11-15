import Foundation

/// Typed wrappers for `/api/v1/households/{householdId}/plus` (docs/api.md). Use them through
/// `AuthSession.authorized`.
nonisolated struct PlusAPI: Sendable {
    let client: APIClient

    func status(householdID: String, accessToken: String) async throws -> PlusStatus {
        try await client.send(APIRequest.get(Self.path(householdID)).authorized(with: accessToken))
    }

    func referral(householdID: String, accessToken: String) async throws -> PlusReferral {
        try await client.send(APIRequest.get(Self.path(householdID) + "/referral").authorized(with: accessToken))
    }

    /// Uses another household's code. Needs `plan.edit`.
    /// The referral is pending until the new household sets up (decision 698); `deviceCheck`
    /// lets the API check this device isn't already another household's.
    func redeem(
        householdID: String, code: String, deviceCheck: DeviceCheckToken? = nil, accessToken: String
    ) async throws -> PlusStatus {
        let request = try APIRequest.post(
            Self.path(householdID) + "/referral/redeem", body: RedeemRequest(code: code, deviceCheck: deviceCheck))
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Your Plus: plan, status, history and stats.
    func details(householdID: String, accessToken: String) async throws -> PlusDetails {
        try await client.send(APIRequest.get(Self.path(householdID) + "/details").authorized(with: accessToken))
    }

    /// Referrals across every household. 403 for anyone who doesn't run DinnerOS.
    func referralSummary(accessToken: String) async throws -> PlusReferralSummary {
        try await client.send(APIRequest.get("/api/v1/plus/referrals/summary").authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/plus"
    }

    private struct RedeemRequest: Encodable {
        let code: String
        let deviceCheck: DeviceCheckToken?
    }
}
