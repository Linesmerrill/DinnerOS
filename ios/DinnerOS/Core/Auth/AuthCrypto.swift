import CryptoKit
import Foundation
import Security

/// Cryptographically secure random values for nonces, OAuth `state`, and PKCE.
nonisolated enum SecureRandom {
    static func bytes(count: Int) -> Data {
        var bytes = [UInt8](repeating: 0, count: count)
        let status = SecRandomCopyBytes(kSecRandomDefault, count, &bytes)
        // SecRandomCopyBytes doesn't fail in practice. Continuing with predictable
        // bytes would silently weaken sign-in, so stop instead.
        precondition(status == errSecSuccess, "SecRandomCopyBytes failed: \(status)")
        return Data(bytes)
    }

    /// 32 random bytes as base64url: 43 characters from the URL-safe alphabet.
    static func token(byteCount: Int = 32) -> String {
        Base64URL.encode(bytes(count: byteCount))
    }
}

nonisolated enum Base64URL {
    /// Base64url without padding (RFC 4648 §5).
    static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}

/// Nonces bind a provider identity token to one sign-in attempt, so an intercepted
/// token can't be replayed.
nonisolated enum Nonce {
    static func generate() -> String {
        SecureRandom.token()
    }

    /// Lowercase hex SHA-256 of the UTF-8 bytes. Apple receives this hash in the
    /// authorization request and puts it in the token's `nonce` claim; the app sends
    /// the raw value to the API, which hashes it again to compare.
    static func sha256Hex(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8))
            .map { String(format: "%02x", $0) }
            .joined()
    }
}

/// Proof Key for Code Exchange (RFC 7636) with the `S256` method.
nonisolated struct PKCE: Equatable, Sendable {
    static let method = "S256"

    /// 43–128 characters from the unreserved set; ours is 32 random bytes, base64url.
    let verifier: String

    var challenge: String { Self.challenge(for: verifier) }

    static func generate() -> PKCE {
        PKCE(verifier: SecureRandom.token())
    }

    /// `BASE64URL(SHA256(ASCII(code_verifier)))`.
    static func challenge(for verifier: String) -> String {
        Base64URL.encode(Data(SHA256.hash(data: Data(verifier.utf8))))
    }
}
