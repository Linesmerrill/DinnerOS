import SwiftUI

/// The signed-in tab shell. Shop is a placeholder until its phase is implemented.
struct MainTabView: View {
    @Environment(HouseholdStore.self) private var households
    @State private var selection: AppTab = .recipes

    var body: some View {
        TabView(selection: $selection) {
            ForEach(AppTab.allCases) { tab in
                Tab(value: tab) {
                    switch tab {
                    case .recipes:
                        NavigationStack {
                            RecipesView()
                        }
                        // Another household starts at its own list, not at a recipe
                        // (or search) from the previous one.
                        .id(households.current?.household.id)
                    case .week:
                        NavigationStack {
                            WeekView()
                        }
                        .id(households.current?.household.id)
                    case .household:
                        NavigationStack {
                            HouseholdView()
                        }
                    default:
                        NavigationStack {
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
