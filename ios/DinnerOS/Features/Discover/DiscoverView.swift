import SwiftUI

/// Shared Recipes: rows of recipes the server picks (Chef's Picks, Quick,
/// Feeds a Crowd, and so on), then Browse All with a search field and filter
/// chips.
///
/// The rows and their words come from the server (`GET .../discover/home`) and
/// are saved like every answer, so they still show offline. Browse All with no
/// search text lists recipes the household doesn't have yet; typing searches
/// everything and marks the ones it already has.
struct DiscoverView: View {
    @Environment(DiscoverStore.self) private var discover
    @Environment(HouseholdStore.self) private var households
    @Environment(\.horizontalSizeClass) private var sizeClass

    @State private var searchText = ""
    /// The recipe whose report reasons are showing.
    @State private var reporting: CatalogSummary?
    @FocusState private var searchFocused: Bool

    private var canAdd: Bool { households.access?.can(.recipesEdit) == true }
    private var margin: CGFloat { sizeClass == .regular ? 28 : 16 }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 32) {
                    if let message = discover.refreshError {
                        DiscoverNotice(message: message)
                            .padding(.horizontal, margin)
                    }
                    rows
                    browseAll
                        .id(Self.browseAnchor)
                }
                .padding(.vertical, 16)
            }
            .onChange(of: searchFocused) { _, focused in
                guard focused else { return }
                withAnimation { proxy.scrollTo(Self.browseAnchor, anchor: .top) }
            }
        }
        .scrollDismissesKeyboard(.immediately)
        .navigationTitle(SharedRecipes.title)
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: DiscoverSectionRoute.self) { route in
            DiscoverSectionView(route: route)
        }
        .refreshable { await discover.refresh() }
        .alert(
            "Couldn't Add That Recipe", isPresented: .constant(discover.addError != nil),
            presenting: discover.addError
        ) { _ in
            Button("OK") { discover.addError = nil }
        } message: { message in
            Text(message)
        }
        .confirmationDialog(
            "Why are you reporting this recipe?", isPresented: Binding(presenting: $reporting),
            titleVisibility: .visible, presenting: reporting
        ) { item in
            ForEach(CatalogReportReason.allCases) { reason in
                Button(reason.title) {
                    Task { await discover.report(id: item.id, reason: reason) }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("You won't see it again. We'll take a look.")
        }
        .alert(
            "Reported", isPresented: Binding(presenting: Bindable(discover).reportMessage),
            presenting: discover.reportMessage
        ) { _ in
            Button("OK") {}
        } message: { message in
            Text(message)
        }
        .alert(
            "Couldn't Report That Recipe", isPresented: Binding(presenting: Bindable(discover).reportError),
            presenting: discover.reportError
        ) { _ in
            Button("OK") {}
        } message: { message in
            Text(message)
        }
        .task(id: households.current?.household.id) {
            guard let id = households.current?.household.id else { return }
            await discover.activate(householdID: id)
        }
        .task(id: searchText) {
            // Debounced: a search per keystroke would be a request per keystroke.
            guard searchText != discover.query.search else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await discover.setSearch(searchText)
        }
    }

    private static let browseAnchor = "browse-all"

    // MARK: - Rows

    @ViewBuilder
    private var rows: some View {
        if let home = discover.home {
            ForEach(Array(home.sections.enumerated()), id: \.element.id) { index, section in
                DiscoverSectionRow(
                    section: section, featured: index == 0, seeAllTitle: home.seeAllTitle, margin: margin,
                    canAdd: canAdd, report: { reporting = $0 })
            }
        } else if discover.isLoadingHome || discover.homeError == nil {
            DiscoverSectionRow(
                section: DiscoverSection(
                    id: "placeholder", title: "Chef's Picks This Week",
                    items: Array(repeating: CatalogPreviewData.placeholder, count: 4)),
                featured: true, seeAllTitle: nil, margin: margin, canAdd: false, report: { _ in }
            )
            .redacted(reason: .placeholder)
            .disabled(true)
            .accessibilityHidden(true)
        }
    }

    // MARK: - Browse All

    private var browseAll: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(discover.home?.browseTitle ?? String(localized: "Browse All"))
                .font(.title2.bold())
                .accessibilityAddTraits(.isHeader)
                .padding(.horizontal, margin)
            searchField
                .padding(.horizontal, margin)
            filterChips
            browseResults
                .padding(.horizontal, margin)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("DiscoverBrowseAll")
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            TextField(
                discover.home?.searchPrompt ?? String(localized: "Search shared recipes"), text: $searchText
            )
            .focused($searchFocused)
            .submitLabel(.search)
            .autocorrectionDisabled()
            .accessibilityIdentifier("DiscoverSearch")
            if !searchText.isEmpty {
                Button {
                    searchText = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 12))
    }

    private var filterChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                // Clear comes first, so a small phone never has it scrolled out of sight.
                if discover.query.hasFilters {
                    Button("Clear") {
                        Task { await discover.clearFilters() }
                    }
                    .font(.subheadline)
                    .padding(.horizontal, 4)
                    .accessibilityIdentifier("DiscoverFilterClear")
                }
                ForEach(discover.filterGroups) { group in
                    DiscoverFilterChip(group: group, selected: discover.query.value(forParam: group.param)) { value in
                        Task { await discover.setFilter(param: group.param, value: value) }
                    }
                }
            }
            .padding(.horizontal, margin)
        }
        .scrollIndicators(.hidden)
    }

    @ViewBuilder
    private var browseResults: some View {
        switch discover.phase {
        case .idle, .loading:
            grid(Array(repeating: CatalogPreviewData.placeholder, count: 4), placeholder: true)
        case .failed(let message):
            DiscoverErrorRow(message: message) {
                Task { await discover.retry() }
            }
        case .loaded where discover.items.isEmpty:
            emptyState
        case .loaded:
            grid(discover.items, placeholder: false)
            if discover.hasMore {
                loadMoreRow
            }
        }
    }

    private func grid(_ items: [CatalogSummary], placeholder: Bool) -> some View {
        LazyVGrid(columns: DiscoverCard.gridColumns, alignment: .leading, spacing: 20) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                DiscoverCard(summary: item, width: nil, canAdd: canAdd && !placeholder, report: { reporting = $0 })
            }
        }
        .redacted(reason: placeholder ? .placeholder : [])
        .disabled(placeholder)
    }

    private var emptyState: some View {
        Group {
            if discover.isSearching || discover.query.hasFilters {
                ContentUnavailableView(
                    discover.home?.noMatchTitle ?? String(localized: "No Recipes Match"),
                    systemImage: "magnifyingglass",
                    description: Text(
                        discover.home?.noMatchText ?? String(localized: "Try another word or fewer filters.")))
            } else {
                ContentUnavailableView(
                    discover.emptyTitle ?? String(localized: "Nothing New Right Now"),
                    systemImage: SharedRecipes.systemImage,
                    description: Text(
                        discover.emptyText
                            ?? String(
                                localized:
                                    "You already have every recipe other households have shared. Check back later.")))
            }
        }
        .frame(maxWidth: .infinity)
    }

    private var loadMoreRow: some View {
        Group {
            if let message = discover.loadMoreError {
                DiscoverErrorRow(message: message) {
                    Task { await discover.loadMore() }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .task { await discover.loadMore() }
            }
        }
    }
}

