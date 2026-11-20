import AuthenticationServices
import SwiftUI

/// "Continue with Apple" as a custom Sign in with Apple button (build 121), so its title uses
/// the same design-system font as the Face ID and Google buttons. It follows Apple's rules for
/// custom buttons: the Apple logo and the system font, a black button in light mode and a white
/// one in dark mode, at least as tall as Apple's own minimum (44 pt), with Apple's wording.
struct AppleSignInButton: View {
    let height: CGFloat
    let prepare: (ASAuthorizationAppleIDRequest) -> Void
    let completion: (Result<ASAuthorization, any Error>) -> Void

    @Environment(\.colorScheme) private var colorScheme
    @State private var coordinator = AppleSignInCoordinator()

    var body: some View {
        Button {
            coordinator.start(prepare: prepare, completion: completion)
        } label: {
            SignInButtonLabel(title: Text("Continue with Apple"), height: height) {
                Image(systemName: "apple.logo")
                    .resizable()
                    .scaledToFit()
                    // The logo sits a little lower in its box than SF Symbols center it.
                    .padding(.bottom, height * 0.03)
            }
        }
        .buttonStyle(
            DSOutlineButtonStyle(
                size: .large, fullWidth: true,
                palette: { $0 == .dark ? Self.dark : Self.light })
        )
        .accessibilityIdentifier("signIn.apple")
    }

    private static let light = DSOutlineButtonStyle.Palette(fill: .black, stroke: .black, text: .white)
    private static let dark = DSOutlineButtonStyle.Palette(fill: .white, stroke: .white, text: .black)
}

/// Runs one Sign in with Apple request and hands the result back.
@MainActor
final class AppleSignInCoordinator: NSObject, ASAuthorizationControllerDelegate,
    ASAuthorizationControllerPresentationContextProviding
{
    private var completion: ((Result<ASAuthorization, any Error>) -> Void)?
    private var controller: ASAuthorizationController?

    func start(
        prepare: (ASAuthorizationAppleIDRequest) -> Void,
        completion: @escaping (Result<ASAuthorization, any Error>) -> Void
    ) {
        let request = ASAuthorizationAppleIDProvider().createRequest()
        prepare(request)
        self.completion = completion
        let controller = ASAuthorizationController(authorizationRequests: [request])
        controller.delegate = self
        controller.presentationContextProvider = self
        self.controller = controller
        controller.performRequests()
    }

    nonisolated func authorizationController(
        controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization
    ) {
        MainActor.assumeIsolated { finish(.success(authorization)) }
    }

    nonisolated func authorizationController(
        controller: ASAuthorizationController, didCompleteWithError error: any Error
    ) {
        MainActor.assumeIsolated { finish(.failure(error)) }
    }

    nonisolated func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
            let active = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
            return active?.keyWindow ?? active?.windows.first ?? ASPresentationAnchor()
        }
    }

    private func finish(_ result: Result<ASAuthorization, any Error>) {
        completion?(result)
        completion = nil
        controller = nil
    }
}
