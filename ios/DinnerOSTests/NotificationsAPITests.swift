import Foundation
import Testing

@testable import DinnerOS

struct NotificationsAPITests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    @Test func firstPageSendsOnlyTheLimitAndDecodes() async throws {
        let transport = StubTransport { _ in (200, NotificationFixtures.pageJSON) }

        let page = try await NotificationsAPI(client: makeClient(transport))
            .list(householdID: "household-1", limit: 50, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/notifications")
        #expect(request.url?.query() == "limit=50")
        #expect(request.bearerToken == "token-1")

        #expect(page.nextCursor == "cursor-abc")
        let low = try #require(page.items.first)
        #expect(low.id == "n-2")
        #expect(low.type == .pantryLow)
        #expect(low.title == "Butter is running low")
        #expect(low.subject.pantryItemID == "item-butter")
        #expect(!low.read)
        #expect(low.createdAt == JSONCoding.parseDate("2026-09-20T18:30:00.123Z"))

        // A type this build doesn't know still decodes, and opens nothing.
        let other = try #require(page.items.last)
        #expect(other.type.rawValue == "household.joined")
        #expect(other.subject.pantryItemID == nil)
        #expect(other.read)
    }

    @Test func laterPagesSendTheCursorAndClampTheLimit() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"items":[],"nextCursor":null}"#.utf8)) }

        let page = try await NotificationsAPI(client: makeClient(transport))
            .list(householdID: "household-1", before: "cursor+1", limit: 500, unreadOnly: true, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.url?.query(percentEncoded: true) == "limit=100&before=cursor%2B1&unread=true")
        #expect(page.items.isEmpty)
        #expect(page.nextCursor == nil)
        #expect(NotificationsAPI.queryItems(before: "", limit: 0, unreadOnly: false).map(\.value) == ["1"])
    }

    @Test func unreadCountDecodes() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"unreadCount":4}"#.utf8)) }

        let count = try await NotificationsAPI(client: makeClient(transport))
            .unreadCount(householdID: "household-1", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/notifications/unread-count")
        #expect(count == 4)
    }

    @Test func markReadSendsIDsOrAll() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"unreadCount":1}"#.utf8)) }
        let api = NotificationsAPI(client: try makeClient(transport))

        let afterOne = try await api.markRead(householdID: "household-1", ids: ["n-2"], accessToken: "t")
        let afterAll = try await api.markAllRead(householdID: "household-1", accessToken: "t")

        #expect(afterOne == 1)
        #expect(afterAll == 1)
        let requests = transport.requests
        #expect(requests.allSatisfy { $0.httpMethod == "POST" })
        #expect(requests.allSatisfy { $0.url?.path() == "/api/v1/households/household-1/notifications/read" })
        let idsBody = PantryFixtures.body(of: requests[0])
        #expect(Set(idsBody.keys) == ["ids"])
        #expect(idsBody["ids"] as? [String] == ["n-2"])
        let allBody = PantryFixtures.body(of: requests[1])
        #expect(Set(allBody.keys) == ["all"])
        #expect(allBody["all"] as? Bool == true)
    }

    @Test func markReadSendsAtMostTheAPILimit() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"unreadCount":0}"#.utf8)) }
        let ids = (0..<150).map { "n-\($0)" }

        _ = try await NotificationsAPI(client: makeClient(transport))
            .markRead(householdID: "household-1", ids: ids, accessToken: "t")

        let body = PantryFixtures.body(of: try #require(transport.requests.first))
        #expect((body["ids"] as? [String])?.count == NotificationsAPI.maxMarkRead)
    }
}
