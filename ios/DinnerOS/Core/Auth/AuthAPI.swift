import Foundation

/// Typed wrappers for the `/api/v1/auth/*` and `/api/v1/me` endpoints.
nonisolated struct AuthAPI: Sendable {
    let client: APIClient

    func signInWithApple(identityToken: String, rawNonce: String, fullName: String?) async throws -> SessionResponse {
        let body = AppleSignInBody(identityToken: identityToken, nonce: rawNonce, fullName: fullName)
        return try await client.send(try .post("/api/v1/auth/apple", body: body))
    }

    func signInWithGoogle(idToken: String, rawNonce: String) async throws -> SessionResponse {
        let body = GoogleSignInBody(idToken: idToken, nonce: rawNonce)
        return try await client.send(try .post("/api/v1/auth/google", body: body))
    }

    #if DEBUG
        /// `POST /auth/dev`. Exists only on a development API with dev login enabled.
        func signInForDevelopment(subject: String, email: String?, displayName: String?) async throws
            -> SessionResponse
        {
            let body = DevSignInBody(subject: subject, email: email, displayName: displayName)
            return try await client.send(try .post("/api/v1/auth/dev", body: body))
        }
    #endif

    func refresh(refreshToken: String) async throws -> AuthTokens {
        try await client.send(try .post("/api/v1/auth/refresh", body: RefreshTokenBody(refreshToken: refreshToken)))
    }

    func logout(refreshToken: String) async throws {
        try await client.sendIgnoringBody(
            try .post("/api/v1/auth/logout", body: RefreshTokenBody(refreshToken: refreshToken)))
    }

    func me(accessToken: String) async throws -> MeResponse {
        try await client.send(APIRequest.get("/api/v1/me").authorized(with: accessToken))
    }
}

// Request bodies. The API rejects unknown fields; optional fields that are nil are
// omitted from the JSON.

private nonisolated struct AppleSignInBody: Encodable {
    let identityToken: String
    let nonce: String
    let fullName: String?
}

private nonisolated struct GoogleSignInBody: Encodable {
    let idToken: String
    let nonce: String
}

private nonisolated struct DevSignInBody: Encodable {
    let subject: String
    let email: String?
    let displayName: String?
}

private nonisolated struct RefreshTokenBody: Encodable {
    let refreshToken: String
}
