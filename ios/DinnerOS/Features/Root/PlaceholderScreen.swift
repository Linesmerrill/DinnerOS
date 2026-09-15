import SwiftUI

/// Empty state shown for tabs whose features have not been built yet.
struct PlaceholderScreen: View {
    let tab: AppTab

    @Environment(\.appConfiguration) private var configuration

    var body: some View {
        ContentUnavailableView {
            Label(tab.title, systemImage: tab.systemImage)
        } description: {
            Text("Coming soon to \(configuration.displayName).")
        }
        .navigationTitle(Text(tab.title))
    }
}

#Preview {
    NavigationStack {
        PlaceholderScreen(tab: .pantry)
    }
}
