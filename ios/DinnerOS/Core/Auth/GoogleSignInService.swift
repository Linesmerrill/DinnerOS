import AuthenticationServices
import UIKit

/// Runs Google's OAuth authorization-code flow in `ASWebAuthenticationSession`.
final class GoogleSignInService: NSObject {
    /// What the app sends to `POST /auth/google`.
    struct Credential: Equatable, Sendable {
        let idToken: String
        let rawNonce: String
    }

    let configuration: GoogleOAuthConfiguration
    private let transport: any HTTPTransport
    /// Kept alive while the browser sheet is on screen.
    private var activeSession: ASWebAuthenticationSession?

    init(configuration: GoogleOAuthConfiguration, transport: any HTTPTransport = URLSessionTransport()) {
        self.configuration = configuration
        self.transport = transport
    }

    func signIn() async throws -> Credential {
        let request = GoogleAuthorizationRequest(configuration: configuration)
        let callbackURL = try await authorize(url: try request.authorizationURL())
        let code = try request.authorizationCode(from: callbackURL)
        let idToken = try await request.exchange(code: code, transport: transport)
        return Credential(idToken: idToken, rawNonce: request.rawNonce)
    }

    private func authorize(url: URL) async throws -> URL {
        activeSession?.cancel()
        defer { activeSession = nil }

        return try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(
                url: url,
                callback: .customScheme(configuration.redirectScheme)
            ) { callbackURL, error in
                if let error {
                    let cancelled = (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin
                    continuation.resume(throwing: cancelled ? GoogleSignInError.cancelled : error)
                } else if let callbackURL {
                    continuation.resume(returning: callbackURL)
                } else {
                    continuation.resume(throwing: GoogleSignInError.unexpectedCallback)
                }
            }
            // Share Safari's cookies so someone already signed in to Google can continue
            // with one tap.
            session.prefersEphemeralWebBrowserSession = false
            session.presentationContextProvider = self
            activeSession = session
            if !session.start() {
                activeSession = nil
                continuation.resume(throwing: GoogleSignInError.invalidConfiguration)
            }
        }
    }
}

extension GoogleSignInService: ASWebAuthenticationPresentationContextProviding {
    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        if let keyWindow = scenes.flatMap(\.windows).first(where: \.isKeyWindow) {
            return keyWindow
        }
        if let window = scenes.first?.windows.first {
            return window
        }
        return ASPresentationAnchor()
    }
}
