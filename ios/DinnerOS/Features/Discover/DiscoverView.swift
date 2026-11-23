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
    private var margin: CGFloat { DiscoverLayout.margin(isWide: sizeClass == .regular) }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: DS.Space.xxxl) {
                    if let message = discover.refreshError {
                        DiscoverNotice(message: message)
                            .padding(.horizontal, margin)
                    }
                    rows
                    browseAll
                        .id(Self.browseAnchor)
                }
                .padding(.vertical, DS.Space.l)
            }
            // The fade into the solid band under the tab bar, as on the tab roots.
            .tabBarEdge()
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
                .tabBarBacking(.top)
        }
        .refreshable { await discover.refresh() }
        .alert(
            "Couldn’t Add That Recipe", isPresented: .constant(discover.addError != nil),
            presenting: discover.addError
        ) { _ in
            Button("OK") { discover.addError = nil }
        } message: { message in
            Text(message)
        }
        .confirmationDialog(
            "Why Are You Reporting This Recipe?", isPresented: Binding(presenting: $reporting),
            titleVisibility: .visible, presenting: reporting
        ) { item in
            ForEach(CatalogReportReason.allCases) { reason in
                Button(reason.title) {
                    Task { await discover.report(id: item.id, reason: reason) }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("You won’t see it again. We’ll take a look.")
        }
        .alert(
            discover.reportTitle, isPresented: Binding(presenting: Bindable(discover).reportMessage),
            presenting: discover.reportMessage
        ) { _ in
            Button("OK") {}
        } message: { message in
            Text(message)
        }
        .alert(
            discover.reportErrorTitle, isPresented: Binding(presenting: Bindable(discover).reportError),
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
                    id: "placeholder", title: "Chef’s Picks This Week",
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
        VStack(alignment: .leading, spacing: DS.Space.ml) {
            Text(discover.home?.browseTitle ?? String(localized: "Browse All"))
                .font(DS.Typography.sectionTitle)
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
        HStack(spacing: DS.Space.s) {
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
                .accessibilityLabel("Clear Search")
            }
        }
        .padding(.horizontal, DS.Space.m)
        .padding(.vertical, DS.Space.sm)
        .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.medium))
    }

    private var filterChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: DS.Space.s) {
                // Clear comes first, so a small phone never has it scrolled out of sight.
                if discover.query.hasFilters {
                    Button("Clear") {
                        Task { await discover.clearFilters() }
                    }
                    .font(.subheadline)
                    .padding(.horizontal, DS.Space.xxs)
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
        .carouselInSafeArea()
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
        LazyVGrid(columns: DiscoverCard.gridColumns, alignment: .leading, spacing: DS.Space.xl) {
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
                                    "You have every recipe other households shared. Check back later.")))
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

/// Shared Recipes' sizes, kept in one place so every row and grid agrees.
enum DiscoverLayout {
    /// The screen margin: wider on an iPad.
    static func margin(isWide: Bool) -> CGFloat { isWide ? DS.Space.xxl : DS.Space.l }
    /// One card width for every row, so no row's cards look smaller than another's.
    static func cardWidth(isWide: Bool) -> CGFloat { isWide ? 240 : 180 }
    /// The gap between cards in a row.
    static let rowSpacing: CGFloat = DS.Space.ml

    /// A row's card width once the row's width is known: about `cardWidth(isWide:)`, adjusted
    /// so a whole number of cards fills the space between the margins. At rest a row never
    /// shows a card cut mid-title at the content's edge (Hark's review item 16: the Duo's cover
    /// screen, whose content stops short of the screen's edge). Never narrower than
    /// `gridMinimum`. `rowWidth` of zero (not measured yet) gives the plain width.
    static func fittedCardWidth(rowWidth: CGFloat, isWide: Bool) -> CGFloat {
        let base = cardWidth(isWide: isWide)
        let available = rowWidth - margin(isWide: isWide) * 2
        guard available > 0 else { return base }
        var count = max(1, ((available + rowSpacing) / (base + rowSpacing)).rounded())
        func width(_ count: CGFloat) -> CGFloat {
            ((available - rowSpacing * (count - 1)) / count).rounded(.down)
        }
        while count > 1, width(count) < gridMinimum { count -= 1 }
        return width(count)
    }

    /// How wide a row's title line is, so See All sits over the row's last card: the cards' own
    /// width when they don't fill the row, `nil` (the whole row) when they do or before the row
    /// is measured. Three recipes on an iPad in landscape left See All alone at the far side,
    /// over 40% of empty screen (Hark's build 122 round 5, item 13).
    static func headerWidth(rowWidth: CGFloat, cardWidth: CGFloat, count: Int, isWide: Bool) -> CGFloat? {
        let available = rowWidth - margin(isWide: isWide) * 2
        guard available > 0, count > 0 else { return nil }
        let cards = CGFloat(count) * cardWidth + CGFloat(count - 1) * rowSpacing
        return cards < available ? cards : nil
    }

    /// The grid's card widths (Browse All, See All).
    static let gridMinimum: CGFloat = 150
    static let gridMaximum: CGFloat = 260
    /// A card's photo is 4:3.
    static func photoHeight(forWidth width: CGFloat) -> CGFloat { (width * 3 / 4).rounded() }
    /// The round save button on a card's photo.
    static let saveButtonSize: CGFloat = 32
}

/// Opens one row of Shared Recipes in full.
struct DiscoverSectionRoute: Hashable {
    let id: String
    let title: String
}

/// One server row: its title, an optional subtitle and sponsor, and a
/// sideways-scrolling line of cards, all one size.
struct DiscoverSectionRow: View {
    let section: DiscoverSection
    let featured: Bool
    let seeAllTitle: String?
    let margin: CGFloat
    let canAdd: Bool
    let report: (CatalogSummary) -> Void

