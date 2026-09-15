import Foundation

/// The queued events and whose they are. Events are sent as the signed-in user to one
/// household, so the queue remembers both and is discarded when either changes.
nonisolated struct EventQueueSnapshot: Codable, Equatable, Sendable {
    static let currentVersion = 1

    var version = EventQueueSnapshot.currentVersion
    var userID: String?
    var householdID: String?
    /// Oldest first.
    var events: [ClientEvent] = []
}

/// Where the event queue survives app launches.
protocol EventQueueStorage: AnyObject {
    /// `nil` when nothing was stored.
    func load() throws -> EventQueueSnapshot?
    func save(_ snapshot: EventQueueSnapshot) throws
    func clear() throws
}

/// A JSON file in Application Support, excluded from backups and readable after the
/// first unlock so a background flush can use it.
final class FileEventQueueStorage: EventQueueStorage {
    let url: URL

    init(url: URL) {
        self.url = url
    }

    /// `Application Support/DinnerOS/event-queue.json`.
    static func applicationSupport() -> FileEventQueueStorage {
        FileEventQueueStorage(
            url: URL.applicationSupportDirectory
                .appending(path: "DinnerOS", directoryHint: .isDirectory)
                .appending(path: "event-queue.json", directoryHint: .notDirectory))
    }

    func load() throws -> EventQueueSnapshot? {
        guard FileManager.default.fileExists(atPath: url.path(percentEncoded: false)) else { return nil }
        let data = try Data(contentsOf: url)
        let snapshot = try JSONCoding.makeDecoder().decode(EventQueueSnapshot.self, from: data)
        return snapshot.version == EventQueueSnapshot.currentVersion ? snapshot : nil
    }

    func save(_ snapshot: EventQueueSnapshot) throws {
        let directory = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let data = try JSONCoding.makeEncoder().encode(snapshot)
        try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        var fileURL = url
        try? fileURL.setResourceValues(values)
    }

    func clear() throws {
        guard FileManager.default.fileExists(atPath: url.path(percentEncoded: false)) else { return }
        try FileManager.default.removeItem(at: url)
    }
}

/// A queue that lasts only as long as the process, for tests and previews.
final class InMemoryEventQueueStorage: EventQueueStorage {
    private(set) var snapshot: EventQueueSnapshot?

    init(snapshot: EventQueueSnapshot? = nil) {
        self.snapshot = snapshot
    }

    func load() throws -> EventQueueSnapshot? {
        snapshot
    }

    func save(_ snapshot: EventQueueSnapshot) throws {
        self.snapshot = snapshot
    }

    func clear() throws {
        snapshot = nil
    }
}
