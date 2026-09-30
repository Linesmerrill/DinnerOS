import CryptoKit
import Foundation

/// The last answer to every `GET` the app made, kept on disk so the app still shows the week,
/// recipes, the cooking screen, lists, and the pantry without a connection.
///
/// The server stays the source of truth: nothing here is edited or computed, only replayed when
/// the network can't be reached. A fresh answer always replaces the saved one. Files are
/// protected until the phone is first unlocked and removed on sign-out (`clear`).
nonisolated final class ResponseCache: Sendable {
    /// Where the replay came from, for the "Offline" line.
    struct Hit: Sendable {
        let data: Data
        let savedAt: Date
    }

    let directory: URL
    /// Told when a saved answer stands in for the network, and when the network answers again.
    let onReplay: @Sendable (Date) -> Void
    let onLive: @Sendable () -> Void

    init(
        directory: URL = ResponseCache.defaultDirectory, onReplay: @escaping @Sendable (Date) -> Void = { _ in },
        onLive: @escaping @Sendable () -> Void = {}
    ) {
        self.directory = directory
        self.onReplay = onReplay
        self.onLive = onLive
    }

    static var defaultDirectory: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return base.appending(path: "APIResponses", directoryHint: .isDirectory)
    }

    /// The key is the request's URL, which carries the household and every parameter.
    /// Live state that's worse stale than missing is never saved: a replayed cooking session
    /// would undo checks made since.
    static func key(for request: URLRequest) -> String? {
        guard request.httpMethod == "GET", let url = request.url?.absoluteString,
            !url.contains("/cook-sessions/")
        else { return nil }
        let digest = SHA256.hash(data: Data(url.utf8))
        return digest.map { String(format: "%02x", $0) }.joined()
    }

    func store(_ data: Data, for request: URLRequest) {
        guard let key = Self.key(for: request) else { return }
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try data.write(
                to: directory.appending(path: key),
                options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        } catch {
            // A cache that can't be written only means less works offline.
        }
    }

    func load(for request: URLRequest) -> Hit? {
        guard let key = Self.key(for: request) else { return nil }
        let url = directory.appending(path: key)
        guard let data = try? Data(contentsOf: url) else { return nil }
        let savedAt = (try? FileManager.default.attributesOfItem(atPath: url.path)[.modificationDate] as? Date) ?? .now
        return Hit(data: data, savedAt: savedAt)
    }

    /// Everything saved, for sign-out: the next account never sees this one's data.
    func clear() {
        try? FileManager.default.removeItem(at: directory)
    }

    /// Drops answers older than `age`, so a phone doesn't keep a year of weeks.
    func prune(olderThan age: TimeInterval = 45 * 24 * 3600, now: Date = .now) {
        guard
            let files = try? FileManager.default.contentsOfDirectory(
                at: directory, includingPropertiesForKeys: [.contentModificationDateKey])
        else { return }
        for file in files {
            let modified = (try? file.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
            if let modified, now.timeIntervalSince(modified) > age {
                try? FileManager.default.removeItem(at: file)
            }
        }
    }

    /// The failures where a saved answer is better than an error: no connection, or a server
    /// that can't be reached.
    static func shouldReplay(_ error: APIError) -> Bool {
        switch error {
        case .transport(let code): code != .cancelled
        case .server(let status, _, _, _): [502, 503, 504].contains(status)
        case .invalidResponse, .decoding: false
        }
    }
}
