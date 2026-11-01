import AuthenticationServices
import UIKit

/// Shows a store's own sign-in page (Fry's, through Kroger) in `ASWebAuthenticationSession`.
/// The API finishes the sign-in and redirects to the app's URL scheme with the outcome; the
/// password never touches DinnerOS.
final class StoreSignInService: NSObject {
    enum SignInError: Error {
        case cancelled
        case couldNotStart
    }

    /// Kept alive while the browser sheet is on screen.
    private var activeSession: ASWebAuthenticationSession?

    /// Opens `url` and returns the URL the sign-in ended on.
    func signIn(at url: URL, callbackScheme: String) async throws -> URL {
        activeSession?.cancel()
        defer { activeSession = nil }
        return try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(url: url, callback: .customScheme(callbackScheme)) {
                callbackURL, error in
                if let error {
                    let cancelled = (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin
                    continuation.resume(throwing: cancelled ? SignInError.cancelled : error)
                } else if let callbackURL {
                    continuation.resume(returning: callbackURL)
                } else {
                    continuation.resume(throwing: SignInError.couldNotStart)
                }
            }
            // Share Safari's cookies, so someone already signed in to the store continues
            // with one tap.
            session.prefersEphemeralWebBrowserSession = false
            session.presentationContextProvider = self
            activeSession = session
            if !session.start() {
                activeSession = nil
                continuation.resume(throwing: SignInError.couldNotStart)
            }
        }
    }
}

extension StoreSignInService: ASWebAuthenticationPresentationContextProviding {
    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        if let keyWindow = scenes.flatMap(\.windows).first(where: \.isKeyWindow) {
            return keyWindow
        }
        return scenes.first?.windows.first ?? ASPresentationAnchor()
    }
}
