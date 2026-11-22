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
    @State private var actionTitle = String(localized: "Couldn’t Mark Notifications Read")

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Notifications")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    // Only when there's something to mark: a grayed-out button read as broken.
                    if notifications.unreadCount > 0 || !notifications.items.allSatisfy(\.read) {
                        ToolbarItem(placement: .topBarLeading) {
                            Button("Mark All Read") { markAllRead() }
                        }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
                .alert(
                    actionTitle,
                    isPresented: Binding(presenting: $actionError),
                    presenting: actionError
                ) { _ in
                    Button("OK") {}
                } message: { message in
                    Text(message)
                }
        }
        // As tall as the list, with no empty space under a few rows (build 122, Hark's item 16).
        .dsFittedSheet(background: DS.Colors.groupedBackground)
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
                Label("Couldn’t Load Notifications", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await notifications.load() }
                }
                .buttonStyle(.dsPrimary(.small))
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
            ForEach(NotificationGrouping.groups(notifications.items)) { group in
                row(group)
            }
            if notifications.hasMore {
                nextPageRow
            }
        }
        .overlay {
            if notifications.items.isEmpty {
                ContentUnavailableView(
                    "No Notifications", systemImage: "bell",
                    description: Text("Things like a pantry item running low show up here."))
            }
        }
        .refreshable {
            await notifications.refresh()
        }
        .dsSheetScrollContent(isOn: !notifications.items.isEmpty)
    }

    /// One row for a notification and any repeats of it (`NotificationGrouping`); its actions
    /// apply to all of them.
    private func row(_ group: NotificationGroup) -> some View {
        Button {
            open(group)
        } label: {
            NotificationRow(notification: group.latest, isRead: group.isRead, moreCount: group.moreCount)
        }
        .accessibilityHint(hint(for: group.latest))
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button("Delete", systemImage: "trash", role: .destructive) {
                delete(group)
            }
        }
        .swipeActions(edge: .leading) {
            if !group.isRead {
                Button("Mark Read", systemImage: "envelope.open") {
                    markRead(group)
                }
                .tint(DS.Colors.accent)
            }
        }
    }

    private func markRead(_ group: NotificationGroup) {
        let unread = group.unread
        Task {
            for notification in unread {
                await notifications.markRead(notification)
            }
        }
    }

    @ViewBuilder
    private var nextPageRow: some View {
        if let loadMoreError = notifications.loadMoreError {
            VStack(alignment: .leading, spacing: DS.Space.s) {
                FormErrorLabel(message: loadMoreError)
                Button("Try Again") {
                    Task { await notifications.loadMore() }
                }
            }
        } else {
            HStack {
                Spacer()
                ProgressView()
                    .accessibilityLabel("Loading More Notifications")
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

    /// Gone from this member's list only; the rest of the household still sees it.
    private func delete(_ group: NotificationGroup) {
        let items = group.items
        Task {
            do {
                for notification in items {
                    try await notifications.dismiss(notification)
                }
            } catch is CancellationError {
                return
            } catch {
                actionTitle = String(localized: "Couldn’t Delete Notification")
                actionError = HouseholdStore.message(for: error)
            }
        }
    }

    private func open(_ group: NotificationGroup) {
        markRead(group)
        let destination = NotificationRouting.destination(for: group.latest)
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
                actionTitle = String(localized: "Couldn’t Mark Notifications Read")
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// One notification: an icon for its type, title, body, age, and an unread dot. A row standing
/// for repeats says how many more there are.
struct NotificationRow: View {
    let notification: AppNotification
    var isRead: Bool
    var moreCount = 0

    init(notification: AppNotification, isRead: Bool? = nil, moreCount: Int = 0) {
        self.notification = notification
        self.isRead = isRead ?? notification.read
        self.moreCount = moreCount
    }

    private var icon: String {
        switch notification.type {
        case .pantryLow: "cabinet"
        case .pantryThaw: "snowflake"
        case .shoppingOrderDue: "cart"
        default: "bell"
        }
    }

    private var iconStyle: AnyShapeStyle {
        switch notification.type {
        case .pantryLow: AnyShapeStyle(DS.Colors.warning)
        default: AnyShapeStyle(DS.Colors.accent)
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: DS.Space.m) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(iconStyle)
                .dsIconColumn()
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                // A concrete color: `.primary` inside a list Button resolves to the tint.
                Text(notification.title)
                    .font(isRead ? DS.Typography.body : DS.Typography.body.weight(.semibold))
                    .foregroundStyle(Color.primary)
                Text(notification.body)
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
                HStack(spacing: DS.Space.xs) {
                    Text(notification.createdAt, format: .relative(presentation: .named))
                    if let more = NotificationGrouping.moreText(moreCount) {
                        Text(verbatim: "·")
                            .accessibilityHidden(true)
                        Text(more)
                    }
                }
                .font(DS.Typography.caption)
                .foregroundStyle(DS.Colors.secondaryText)
            }
            Spacer(minLength: 0)
            if !isRead {
                Circle()
                    .fill(DS.Colors.accent)
                    .frame(width: DS.Space.sm, height: DS.Space.sm)
                    .padding(.top, DS.Space.xs)
                    .accessibilityHidden(true)
            }
        }
        // Keeps `.secondary` text gray inside the list Button instead of tinted.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityValue(isRead ? Text("") : Text("Unread"))
    }
}

/// The bell toolbar button with the unread count.
struct NotificationBell: View {
    let count: Int
    let action: () -> Void

    @ScaledMetric(relativeTo: .caption2) private var badgeSize = 16.0

    /// How far the badge's center sits past the icon's corner, as a share of its size: inside
    /// the button's own margin, so the toolbar doesn't clip it.
    static let badgeOffset: CGFloat = 0.4

    var body: some View {
        Button(action: action) {
            Image(systemName: "bell")
                // The badge sits in the margin the toolbar already gives the icon, so the bell
                // is the same size as every other toolbar button. Padding it out for the badge
                // made it wider than the iPhone Duo's side column, which then moved the bell out
                // of the column to the bottom of the screen (build 121).
                .overlay(alignment: .topTrailing) {
                    if count > 0 {
                        Text(count > 99 ? "99+" : count.formatted())
                            .font(.caption2.weight(.bold))
                            .monospacedDigit()
                            .foregroundStyle(.white)
                            .padding(.horizontal, DS.Space.xxs)
                            .frame(minWidth: badgeSize, minHeight: badgeSize)
                            .background(.red, in: .capsule)
                            .fixedSize()
                            .offset(
                                x: badgeSize * NotificationBell.badgeOffset,
                                y: -badgeSize * NotificationBell.badgeOffset)
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
            .dsSheet(
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
