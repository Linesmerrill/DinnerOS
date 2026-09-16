import Foundation
import Testing

@testable import DinnerOS

struct NotificationStoreTests {
    private struct Harness {
        let store: NotificationStore
        let server: FakeNotificationServer
    }

    private func makeHarness(
        _ households: [String: [FakeNotificationServer.Entry]] = [
            "household-1": FakeNotificationServer.entries(count: 120, unread: 3)
        ]
    ) async throws -> Harness {
        let server = FakeNotificationServer(households)
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return Harness(
            store: NotificationStore(session: session, api: NotificationsAPI(client: client)), server: server)
    }

    @Test func activatingLoadsOnlyTheUnreadCount() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        await store.activate(householdID: "household-1")

        #expect(store.unreadCount == 3)
        #expect(store.phase == .idle)
        #expect(store.items.isEmpty)
        #expect(harness.server.log == ["GET /households/household-1/notifications/unread-count"])
    }

    @Test func pagesThroughEveryNotification() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        await store.load()
        #expect(store.phase == .loaded)
        #expect(store.items.count == 50)
        #expect(store.items.first?.id == "n-1")
        #expect(store.hasMore)

        await store.loadMore()
        #expect(store.items.count == 100)
        await store.loadMore()
        #expect(store.items.count == 120)
        #expect(!store.hasMore)
        #expect(store.items.map(\.id) == (1...120).map { "n-\($0)" })

        let requests = harness.server.log.count
        await store.loadMore()
        #expect(harness.server.log.count == requests)
        #expect(
            harness.server.log.filter { $0.hasPrefix("GET /households/household-1/notifications?") }
                == [
                    "GET /households/household-1/notifications?limit=50",
                    "GET /households/household-1/notifications?limit=50&before=n-50",
                    "GET /households/household-1/notifications?limit=50&before=n-100",
                ])
    }

    @Test func markingOneReadUpdatesAtOnceAndSendsItsID() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()
        let first = try #require(store.items.first)

        await store.markRead(first)

        #expect(store.items.first?.read == true)
        #expect(store.unreadCount == 2)
        #expect(harness.server.readBodies == [#"{"ids":["n-1"]}"#])
        #expect(harness.server.entries(in: "household-1").first?.read == true)

        // Already read: nothing is sent.
        await store.markRead(try #require(store.items.first))
        #expect(harness.server.readBodies.count == 1)
    }

    @Test func aTappedPushIsMarkedReadByIDEvenWhenNotLoaded() async throws {
        let harness = try await makeHarness([
            "household-1": FakeNotificationServer.entries(count: 3, unread: 3),
            "household-2": FakeNotificationServer.entries(count: 2, unread: 2),
        ])
        let store = harness.store
        await store.activate(householdID: "household-1")
        #expect(store.unreadCount == 3)

        // The shown household: the badge follows.
        await store.markRead(notificationID: "n-2", householdID: "household-1")
        #expect(store.unreadCount == 2)
        #expect(harness.server.entries(in: "household-1")[1].read)

        // Another household: marked on the server, this household's badge untouched.
        await store.markRead(notificationID: "n-1", householdID: "household-2")
        #expect(harness.server.entries(in: "household-2").first?.read == true)
        #expect(store.unreadCount == 2)
        #expect(harness.server.readBodies == [#"{"ids":["n-2"]}"#, #"{"ids":["n-1"]}"#])
    }

    @Test func aFailedMarkReadPutsTheNotificationBack() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()
        harness.server.failNext()

        await store.markRead(try #require(store.items.first))

        #expect(store.items.first?.read == false)
        #expect(store.unreadCount == 3)
    }

    @Test func markAllReadClearsTheBadge() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()

        try await store.markAllRead()

        #expect(store.unreadCount == 0)
        #expect(store.items.allSatisfy { $0.read })
        #expect(harness.server.readBodies == [#"{"all":true}"#])
    }

    @Test func aFailedMarkAllReadRestoresAndThrows() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()
        harness.server.failNext()

        await #expect(throws: APIError.self) {
            try await store.markAllRead()
        }

        #expect(store.unreadCount == 3)
        #expect(store.items.prefix(3).allSatisfy { !$0.read })
        #expect(store.items.dropFirst(3).allSatisfy { $0.read })
    }

    @Test func aFailedPageKeepsWhatsLoaded() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()
        harness.server.failNext()

        await store.loadMore()
        #expect(store.loadMoreError != nil)
        #expect(store.items.count == 50)
        #expect(!store.isLoadingMore)

        await store.loadMore()
        #expect(store.loadMoreError == nil)
        #expect(store.items.count == 100)
    }

    @Test func loadFailureThenRetry() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        harness.server.failNext()

        await store.load()
        guard case .failed = store.phase else {
            Issue.record("expected a failure, got \(store.phase)")
            return
        }

        await store.load()
        #expect(store.phase == .loaded)
        #expect(store.items.count == 50)
    }

    @Test func refreshKeepsItemsWhenItFails() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()
        harness.server.failNext()

        await store.refresh()

        #expect(store.phase == .loaded)
        #expect(store.refreshError != nil)
        #expect(store.items.count == 50)
    }

    @Test func switchingHouseholdsStartsOver() async throws {
        let harness = try await makeHarness([
            "household-1": FakeNotificationServer.entries(count: 5, unread: 2),
            "household-2": FakeNotificationServer.entries(count: 1, unread: 1),
        ])
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()

        await store.activate(householdID: "household-2")

        #expect(store.householdID == "household-2")
        #expect(store.phase == .idle)
        #expect(store.items.isEmpty)
        #expect(store.unreadCount == 1)
    }

    @Test func resetForgetsEverything() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        await store.load()

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.items.isEmpty)
        #expect(store.unreadCount == 0)
        #expect(store.phase == .idle)
        let requests = harness.server.log.count
        await store.refreshUnreadCount()
        #expect(harness.server.log.count == requests)
        await #expect(throws: AuthSessionError.self) {
            try await store.markAllRead()
        }
    }
}
