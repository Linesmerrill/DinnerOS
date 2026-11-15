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

    /// Whether `code` could be this household's custom code ("That code is taken.").
    func codeAvailability(householdID: String, code: String, accessToken: String) async throws
        -> ReferralCodeAvailability
    {
        var request = APIRequest.get(Self.path(householdID) + "/referral/code-availability")
        request.queryItems = [URLQueryItem(name: "code", value: code)]
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Replaces this household's code with `custom`, or a new random one when it's nil. The
    /// old code and link stop working for good. Needs `plan.edit`.
    func changeCode(householdID: String, custom: String?, accessToken: String) async throws -> PlusReferral {
        let body = ChangeCodeRequest(custom: custom, regenerate: custom == nil ? true : nil)
        let request = try APIRequest.post(Self.path(householdID) + "/referral/code", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    /// Who an invite code is from. No sign-in: a link opens the app before anyone has an
    /// account. Unknown and replaced codes are 404s with the words to show.
    func invitePreview(code: String) async throws -> InvitePreview {
        let escaped = code.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(["/"])) ?? code
        return try await client.send(APIRequest.get("/api/v1/referral-codes/" + escaped))
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

    private struct ChangeCodeRequest: Encodable {
        let custom: String?
        let regenerate: Bool?
    }

    private struct RedeemRequest: Encodable {
        let code: String
        let deviceCheck: DeviceCheckToken?
    }
}
