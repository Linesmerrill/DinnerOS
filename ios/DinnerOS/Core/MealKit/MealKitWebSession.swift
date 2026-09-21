import Foundation

/// The meal-kit session a member's own sign-in produced, read out of the service's auth cookie
/// in the sign-in web view (`docs/meal-kit-import.md`).
///
/// This is the only thing the app ever takes out of that web view. The page itself is untrusted
/// data: nothing reads its contents, nothing screenshots it, and nothing about it is logged. The
/// tokens live in memory until they are `PUT` to our own API and are never written to the
/// Keychain, `UserDefaults`, or a file on this device.
nonisolated struct MealKitWebSession: Equatable, Sendable {
    /// The session token the importer replays against the meal kit's account API.
    let accessToken: String
    /// The refresh token, or "" when the cookie carried none.
    let refreshToken: String
    /// When the session stops working, when the cookie said enough to work it out.
    let expiresAt: Date?

    init(accessToken: String, refreshToken: String = "", expiresAt: Date? = nil) {
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.expiresAt = expiresAt
    }

    /// Reads a session out of the service's auth cookie, whose value is URL-encoded JSON.
    ///
    /// Returns `nil` for anything that isn't a usable session, which is how a cookie that exists
    /// but hasn't been signed in yet is told from one that has. Nothing is guessed: a cookie this
    /// build cannot read is no session at all.
    init?(cookieValue: String, now: Date = .now) {
        let decoded = cookieValue.removingPercentEncoding ?? cookieValue
        guard let data = decoded.data(using: .utf8),
            let payload = try? JSONDecoder().decode(Payload.self, from: data)
        else { return nil }
        let access = payload.accessToken.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !access.isEmpty else { return nil }
        self.init(
            accessToken: access,
            refreshToken: payload.refreshToken?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "",
            expiresAt: Self.expiry(payload, now: now))
    }

    /// The fields of the cookie this build reads. Everything else it carries — the account's
    /// email, id, roles — is deliberately not decoded: we have no use for it, so we don't take it.
    private struct Payload: Decodable {
        let accessToken: String
        let refreshToken: String?
        let expiresIn: Double?
        let issuedAt: Double?

        private enum CodingKeys: String, CodingKey {
            case accessToken = "access_token"
            case refreshToken = "refresh_token"
            case expiresIn = "expires_in"
            case issuedAt = "issued_at"
        }
    }

    /// `issued_at` plus `expires_in`, or `now` plus `expires_in` when the cookie didn't say when
    /// it was issued. `nil` when there is no lifetime at all: an invented one would either throw
    /// away a working session or keep a dead one.
    private static func expiry(_ payload: Payload, now: Date) -> Date? {
        guard let seconds = payload.expiresIn, seconds > 0 else { return nil }
        guard let issued = payload.issuedAt, issued > 0 else { return now.addingTimeInterval(seconds) }
        // These timestamps come back in seconds or in milliseconds depending on the field.
        let epoch = issued > 1e11 ? issued / 1000 : issued
        return Date(timeIntervalSince1970: epoch).addingTimeInterval(seconds)
    }
}
