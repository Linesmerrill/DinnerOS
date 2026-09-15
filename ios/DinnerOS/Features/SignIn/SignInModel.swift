import AuthenticationServices
import Foundation
import Observation

/// Drives the sign-in screen: one attempt at a time, inline errors, and retry.
@Observable
final class SignInModel {
    enum Method: Equatable {
        case apple
        case google
        case developer
    }

    private(set) var inFlight: Method?
    private(set) var errorMessage: String?
    private(set) var retryMethod: Method?

    var isWorking: Bool { inFlight != nil }
    var canRetry: Bool { retryMethod != nil }
    var isGoogleAvailable: Bool { google != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let google: GoogleSignInService?
    @ObservationIgnored private var appleRawNonce: String?
    /// An Apple credential whose API call failed. Its identity token stays valid for a
    /// few minutes, so retry can resend it without another Apple prompt.
    @ObservationIgnored private var pendingApple: (credential: AppleSignIn.Credential, rawNonce: String)?

    init(session: AuthSession, google: GoogleSignInService?) {
        self.session = session
        self.google = google
    }

    // MARK: Apple

    func prepareAppleRequest(_ request: ASAuthorizationAppleIDRequest) {
        errorMessage = nil
        retryMethod = nil
        appleRawNonce = AppleSignIn.configure(request)
    }

    func handleAppleCompletion(_ result: Result<ASAuthorization, any Error>) {
        let rawNonce = appleRawNonce
        appleRawNonce = nil
        switch result {
        case .failure(let error):
            if !AppleSignIn.isCancellation(error) {
                errorMessage = String(localized: "Sign in with Apple didn't finish. Try again.")
            }
        case .success(let authorization):
            do {
                guard let rawNonce else { throw AppleSignIn.Failure.missingIdentityToken }
                let credential = try AppleSignIn.credential(from: authorization)
                pendingApple = (credential, rawNonce)
                Task { await submitApple() }
            } catch {
                errorMessage = String(localized: "Sign in with Apple didn't return a usable credential. Try again.")
            }
        }
    }

    private func submitApple() async {
        guard let pending = pendingApple else { return }
        await run(.apple) { [session] in
            try await session.signIn { api in
                try await api.signInWithApple(
                    identityToken: pending.credential.identityToken,
                    rawNonce: pending.rawNonce,
                    fullName: pending.credential.fullName)
            }
        }
        if errorMessage == nil { pendingApple = nil }
    }

    // MARK: Google

    func signInWithGoogle() async {
        guard let google else { return }
        await run(.google) { [session] in
            let credential = try await google.signIn()
            try await session.signIn { api in
                try await api.signInWithGoogle(idToken: credential.idToken, rawNonce: credential.rawNonce)
            }
        }
    }

    // MARK: Development

    #if DEBUG
        static let developerSubject = "dev-simulator"

        func signInForDevelopment() async {
            await run(.developer) { [session] in
                try await session.signIn { api in
                    try await api.signInForDevelopment(
                        subject: Self.developerSubject,
                        email: "dev-simulator@example.com",
                        displayName: "Simulator Developer")
                }
            }
        }
    #endif

    // MARK: Retry

    func retry() async {
        switch retryMethod {
        case .apple:
            await submitApple()
        case .google:
            await signInWithGoogle()
        case .developer:
            #if DEBUG
                await signInForDevelopment()
            #endif
        case nil:
            break
        }
    }

    private func run(_ method: Method, _ work: () async throws -> Void) async {
        guard inFlight == nil else { return }
        inFlight = method
        errorMessage = nil
        retryMethod = nil
        defer { inFlight = nil }

        do {
            try await work()
        } catch {
            guard !Self.isCancellation(error) else { return }
            errorMessage =
                (error as? LocalizedError)?.errorDescription
                ?? String(localized: "Sign-in failed. Try again.")
            retryMethod = method
        }
    }

    private static func isCancellation(_ error: any Error) -> Bool {
        error is CancellationError
            || (error as? GoogleSignInError) == .cancelled
            || AppleSignIn.isCancellation(error)
    }
}
