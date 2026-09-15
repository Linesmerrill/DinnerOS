import SwiftUI

/// The household's notifications, newest first, a page at a time. Tapping one marks it read,
/// and a pantry alert opens its item.
///
/// Presented as a sheet from the bell on the Pantry and Week screens, because the tab bar
/// is full.
struct NotificationsView: View {
    @Environment(NotificationStore.self) private var notifications
    @Environment(\.dismiss) private var dismiss

    @State private var path: [PantryItemRoute] = []
    @State private var actionError: String?

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle("Notifications")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("Mark All Read") { markAllRead() }
                            .disabled(notifications.unreadCount == 0 && notifications.items.allSatisfy(\.read))
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
                .navigationDestination(for: PantryItemRoute.self) { route in
                    PantryItemDetailView(itemID: route.itemID)
                }
                .alert(
                    "Couldn't Mark Notifications Read",
                    isPresented: Binding(presenting: $actionError),
                    presenting: actionError
                ) { _ in
                    Button("OK") {}
                } message: { message in
                    Text(message)
                }
        }
        .task { await notifications.load() }
    }

    @ViewBuilder
    private var content: some View {
        switch notifications.phase {
        case .idle, .loading:
            ProgressView("Loading notifications…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Notifications", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await notifications.load() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            if let refreshError = notifications.refreshError {
                FormErrorLabel(message: refreshError)
            }
            ForEach(notifications.items) { notification in
                row(notification)
            }
            if notifications.hasMore {
                nextPageRow
            }
        }
        .overlay {
            if notifications.items.isEmpty {
                ContentUnavailableView(
                    "No Notifications", systemImage: "bell",
                    description: Text("Alerts such as a pantry item running low appear here."))
            }
        }
        .refreshable {
            await notifications.refresh()
        }
    }

    private func row(_ notification: AppNotification) -> some View {
        Button {
            open(notification)
        } label: {
            NotificationRow(notification: notification)
        }
        .accessibilityHint(notification.subject.pantryItemID == nil ? Text("") : Text("Opens the pantry item."))
        .swipeActions(edge: .leading) {
            if !notification.read {
                Button("Mark Read", systemImage: "envelope.open") {
                    Task { await notifications.markRead(notification) }
                }
                .tint(.blue)
            }
        }
    }

    @ViewBuilder
    private var nextPageRow: some View {
        if let loadMoreError = notifications.loadMoreError {
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: loadMoreError)
                Button("Try Again") {
                    Task { await notifications.loadMore() }
                }
            }
        } else {
            HStack {
                Spacer()
                ProgressView()
                    .accessibilityLabel("Loading more notifications")
                Spacer()
            }
            // Loads when the row scrolls into view.
            .task(id: notifications.nextCursor) {
                await notifications.loadMore()
            }
        }
    }

    private func open(_ notification: AppNotification) {
        Task { await notifications.markRead(notification) }
        if let itemID = notification.subject.pantryItemID {
            path.append(PantryItemRoute(itemID: itemID))
        }
    }

    private func markAllRead() {
        Task {
            do {
                try await notifications.markAllRead()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// One notification: an icon for its type, title, body, age, and an unread dot.
struct NotificationRow: View {
    let notification: AppNotification

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: notification.type == .pantryLow ? "cabinet" : "bell")
                .font(.title3)
                .foregroundStyle(notification.type == .pantryLow ? AnyShapeStyle(.orange) : AnyShapeStyle(.tint))
                .frame(minWidth: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                // A concrete color: `.primary` inside a list Button resolves to the tint.
                Text(notification.title)
                    .font(notification.read ? .body : .body.weight(.semibold))
                    .foregroundStyle(Color.primary)
                Text(notification.body)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(notification.createdAt, format: .relative(presentation: .named))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if !notification.read {
                Circle()
                    .fill(.tint)
                    .frame(width: 10, height: 10)
                    .padding(.top, 6)
                    .accessibilityHidden(true)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityValue(notification.read ? Text("") : Text("Unread"))
    }
}

/// The bell toolbar button with the unread count.
struct NotificationBell: View {
    let count: Int
    let action: () -> Void

    @ScaledMetric(relativeTo: .caption2) private var badgeSize = 16.0

    var body: some View {
        Button(action: action) {
            Image(systemName: "bell")
                .overlay(alignment: .topTrailing) {
                    if count > 0 {
                        Text(count > 99 ? "99+" : count.formatted())
                            .font(.caption2.weight(.bold))
                            .monospacedDigit()
                            .foregroundStyle(.white)
                            .padding(.horizontal, 4)
                            .frame(minWidth: badgeSize, minHeight: badgeSize)
                            .background(.red, in: .capsule)
                            .fixedSize()
                            .offset(x: badgeSize * 0.55, y: -badgeSize * 0.45)
                    }
                }
        }
        .accessibilityLabel("Notifications")
        .accessibilityValue(count == 0 ? Text("None unread") : Text("\(count) unread"))
    }
}

private struct NotificationsToolbarModifier: ViewModifier {
    let isHidden: Bool

    @Environment(NotificationStore.self) private var notifications
    @State private var isPresented = false

    func body(content: Content) -> some View {
        content
            .toolbar {
                if !isHidden {
                    ToolbarItem(placement: .topBarTrailing) {
                        NotificationBell(count: notifications.unreadCount) {
                            isPresented = true
                        }
                    }
                }
            }
            .sheet(
                isPresented: $isPresented,
                onDismiss: {
                    Task { await notifications.refreshUnreadCount() }
                }
            ) {
                NotificationsView()
            }
    }
}

extension View {
    /// Adds the notifications bell to the toolbar and presents the list from it.
    func notificationsToolbar(isHidden: Bool = false) -> some View {
        modifier(NotificationsToolbarModifier(isHidden: isHidden))
    }
}

/// Sample data for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are
/// type-checked in Release builds too.
enum NotificationPreviewData {
    static let items: [AppNotification] = [
        AppNotification(
            id: "notification-1", householdID: HouseholdPreviewData.household.id, type: .pantryLow,
            title: "Carrots are running low", body: "About 19% left: 4 recipes used 5 carrots.",
            subject: AppNotificationSubject(kind: AppNotificationSubject.pantryItemKind, id: "p2"), read: false,
            createdAt: .now.addingTimeInterval(-3_600)),
        AppNotification(
            id: "notification-2", householdID: HouseholdPreviewData.household.id, type: .pantryLow,
            title: "Olive oil is running low", body: "About 12% left: 3 recipes used 1 cup.",
            subject: AppNotificationSubject(kind: AppNotificationSubject.pantryItemKind, id: "p1"), read: true,
            createdAt: .now.addingTimeInterval(-3 * 86_400)),
    ]

    static func store(session: AuthSession) -> NotificationStore {
        .preview(session: session, items: items, householdID: HouseholdPreviewData.household.id)
    }
}

#Preview("List") {
    let session = HouseholdPreviewData.session()
    NotificationsView()
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(PantryPreviewData.store(session: session))
        .environment(NotificationPreviewData.store(session: session))
}

#Preview("Empty") {
    let session = HouseholdPreviewData.session()
    NotificationsView()
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(PantryPreviewData.store(session: session))
        .environment(NotificationStore.preview(session: session))
}
