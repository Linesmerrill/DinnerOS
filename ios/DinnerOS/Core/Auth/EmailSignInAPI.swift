import Foundation

/// What `GET /auth/options` says the sign-in screen offers (decision 752).
nonisolated struct SignInOptions: Decodable, Equatable, Sendable {
    /// The server has the App Review account set up, so "Sign in with email" shows.
    var reviewSignIn: Bool

    static let none = SignInOptions(reviewSignIn: false)
}

extension AuthAPI {
    /// `GET /auth/options`, public and read before sign-in. Never replayed from the saved
    /// answers (`ResponseCache` skips `/auth/`), so offline it fails and nothing extra shows.
    func signInOptions() async throws -> SignInOptions {
        try await client.send(APIRequest.get("/api/v1/auth/options"))
    }

    /// `POST /auth/review-sign-in`: the App Review account's email and password. Only that
    /// one account can sign in this way.
    func signInWithEmail(email: String, password: String) async throws -> SessionResponse {
        try await client.send(
            try .post("/api/v1/auth/review-sign-in", body: EmailSignInBody(email: email, password: password)))
    }
}

private nonisolated struct EmailSignInBody: Encodable {
    let email: String
    let password: String
}
