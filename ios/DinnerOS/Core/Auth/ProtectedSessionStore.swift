import Foundation
import LocalAuthentication
import Security
import UIKit

/// The session behind Face ID: readable only after a check passes.
protocol ProtectedSessionStore: AnyObject {
    /// Whether the item exists, without asking for Face ID.
    func hasItem() -> Bool
    /// `nil` when the item is gone (for example, invalidated when faces changed).
    func load(proof: BiometricProof) throws -> StoredSession?
    func save(_ session: StoredSession) throws
    func clear()
}

/// A Keychain item that needs a face (or finger) from the set enrolled when it was saved, or the
/// device passcode, and is readable only while the device is unlocked, never on another device.
final class KeychainProtectedSessionStore: ProtectedSessionStore {
    struct KeychainError: Error, Equatable {
        let status: OSStatus
    }

    let service: String
    private let account = "session.biometric"
    /// Off only for the UI tests' fake biometrics, which can't pass a real check.
    private let requiresBiometrics: Bool
    private let isProtectedDataAvailable: () -> Bool

    init(
        service: String = KeychainTokenStore.defaultService,
        requiresBiometrics: Bool = true,
        isProtectedDataAvailable: @escaping () -> Bool = { UIApplication.shared.isProtectedDataAvailable }
    ) {
        self.service = service
        self.requiresBiometrics = requiresBiometrics
        self.isProtectedDataAvailable = isProtectedDataAvailable
    }

    func hasItem() -> Bool {
        var query = baseQuery
        let context = LAContext()
        context.interactionNotAllowed = true
        query[kSecUseAuthenticationContext as String] = context
        query[kSecReturnAttributes as String] = true
        let status = SecItemCopyMatching(query as CFDictionary, nil)
        // Not allowed to ask means it's there, behind Face ID.
        return status == errSecSuccess || status == errSecInteractionNotAllowed
    }

    func load(proof: BiometricProof) throws -> StoredSession? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        if let context = proof.context {
            query[kSecUseAuthenticationContext as String] = context
        }
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data,
                let session = try? JSONCoding.makeDecoder().decode(StoredSession.self, from: data)
            else {
                clear()
                return nil
            }
            return session
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    /// Replaces the item. Deleting needs no Face ID, while updating an item behind Face ID would
    /// ask for it, so the old one goes first.
    func save(_ session: StoredSession) throws {
        // Locked, the new item can't be written: keep the old one rather than lose both.
        guard isProtectedDataAvailable() else { throw KeychainError(status: errSecInteractionNotAllowed) }
        let data = try JSONCoding.makeEncoder().encode(session)
        var attributes = baseQuery
        attributes[kSecValueData as String] = data
        if requiresBiometrics {
            var error: Unmanaged<CFError>?
            guard
                let access = SecAccessControlCreateWithFlags(
                    nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, [.biometryCurrentSet, .or, .devicePasscode],
                    &error)
            else {
                throw KeychainError(status: errSecParam)
            }
            attributes[kSecAttrAccessControl as String] = access
        } else {
            attributes[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        }
        clear()
        let status = SecItemAdd(attributes as CFDictionary, nil)
        guard status == errSecSuccess else { throw KeychainError(status: status) }
    }

    func clear() {
        SecItemDelete(baseQuery as CFDictionary)
    }

    private var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecUseDataProtectionKeychain as String: true,
        ]
    }
}

/// For tests and previews.
final class InMemoryProtectedSessionStore: ProtectedSessionStore {
    var session: StoredSession?
    var failsSaving = false
    private(set) var saveCount = 0

    init(session: StoredSession? = nil) {
        self.session = session
    }

    func hasItem() -> Bool { session != nil }

    func load(proof: BiometricProof) throws -> StoredSession? { session }

    func save(_ session: StoredSession) throws {
        if failsSaving { throw KeychainProtectedSessionStore.KeychainError(status: errSecInteractionNotAllowed) }
        self.session = session
        saveCount += 1
    }

    func clear() { session = nil }
}
