import SwiftUI

/// One name and icon for recipes other households shared, so every way in reads the same.
nonisolated enum SharedRecipes {
    static let title = String(localized: "Shared Recipes")
    static let subtitle = String(localized: "Recipes other households shared")
    static let systemImage = "books.vertical"
}

/// The Menu's always-there way into Shared Recipes: a labeled row under the week's meals.
struct SharedRecipesEntry: View {
    var body: some View {
        NavigationLink(value: DiscoverRoute()) {
            HStack(spacing: 12) {
                Image(systemName: SharedRecipes.systemImage)
                    .font(.title3)
                    .foregroundStyle(.tint)
                    .frame(minWidth: 28)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(SharedRecipes.title)
                        .font(.headline)
                        .foregroundStyle(Color.primary)
                    Text(SharedRecipes.subtitle)
                        .font(.subheadline)
                        .foregroundStyle(Color.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.secondary)
                    .accessibilityHidden(true)
            }
            .padding(16)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
        .padding(.horizontal, 16)
    }
}
