import SwiftUI

/// A new member's first look: what DinnerOS does, in three lines, and a few ways to get
/// recipes. Everything here is optional; "Start Planning" just closes it.
struct WelcomeView: View {
    /// Whether the library already has recipes (a new household gets a starter set).
    let hasRecipes: Bool
    let canImport: Bool
    let canAddRecipe: Bool
    let serviceName: String
    let choose: (Welcome.Choice) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 8) {
                    Image(systemName: "fork.knife.circle.fill")
                        .font(.system(size: 52))
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text("Welcome to DinnerOS")
                        .font(.largeTitle.bold())
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityAddTraits(.isHeader)
                    Text(
                        hasRecipes ? "Your recipe library is ready." : "Let's get some recipes in."
                    )
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                }
                VStack(alignment: .leading, spacing: 14) {
                    point("calendar", "Pick dinners for the week.")
                    point("cart", "Get one grocery list for all of them.")
                    point("frying.pan", "Cook step by step, with timers.")
                }
                VStack(spacing: 10) {
                    if canImport {
                        option(
                            "Import My \(serviceName) Recipes", systemImage: "square.and.arrow.down",
                            detail: "Bring in meals you've ordered.", choice: .importMealKit)
                    }
                    option(
                        "Browse Shared Recipes", systemImage: "books.vertical",
                        detail: "Add recipes other households share.", choice: .browseShared)
                    if canAddRecipe {
                        option(
                            "Add a Recipe", systemImage: "square.and.pencil",
                            detail: "Paste a link or type one in.", choice: .addRecipe)
                    }
                }
            }
            .padding(24)
        }
        .safeAreaInset(edge: .bottom) {
            Button {
                choose(.startPlanning)
            } label: {
                Text("Start Planning")
                    .font(.headline)
                    .frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(.borderedProminent)
            .padding(.horizontal, 24)
            .padding(.bottom, 12)
            .background(.bar)
        }
        .interactiveDismissDisabled(false)
    }

    private func point(_ symbol: String, _ text: LocalizedStringKey) -> some View {
        Label {
            Text(text).fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: symbol).foregroundStyle(.tint)
        }
        .font(.body)
    }

    private func option(
        _ title: LocalizedStringKey, systemImage: String, detail: LocalizedStringKey, choice: Welcome.Choice
    ) -> some View {
        Button {
            choose(choice)
        } label: {
            HStack(spacing: 14) {
                Image(systemName: systemImage)
                    .font(.title3)
                    .foregroundStyle(.tint)
                    .frame(width: 32)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.headline).foregroundStyle(Color.primary)
                    Text(detail).font(.subheadline).foregroundStyle(.secondary)
                }
                .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 14))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }
}

#Preview("Welcome") {
    WelcomeView(hasRecipes: true, canImport: true, canAddRecipe: true, serviceName: "HelloFresh") { _ in }
}

#Preview("Welcome, empty, large text") {
    WelcomeView(hasRecipes: false, canImport: true, canAddRecipe: true, serviceName: "HelloFresh") { _ in }
        .environment(\.dynamicTypeSize, .accessibility2)
}
