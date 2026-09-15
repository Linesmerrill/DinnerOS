import SwiftUI

/// The signed-in tab shell.
struct MainTabView: View {
    @Environment(HouseholdStore.self) private var households
    @Environment(NotificationStore.self) private var notifications
    @Environment(ShoppingStore.self) private var shopping
    @State private var selection: AppTab = .menu

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
        .task(id: households.current?.household.id) {
            guard let household = households.current?.household else { return }
            shopping.activate(householdID: household.id, timeZone: household.planningTimeZone)
            await shopping.refreshOpenHandoff(presenting: false)
        }
        // Hiding controls is a convenience; the API enforces both permissions.
        .onChange(of: households.access, initial: true) { _, access in
            shopping.setPermissions(
                canEdit: access?.can(.shoppingEdit) == true, canConfirm: access?.can(.pantryEdit) == true)
        }
        .sheet(item: confirmationPrompt) { handoff in
            OrderConfirmationSheet(handoff: handoff)
        }
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
