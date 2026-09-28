import SwiftUI

/// The household's notifications, newest first, a page at a time. Tapping one marks it read,
/// closes the sheet, and opens the screen it's about (`NotificationRouting`), the same one a
/// tapped push opens.
///
/// Presented as a sheet from the bell on the Menu and Pantry screens, because the tab bar
/// is full, and by the tab shell for a tapped push of an unknown type.
struct NotificationsView: View {
    @Environment(NotificationStore.self) private var notifications
    @Environment(\.dismiss) private var dismiss
    /// `nil` outside the tab shell, where there are no tabs to open.
    @Environment(TabRouter.self) private var tabs: TabRouter?

    @State private var actionError: String?

    var body: some View {
        NavigationStack {
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
        .accessibilityHint(hint(for: notification))
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

    /// Where the row goes, for VoiceOver.
    private func hint(for notification: AppNotification) -> Text {
        let destination = tabs == nil ? .notifications : NotificationRouting.destination(for: notification)
        return Text(destination.accessibilityHint)
    }

    private func open(_ notification: AppNotification) {
        Task { await notifications.markRead(notification) }
        let destination = NotificationRouting.destination(for: notification)
        guard let tabs, destination != .notifications else { return }
        // The bell is a sheet over a tab, so it closes before the tab changes.
        dismiss()
        Task { await tabs.open(destination) }
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

    private var icon: String {
        switch notification.type {
        case .pantryLow: "cabinet"
        case .pantryThaw: "snowflake"
        case .shoppingOrderDue: "cart"
        case .recipeImportFinished: "shippingbox"
        case .recipeImportAttention: "exclamationmark.triangle"
        default: "bell"
        }
    }

    private var iconStyle: AnyShapeStyle {
        switch notification.type {
        case .pantryLow, .recipeImportAttention: AnyShapeStyle(.orange)
        default: AnyShapeStyle(.tint)
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(iconStyle)
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
        // Keeps `.secondary` text gray inside the list Button instead of tinted.
        .foregroundStyle(Color.primary)
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
                // Room for the badge inside the button: a toolbar clips anything drawn past
                // the item's frame (the iPad toolbar cut it in half).
                .padding(.top, count > 0 ? badgeSize * 0.4 : 0)
                .padding(.trailing, count > 0 ? badgeSize * 0.5 : 0)
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
                            .offset(x: badgeSize * 0.15, y: -badgeSize * 0.1)
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
