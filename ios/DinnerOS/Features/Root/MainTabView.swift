import SwiftUI

/// The signed-in tab shell.
struct MainTabView: View {
    @Environment(HouseholdStore.self) private var households
    @Environment(NotificationStore.self) private var notifications
    @Environment(ShoppingStore.self) private var shopping
    @Environment(PantryStore.self) private var pantry
    /// Optional so previews needn't supply one.
    @Environment(MealKitImportStore.self) private var mealKit: MealKitImportStore?
    /// Optional so previews needn't supply one.
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @Environment(AppIntentRouter.self) private var router: AppIntentRouter?
    /// The selected tab and the stacks a notification opens. In the environment of every tab
    /// and every sheet below, so the bell can switch tabs from wherever it's presented.
    @State private var tabs = TabRouter()

    var body: some View {
        TabView(selection: $tabs.selection) {
            ForEach(AppTab.allCases) { tab in
                Tab(value: tab) {
                    switch tab {
                    case .menu:
                        NavigationStack(path: $tabs.menuPath) {
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
                        NavigationStack(path: $tabs.pantryPath) {
                            PantryView()
                        }
                    case .household:
                        NavigationStack(path: $tabs.householdPath) {
                            HouseholdView()
                                .navigationDestination(for: RecipeImportRoute.self) { _ in
                                    MealKitImportStatusView()
                                }
                        }
                    }
                } label: {
                    Label(tab.title, systemImage: tab.systemImage)
                }
            }
        }
        .offlineBanner()
        .onAppear { configureRouter() }
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
            // The prep checklist is loaded here, not on the Shop tab, because the Pantry
            // offers it too: putting the groceries away belongs to both screens.
            await shopping.loadPrepSession()
        }
        // Hiding controls is a convenience; the API enforces both permissions.
        .onChange(of: households.access, initial: true) { _, access in
            shopping.setPermissions(
                canEdit: access?.can(.shoppingEdit) == true, canConfirm: access?.can(.pantryEdit) == true)
        }
        // A widget: open the meal it shows, plan the week, or go to Shop.
        .onChange(of: router?.widgetLink, initial: true) { _, link in
            guard let link else { return }
            router?.widgetLink = nil
            switch link {
            case .recipe(let id, let name):
                tabs.selection = .menu
                tabs.menuPath = NavigationPath([RecipeSummary.placeholder(id: id, name: name, imageURLString: nil)])
            case .autopilot:
                tabs.selection = .menu
                tabs.menuPath = NavigationPath()
                router?.planWeekRequested = true
            case .shop:
                tabs.selection = .shop
            case .menu:
                tabs.selection = .menu
                tabs.menuPath = NavigationPath()
            }
        }
        // Siri planned a week: the Menu tab opens its suggestions.
        .onChange(of: router?.autopilotReviewWeek, initial: true) { _, week in
            if week != nil { tabs.selection = .menu }
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
        .sheet(isPresented: $tabs.showsNotifications) {
            NotificationsView()
        }
        // Outermost, so the sheets above get it too: an environment value set on the TabView
        // alone doesn't reach a sheet presented from a modifier outside it.
        .environment(tabs)
    }

    /// Gives the router what it needs from the stores: whether a notification's subject still
    /// exists, and how to show a week in Shop.
    private func configureRouter() {
        let households = households
        let pantry = pantry
        let mealKit = mealKit
        let shopping = shopping
        tabs.check = NotificationSubjectCheck(
            pantryItemExists: { itemID in
                guard let householdID = households.current?.household.id else { return false }
                await pantry.activate(householdID: householdID)
                if pantry.items.contains(where: { $0.id == itemID }) { return true }
                // A notification can name an item added after the pantry last loaded.
                if pantry.phase == .loaded { await pantry.refresh() }
                return pantry.phase != .loaded || pantry.items.contains { $0.id == itemID }
            },
            canOpenRecipeImport: {
                guard let householdID = households.current?.household.id,
                    households.access?.can(.recipesImport) == true, let mealKit, mealKit.isAvailable
                else { return false }
                // Recipe Import loads the household it was activated for; the Household tab
                // usually does this, but a notification can open the screen first.
                mealKit.activate(householdID: householdID)
                return true
            })
        tabs.showWeek = { week in
            if let current = households.current?.household {
                shopping.activate(
                    householdID: current.id, timeZone: current.planningTimeZone, weekStartsOn: current.weekStartsOn)
            }
            Task { await shopping.show(week: week) }
        }
    }

    /// Opens a tapped push where its row on the bell leads (`NotificationRouting`). A push for
    /// another of the member's households switches to it first; this runs again when that
    /// household becomes current.
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
        // `.onChange(initial:)` can run before `.onAppear`.
        configureRouter()
        let tabs = tabs
        let destination = NotificationRouting.destination(for: route)
        Task { await tabs.open(destination) }
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
