import Foundation

/// A DinnerOS access/refresh token pair (`TokenPair` in `api/openapi.yaml`).
nonisolated struct AuthTokens: Codable, Equatable, Sendable {
    let accessToken: String
    let accessTokenExpiresAt: Date
    let refreshToken: String
    let refreshTokenExpiresAt: Date

    /// Treats the access token as expired slightly early so a request doesn't race
    /// the expiry on its way to the server.
    func isAccessTokenExpired(at now: Date, leeway: TimeInterval = 30) -> Bool {
        accessTokenExpiresAt.addingTimeInterval(-leeway) <= now
    }

    func isRefreshTokenExpired(at now: Date) -> Bool {
        refreshTokenExpiresAt <= now
    }
}

/// The signed-in user (`User` in `api/openapi.yaml`).
nonisolated struct UserSummary: Codable, Equatable, Sendable, Identifiable {
    let id: String
    /// May be empty when no provider supplied a name.
    let displayName: String
    let primaryEmail: String?
    let createdAt: Date
}

/// Everything persisted between launches.
nonisolated struct StoredSession: Codable, Equatable, Sendable {
    var tokens: AuthTokens
    var user: UserSummary
}

/// Response to every sign-in endpoint (`SessionResponse`).
nonisolated struct SessionResponse: Decodable, Equatable, Sendable {
    let accessToken: String
    let accessTokenExpiresAt: Date
    let refreshToken: String
    let refreshTokenExpiresAt: Date
    let user: UserSummary
    let isNewUser: Bool

    var tokens: AuthTokens {
        AuthTokens(
            accessToken: accessToken,
            accessTokenExpiresAt: accessTokenExpiresAt,
            refreshToken: refreshToken,
            refreshTokenExpiresAt: refreshTokenExpiresAt)
    }
}

/// Response to `GET /api/v1/me`.
nonisolated struct MeResponse: Decodable, Equatable, Sendable {
    nonisolated struct Identity: Decodable, Equatable, Sendable, Hashable {
        let provider: String
        let email: String?
    }

    let user: UserSummary
    let identities: [Identity]
}
