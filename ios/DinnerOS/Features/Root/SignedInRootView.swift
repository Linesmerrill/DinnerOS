import SwiftUI

/// Chooses the signed-in screen from the household state: onboarding until the user
/// belongs to a household, then the tab shell.
struct SignedInRootView: View {
    @Environment(HouseholdStore.self) private var households

    var body: some View {
        Group {
            switch households.phase {
            case .idle, .loading:
                HouseholdLoadingView()
            case .needsHousehold:
                HouseholdOnboardingView()
            case .ready:
                MainTabView()
            case .failed(let message):
                HouseholdLoadFailedView(message: message)
            }
        }
        .animation(.default, value: households.phase)
    }
}

private struct HouseholdLoadingView: View {
    var body: some View {
        ProgressView("Loading your household…")
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct HouseholdLoadFailedView: View {
    let message: String

    @Environment(AuthSession.self) private var session
    @Environment(HouseholdStore.self) private var households

    var body: some View {
        ContentUnavailableView {
            Label("Couldn't Load Your Household", systemImage: "exclamationmark.triangle")
        } description: {
            Text(message)
        } actions: {
            Button("Try Again") {
                Task { await households.load() }
            }
            .buttonStyle(.borderedProminent)
            Button("Sign Out", role: .destructive) {
                Task { await session.signOut() }
            }
        }
    }
}

#Preview("Onboarding") {
    let session = HouseholdPreviewData.session()
    SignedInRootView()
        .environment(session)
        .environment(HouseholdStore.preview(session: session, phase: .needsHousehold))
}

#Preview("Failed") {
    let session = HouseholdPreviewData.session()
    SignedInRootView()
        .environment(session)
        .environment(
            HouseholdStore.preview(session: session, phase: .failed("You appear to be offline.")))
}
