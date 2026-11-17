import Foundation
import Testing

@testable import DinnerOS

/// Email sign-in for the App Review account (decision 752): the flag the sign-in screen reads,
/// and the form's model.
@MainActor
struct EmailSignInTests {
    private let optionsPath = "/api/v1/auth/options"
    private let signInPath = "/api/v1/auth/review-sign-in"

    private func makeModel(_ handler: @escaping StubTransport.Handler) throws -> (
        SignInModel, AuthSession, StubTransport
    ) {
        let transport = StubTransport(handler)
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        let session = AuthSession(api: api, store: InMemoryTokenStore())
        return (SignInModel(session: session, google: nil), session, transport)
    }

    @Test func showsOnlyWhenTheServerSaysSo() async throws {
        let (on, _, transport) = try makeModel { _ in (200, Data(#"{"reviewSignIn":true}"#.utf8)) }
        #expect(!on.isEmailSignInAvailable)
        await on.loadOptions()
        #expect(on.isEmailSignInAvailable)
        #expect(transport.requests(to: optionsPath).count == 1)
        #expect(transport.requests.first?.value(forHTTPHeaderField: "Authorization") == nil)

        let (off, _, _) = try makeModel { _ in (200, Data(#"{"reviewSignIn":false}"#.utf8)) }
        await off.loadOptions()
        #expect(!off.isEmailSignInAvailable)
    }

    @Test func staysHiddenOfflineOrOnAnError() async throws {
        let (offline, _, _) = try makeModel { _ in throw URLError(.notConnectedToInternet) }
        await offline.loadOptions()
        #expect(!offline.isEmailSignInAvailable)

        // An older server without the route.
        let (old, _, _) = try makeModel { _ in (404, Data()) }
        await old.loadOptions()
        #expect(!old.isEmailSignInAvailable)

        let session = AuthSession(api: nil, store: InMemoryTokenStore())
        #expect(await session.signInOptions() == .none)
    }

    @Test func signInOptionsAreNeverReplayedFromSavedAnswers() throws {
        var request = URLRequest(url: try #require(URL(string: "https://api.test/api/v1/auth/options")))
        request.httpMethod = "GET"
        #expect(ResponseCache.key(for: request) == nil)
    }

    @Test func signsInWithTheTypedEmailAndPassword() async throws {
        let (model, session, transport) = try makeModel { _ in
            (200, Fixtures.sessionJSON(access: "access-review", refresh: "refresh-review"))
        }
        await session.restore()
        #expect(!model.canSubmitEmail)
        model.email = "  review@example.test "
        model.password = "secret-password"
        #expect(model.canSubmitEmail)

        await model.signInWithEmail()

        #expect(model.errorMessage == nil)
        #expect(session.currentUser?.id == Fixtures.user.id)
        let sent = try #require(transport.requests(to: signInPath).first)
        #expect(sent.httpMethod == "POST")
        #expect(sent.jsonBody == ["email": "review@example.test", "password": "secret-password"])
        #expect(model.password.isEmpty, "the password isn't kept after signing in")
    }

    @Test func wrongPasswordShowsThePlainMessage() async throws {
        let body = Data(
            #"{"error":{"code":"sign_in_failed","message":"That email and password didn't work.","requestId":"r1"}}"#
                .utf8)
        let (model, session, _) = try makeModel { _ in (401, body) }
        await session.restore()
        model.email = "review@example.test"
        model.password = "wrong"

        await model.signInWithEmail()

        #expect(model.errorMessage == "That email and password didn't work.")
        #expect(!model.canRetry, "a wrong password is typed again, not resent")
        #expect(model.password == "wrong", "the fields stay so it can be fixed")
        #expect(session.currentUser == nil)
    }

    @Test func emptyFieldsSendNothing() async throws {
        let (model, _, transport) = try makeModel { _ in (500, Data()) }
        model.email = "review@example.test"
        await model.signInWithEmail()
        model.email = "   "
        model.password = "secret"
        await model.signInWithEmail()
        #expect(transport.requests(to: signInPath).isEmpty)
    }
}
