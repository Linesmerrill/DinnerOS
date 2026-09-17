import SwiftUI

/// The signed-in tab shell.
struct MainTabView: View {
    @Environment(HouseholdStore.self) private var households
    @Environment(NotificationStore.self) private var notifications
    @Environment(ShoppingStore.self) private var shopping
    /// Optional so previews needn't supply one.
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @Environment(AppIntentRouter.self) private var router: AppIntentRouter?
    @State private var selection: AppTab = .menu
    /// A pantry item a tapped push opened; the bell opens the same screen.
    @State private var pushedPantryItem: PushedPantryItem?
    /// A tapped push of a type with no screen of its own opens the bell.
    @State private var showsPushedNotifications = false

    var body: some View {
        TabView(selection: $selection) {
            ForEach(AppTab.allCases) { tab in
                Tab(value: tab) {
                    switch tab {
                    case .menu:
                        NavigationStack {
                            MenuView()
                        }
                        // Another household starts at its own menu, not at a recipe
                        // (or search) from the previous one.
                        .id(households.current?.household.id)
                    case .shop:
                        NavigationStack {
                            ShopView()
                        }
                        .id(households.current?.household.id)
                    case .pantry:
                        NavigationStack {
                            PantryView()
                        }
                    case .household:
                        NavigationStack {
                            HouseholdView()
                        }
                    }
                } label: {
                    Label(tab.title, systemImage: tab.systemImage)
                }
            }
        }
        .environment(
            \.openShop,
            OpenShopAction { week in
                selection = .shop
                Task { await shopping.show(week: week) }
            }
        )
        // Another household clears the previous one's notifications and badge.
        .task(id: households.current?.household.id) {
            guard let householdID = households.current?.household.id else { return }
            await notifications.activate(householdID: householdID)
        }
        // Another household clears the previous one's shopping state. The open handoff is read
        // so "Did you order these?" can be asked when the app returns from Walmart.
        .task(id: households.current?.household.weekScope) {
            guard let household = households.current?.household else { return }
            shopping.activate(
                householdID: household.id, timeZone: household.planningTimeZone, weekStartsOn: household.weekStartsOn)
            await shopping.refreshOpenHandoff(presenting: false)
        }
        // Hiding controls is a convenience; the API enforces both permissions.
        .onChange(of: households.access, initial: true) { _, access in
            shopping.setPermissions(
                canEdit: access?.can(.shoppingEdit) == true, canConfirm: access?.can(.pantryEdit) == true)
        }
        // Siri planned a week: the Menu tab opens its suggestions.
        .onChange(of: router?.autopilotReviewWeek, initial: true) { _, week in
            if week != nil { selection = .menu }
        }
        .sheet(item: confirmationPrompt) { handoff in
            OrderConfirmationSheet(handoff: handoff)
        }
        // A tapped push opens once its household is the current one.
        .onChange(
            of: PushRouteScope(route: push?.pendingRoute, householdID: households.current?.household.id), initial: true
        ) {
            openPendingPush()
        }
        .sheet(item: $pushedPantryItem) { pushed in
            NavigationStack {
                PantryItemDetailView(itemID: pushed.itemID)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") { pushedPantryItem = nil }
                        }
                    }
            }
        }
        .sheet(isPresented: $showsPushedNotifications) {
            NotificationsView()
        }
    }

    /// Opens a tapped push where its row on the bell leads: a pantry item, or the week in
    /// Shop. A push for another of the member's households switches to it first; this runs
    /// again when that household becomes current.
    private func openPendingPush() {
        guard let push, let route = push.pendingRoute, let current = households.current?.household else { return }
        if route.householdID != current.id {
            if households.households.contains(where: { $0.id == route.householdID }) {
                // This runs again once the other household is current.
                let households = households
                Task { await households.selectHousehold(id: route.householdID) }
            } else {
                // No longer a member: there's nothing of theirs to open.
                push.finishOpening(route)
            }
            return
        }
        let notifications = notifications
        Task { await notifications.markRead(notificationID: route.notificationID, householdID: route.householdID) }
        if let itemID = route.subject?.pantryItemID {
            pushedPantryItem = PushedPantryItem(itemID: itemID)
        } else if let week = route.subject?.shoppingWeek.flatMap(ISOWeek.init) {
            selection = .shop
            shopping.activate(
                householdID: current.id, timeZone: current.planningTimeZone, weekStartsOn: current.weekStartsOn)
            let shopping = shopping
            Task { await shopping.show(week: week) }
        } else {
            showsPushedNotifications = true
        }
        push.finishOpening(route)
    }

    /// Swiping the question away is "Not yet".
    private var confirmationPrompt: Binding<ShoppingHandoff?> {
        Binding(
            get: { shopping.confirmationPrompt },
            set: { prompt in
                if prompt == nil {
                    shopping.dismissConfirmation()
                }
            })
    }
}

private struct PushRouteScope: Equatable {
    let route: PushRoute?
    let householdID: String?
}

private struct PushedPantryItem: Identifiable {
    let itemID: String
    var id: String { itemID }
}
