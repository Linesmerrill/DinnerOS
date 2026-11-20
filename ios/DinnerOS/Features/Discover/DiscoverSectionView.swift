import SwiftUI

/// One row of Shared Recipes in full (See All), as a grid of cards.
struct DiscoverSectionView: View {
    let route: DiscoverSectionRoute

    @Environment(DiscoverStore.self) private var discover
    @Environment(HouseholdStore.self) private var households
    @Environment(\.horizontalSizeClass) private var sizeClass

    @State private var section: DiscoverSection?
    @State private var nextCursor: String?
    @State private var loadError: String?
    @State private var isLoadingMore = false

    private var canAdd: Bool { households.access?.can(.recipesEdit) == true }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Space.l) {
                if let section {
                    if let subtitle = section.subtitle, !subtitle.isEmpty {
                        Text(subtitle)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    if let sponsor = section.sponsorLabel, !sponsor.isEmpty {
                        Text(sponsor)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .accessibilityIdentifier("DiscoverSponsor")
                    }
                    LazyVGrid(columns: DiscoverCard.gridColumns, alignment: .leading, spacing: DS.Space.xl) {
                        ForEach(Array(section.items.enumerated()), id: \.offset) { _, item in
                            DiscoverCard(summary: item, width: nil, canAdd: canAdd, report: { _ in })
                        }
                    }
                    if nextCursor != nil {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                            .task { await loadMore() }
                    }
                } else if let loadError {
                    DiscoverErrorRow(message: loadError) {
                        Task { await load() }
                    }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .padding(.top, DS.Space.huge)
                }
            }
            .padding(.horizontal, sizeClass == .regular ? 28 : 16)
            .padding(.vertical, DS.Space.l)
        }
        .navigationTitle(section?.title ?? route.title)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { if section == nil { await load() } }
    }

    private func load() async {
        loadError = nil
        do {
            let page = try await discover.sectionPage(id: route.id, cursor: nil)
            section = page.section
            nextCursor = page.nextCursor
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func loadMore() async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await discover.sectionPage(id: route.id, cursor: cursor)
            let known = Set(section?.items.map(\.id) ?? [])
            section?.items.append(contentsOf: page.section.items.filter { !known.contains($0.id) })
            nextCursor = page.nextCursor
        } catch {
            // The cards already shown stay; pulling down tries again.
            nextCursor = nil
        }
    }
}
