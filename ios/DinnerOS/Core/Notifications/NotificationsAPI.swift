import Foundation

/// Typed wrappers for the household notification endpoints
/// (`/api/v1/households/{householdId}/notifications`). Every call needs only
/// `household.view`. Use them through `AuthSession.authorized`.
nonisolated struct NotificationsAPI: Sendable {
    static let maxPageSize = 100
    /// The API marks at most this many IDs read per request.
    static let maxMarkRead = 100

    let client: APIClient

    /// One page, newest first. `before` is the previous page's `nextCursor`.
    func list(
        householdID: String, before: String? = nil, limit: Int, unreadOnly: Bool = false, accessToken: String
    ) async throws -> NotificationListResponse {
        var request = APIRequest.get(Self.path(householdID))
        request.queryItems = Self.queryItems(before: before, limit: limit, unreadOnly: unreadOnly)
        return try await client.send(request.authorized(with: accessToken))
    }

    func unreadCount(householdID: String, accessToken: String) async throws -> Int {
        let response: NotificationUnreadCount = try await client.send(
            APIRequest.get(Self.path(householdID) + "/unread-count").authorized(with: accessToken))
        return response.unreadCount
    }

    /// Marks up to `maxMarkRead` notifications read and returns the unread count afterwards.
    func markRead(householdID: String, ids: [String], accessToken: String) async throws -> Int {
        try await markRead(
            householdID: householdID, body: MarkNotificationsReadRequest(ids: Array(ids.prefix(Self.maxMarkRead))),
            accessToken: accessToken)
    }

    /// Marks every notification read and returns the unread count afterwards.
    func markAllRead(householdID: String, accessToken: String) async throws -> Int {
        try await markRead(
            householdID: householdID, body: MarkNotificationsReadRequest(all: true), accessToken: accessToken)
    }

    private func markRead(
        householdID: String, body: MarkNotificationsReadRequest, accessToken: String
    ) async throws -> Int {
        let response: NotificationUnreadCount = try await client.send(
            try APIRequest.post(Self.path(householdID) + "/read", body: body).authorized(with: accessToken))
        return response.unreadCount
    }

    /// `limit` kept within 1...100; `before` and `unread` only when they apply.
    static func queryItems(before: String?, limit: Int, unreadOnly: Bool) -> [URLQueryItem] {
        var items = [URLQueryItem(name: "limit", value: String(min(max(limit, 1), maxPageSize)))]
        if let before, !before.isEmpty {
            items.append(URLQueryItem(name: "before", value: before))
        }
        if unreadOnly {
            items.append(URLQueryItem(name: "unread", value: "true"))
        }
        return items
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/notifications"
    }
}
