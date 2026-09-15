import Foundation

/// Persists the signed-in session (tokens, expiries, and user summary).
protocol TokenStore: AnyObject {
    func load() throws -> StoredSession?
    func save(_ session: StoredSession) throws
    func clear() throws
}

/// A non-persistent store for tests and previews.
final class InMemoryTokenStore: TokenStore {
    private(set) var session: StoredSession?
    private(set) var saveCount = 0

    init(session: StoredSession? = nil) {
        self.session = session
    }

    func load() throws -> StoredSession? { session }

    func save(_ session: StoredSession) throws {
        self.session = session
        saveCount += 1
    }

    func clear() throws {
        session = nil
    }
}
