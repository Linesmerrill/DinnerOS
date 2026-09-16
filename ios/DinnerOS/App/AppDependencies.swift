import SwiftUI
import UIKit

/// Builds the app's long-lived services from configuration. `DinnerOSApp` is the only
/// place concrete implementations are chosen.
final class AppDependencies {
    let configuration: AppConfiguration
    let session: AuthSession
    let households: HouseholdStore
    let recipes: RecipeLibrary
    let plans: PlanStore
    let pantry: PantryStore
    let specialties: SpecialtyStore
    let autopilot: AutopilotStore
    let events: EventReporter
    let notifications: NotificationStore
    let shopping: ShoppingStore
    let menu: MenuStore
    let planner: MealPlanner
    let pairings: PairingsStore
    /// `nil` when the build has no Google client ID; the Google button is then hidden.
    let googleSignIn: GoogleSignInService?

    init(configuration: AppConfiguration = .main) {
        self.configuration = configuration
        // `AsyncImage` loads through `URLSession.shared`. The default cache is tiny, so recipe photos
        // scrolled back into view would download again; card-sized URLs keep entries small.
        URLCache.shared = URLCache(memoryCapacity: 48 * 1024 * 1024, diskCapacity: 256 * 1024 * 1024)
        let transport = URLSessionTransport()
        let client = configuration.apiBaseURL.map { APIClient(baseURL: $0, transport: transport) }
        session = AuthSession(api: client.map { AuthAPI(client: $0) }, store: KeychainTokenStore())
        households = HouseholdStore(
            session: session,
            api: client.map { HouseholdsAPI(client: $0) },
            selection: UserDefaultsHouseholdSelection(),
            inviteURLScheme: configuration.urlScheme,
            inviteLinkHost: configuration.appLinkDomain)
        recipes = RecipeLibrary(session: session, api: client.map { RecipesAPI(client: $0) })
        // Grocery lists and the Shop tab share check-offs: confirming an order checks lines off.
        let groceryChecks = UserDefaultsGroceryChecks()
        let plans = PlanStore(session: session, api: client.map { PlansAPI(client: $0) }, checks: groceryChecks)
        self.plans = plans
        let menu = MenuStore(session: session, api: client.map { MenuAPI(client: $0) })
        self.menu = menu
        planner = MealPlanner(plans: plans, library: recipes, households: households)
        let pantry = PantryStore(
            session: session,
            api: client.map { PantryAPI(client: $0) },
            ingredientsAPI: client.map { IngredientsAPI(client: $0) })
        self.pantry = pantry
        let notifications = NotificationStore(
            session: session, api: client.map { NotificationsAPI(client: $0) })
        self.notifications = notifications
        // Pantry reads and changes can create notifications, so the badge follows them.
        pantry.onChange = { [notifications] in
            Task { await notifications.refreshUnreadCount() }
        }
        specialties = SpecialtyStore(session: session, api: client.map { SpecialtiesAPI(client: $0) })
        // A recorded batch restocks its pantry item.
        specialties.onBatchRecorded = { [pantry] item, householdID in
            pantry.applyChangedItem(item, householdID: householdID)
        }
        autopilot = AutopilotStore(
            session: session, api: client.map { AutopilotAPI(client: $0) }, prompts: UserDefaultsAutopilotPrompts())
        // Accepting a proposal returns the plan, so the Menu shows it without reloading the plan. The
        // menu reloads too: its proposal card and Autopilot badges changed.
        autopilot.planDidChange = { [plans, menu] plan in
            plans.present(plan)
            if let week = ISOWeek(plan.week) {
                Task { await menu.reloadWeek(week) }
            }
        }
        // Add-on pairings change the plan (an add-on entry) and the week's grocery list, so
        // every result goes back through `PlanStore`, and a new rule refreshes the profile.
        let pairings = PairingsStore(
            session: session, api: client.map { AutopilotAPI(client: $0) }, plans: plans, planner: planner)
        self.pairings = pairings
        pairings.planDidChange = { [plans, menu] plan in
            plans.present(plan)
            if let week = ISOWeek(plan.week) {
                Task { await menu.reloadWeek(week) }
            }
        }
        pairings.profileDidChange = { [autopilot] profile in
            autopilot.present(profile)
        }
        let shopping = ShoppingStore(
            session: session, api: client.map { ShoppingAPI(client: $0) }, checks: groceryChecks,
            // Cart links open the Walmart app when it's installed (a universal link), otherwise Safari.
            openURL: { url in await UIApplication.shared.open(url) })
        self.shopping = shopping
        // Every plan the server returns patches the Menu screen's cards and week counts, and
        // re-matches the Shop tab when it changes what the week's grocery list asks for.
        plans.planDidChange = { [menu, shopping] plan in
            menu.applyPlan(plan)
            Task { await shopping.planDidChange(plan) }
        }
        // A confirmed order records pantry purchases.
        shopping.onPantryChanged = { [pantry, notifications] in
            await pantry.refresh()
            await notifications.refreshUnreadCount()
        }
        events = EventReporter(
            session: session, api: client.map { EventsAPI(client: $0) },
            storage: FileEventQueueStorage.applicationSupport())
        googleSignIn = GoogleOAuthConfiguration(clientID: configuration.googleIOSClientID).map {
            GoogleSignInService(configuration: $0, transport: transport)
        }
    }
}

extension EnvironmentValues {
    @Entry var googleSignIn: GoogleSignInService? = nil
}
