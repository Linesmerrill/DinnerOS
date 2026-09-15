import SwiftUI

@main
struct DinnerOSApp: App {
    @State private var dependencies = AppDependencies()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(\.appConfiguration, dependencies.configuration)
                .environment(\.googleSignIn, dependencies.googleSignIn)
                .environment(dependencies.session)
                .environment(dependencies.households)
                .environment(dependencies.recipes)
                .environment(dependencies.plans)
                .environment(dependencies.pantry)
                .environment(dependencies.specialties)
                .environment(dependencies.autopilot)
                .environment(dependencies.events)
                .environment(dependencies.notifications)
                .environment(dependencies.shopping)
                .environment(dependencies.menu)
                .environment(dependencies.planner)
                .onChange(of: scenePhase, initial: true) { _, phase in
                    let events = dependencies.events
                    switch phase {
                    case .active:
                        events.appDidBecomeActive()
                        let notifications = dependencies.notifications
                        Task { await notifications.refreshUnreadCount() }
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
