import Foundation
import os

/// Deletes the account. Someone who signs in with Apple confirms with Apple first, which gives
/// the API a fresh code to end the app's Apple sign-in with (as Apple asks of apps that offer
/// Sign in with Apple). Closing Apple's sheet deletes nothing.
struct AccountDeletion {
    enum Outcome: Equatable {
        case deleted
        case cancelled
    }

    let session: AuthSession
    let apple: any AppleCodeProviding

    private static let logger = Logger(subsystem: "DinnerOS", category: "auth")

    /// Whether the account signs in with Apple. `false` if that can't be loaded; the delete
    /// request reports its own errors.
    func usesApple() async -> Bool {
        guard let me = try? await session.loadCurrentUser() else { return false }
        return me.identities.contains { $0.provider == "apple" }
    }

    func delete() async throws -> Outcome {
        var code: String?
        if await usesApple() {
            do {
                code = try await apple.authorizationCode()
            } catch let error where AppleSignIn.isCancellation(error) || error is CancellationError {
                return .cancelled
            } catch {
                // Apple's sheet failed for another reason. The deletion still goes ahead: the
                // server revokes the token it kept at sign-in, if it has one.
                Self.logger.notice(
                    "Apple confirmation failed: \(String(describing: type(of: error)), privacy: .public)")
            }
        }
        try await session.deleteAccount(appleAuthorizationCode: code)
        return .deleted
    }
}
