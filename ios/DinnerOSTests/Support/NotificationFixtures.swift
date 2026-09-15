import Foundation
import Synchronization

@testable import DinnerOS

nonisolated enum NotificationFixtures {
    static let pageJSON = Data(
        #"""
        {"items":[
          {"id":"n-2","householdId":"household-1","type":"pantry.low","title":"Butter is running low",
           "body":"About 19% left: 4 recipes used 0.81 cup.","subject":{"kind":"pantry_item","id":"item-butter"},
           "read":false,"createdAt":"2026-09-20T18:30:00.123456789Z"},
          {"id":"n-1","householdId":"household-1","type":"household.joined","title":"Ada joined",
           "body":"Say hello.","subject":{"kind":"member","id":"user-2"},"read":true,
           "createdAt":"2026-09-19T18:30:00Z"}
        ],"nextCursor":"cursor-abc"}
        """#.utf8)
}

/// An in-memory stand-in for the notification endpoints: newest first, cursor paging by the
/// last ID of a page, and per-member read state.
nonisolated final class FakeNotificationServer: Sendable {
    struct Entry: Sendable, Equatable {
        let id: String
        var read: Bool
    }

    struct State: Sendable {
        /// Newest first, by household.
        var households: [String: [Entry]] = [:]
        var failuresRemaining = 0
        /// `METHOD /path?query` for every request, in order.
        var log: [String] = []
        /// The body of every mark-read request.
        var readBodies: [String] = []
    }

    private let state: Mutex<State>

    init(_ households: [String: [Entry]]) {
        state = Mutex(State(households: households))
    }

    /// `count` notifications, `n-1` newest, the first `unread` of them unread.
    static func entries(count: Int, unread: Int) -> [Entry] {
        (1...max(count, 1)).prefix(count).map { Entry(id: "n-\($0)", read: $0 > unread) }
    }

    var log: [String] { state.withLock { $0.log } }
    var readBodies: [String] { state.withLock { $0.readBodies } }

    func entries(in householdID: String) -> [Entry] {
        state.withLock { $0.households[householdID] ?? [] }
    }

    func failNext(_ count: Int = 1) {
        state.withLock { $0.failuresRemaining = count }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let queryItems = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let query = Dictionary(queryItems.map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { _, last in last })

        return state.withLock { state in
            let queryText = url.query().map { "?\($0)" } ?? ""
            state.log.append("\(method) /\(route.joined(separator: "/"))\(queryText)")
            if state.failuresRemaining > 0 {
                state.failuresRemaining -= 1
                return (500, Fixtures.errorJSON(code: "internal"))
            }
            guard route.count >= 3, route[0] == "households", route[2] == "notifications" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            let householdID = route[1]
            let rest = Array(route.dropFirst(3))
            var entries = state.households[householdID] ?? []
            defer { state.households[householdID] = entries }

            switch (method, rest) {
            case ("GET", []):
                let limit = Int(query["limit"] ?? "") ?? 50
                let visible = query["unread"] == "true" ? entries.filter { !$0.read } : entries
                var start = 0
                if let before = query["before"] {
                    guard let index = visible.firstIndex(where: { $0.id == before }) else {
                        return (400, Fixtures.errorJSON(code: "validation_failed", message: "bad cursor"))
                    }
                    start = index + 1
                }
                let page = Array(visible[start...].prefix(limit))
                let next = start + page.count < visible.count ? page.last.map { #""\#($0.id)""# } : nil
                let items = page.map { Self.json($0, householdID: householdID) }.joined(separator: ",")
                return (200, Data(#"{"items":[\#(items)],"nextCursor":\#(next ?? "null")}"#.utf8))
            case ("GET", ["unread-count"]):
                return (200, Self.count(entries))
            case ("POST", ["read"]):
                let body = request.httpBody.flatMap { String(data: $0, encoding: .utf8) } ?? ""
                state.readBodies.append(body)
                let fields = PantryFixtures.body(of: request)
                if fields["all"] as? Bool == true {
                    for index in entries.indices {
                        entries[index].read = true
                    }
                } else if let ids = fields["ids"] as? [String], !ids.isEmpty {
                    for index in entries.indices where ids.contains(entries[index].id) {
                        entries[index].read = true
                    }
                } else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "ids or all"))
                }
                return (200, Self.count(entries))
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private static func count(_ entries: [Entry]) -> Data {
        Data(#"{"unreadCount":\#(entries.count { !$0.read })}"#.utf8)
    }

    private static func json(_ entry: Entry, householdID: String) -> String {
        #"{"id":"\#(entry.id)","householdId":"\#(householdID)","type":"pantry.low","#
            + #""title":"Item \#(entry.id) is running low","body":"About 10% left.","#
            + #""subject":{"kind":"pantry_item","id":"item-\#(entry.id)"},"read":\#(entry.read),"#
            + #""createdAt":"2026-09-15T18:30:00Z"}"#
    }
}
