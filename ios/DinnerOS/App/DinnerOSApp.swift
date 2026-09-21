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
                .environment(dependencies.plans)
                .environment(dependencies.pantry)
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
                    // Custom-scheme links (dinneros://invite?token=...).
                    dependencies.households.handleOpenURL(url)
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
