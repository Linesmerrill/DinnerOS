import Foundation
import Testing

@testable import DinnerOS

struct APIClientTests {
    private func makeClient(_ transport: StubTransport, base: String = Fixtures.baseURLString) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: base)), transport: transport)
    }

    @Test func decodesErrorEnvelope() async throws {
        let transport = StubTransport { _ in
            (401, Fixtures.errorJSON(code: "token_expired", message: "access token expired", requestID: "abc12345"))
        }
        let client = try makeClient(transport)

        await #expect(
            throws: APIError.server(
                status: 401, code: "token_expired", message: "access token expired", requestID: "abc12345")
        ) {
            try await client.send(APIRequest.get("/api/v1/me").authorized(with: "t"), as: MeResponse.self)
        }
    }

    @Test func nonEnvelopeErrorFallsBackToStatusAndRequestID() async throws {
        let transport = StubTransport { _ in (502, Data("<html>Bad gateway</html>".utf8)) }
        let client = try makeClient(transport)

        do {
            _ = try await client.send(APIRequest.get("/api/v1/me"), as: MeResponse.self)
            Issue.record("expected an error")
        } catch let error as APIError {
            let sentRequestID = transport.requests.first?.value(forHTTPHeaderField: "X-Request-ID")
            #expect(error.status == 502)
            #expect(error.code == "http_502")
            #expect(sentRequestID != nil)
            if case .server(_, _, _, let requestID) = error {
                #expect(requestID == sentRequestID)
            }
            #expect(error.errorDescription?.isEmpty == false)
        }
    }

    @Test func mapsTransportErrors() async throws {
        let transport = StubTransport { _ in throw URLError(.notConnectedToInternet) }
        let client = try makeClient(transport)

        await #expect(throws: APIError.transport(.notConnectedToInternet)) {
            try await client.sendIgnoringBody(APIRequest.get("/health"))
        }
    }

    @Test func malformedSuccessBodyIsADecodingError() async throws {
        let transport = StubTransport { _ in (200, Data("{}".utf8)) }
        let client = try makeClient(transport)

        await #expect(throws: APIError.decoding(type: "MeResponse")) {
            try await client.send(APIRequest.get("/api/v1/me"), as: MeResponse.self)
        }
    }

    @Test func sendsJSONBodyAndHeadersAndDecodesSession() async throws {
        let transport = StubTransport { _ in (200, Fixtures.sessionJSON(access: "a", refresh: "r")) }
        let client = try makeClient(transport, base: "https://api.example.test/")
        let api = AuthAPI(client: client)

        let session = try await api.signInWithApple(identityToken: "id.token", rawNonce: "raw", fullName: nil)

        #expect(session.accessToken == "a")
        #expect(session.isNewUser)
        #expect(session.user.primaryEmail == "ada@example.com")

        let request = try #require(transport.requests.first)
        #expect(request.url?.absoluteString == "https://api.example.test/api/v1/auth/apple")
        #expect(request.httpMethod == "POST")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        #expect(request.value(forHTTPHeaderField: "Authorization") == nil)
        // `fullName` is omitted, not sent as null: the API rejects unknown or invalid fields.
        #expect(request.jsonBody == ["identityToken": "id.token", "nonce": "raw"])
    }

    @Test func attachesBearerToken() async throws {
        let transport = StubTransport { _ in (200, Fixtures.meJSON) }
        let me = try await AuthAPI(client: try makeClient(transport)).me(accessToken: "secret-token")

        #expect(me.identities == [MeResponse.Identity(provider: "apple", email: "ada@example.com")])
        #expect(transport.requests.first?.bearerToken == "secret-token")
        #expect(transport.requests.first?.httpMethod == "GET")
    }

    @Test func logoutAcceptsEmptyNoContentResponse() async throws {
        let transport = StubTransport { _ in (204, Data()) }
        try await AuthAPI(client: try makeClient(transport)).logout(refreshToken: "refresh")
        #expect(transport.requests.first?.jsonBody == ["refreshToken": "refresh"])
    }
}

struct JSONCodingTests {
    @Test(arguments: [
        ("2026-09-14T18:45:00Z", 1_789_411_500.0),
        ("2026-09-14T18:45:00.5Z", 1_789_411_500.5),
        ("2026-09-14T18:45:00.123Z", 1_789_411_500.123),
        ("2026-09-14T18:45:00.123456789Z", 1_789_411_500.123),
    ])
    func parsesDatesWithAndWithoutFractionalSeconds(string: String, expected: TimeInterval) throws {
        let date = try #require(JSONCoding.parseDate(string))
        #expect(abs(date.timeIntervalSince1970 - expected) < 0.001)
    }

    @Test func rejectsNonDates() {
        #expect(JSONCoding.parseDate("yesterday") == nil)
        #expect(JSONCoding.parseDate("") == nil)
    }
}
