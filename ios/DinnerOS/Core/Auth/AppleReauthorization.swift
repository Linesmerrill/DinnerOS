import AuthenticationServices
import UIKit

/// Gets a fresh Sign in with Apple authorization code, so the API can end the app's Apple
/// sign-in when the account is deleted. Injected so tests never show Apple's sheet.
protocol AppleCodeProviding {
    /// Shows Apple's sheet and returns the new code, or `nil` if Apple sent none.
    /// Throws `ASAuthorizationError.canceled` when the person closes the sheet.
    func authorizationCode() async throws -> String?
}

/// The real provider: an `ASAuthorizationController` request with no scopes, which Apple shows
/// as a quick "Continue with Apple" for someone already signed in.
final class AppleReauthorization: NSObject, AppleCodeProviding {
    /// Kept alive while Apple's sheet is on screen.
    private var controller: ASAuthorizationController?
    private var continuation: CheckedContinuation<String?, any Error>?

    func authorizationCode() async throws -> String? {
        finish(.failure(CancellationError()))
        let request = ASAuthorizationAppleIDProvider().createRequest()
        request.requestedScopes = []
        let controller = ASAuthorizationController(authorizationRequests: [request])
        controller.delegate = self
        controller.presentationContextProvider = self
        self.controller = controller
        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            controller.performRequests()
        }
    }

    private func finish(_ result: Result<String?, any Error>) {
        controller = nil
        guard let continuation else { return }
        self.continuation = nil
        continuation.resume(with: result)
    }
}

extension AppleReauthorization: ASAuthorizationControllerDelegate {
    func authorizationController(
        controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization
    ) {
        guard let appleID = authorization.credential as? ASAuthorizationAppleIDCredential else {
            finish(.failure(AppleSignIn.Failure.unexpectedCredential))
            return
        }
        finish(.success(AppleSignIn.authorizationCode(from: appleID)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: any Error) {
        finish(.failure(error))
    }
}

extension AppleReauthorization: ASAuthorizationControllerPresentationContextProviding {
    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
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
