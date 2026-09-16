import Foundation
import Testing

@testable import DinnerOS

struct DeviceTokensAPITests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    @Test func registerPutsTheTokenInTheBodyNotThePath() async throws {
        let transport = StubTransport { _ in
            (
                200,
                Data(
                    #"{"token":"abcd","environment":"production","platform":"ios","createdAt":"2026-09-16T12:00:00Z","updatedAt":"2026-09-16T12:00:00Z"}"#
                        .utf8)
            )
        }

        let registration = try await DeviceTokensAPI(client: makeClient(transport))
            .register(token: "abcd", environment: .production, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/me/device-tokens")
        #expect(request.bearerToken == "token-1")
        #expect(request.jsonBody == ["token": "abcd", "environment": "production", "platform": "ios"])
        #expect(registration == DeviceTokenRegistration(token: "abcd", environment: .production, platform: "ios"))
    }

    @Test func deleteSendsTheTokenInTheBody() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await DeviceTokensAPI(client: makeClient(transport)).delete(token: "abcd", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/me/device-tokens")
        #expect(request.jsonBody == ["token": "abcd"])
        #expect(request.bearerToken == "token-1")
    }
}
