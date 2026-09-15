import Foundation
import Security

/// Stores the session as a single generic-password Keychain item.
///
/// The item is readable only after the device's first unlock and never leaves this
/// device (no iCloud Keychain sync, no restore to another device), so background work
/// after a reboot can still refresh tokens.
final class KeychainTokenStore: TokenStore {
    struct KeychainError: Error, Equatable {
        let status: OSStatus
    }

    let service: String
    private let account = "session"

    init(service: String = KeychainTokenStore.defaultService) {
        self.service = service
    }

    /// Derived from the bundle identifier so each app variant has its own item.
    static var defaultService: String {
        "\(Bundle.main.bundleIdentifier ?? "app").auth"
    }

    func load() throws -> StoredSession? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            do {
                return try JSONCoding.makeDecoder().decode(StoredSession.self, from: data)
            } catch {
                // An unreadable item (for example, from an incompatible older format) is
                // treated as signed out rather than blocking launch.
                try clear()
                return nil
            }
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    func save(_ session: StoredSession) throws {
        let data = try JSONCoding.makeEncoder().encode(session)
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]

        let updateStatus = SecItemUpdate(baseQuery as CFDictionary, attributes as CFDictionary)
        switch updateStatus {
        case errSecSuccess:
            return
        case errSecItemNotFound:
            let addQuery = baseQuery.merging(attributes) { _, new in new }
            let addStatus = SecItemAdd(addQuery as CFDictionary, nil)
            guard addStatus == errSecSuccess else { throw KeychainError(status: addStatus) }
        default:
            throw KeychainError(status: updateStatus)
        }
    }

    func clear() throws {
        let status = SecItemDelete(baseQuery as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainError(status: status)
        }
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
