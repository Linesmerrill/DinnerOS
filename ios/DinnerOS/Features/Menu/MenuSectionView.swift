import SwiftUI

/// One of the server's sections: a title, an optional subtitle, a horizontal row of cards, and
/// "Show More" when the section says how to see the rest. Sections are rendered generically, so
/// a new section from the API needs no change here.
struct MenuSectionView: View {
    let section: MenuSection
    /// Past weeks and members without `plan.edit` see cards without the add control.
    var canAdd = true

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var photoURLs: [URL?] { section.items.map(\.recipe.imageURL) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            MenuSectionHeader(title: section.title, subtitle: section.subtitle) {
                if let query = section.moreQuery {
                    NavigationLink(value: AllMealsRoute(title: section.title, query: query)) {
                        Text("Show More")
                            .font(.subheadline.weight(.semibold))
                    }
                    .accessibilityLabel("Show more \(section.title)")
                }
            }
            .padding(.horizontal, 16)

            if section.items.isEmpty {
                Text("Nothing here yet.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                    .padding(.horizontal, 16)
            } else {
                ScrollView(.horizontal) {
                    LazyHStack(alignment: .top, spacing: 12) {
                        ForEach(Array(section.items.enumerated()), id: \.element.id) { index, card in
                            MenuRecipeCard(card: card, canAdd: canAdd)
                                .prefetchesPhotos(
                                    after: index, in: photoURLs,
                                    pointWidth: MenuRecipeCard.carouselWidth(for: dynamicTypeSize))
                        }
                    }
                    .scrollTargetLayout()
                    .padding(.horizontal, 16)
                }
                .scrollIndicators(.hidden)
                .scrollTargetBehavior(.viewAligned)
            }
        }
        .accessibilityElement(children: .contain)
    }
}

#Preview("Sections") {
    NavigationStack {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 28) {
                ForEach(MenuPreviewData.menu.sections) { section in
                    MenuSectionView(section: section)
                }
            }
            .padding(.vertical)
        }
    }
    .menuPreviewEnvironment()
}
