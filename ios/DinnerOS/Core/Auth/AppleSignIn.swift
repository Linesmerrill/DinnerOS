import AuthenticationServices
import Foundation

/// Helpers for Sign in with Apple. The UI uses SwiftUI's `SignInWithAppleButton`,
/// which hands us the request to configure and the authorization result.
enum AppleSignIn {
    /// What the app sends to `POST /auth/apple` after Apple authorizes.
    nonisolated struct Credential: Equatable, Sendable {
        let identityToken: String
        let fullName: String?
    }

    nonisolated enum Failure: Error, Equatable {
        case missingIdentityToken
        case unexpectedCredential
    }

    /// Requests name and email, and binds the request to a fresh nonce.
    ///
    /// - Returns: the raw nonce. Apple only ever sees its SHA-256 hash; the raw value
    ///   is sent to the API with the identity token.
    static func configure(_ request: ASAuthorizationAppleIDRequest) -> String {
        let rawNonce = Nonce.generate()
        request.requestedScopes = [.fullName, .email]
        request.nonce = Nonce.sha256Hex(rawNonce)
        return rawNonce
    }

    static func credential(from authorization: ASAuthorization) throws -> Credential {
        guard let appleID = authorization.credential as? ASAuthorizationAppleIDCredential else {
            throw Failure.unexpectedCredential
        }
        guard
            let tokenData = appleID.identityToken,
            let identityToken = String(data: tokenData, encoding: .utf8),
            !identityToken.isEmpty
        else {
            throw Failure.missingIdentityToken
        }
        return Credential(identityToken: identityToken, fullName: formattedFullName(appleID.fullName))
    }

    /// Apple provides the name only on the first authorization. Returns `nil` when no
    /// name parts were shared. The API accepts at most 100 characters.
    nonisolated static func formattedFullName(_ components: PersonNameComponents?) -> String? {
        guard let components else { return nil }
        let formatter = PersonNameComponentsFormatter()
        formatter.style = .default
        let name = formatter.string(from: components).trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? nil : String(name.prefix(100))
    }

    static func isCancellation(_ error: any Error) -> Bool {
        (error as? ASAuthorizationError)?.code == .canceled
    }
}
