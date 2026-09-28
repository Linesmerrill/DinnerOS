import SwiftUI

@main
struct DinnerOSApp: App {
    /// Owns `AppDependencies`, so the push device token and tapped pushes reach them.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase

    private var dependencies: AppDependencies { appDelegate.dependencies }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(\.appConfiguration, dependencies.configuration)
                .environment(\.googleSignIn, dependencies.googleSignIn)
                .environment(dependencies.session)
                .environment(dependencies.households)
                .environment(dependencies.recipes)
                .environment(dependencies.importReviews)
                .environment(dependencies.mealKitImport)
                .environment(dependencies.discover)
                .environment(dependencies.plans)
                .environment(dependencies.pantry)
                .environment(dependencies.thaw)
                .environment(dependencies.specialties)
                .environment(dependencies.grocerySkips)
                .environment(dependencies.autopilot)
                .environment(dependencies.events)
                .environment(dependencies.notifications)
                .environment(dependencies.push)
                .environment(dependencies.shopping)
                .environment(dependencies.menu)
                .environment(dependencies.planner)
                .environment(dependencies.pairings)
                .environment(dependencies.mealSwaps)
                .environment(dependencies.deviceContext)
                .environment(dependencies.intentRouter)
                .onChange(of: scenePhase, initial: true) { _, phase in
                    let events = dependencies.events
                    switch phase {
                    case .active:
                        events.appDidBecomeActive()
                        let notifications = dependencies.notifications
                        Task { await notifications.refreshUnreadCount() }
                        // Notices permission changed in Settings, and re-registers so a
                        // rotated device token reaches the API.
                        let push = dependencies.push
                        Task { await push.refresh() }
                        // Back from Walmart: "Did you order these?"
                        let shopping = dependencies.shopping
                        Task { await shopping.appDidBecomeActive() }
                        // A new day, or someone else's change: keep the widgets current.
                        dependencies.widgets.planChanged()
                    case .background:
                        BackgroundActivity.run(named: "Send events") {
                            await events.appDidEnterBackground()
                        }
                    default:
                        break
                    }
                }
                // Never log these URLs: invitation links carry a secret token.
                .onOpenURL { url in
                    // A widget: open the meal, Autopilot, or Shop.
                    if let link = DinnerWidgetLink(url: url) {
                        dependencies.intentRouter.widgetLink = link
                        return
                    }
                    // Custom-scheme links (dinneros://invite?token=...).
                    dependencies.households.handleOpenURL(url)
                }
                // Signed out: the widgets stop showing this household's dinners.
                .onChange(of: dependencies.session.state) { _, state in
                    if case .signedOut = state { dependencies.widgets.clear() }
                }
                .onContinueUserActivity(NSUserActivityTypeBrowsingWeb) { activity in
                    // Universal links (https://api.tlps.dev/invite#token=...).
                    if let url = activity.webpageURL {
                        dependencies.households.handleOpenURL(url)
                    }
                }
        }
    }
}
