import SwiftUI

/// Chooses the top-level screen from the authentication state.
struct RootView: View {
    @Environment(AuthSession.self) private var session
    @Environment(HouseholdStore.self) private var households

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
                }
            }
            .inviteLinkPrompt()
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
}

#Preview("Signed out") {
    let session = AuthSession.preview(.signedOut)
    RootView()
        .environment(session)
        .environment(HouseholdStore.preview(session: session, phase: .idle))
}
