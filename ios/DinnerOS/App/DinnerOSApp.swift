import SwiftUI

@main
struct DinnerOSApp: App {
    /// Owns `AppDependencies`, so the push device token and tapped pushes reach them.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase
    @State private var activation = PlanRefresh.Tracker()

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
                .environment(dependencies.offline)
                .task {
                    // Back online: reload what's on screen so saved answers give way to live ones.
                    let dependencies = dependencies
                    dependencies.offline.onReconnect = {
                        Task {
                            await dependencies.plans.reload()
                            await dependencies.menu.reload()
                            await dependencies.pantry.refresh()
                            await dependencies.thaw.load()
                            await dependencies.shopping.reload()
                            await dependencies.recipes.refresh()
                            await dependencies.prefetch.run()
                        }
                    }
                    dependencies.offline.start()
                }
                .onChange(of: scenePhase, initial: true) { _, phase in
                    let events = dependencies.events
                    switch phase {
                    case .active:
                        events.appDidBecomeActive()
                        // Another member may have moved meals while this device sat in the
                        // background; the plan and menu reload without needing a pull.
                        if activation.phaseChanged(to: phase) {
                            let plans = dependencies.plans
                            let menu = dependencies.menu
                            Task {
                                async let week: Void = plans.reload()
                                await menu.reload()
                                await week
                            }
                        }
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
                        // Save the week ahead for use offline, once the household has loaded.
                        let prefetch = dependencies.prefetch
                        Task {
                            try? await Task.sleep(for: .seconds(4))
                            await prefetch.run()
                        }
                    case .background:
                        _ = activation.phaseChanged(to: phase)
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
                    if case .signedOut = state {
                        dependencies.widgets.clear()
                        // The next account never sees this one's saved answers.
                        dependencies.responseCache.clear()
                    }
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
