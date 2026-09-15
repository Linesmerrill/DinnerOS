import SwiftUI

/// The app shell. Each tab is a placeholder until its phase is implemented.
struct RootView: View {
    @State private var selection: AppTab = .week

    var body: some View {
        TabView(selection: $selection) {
            ForEach(AppTab.allCases) { tab in
                Tab(value: tab) {
                    NavigationStack {
                        PlaceholderScreen(tab: tab)
                    }
                } label: {
                    Label(tab.title, systemImage: tab.systemImage)
                }
            }
        }
    }
}

#Preview {
    RootView()
}
