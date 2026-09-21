import Foundation

/// The meal-kit session a member's own sign-in produced, read out of the service's auth cookie
/// in the sign-in web view (`docs/meal-kit-import.md`).
///
/// It never leaves the device. It exists so the harvest script can make the same account
/// requests the site itself makes, inside that web view; when the sheet closes it is gone, along
/// with the web view's whole data store. It is never sent to our API, never written to the
/// Keychain or `UserDefaults`, and never logged.
///
/// The page itself is untrusted data: nothing reads its contents, and nothing screenshots it.
nonisolated struct MealKitWebSession: Equatable, Sendable {
    /// The token the harvest sends in its Authorization header, in the web view and nowhere
    /// else.
    let accessToken: String
    /// The scheme the Authorization header wants, e.g. `Bearer`.
    let tokenType: String

    init(accessToken: String, tokenType: String = "Bearer") {
        self.accessToken = accessToken
        self.tokenType = tokenType.isEmpty ? "Bearer" : tokenType
    }

    /// Reads a session out of the service's auth cookie, whose value is URL-encoded JSON.
    ///
    /// Returns `nil` for anything that isn't a usable session, which is how a cookie that exists
    /// but hasn't been signed in yet is told from one that has. Nothing is guessed: a cookie this
    /// build cannot read is no session at all.
    init?(cookieValue: String) {
        let decoded = cookieValue.removingPercentEncoding ?? cookieValue
        guard let data = decoded.data(using: .utf8),
            let payload = try? JSONDecoder().decode(Payload.self, from: data)
        else { return nil }
        let access = payload.accessToken.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !access.isEmpty else { return nil }
        self.init(accessToken: access, tokenType: payload.tokenType ?? "Bearer")
    }

    /// The two fields of the cookie this build reads. Everything else it carries — the account's
    /// email, id, roles, the refresh token — is deliberately not decoded: none of it is needed,
    /// and none of it is going anywhere, so it is never taken out of the cookie at all.
    private struct Payload: Decodable {
        let accessToken: String
        let tokenType: String?

        private enum CodingKeys: String, CodingKey {
            case accessToken = "access_token"
            case tokenType = "token_type"
        }
    }

}
