import Foundation

/// Google's iOS OAuth client, used without the Google Sign-In SDK.
///
/// iOS clients are public clients (no secret), so the authorization code is protected
/// with PKCE and the redirect uses the reversed client ID as a custom URL scheme.
nonisolated struct GoogleOAuthConfiguration: Equatable, Sendable {
    static let clientIDSuffix = ".apps.googleusercontent.com"

    let clientID: String

    /// Returns `nil` unless `clientID` looks like a Google OAuth client ID.
    init?(clientID: String?) {
        guard
            let clientID = clientID?.trimmingCharacters(in: .whitespacesAndNewlines),
            clientID.hasSuffix(Self.clientIDSuffix),
            clientID.count > Self.clientIDSuffix.count
        else { return nil }
        self.clientID = clientID
    }

    /// `600…-abc.apps.googleusercontent.com` → `com.googleusercontent.apps.600…-abc`.
    var redirectScheme: String {
        clientID.split(separator: ".").reversed().joined(separator: ".")
    }

    var redirectURI: String { "\(redirectScheme):/oauth2redirect" }
}

nonisolated enum GoogleSignInError: Error, Equatable {
    case cancelled
    case invalidConfiguration
    case unexpectedCallback
    case stateMismatch
    case missingAuthorizationCode
    case authorizationFailed(String)
    case tokenExchangeFailed(status: Int)
    case missingIDToken
}

extension GoogleSignInError: LocalizedError {
    nonisolated var errorDescription: String? {
        switch self {
        case .cancelled:
            String(localized: "Google sign-in was cancelled.")
        case .invalidConfiguration:
            String(localized: "Google sign-in isn't configured for this build.")
        case .tokenExchangeFailed, .missingIDToken:
            String(localized: "Google didn't complete the sign-in. Try again.")
        case .unexpectedCallback, .stateMismatch, .missingAuthorizationCode, .authorizationFailed:
            String(localized: "Google sign-in couldn't be verified. Try again.")
        }
    }
}

/// One Google authorization attempt: its `state`, nonce, and PKCE verifier.
nonisolated struct GoogleAuthorizationRequest: Sendable {
    static let authorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
    static let tokenEndpoint = "https://oauth2.googleapis.com/token"
    static let scope = "openid email profile"

    let configuration: GoogleOAuthConfiguration
    /// Random value echoed back on the redirect; rejects callbacks we didn't start.
    let state: String
    /// Raw nonce. Google copies it into the ID token's `nonce` claim unchanged, and the
    /// API checks that the claim equals the raw nonce the app sends.
    let rawNonce: String
    let pkce: PKCE

    init(
        configuration: GoogleOAuthConfiguration,
        state: String = SecureRandom.token(),
        rawNonce: String = Nonce.generate(),
        pkce: PKCE = .generate()
    ) {
        self.configuration = configuration
        self.state = state
        self.rawNonce = rawNonce
        self.pkce = pkce
    }

    func authorizationURL() throws -> URL {
        guard var components = URLComponents(string: Self.authorizationEndpoint) else {
            throw GoogleSignInError.invalidConfiguration
        }
        components.queryItems = [
            URLQueryItem(name: "client_id", value: configuration.clientID),
            URLQueryItem(name: "redirect_uri", value: configuration.redirectURI),
            URLQueryItem(name: "response_type", value: "code"),
            URLQueryItem(name: "scope", value: Self.scope),
            URLQueryItem(name: "code_challenge", value: pkce.challenge),
            URLQueryItem(name: "code_challenge_method", value: PKCE.method),
            URLQueryItem(name: "state", value: state),
            URLQueryItem(name: "nonce", value: rawNonce),
        ]
        guard let url = components.url else { throw GoogleSignInError.invalidConfiguration }
        return url
    }

    /// Validates the redirect and returns the authorization code.
    func authorizationCode(from callbackURL: URL) throws -> String {
        guard
            let components = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false),
            components.scheme?.lowercased() == configuration.redirectScheme.lowercased()
        else {
            throw GoogleSignInError.unexpectedCallback
        }
        let items = components.queryItems ?? []
        func value(_ name: String) -> String? {
            items.first { $0.name == name }?.value
        }

        // Check state before anything else so an injected callback can't even surface
        // an error message of its choosing.
        guard let returnedState = value("state"), returnedState == state else {
            throw GoogleSignInError.stateMismatch
        }
        if let error = value("error") {
            throw error == "access_denied" ? GoogleSignInError.cancelled : .authorizationFailed(error)
        }
        guard let code = value("code"), !code.isEmpty else {
            throw GoogleSignInError.missingAuthorizationCode
        }
        return code
    }

    /// The form-encoded authorization-code exchange. No client secret: PKCE's
    /// `code_verifier` proves this app started the authorization.
    func tokenExchangeRequest(code: String) throws -> URLRequest {
        guard let url = URL(string: Self.tokenEndpoint) else { throw GoogleSignInError.invalidConfiguration }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.httpBody = FormEncoding.encode([
            ("client_id", configuration.clientID),
            ("code", code),
            ("code_verifier", pkce.verifier),
            ("grant_type", "authorization_code"),
            ("redirect_uri", configuration.redirectURI),
        ])
        return request
    }

    /// Exchanges the code for tokens and returns Google's ID token.
    func exchange(code: String, transport: any HTTPTransport) async throws -> String {
        let (data, response): (Data, HTTPURLResponse)
        do {
            (data, response) = try await transport.send(try tokenExchangeRequest(code: code))
        } catch let error as URLError {
            throw APIError.transport(error.code)
        }
        guard (200..<300).contains(response.statusCode) else {
            throw GoogleSignInError.tokenExchangeFailed(status: response.statusCode)
        }
        guard
            let body = try? JSONDecoder().decode(TokenResponse.self, from: data),
            let idToken = body.idToken, !idToken.isEmpty
        else {
            throw GoogleSignInError.missingIDToken
        }
        return idToken
    }

    private struct TokenResponse: Decodable {
        let idToken: String?

        enum CodingKeys: String, CodingKey {
            case idToken = "id_token"
        }
    }
}

/// `application/x-www-form-urlencoded` bodies.
nonisolated enum FormEncoding {
    private static let allowed = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    static func encode(_ fields: [(String, String)]) -> Data {
        let pairs = fields.map { name, value in
            "\(escape(name))=\(escape(value))"
        }
        return Data(pairs.joined(separator: "&").utf8)
    }

    static func escape(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value
    }
}
