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
    /// `nil` when the build has no Google client ID; the Google button is then hidden.
    let googleSignIn: GoogleSignInService?

    init(configuration: AppConfiguration = .main) {
        self.configuration = configuration
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
        plans = PlanStore(session: session, api: client.map { PlansAPI(client: $0) }, checks: groceryChecks)
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
        // Accepting a proposal returns the plan, so the Week tab shows it without reloading.
        autopilot.planDidChange = { [plans] plan in plans.present(plan) }
        shopping = ShoppingStore(
            session: session, api: client.map { ShoppingAPI(client: $0) }, checks: groceryChecks,
            // Cart links open the Walmart app when it's installed (a universal link), otherwise Safari.
            openURL: { url in await UIApplication.shared.open(url) })
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
