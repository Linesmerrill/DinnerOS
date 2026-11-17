import AuthenticationServices
import Foundation
import Testing

@testable import DinnerOS

/// A stand-in for Apple's sheet.
final class FakeAppleCodes: AppleCodeProviding {
    enum Answer {
        case code(String?)
        case cancel
        case fail
    }

    var answer: Answer
    private(set) var asked = 0

    init(_ answer: Answer) { self.answer = answer }

    func authorizationCode() async throws -> String? {
        asked += 1
        switch answer {
        case .code(let code): return code
        case .cancel: throw ASAuthorizationError(.canceled)
        case .fail: throw ASAuthorizationError(.failed)
        }
    }
}

struct AccountDeletionTests {
    private static let googleMeJSON = Data(
        """
        {"user":{"id":"66e5a1f2c3b4a5d6e7f80912","displayName":"Ada Lovelace","primaryEmail":"ada@example.com",
                 "createdAt":"2025-09-08T15:33:20Z"},
         "identities":[{"provider":"google","email":"ada@example.com"}]}
        """.utf8)

    /// A signed-in session whose `GET /me` answers `meJSON` and whose `DELETE /me` answers 204.
    private func makeSession(meJSON: Data = Fixtures.meJSON) async throws -> (AuthSession, StubTransport) {
        let transport = StubTransport { request in
            switch (request.httpMethod, request.url?.path()) {
            case ("GET", "/api/v1/me"): (200, meJSON)
            case ("DELETE", "/api/v1/me"): (204, Data())
            default: (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        let session = AuthSession(
            api: api, store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        return (session, transport)
    }

    private func deleteRequest(_ transport: StubTransport) -> URLRequest? {
        transport.requests.first { $0.httpMethod == "DELETE" }
    }

    @Test func appleUserConfirmsWithAppleAndSendsTheCode() async throws {
        let (session, transport) = try await makeSession()
        let apple = FakeAppleCodes(.code("fresh-code"))

        let outcome = try await AccountDeletion(session: session, apple: apple).delete()

        #expect(outcome == .deleted)
        #expect(apple.asked == 1)
        let request = try #require(deleteRequest(transport))
        #expect(request.jsonBody == ["appleAuthorizationCode": "fresh-code"])
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        #expect(session.state == .signedOut)
    }

    @Test func closingApplesSheetDeletesNothing() async throws {
        let (session, transport) = try await makeSession()
        let apple = FakeAppleCodes(.cancel)

        let outcome = try await AccountDeletion(session: session, apple: apple).delete()

        #expect(outcome == .cancelled)
        #expect(deleteRequest(transport) == nil)
        #expect(session.currentUser?.id == Fixtures.user.id)
    }

    @Test func appleSheetFailureStillDeletesWithoutACode() async throws {
        let (session, transport) = try await makeSession()

        let outcome = try await AccountDeletion(session: session, apple: FakeAppleCodes(.fail)).delete()

        #expect(outcome == .deleted)
        let request = try #require(deleteRequest(transport))
        #expect(request.httpBody == nil)
    }

    @Test func appleWithoutACodeSendsNoBody() async throws {
        let (session, transport) = try await makeSession()

        _ = try await AccountDeletion(session: session, apple: FakeAppleCodes(.code(nil))).delete()

        #expect(try #require(deleteRequest(transport)).httpBody == nil)
    }

    @Test func googleUserIsNotAskedToConfirmWithApple() async throws {
        let (session, transport) = try await makeSession(meJSON: Self.googleMeJSON)
        let apple = FakeAppleCodes(.code("unused"))
        let deletion = AccountDeletion(session: session, apple: apple)

        #expect(await deletion.usesApple() == false)
        let outcome = try await deletion.delete()

        #expect(outcome == .deleted)
        #expect(apple.asked == 0)
        #expect(try #require(deleteRequest(transport)).httpBody == nil)
    }

    @Test func appleSignInSendsTheAuthorizationCode() async throws {
        let transport = StubTransport { _ in (200, Fixtures.sessionJSON(access: "a", refresh: "r")) }
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))

        _ = try await api.signInWithApple(
            identityToken: "id.token", rawNonce: "raw", fullName: nil, authorizationCode: "code-1")

        #expect(
            transport.requests.first?.jsonBody
                == ["identityToken": "id.token", "nonce": "raw", "authorizationCode": "code-1"])
    }
}