/// Opens one row of Shared Recipes in full.
struct DiscoverSectionRoute: Hashable {
    let id: String
    let title: String
}

/// One server row: its title, an optional subtitle and sponsor, and a
/// sideways-scrolling line of cards. The first row's cards are larger.
struct DiscoverSectionRow: View {
    let section: DiscoverSection
    let featured: Bool
    let seeAllTitle: String?
    let margin: CGFloat
    let canAdd: Bool
    let report: (CatalogSummary) -> Void

    @Environment(\.horizontalSizeClass) private var sizeClass

    private var cardWidth: CGFloat {
        switch (featured, sizeClass == .regular) {
        case (true, true): 320
        case (true, false): 260
        case (false, true): 220
        case (false, false): 168
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(section.title)
                        .font(featured ? .title2.bold() : .title3.bold())
                        .accessibilityAddTraits(.isHeader)
                    if let subtitle = section.subtitle, !subtitle.isEmpty {
                        Text(subtitle)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    // A paid row always says so, in the server's words.
                    if let sponsor = section.sponsorLabel, !sponsor.isEmpty {
                        Text(sponsor)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Color(.tertiarySystemFill), in: .capsule)
                            .padding(.top, 4)
                            .accessibilityIdentifier("DiscoverSponsor")
                    }
                }
                Spacer(minLength: 8)
                NavigationLink(value: DiscoverSectionRoute(id: section.id, title: section.title)) {
                    Text(seeAllTitle ?? String(localized: "See All"))
                        .font(.subheadline.weight(.semibold))
                }
                .accessibilityLabel("See all \(section.title)")
                .accessibilityIdentifier("DiscoverSeeAll-\(section.id)")
            }
            .padding(.horizontal, margin)

            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 14) {
                    ForEach(Array(section.items.enumerated()), id: \.offset) { _, item in
                        DiscoverCard(summary: item, width: cardWidth, canAdd: canAdd, report: report)
                    }
                }
                .scrollTargetLayout()
            }
            .contentMargins(.horizontal, margin, for: .scrollContent)
            .scrollTargetBehavior(.viewAligned)
            .scrollIndicators(.hidden)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("DiscoverSection-\(section.id)")
    }
}

/// A recipe as a photo card: the photo with a save button on it, the name, and
/// its time and cuisine. Tapping it opens the recipe.
struct DiscoverCard: View {
    let summary: CatalogSummary
    /// `nil` fills a grid column.
    let width: CGFloat?
    let canAdd: Bool
    let report: (CatalogSummary) -> Void

    @Environment(DiscoverStore.self) private var discover

    static let gridColumns = [GridItem(.adaptive(minimum: 150, maximum: 260), spacing: 16, alignment: .top)]

    private var isSaved: Bool { discover.isSaved(summary) }

