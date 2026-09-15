import SwiftUI

/// The bar above the tab bar: how many meals the week has, a shortcut to the Shop tab, and the
/// grocery list.
struct MenuBottomBar: View {
    @Environment(PlanStore.self) private var plans
    @Environment(MenuStore.self) private var menu
    @Environment(HouseholdStore.self) private var households
    @Environment(\.openShop) private var openShop
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var count: Int { plans.plan?.entries.count ?? 0 }

    private var canShop: Bool {
        openShop != nil && households.access?.can(.shoppingEdit) == true
    }

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 2) {
                Text(MenuFormat.bottomBarTitle(count: count, timing: menu.selectedTiming))
                    .font(.headline)
                Text(plans.week.rangeLabel())
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
            }
            .accessibilityElement(children: .combine)
            if !dynamicTypeSize.isAccessibilitySize {
                Spacer(minLength: 8)
            }
            HStack(spacing: 8) {
                if canShop {
                    Button {
                        openShop?(plans.week)
                    } label: {
                        Image(systemName: "cart")
                            .frame(minWidth: 30, minHeight: 30)
                    }
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Shop this week")
                }
                NavigationLink(value: GroceryListRoute(week: plans.week)) {
                    Text("Grocery List")
                        .fontWeight(.semibold)
                }
                .buttonStyle(.borderedProminent)
                .disabled(plans.plan == nil)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bar)
        .overlay(alignment: .top) { Divider() }
    }
}

#Preview("Bottom bar") {
    NavigationStack {
        Color(.systemGroupedBackground)
            .safeAreaInset(edge: .bottom, spacing: 0) { MenuBottomBar() }
    }
    .menuPreviewEnvironment()
}
