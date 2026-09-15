import SwiftUI

/// The signed-in tab shell. Tabs other than Household are placeholders until their
/// phase is implemented.
struct MainTabView: View {
    @State private var selection: AppTab = .week

    var body: some View {
        TabView(selection: $selection) {
            ForEach(AppTab.allCases) { tab in
                Tab(value: tab) {
                    NavigationStack {
                        switch tab {
                        case .household:
                            HouseholdView()
                        default:
                            PlaceholderScreen(tab: tab)
                        }
                    }
                } label: {
                    Label(tab.title, systemImage: tab.systemImage)
                }
            }
        }
    }
}
