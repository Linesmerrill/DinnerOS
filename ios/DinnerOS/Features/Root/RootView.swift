import SwiftUI

/// Chooses the top-level screen from the authentication state.
struct RootView: View {
    @Environment(AuthSession.self) private var session

    var body: some View {
        content
            .animation(.default, value: session.state)
            .task { await session.restore() }
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
            MainTabView()
                .transition(.opacity)
        }
    }
}

#Preview("Signed in") {
    RootView()
        .environment(
            AuthSession.preview(
                .signedIn(UserSummary(id: "preview", displayName: "Ada", primaryEmail: nil, createdAt: .now))))
}

#Preview("Signed out") {
    RootView()
        .environment(AuthSession.preview(.signedOut))
}
