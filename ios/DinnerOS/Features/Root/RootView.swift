import SwiftUI

/// Chooses the top-level screen from the authentication state.
struct RootView: View {
    @Environment(AuthSession.self) private var session
    @Environment(HouseholdStore.self) private var households
    @Environment(RecipeLibrary.self) private var recipes
    @Environment(ImportReviewStore.self) private var importReviews
    @Environment(PlanStore.self) private var plans
    @Environment(PantryStore.self) private var pantry
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(EventReporter.self) private var events
    @Environment(NotificationStore.self) private var notifications
    @Environment(ShoppingStore.self) private var shopping
    @Environment(MenuStore.self) private var menu
    @Environment(MealPlanner.self) private var planner
    @Environment(PairingsStore.self) private var pairings

    var body: some View {
        content
            .animation(.default, value: session.state)
            .task { await session.restore() }
            .task(id: session.currentUser?.id) {
                // Runs at launch, after every sign-in, and after sign-out.
                if session.currentUser != nil {
                    await households.load()
                } else {
                    households.reset()
                    recipes.reset()
                    importReviews.reset()
                    plans.reset()
                    pantry.reset()
                    specialties.reset()
                    autopilot.reset()
                    notifications.reset()
                    shopping.reset()
                    menu.reset()
                    planner.reset()
                    pairings.reset()
                    // At launch there's no user while the session restores; only a real
                    // sign-out discards queued events.
                    if session.state == .signedOut {
                        events.reset()
                    }
                }
            }
            .onChange(of: eventScope, initial: true) { _, scope in
                guard let userID = scope.userID else { return }
                if let householdID = scope.householdID {
                    events.activate(householdID: householdID, userID: userID)
                } else if scope.needsHousehold {
                    events.reset()
                }
            }
            .inviteLinkPrompt()
    }

    /// Events are recorded for the signed-in user in the selected household.
    private var eventScope: EventScope {
        EventScope(
            userID: session.currentUser?.id, householdID: households.current?.household.id,
            needsHousehold: households.phase == .needsHousehold)
    }

    private struct EventScope: Equatable {
        let userID: String?
        let householdID: String?
        let needsHousehold: Bool
    }

    @ViewBuilder
    private var content: some View {
        switch session.state {
        case .restoring:
            RestoringView()
        case .configurationError(let message):
            ConfigurationErrorView(message: message)
        case .signedOut:
            SignInView()
                .transition(.opacity)
        case .signedIn:
            SignedInRootView()
                .transition(.opacity)
        }
    }
}

#Preview("Signed in") {
    let session = HouseholdPreviewData.session()
    RootView()
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(RecipePreviewData.library(session: session))
        .environment(PlanPreviewData.store(session: session))
        .environment(PantryPreviewData.store(session: session))
        .environment(SpecialtyPreviewData.store(session: session))
        .environment(AutopilotPreviewData.store(session: session))
        .environment(EventReporter.preview(session: session))
        .environment(NotificationPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}

#Preview("Signed out") {
    let session = AuthSession.preview(.signedOut)
    RootView()
        .environment(session)
        .environment(HouseholdStore.preview(session: session, phase: .idle))
        .environment(RecipeLibrary.preview(session: session, phase: .idle))
        .environment(PlanStore.preview(session: session, plan: nil, phase: .idle))
        .environment(PantryStore.preview(session: session, phase: .idle))
        .environment(SpecialtyStore.preview(session: session, phase: .idle))
        .environment(AutopilotStore.preview(session: session, profile: nil, vocabulary: nil))
        .environment(EventReporter.preview(session: session))
        .environment(NotificationStore.preview(session: session, phase: .idle))
        .environment(ShopPreviewData.store(session: session, configured: false))
}
