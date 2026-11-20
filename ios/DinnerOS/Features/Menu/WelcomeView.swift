import SwiftUI

/// A new member's first look: what DinnerOS does, in three lines, and a few ways to get
/// recipes. Everything here is optional; "Start Planning" just closes it.
struct WelcomeView: View {
    /// Whether the library already has recipes (a new household gets a starter set).
    let hasRecipes: Bool
    let canAddRecipe: Bool
    let choose: (Welcome.Choice) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Space.xxl) {
                VStack(alignment: .leading, spacing: DS.Space.s) {
                    Image(systemName: "fork.knife.circle.fill")
                        .font(DS.Typography.largeTitle)
                        .imageScale(.large)
                        .foregroundStyle(DS.Colors.accent)
                        .accessibilityHidden(true)
                    Text("Welcome to DinnerOS")
                        .font(DS.Typography.largeTitle)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityAddTraits(.isHeader)
                    Text(
                        hasRecipes ? "Your recipe library is ready." : "Let’s get some recipes in."
                    )
                    .font(DS.Typography.subtitle)
                    .foregroundStyle(DS.Colors.secondaryText)
                    .fixedSize(horizontal: false, vertical: true)
                }
                VStack(alignment: .leading, spacing: DS.Space.ml) {
                    point("calendar", "Pick dinners for the week.")
                    point("cart", "Get one grocery list for all of them.")
                    point("frying.pan", "Cook step by step, with timers.")
                }
                VStack(spacing: DS.Space.sm) {
                    option(
                        LocalizedStringKey(SharedRecipes.title), systemImage: SharedRecipes.systemImage,
                        detail: "Add recipes other households share.", choice: .browseShared)
                    if canAddRecipe {
                        option(
                            "Add a Recipe", systemImage: "square.and.pencil",
                            detail: "Write your own or import one from a website.", choice: .addRecipe)
                    }
                }
            }
            .padding(DS.Space.xxl)
            .dsReadableWidth()
        }
        // The footer matches the sheet and spans its full width (build 121).
        .safeAreaInset(edge: .bottom, spacing: 0) {
            DSBottomBar {
                Button {
                    choose(.startPlanning)
                } label: {
                    Text("Start Planning")
                }
                .buttonStyle(.dsPrimary(fullWidth: true))
            }
        }
        .interactiveDismissDisabled(false)
    }

    /// A line with its icon in the shared icon column, so the three lines start together.
    private func point(_ symbol: String, _ text: LocalizedStringKey) -> some View {
        DSIconRow(systemImage: symbol) {
            Text(text).fixedSize(horizontal: false, vertical: true)
        }
        .font(DS.Typography.body)
    }

    private func option(
        _ title: LocalizedStringKey, systemImage: String, detail: LocalizedStringKey, choice: Welcome.Choice
    ) -> some View {
        Button {
            choose(choice)
        } label: {
            HStack(spacing: DS.Space.m) {
                Image(systemName: systemImage)
                    .font(DS.Typography.subtitle)
                    .foregroundStyle(DS.Colors.accent)
                    .dsIconColumn()
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: DS.Space.hairline) {
                    Text(title).font(DS.Typography.headline).foregroundStyle(Color.primary)
                    Text(detail).font(DS.Typography.subheadline).foregroundStyle(DS.Colors.secondaryText)
                }
                .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(DS.Typography.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .padding(DS.Space.ml)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }
}

#Preview("Welcome") {
    WelcomeView(hasRecipes: true, canAddRecipe: true) { _ in }
}

#Preview("Welcome, empty, large text") {
    WelcomeView(hasRecipes: false, canAddRecipe: true) { _ in }
        .environment(\.dynamicTypeSize, .accessibility2)
}