    var body: some View {
        NavigationLink(value: CatalogRecipeRoute(id: summary.id, name: summary.name)) {
            VStack(alignment: .leading, spacing: 8) {
                RecipeImage(url: summary.imageURL, pointWidth: width ?? 240, title: summary.name)
                    .clipShape(.rect(cornerRadius: 14))
                // The time and cuisine sit right under the name, one line or two; cards in a
                // row line up at the top, and the space a short name leaves goes below.
                VStack(alignment: .leading, spacing: 4) {
                    Text(summary.name)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                    facts
                }
                Spacer(minLength: 0)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .frame(width: width)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Self.accessibilityText(for: summary, saved: isSaved))
        .accessibilityIdentifier("DiscoverCard")
        // On top of the card, not inside its link, so a tap saves rather than opens.
        .overlay(alignment: .topTrailing) {
            saveButton
                .padding(8)
        }
        .contextMenu {
            Button("Report Recipe", systemImage: "flag") { report(summary) }
        }
    }

    private var facts: some View {
        HStack(spacing: 10) {
            if let minutes = summary.displayMinutes {
                Label(RecipeFormat.minutes(minutes), systemImage: "clock")
                    .labelStyle(.titleAndIcon)
            }
            if let cuisine = summary.cuisines.first {
                Text(cuisine.capitalized)
                    .lineLimit(1)
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .imageScale(.small)
    }

    @ViewBuilder
    private var saveButton: some View {
        if isSaved {
            Image(systemName: "checkmark")
                .font(.footnote.weight(.bold))
                .foregroundStyle(.white)
                .frame(width: 32, height: 32)
                .background(Color.green, in: .circle)
                .accessibilityLabel("In your library")
                .accessibilityIdentifier("DiscoverCardSaved")
        } else if canAdd {
            let adding = discover.adding.contains(summary.id)
            Button {
                Task { await discover.add(summary) }
            } label: {
                Group {
                    if adding {
                        ProgressView()
                    } else {
                        Image(systemName: "plus")
                            .font(.footnote.weight(.bold))
                    }
                }
                .frame(width: 32, height: 32)
                .background(.regularMaterial, in: .circle)
            }
            .buttonStyle(.plain)
            .disabled(adding)
            .accessibilityLabel("Add \(summary.name) to your library")
            .accessibilityIdentifier("DiscoverCardSave")
        }
    }

    /// For example "Quick Tacos, 20 min, Mexican".
    static func accessibilityText(for summary: CatalogSummary, saved: Bool) -> String {
        var parts = [summary.name]
        if let minutes = summary.displayMinutes {
            parts.append(RecipeFormat.minutes(minutes))
        }
        if let cuisine = summary.cuisines.first {
            parts.append(cuisine.capitalized)
        }
        if saved {
            parts.append(String(localized: "In your library"))
        }
        return parts.joined(separator: ", ")
    }
}

/// A filter chip: its title until a choice is made, then the choice. It opens
/// a menu of choices with Any at the top.
struct DiscoverFilterChip: View {
    let group: CatalogFilterGroup
    let selected: String?
    let choose: (String?) -> Void

    private var selectedLabel: String? {
        selected.flatMap { value in group.options.first { $0.value == value }?.label }
    }

    var body: some View {
        Menu {
            Button {
                choose(nil)
            } label: {
                if selected == nil {
                    Label("Any", systemImage: "checkmark")
                } else {
                    Text("Any")
                }
            }
            ForEach(group.options) { option in
                Button {
                    choose(option.value)
                } label: {
                    if option.value == selected {
                        Label(option.label, systemImage: "checkmark")
                    } else {
                        Text(option.label)
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Text(selectedLabel ?? group.title)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.bold))
            }
            .font(.subheadline.weight(.medium))
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .foregroundStyle(selected == nil ? Color.primary : Color.white)
            .background(
                selected == nil ? AnyShapeStyle(Color(.secondarySystemBackground)) : AnyShapeStyle(.tint), in: .capsule)
        }
        .accessibilityLabel(selectedLabel.map { "\(group.title): \($0)" } ?? group.title)
        .accessibilityIdentifier("DiscoverFilter-\(group.id)")
    }
}

/// A non-blocking notice, for a refresh that failed.
struct DiscoverNotice: View {
    let message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle")
            .font(.footnote)
            .foregroundStyle(.secondary)
            .listRowSeparator(.hidden)
    }
}

/// A failure with a way out of it.
struct DiscoverErrorRow: View {
    let message: String
    let retry: () -> Void

    var body: some View {
        VStack(spacing: 8) {
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Try Again", action: retry)
                .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
        .listRowSeparator(.hidden)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    let home = DiscoverHome(
        sections: [
            DiscoverSection(
                id: "chefs-picks", title: "Chef's Picks This Week", subtitle: "Our favorites right now",
                items: CatalogPreviewData.summaries),
            DiscoverSection(
                id: "quick", title: "Quick", subtitle: "30 minutes or less", items: CatalogPreviewData.summaries),
        ], filters: DiscoverHome.fallbackFilters)
    NavigationStack {
        DiscoverView()
    }
    .environment(DiscoverStore.preview(session: session, items: CatalogPreviewData.summaries, home: home))
    .environment(HouseholdPreviewData.store(session: session))
}
