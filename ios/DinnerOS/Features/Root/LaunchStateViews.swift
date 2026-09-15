import SwiftUI

/// Shown while the stored session is restored at launch.
struct RestoringView: View {
    @Environment(\.appConfiguration) private var configuration
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 72

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "fork.knife.circle.fill")
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
            Text(configuration.displayName)
                .font(.largeTitle.bold())
            ProgressView()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading \(configuration.displayName)")
    }
}

/// Shown when the build can't reach an API because none is configured.
struct ConfigurationErrorView: View {
    let message: String

    var body: some View {
        ContentUnavailableView {
            Label("Can't Connect", systemImage: "wrench.and.screwdriver")
        } description: {
            Text(message)
        }
    }
}

#Preview("Restoring") {
    RestoringView()
}

#Preview("Configuration error") {
    ConfigurationErrorView(message: "This build has no valid API server URL.")
}
