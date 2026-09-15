import Foundation
import Observation
import os

/// The current household's notifications and the signed-in member's unread count.
///
/// Main-actor state that lives as long as the app, like `PantryStore`. The count backs the
/// bell badge. It refreshes when a household is activated, when the app comes to the
/// foreground, and after pantry, purchase, and cooking changes; there's no polling and no
/// push yet (docs/pantry-usage.md). The list loads when it's opened, a page at a time.
@Observable
final class NotificationStore {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    static let pageSize = 50

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    /// Newest first.
    private(set) var items: [AppNotification] = []
    private(set) var unreadCount = 0
    private(set) var isLoadingMore = false
    /// Set when the next page failed; the loaded pages stay on screen.
    private(set) var loadMoreError: String?
    /// Set when a reload failed while notifications stayed on screen.
    private(set) var refreshError: String?
    private(set) var nextCursor: String?

    var hasMore: Bool { nextCursor != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: NotificationsAPI?
    /// The user the notifications were loaded for, so another sign-in never sees them.
    @ObservationIgnored private var userID: String?
    /// Incremented when the household or user changes, so a slow response can't write
    /// into the new scope.
    @ObservationIgnored private var scope = 0
    /// Incremented by every first-page load, so an older page can't replace a newer one.
    @ObservationIgnored private var listGeneration = 0
    /// Incremented by every count request and read change; only the latest sets the count.
    @ObservationIgnored private var countGeneration = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "notifications")

    init(session: AuthSession, api: NotificationsAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with the given notifications, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, items: [AppNotification] = [], unreadCount: Int? = nil, phase: Phase = .loaded,
        householdID: String = "household-1"
    ) -> NotificationStore {
        let store = NotificationStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.items = items
        store.unreadCount = unreadCount ?? items.count { !$0.read }
        store.phase = phase
        return store
    }

    // MARK: - Unread count

    /// Starts showing `householdID`'s notifications and refreshes the unread count. Another
    /// household or user clears what was loaded first.
    func activate(householdID: String) async {
        let currentUserID = session.currentUser?.id
        if householdID != self.householdID || currentUserID != userID {
            clear()
            self.householdID = householdID
            userID = currentUserID
        }
        await refreshUnreadCount()
    }

    /// Best effort: a failure keeps the last count and is only logged, because the badge
    /// isn't worth an error message.
    func refreshUnreadCount() async {
        guard let api, let householdID else { return }
        countGeneration += 1
        let started = (countGeneration, scope)
        do {
            let count = try await session.authorized { token in
                try await api.unreadCount(householdID: householdID, accessToken: token)
            }
            guard started == (countGeneration, scope) else { return }
            unreadCount = count
        } catch is CancellationError {
            // The next trigger refreshes again.
        } catch {
            Self.logger.notice("Unread count failed: \(Self.describe(error), privacy: .public)")
        }
    }

    // MARK: - List

    /// The first page, with a progress state when nothing is shown yet.
    func load() async {
        await loadFirstPage(showingProgress: phase != .loaded)
    }

    /// Pull to refresh: notifications stay on screen until the response arrives.
    func refresh() async {
        await loadFirstPage(showingProgress: false)
    }

    private func loadFirstPage(showingProgress: Bool) async {
        guard let api, let householdID else { return }
        listGeneration += 1
        let started = (listGeneration, scope)
        if showingProgress || phase != .loaded {
            items = []
            nextCursor = nil
            phase = .loading
        }
        do {
            let page = try await session.authorized { token in
                try await api.list(householdID: householdID, limit: Self.pageSize, accessToken: token)
            }
            guard started == (listGeneration, scope) else { return }
            items = page.items
            nextCursor = page.nextCursor
            loadMoreError = nil
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == (listGeneration, scope), phase == .loading { phase = .idle }
            return
        } catch {
            guard started == (listGeneration, scope) else { return }
            Self.logger.notice("Notifications load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
            return
        }
        // Reading the list runs the pantry's low-stock check, which can add notifications.
        await refreshUnreadCount()
    }

    /// The next page, when there is one and none is loading.
    func loadMore() async {
        guard let api, let householdID, let cursor = nextCursor, phase == .loaded, !isLoadingMore else { return }
        let started = (listGeneration, scope)
        isLoadingMore = true
        loadMoreError = nil
        defer {
            if started.1 == scope { isLoadingMore = false }
        }
        do {
            let page = try await session.authorized { token in
                try await api.list(householdID: householdID, before: cursor, limit: Self.pageSize, accessToken: token)
            }
            guard started == (listGeneration, scope) else { return }
            let known = Set(items.map(\.id))
            items += page.items.filter { !known.contains($0.id) }
            nextCursor = page.nextCursor
        } catch is CancellationError {
            return
        } catch {
            guard started == (listGeneration, scope) else { return }
            Self.logger.notice("Notifications page failed: \(Self.describe(error), privacy: .public)")
            loadMoreError = HouseholdStore.message(for: error)
        }
    }

    // MARK: - Reading

    /// Marks one notification read on screen at once. A failure puts it back and refreshes
    /// the count without an error message: the member is already opening what they tapped.
    func markRead(_ notification: AppNotification) async {
        guard let api, let householdID, let index = items.firstIndex(where: { $0.id == notification.id }),
            !items[index].read
        else { return }
        items[index].read = true
        unreadCount = max(0, unreadCount - 1)
        countGeneration += 1
        let started = (countGeneration, scope)
        do {
            let count = try await session.authorized { token in
                try await api.markRead(householdID: householdID, ids: [notification.id], accessToken: token)
            }
            guard started == (countGeneration, scope) else { return }
            unreadCount = count
        } catch is CancellationError {
            return
        } catch {
            guard started.1 == scope else { return }
            Self.logger.notice("Mark read failed: \(Self.describe(error), privacy: .public)")
            if let index = items.firstIndex(where: { $0.id == notification.id }) {
                items[index].read = false
            }
            await refreshUnreadCount()
        }
    }

    /// Marks everything read on screen at once. A failure puts the unread state back and throws.
    func markAllRead() async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let householdID else { throw AuthSessionError.signedOut }
        let previouslyUnread = Set(items.filter { !$0.read }.map(\.id))
        let previousCount = unreadCount
        for index in items.indices {
            items[index].read = true
        }
        unreadCount = 0
        countGeneration += 1
        let started = (countGeneration, scope)
        do {
            let count = try await session.authorized { token in
                try await api.markAllRead(householdID: householdID, accessToken: token)
            }
            guard started == (countGeneration, scope) else { return }
            unreadCount = count
        } catch {
            guard started.1 == scope else { throw error }
            Self.logger.notice("Mark all read failed: \(Self.describe(error), privacy: .public)")
            for index in items.indices where previouslyUnread.contains(items[index].id) {
                items[index].read = false
            }
            if started.0 == countGeneration {
                unreadCount = previousCount
            }
            throw error
        }
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        userID = nil
    }

    private func clear() {
        scope += 1
        listGeneration += 1
        countGeneration += 1
        phase = .idle
        items = []
        nextCursor = nil
        unreadCount = 0
        isLoadingMore = false
        loadMoreError = nil
        refreshError = nil
    }

    /// Status and error code only; never tokens or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}
