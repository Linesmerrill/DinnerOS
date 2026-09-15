import Foundation
import Testing

@testable import DinnerOS

struct GoogleOAuthTests {
    nonisolated static let clientID = "600707694145-ifi6jfhmial52rtjgrrs18eh5muiqsnt.apps.googleusercontent.com"
    nonisolated static let scheme = "com.googleusercontent.apps.600707694145-ifi6jfhmial52rtjgrrs18eh5muiqsnt"

    private func makeRequest() throws -> GoogleAuthorizationRequest {
        GoogleAuthorizationRequest(
            configuration: try #require(GoogleOAuthConfiguration(clientID: Self.clientID)),
            state: "state-123",
            rawNonce: "nonce-456",
            pkce: PKCE(verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
    }

    @Test func derivesRedirectFromReversedClientID() throws {
        let configuration = try #require(GoogleOAuthConfiguration(clientID: Self.clientID))
        #expect(configuration.redirectScheme == Self.scheme)
        #expect(configuration.redirectURI == "\(Self.scheme):/oauth2redirect")
    }

    @Test(arguments: [nil, "", "   ", "not-a-client-id", ".apps.googleusercontent.com"])
    func rejectsInvalidClientIDs(_ clientID: String?) {
        #expect(GoogleOAuthConfiguration(clientID: clientID) == nil)
    }

    @Test func buildsAuthorizationURL() throws {
        let url = try makeRequest().authorizationURL()
        let components = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false))
        let query = Dictionary(uniqueKeysWithValues: (components.queryItems ?? []).map { ($0.name, $0.value ?? "") })

        #expect(components.scheme == "https")
        #expect(components.host == "accounts.google.com")
        #expect(components.path == "/o/oauth2/v2/auth")
        #expect(
            query == [
                "client_id": Self.clientID,
                "redirect_uri": "\(Self.scheme):/oauth2redirect",
                "response_type": "code",
                "scope": "openid email profile",
                "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
                "code_challenge_method": "S256",
                "state": "state-123",
                "nonce": "nonce-456",
            ])
    }

    @Test func extractsCodeFromValidCallback() throws {
        let callback = try #require(URL(string: "\(Self.scheme):/oauth2redirect?state=state-123&code=4/0Abc-xyz"))
        #expect(try makeRequest().authorizationCode(from: callback) == "4/0Abc-xyz")
    }

    @Test(arguments: [
        ("\(scheme):/oauth2redirect?state=other&code=abc", GoogleSignInError.stateMismatch),
        ("\(scheme):/oauth2redirect?code=abc", .stateMismatch),
        ("\(scheme):/oauth2redirect?error=server_error&state=other", .stateMismatch),
        ("\(scheme):/oauth2redirect?error=access_denied&state=state-123", .cancelled),
        ("\(scheme):/oauth2redirect?error=invalid_scope&state=state-123", .authorizationFailed("invalid_scope")),
        ("\(scheme):/oauth2redirect?state=state-123", .missingAuthorizationCode),
        ("\(scheme):/oauth2redirect?state=state-123&code=", .missingAuthorizationCode),
        ("https://evil.example/oauth2redirect?state=state-123&code=abc", .unexpectedCallback),
    ])
    func rejectsInvalidCallbacks(callback: String, expected: GoogleSignInError) throws {
        let request = try makeRequest()
        let url = try #require(URL(string: callback))
        #expect(throws: expected) {
            try request.authorizationCode(from: url)
        }
    }

    @Test func buildsFormEncodedTokenExchange() throws {
        let urlRequest = try makeRequest().tokenExchangeRequest(code: "4/0Abc+xyz")

        #expect(urlRequest.url?.absoluteString == "https://oauth2.googleapis.com/token")
        #expect(urlRequest.httpMethod == "POST")
        #expect(urlRequest.value(forHTTPHeaderField: "Content-Type") == "application/x-www-form-urlencoded")
        #expect(
            urlRequest.formBody == [
                "client_id": Self.clientID,
                "code": "4/0Abc+xyz",
                "code_verifier": "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
                "grant_type": "authorization_code",
                "redirect_uri": "\(Self.scheme):/oauth2redirect",
            ])
        let raw = String(data: try #require(urlRequest.httpBody), encoding: .utf8) ?? ""
        #expect(raw.contains("code=4%2F0Abc%2Bxyz"))
        #expect(!raw.contains("client_secret"))
    }

    @Test func exchangeReturnsIDToken() async throws {
        let transport = StubTransport { _ in
            (200, Data(#"{"access_token":"ya29","id_token":"google.jwt.token","expires_in":3599}"#.utf8))
        }
        let idToken = try await makeRequest().exchange(code: "code", transport: transport)
        #expect(idToken == "google.jwt.token")
    }

    @Test func exchangeFailureIsReported() async throws {
        let transport = StubTransport { _ in (400, Data(#"{"error":"invalid_grant"}"#.utf8)) }
        let request = try makeRequest()
        await #expect(throws: GoogleSignInError.tokenExchangeFailed(status: 400)) {
            try await request.exchange(code: "code", transport: transport)
        }
    }

    @Test func exchangeWithoutIDTokenFails() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"access_token":"ya29"}"#.utf8)) }
        let request = try makeRequest()
        await #expect(throws: GoogleSignInError.missingIDToken) {
            try await request.exchange(code: "code", transport: transport)
        }
    }
}