    @Environment(\.horizontalSizeClass) private var sizeClass
    /// The row's width, measured on its scroll view (which doesn't depend on the cards in it).
    @State private var rowWidth: CGFloat = 0

    /// Every row's cards are the same size: a bigger first row made Top Rated look smaller than
    /// Chef's Picks. Whole cards fill the row (`DiscoverLayout.fittedCardWidth`).
    private var cardWidth: CGFloat {
        DiscoverLayout.fittedCardWidth(rowWidth: rowWidth, isWide: sizeClass == .regular)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            HStack(alignment: .firstTextBaseline, spacing: DS.Space.m) {
                VStack(alignment: .leading, spacing: DS.Space.hairline) {
                    Text(section.title)
                        .font(DS.Typography.sectionTitle)
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
                            .padding(.horizontal, DS.Space.s)
                            .padding(.vertical, DS.Space.hairline)
                            .background(DS.Colors.subtleFill, in: .capsule)
                            .padding(.top, DS.Space.xxs)
                            .accessibilityIdentifier("DiscoverSponsor")
                    }
                }
                Spacer(minLength: DS.Space.s)
                NavigationLink(value: DiscoverSectionRoute(id: section.id, title: section.title)) {
                    Text(seeAllTitle ?? String(localized: "See All"))
                        .font(.subheadline.weight(.semibold))
                }
                .accessibilityLabel("See All \(section.title)")
                .accessibilityIdentifier("DiscoverSeeAll-\(section.id)")
            }
            .frame(
                width: DiscoverLayout.headerWidth(
                    rowWidth: rowWidth, cardWidth: cardWidth, count: section.items.count,
                    isWide: sizeClass == .regular),
                alignment: .leading
            )
            .padding(.horizontal, margin)

            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: DiscoverLayout.rowSpacing) {
                    ForEach(Array(section.items.enumerated()), id: \.offset) { _, item in
                        DiscoverCard(summary: item, width: cardWidth, canAdd: canAdd, report: report)
                    }
                }
                .scrollTargetLayout()
            }
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                rowWidth = $0
            }
            .contentMargins(.horizontal, margin, for: .scrollContent)
            .scrollTargetBehavior(.viewAligned)
            .scrollIndicators(.hidden)
            // Inside the safe area with both margins empty: whole cards fill the row between
            // them, so no sliver of the next card shows at the edge (Hark's review item 16).
            .mask {
                Rectangle()
                    .padding(.horizontal, margin)
                    .padding(.vertical, -DS.Space.huge)
            }
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

    static let gridColumns = [
        GridItem(
            .adaptive(minimum: DiscoverLayout.gridMinimum, maximum: DiscoverLayout.gridMaximum), spacing: DS.Space.l,
            alignment: .top)
    ]

    private var isSaved: Bool { discover.isSaved(summary) }

    var body: some View {
        NavigationLink(value: CatalogRecipeRoute(id: summary.id, name: summary.name)) {
            VStack(alignment: .leading, spacing: DS.Space.s) {
                RecipeImage(url: summary.imageURL, pointWidth: width ?? 240, title: summary.name)
                    // A fixed photo size in a row: a fitted photo shrank to make room for a
                    // two-line name, so cards in one row came out different sizes.
                    .frame(width: width, height: width.map { DiscoverLayout.photoHeight(forWidth: $0) })
                    .clipShape(.rect(cornerRadius: DS.Radius.large))
                // The time and cuisine sit right under the name, one line or two; cards in a
                // row line up at the top, and the space a short name leaves goes below.
                VStack(alignment: .leading, spacing: DS.Space.xxs) {
                    Text(summary.name)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)
                        // A row's cards keep two lines for the name, so every card in the row is
                        // one height: the lazy row sizes itself from its first card, and a
                        // two-line name under a one-line first card lost its time and cuisine.
                        .lineLimit(2, reservesSpace: width != nil)
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
        // The card reads as one element, so the photo's own marker is folded into it.
        .qaMarker(QAMarkers.photoPlaceholder, label: summary.name, isShown: summary.imageURL == nil)
        // On top of the card, not inside its link, so a tap saves rather than opens.
        .overlay(alignment: .topTrailing) {
            saveButton
                .padding(DS.Space.s)
        }
        .contextMenu {
            Button("Report Recipe", systemImage: "flag") { report(summary) }
        }
    }

    private var facts: some View {
        HStack(spacing: DS.Space.sm) {
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
                .foregroundStyle(DS.Colors.onAccent)
                .frame(width: DiscoverLayout.saveButtonSize, height: DiscoverLayout.saveButtonSize)
                .background(DS.Colors.success, in: .circle)
                .accessibilityLabel("In Your Library")
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
                .frame(width: DiscoverLayout.saveButtonSize, height: DiscoverLayout.saveButtonSize)
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
            parts.append(String(localized: "In Your Library"))
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
            HStack(spacing: DS.Space.xxs) {
                Text(selectedLabel ?? group.title)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.bold))
            }
            .font(.subheadline.weight(.medium))
            .padding(.horizontal, DS.Space.ml)
            .padding(.vertical, DS.Space.s)
            .foregroundStyle(selected == nil ? Color.primary : DS.Colors.onAccent)
            .background(
                selected == nil ? AnyShapeStyle(DS.Colors.surface) : AnyShapeStyle(DS.Colors.accent), in: .capsule)
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
        VStack(spacing: DS.Space.s) {
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Try Again", action: retry)
                .buttonStyle(.dsSecondary(.small))
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, DS.Space.m)
        .listRowSeparator(.hidden)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    let home = DiscoverHome(
        sections: [
            DiscoverSection(
                id: "chefs-picks", title: "Chef’s Picks This Week", subtitle: "Our favorites right now",
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
