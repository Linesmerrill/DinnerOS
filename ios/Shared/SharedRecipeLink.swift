import Foundation
import Security

/// A recipe link shared from another app (Safari's share sheet) and waiting for DinnerOS to
/// open. The share extension saves it in the keychain group the app shares with its
/// extensions; the app takes it the next time it comes to the front and reads the recipe.
nonisolated enum SharedRecipeLink {
    private static let service = "dinneros.share"
    private static let account = "pending-recipe-link"
    /// A link older than this is dropped rather than opened out of the blue.
    static let maxAge: TimeInterval = 24 * 60 * 60

    private struct Pending: Codable {
        let link: String
        let savedAt: Date
    }

    /// The shared keychain group, from the `AppIdentifierPrefix` each Info.plist carries.
    private static var accessGroup: String? {
        guard let prefix = Bundle.main.object(forInfoDictionaryKey: "AppIdentifierPrefix") as? String,
            !prefix.isEmpty, !prefix.hasPrefix("$(")
        else { return nil }
        return prefix + "com.linesmerrill.dinneros.shared"
    }

    private static func query(group: String?) -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let group { query[kSecAttrAccessGroup as String] = group }
        return query
    }

    /// The first web link in shared text ("Try this https://…"), or nil.
    static func webLink(in text: String) -> URL? {
        guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else {
            return nil
        }
        let range = NSRange(text.startIndex..., in: text)
        for match in detector.matches(in: text, range: range) {
            if let url = match.url, url.scheme == "https" || url.scheme == "http" { return url }
        }
        return nil
    }

    /// Where the link is kept: the shared keychain, or memory in unit tests (an unsigned test
    /// build has no keychain).
    struct Storage: Sendable {
        var read: @Sendable () -> Data?
        var write: @Sendable (Data) -> Bool
        var delete: @Sendable () -> Void
    }

    nonisolated(unsafe) static var storage = Storage(read: keychainRead, write: keychainWrite, delete: keychainDelete)

    /// Saves a link for the app to open, replacing any earlier one.
    @discardableResult
    static func save(_ url: URL, now: Date = Date()) -> Bool {
        guard url.scheme == "https" || url.scheme == "http",
            let data = try? JSONEncoder().encode(Pending(link: url.absoluteString, savedAt: now))
        else { return false }
        return storage.write(data)
    }

    /// Takes the waiting link, if there is a recent one, and forgets it.
    static func take(now: Date = Date()) -> String? {
        let found = storage.read().flatMap { try? JSONDecoder().decode(Pending.self, from: $0) }
        storage.delete()
        guard let found, now.timeIntervalSince(found.savedAt) < maxAge else { return nil }
        return found.link
    }

    static func clear() {
        storage.delete()
    }

    // MARK: Keychain

    private static func keychainWrite(_ data: Data) -> Bool {
        for group in [accessGroup, nil] {
            let base = query(group: group)
            var status = SecItemUpdate(base as CFDictionary, [kSecValueData as String: data] as CFDictionary)
            if status == errSecItemNotFound {
                var add = base
                add[kSecValueData as String] = data
                add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
                status = SecItemAdd(add as CFDictionary, nil)
            }
            if status == errSecSuccess { return true }
        }
        return false
    }

    private static func keychainRead() -> Data? {
        for group in [accessGroup, nil] {
            var query = query(group: group)
            query[kSecReturnData as String] = true
            query[kSecMatchLimit as String] = kSecMatchLimitOne
            var result: AnyObject?
            if SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data {
                return data
            }
        }
        return nil
    }

    private static func keychainDelete() {
        for group in [accessGroup, nil] {
            SecItemDelete(query(group: group) as CFDictionary)
        }
    }
}
