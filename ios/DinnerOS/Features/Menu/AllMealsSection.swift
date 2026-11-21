import SwiftUI

/// Every recipe in the household, under a sticky row of chips: search, filters, sort, protein,
/// time, and cuisine. Pages by cursor as the list scrolls.
struct AllMealsSection: View {
    let list: MenuRecipeList
    var title: String? = String(localized: "All Meals")
    var canAdd = true
    /// The width of the scroll view the section sits in, from a parent that lives as long as the
    /// screen (the Menu). `nil` where the section isn't inside a lazy stack and can measure itself.
    var containerWidth: CGFloat?
    /// Whether the keyboard is up, from the screen that owns the scroll view. With the search
    /// open, results then show as short rows that fit above it (`MenuRecipeCard.Size.searchRow`).
    var isKeyboardUp = false
    /// Told when the search field opens or closes.
    var onSearchChange: (Bool) -> Void = { _ in }

    @Environment(MenuStore.self) private var menu
    @Environment(\.horizontalSizeClass) private var sizeClass

    @State private var isSearching = false
    @State private var searchText = ""
    /// The width the cards share, as measured here, for the iPad's rows of cards
    /// (`AllMealsLayout`). Only used without a `containerWidth`.
    @State private var measuredWidth: CGFloat = 0

    /// Long enough to skip requests while typing, short enough to feel live.
    private static let searchDebounce = Duration.milliseconds(350)

    private var options: MenuFilterOptions {
        menu.filterOptions ?? .fallback
    }

    private var photoURLs: [URL?] { list.items.map(\.recipe.imageURL) }

    var body: some View {
        LazyVStack(alignment: .leading, spacing: DS.Space.xl, pinnedViews: [.sectionHeaders]) {
            Section {
                content
                    .padding(.horizontal, DS.Space.l)
            } header: {
                VStack(alignment: .leading, spacing: DS.Space.s) {
                    // The title steps aside while the search field is open, so a result clears
                    // the tab bar after Done on a small phone and shows more of itself above the
                    // keyboard (Hark's review items 4 and 10). Driven by the search chip's tap
                    // alone: a header that followed the field's keyboard focus froze the Menu on
                    // an iPad, because the lazy stack rebuilds this pinned header as it scrolls
                    // and each rebuild changed the section's state again (QA, build 121).
                    if AllMealsLayout.showsTitle(title, isSearching: isSearching), let title {
                        Text(title)
                            .font(.title3.bold())
                            .accessibilityAddTraits(.isHeader)
                            .padding(.horizontal, DS.Space.l)
                    }
                    MenuChipBar(
                        list: list, options: options, isSearching: $isSearching, searchText: $searchText)
                }
                .padding(.vertical, DS.Space.s)
                .background(.bar)
                .overlay(alignment: .bottom) { Divider() }
            }
        }
        .task(id: searchText) {
            guard searchText != list.query.search else { return }
            // A newer keystroke cancels this task during the sleep.
            do {
                try await Task.sleep(for: Self.searchDebounce)
            } catch {
                return
            }
            var query = list.query
            query.search = searchText
            await list.setQuery(query)
        }
        .task(id: list.week) {
            await list.load()
        }
        .onChange(of: isSearching) { _, searching in
            onSearchChange(searching)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch list.phase {
        case .idle, .loading:
            VStack(spacing: DS.Space.xl) {
                ForEach(0..<3, id: \.self) { _ in
                    VStack(alignment: .leading, spacing: DS.Space.s) {
                        RecipePhoto(url: nil, aspectRatio: 16.0 / 10.0, pointWidth: 400)
                        RoundedRectangle(cornerRadius: DS.Radius.small).fill(.quaternary).frame(height: 14)
                    }
                }
            }
            .redacted(reason: .placeholder)
            .accessibilityHidden(true)
        case .failed(let message):
            MenuSectionError(message: message) {
                Task { await list.retry() }
            }
        case .loaded:
            if list.items.isEmpty {
                emptyState
            } else {
                // Read outside the row builder, which can run on SwiftUI's render thread: the
                // `@Sendable` row closure isn't tied to the main actor and only makes the row
                // (decision 701).
                let urls = photoURLs
                let canAdd = canAdd
                let width = MenuRecipeCard.fullWidthEstimate
                // Lazy all the way down: All Meals can hold hundreds of cards. One column on a
                // phone; on an iPad rows of cards, so one search result stays card-sized instead
                // of filling the screen. Rows of this lazy stack, not a LazyVGrid nested in it:
                // the nested grid kept re-measuring cards of different heights while the Menu
                // scrolled, and on an iPad in landscape the main thread never got free (QA, build
                // 121).
                LazyVStack(alignment: .leading, spacing: DS.Space.xxl) {
                    if let refreshError = list.refreshError {
                        FormErrorLabel(message: refreshError)
                    }
                    if AllMealsLayout.showsSearchRows(isSearching: isSearching, isKeyboardUp: isKeyboardUp) {
                        // Typing a search: a whole card is taller than the room between the
                        // chips and the keyboard on most screens, so each result is one short
                        // row with its name, time and Add in view (Hark's review item 7).
                        ForEach(Array(list.items.enumerated()), id: \.element.id) { @Sendable index, card in
                            MenuCardRow(
                                index: index, card: card, size: .searchRow, canAdd: canAdd, photoURLs: urls,
                                photoWidth: DS.Size.searchThumbnail)
                        }
                    } else if sizeClass == .regular {
                        let cardsWidth = AllMealsLayout.cardsWidth(
                            containerWidth: containerWidth, measured: measuredWidth)
                        let columns = AllMealsLayout.columnCount(width: cardsWidth)
                        let cardWidth = AllMealsLayout.cardWidth(width: cardsWidth, columns: columns)
                        ForEach(AllMealsLayout.rows(list.items, columns: columns), id: \.id) { @Sendable row in
                            MenuCardGridRow(
                                row: row, cardWidth: cardWidth, canAdd: canAdd, photoURLs: urls, photoWidth: width)
                        }
                    } else {
                        ForEach(Array(list.items.enumerated()), id: \.element.id) { @Sendable index, card in
                            MenuCardRow(
                                index: index, card: card, size: .fullWidth, canAdd: canAdd, photoURLs: urls,
                                photoWidth: width)
                        }
                    }
                    pageFooter
                }
                .onGeometryChange(for: CGFloat.self) {
                    $0.size.width
                } action: {
                    measuredWidth = $0
                }
            }
        }
    }

    @ViewBuilder
    private var pageFooter: some View {
        if let loadMoreError = list.loadMoreError {
            MenuSectionError(message: loadMoreError) {
                Task { await list.retry() }
            }
        } else if list.hasMore {
            ProgressView()
                .frame(maxWidth: .infinity)
                // Re-runs for each new cursor while the footer stays on screen.
                .task(id: list.nextCursor) {
                    await list.loadMore()
                }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let search = list.query.normalizedSearch
        if !search.isEmpty {
            ContentUnavailableView.search(text: search)
        } else if list.query.isNarrowed {
            ContentUnavailableView {
                Label("No Meals Match", systemImage: "line.3.horizontal.decrease.circle")
            } description: {
                Text("No recipes in this household match these filters.")
            } actions: {
                Button("Clear Filters") {
                    Task { await list.setQuery(MenuRecipeQuery(sort: list.query.sort)) }
                }
            }
        } else {
            // No Shared Recipes button here: the Menu already offers it above (`MenuEntryPoints`).
            ContentUnavailableView {
                Label("No Recipes Yet", systemImage: "book.closed")
            } description: {
                Text("Recipes you add show up here.")
            }
        }
    }
}

/// How All Meals lays out its cards.
nonisolated enum AllMealsLayout {
    /// The narrowest a card gets in an iPad row.
    static let minimumCardWidth: CGFloat = 280
    /// The widest a card gets in an iPad row, so one result is card-sized, not screen-sized.
    static let maximumCardWidth: CGFloat = 400
    /// The gap between cards in a row.
    static let columnSpacing: CGFloat = 16

    /// One row of cards, with each card's place in the whole list (for warming photos).
    struct Row: Sendable {
        let id: String
        let cards: [(index: Int, card: MenuCard)]
    }

    /// The section's side margins (the cards sit inside them).
    static let horizontalMargin: CGFloat = 16

    /// The width the cards share. From the Menu's own width when it's known: a width measured
    /// inside the section starts at zero every time the Menu's lazy stack makes the section
    /// again, which laid the cards out one to a row (three times as tall), then three to a row,
    /// and the jump in height moved the page and made the section again. On an iPad in
    /// landscape the Menu never stopped laying itself out (QA, build 121).
    static func cardsWidth(containerWidth: CGFloat?, measured: CGFloat) -> CGFloat {
        guard let containerWidth, containerWidth > 0 else { return measured }
        return max(0, containerWidth - horizontalMargin * 2)
    }

    /// Whether results show as short rows: only while the search is open and the keyboard up.
    static func showsSearchRows(isSearching: Bool, isKeyboardUp: Bool) -> Bool {
        isSearching && isKeyboardUp
    }

    /// Whether the section shows its title: not while the search field is open.
    static func showsTitle(_ title: String?, isSearching: Bool) -> Bool {
        title != nil && !isSearching
    }

    /// How many cards fit across `width`, each at least `minimumCardWidth`. At least one, and one
    /// before the width is measured.
    static func columnCount(width: CGFloat) -> Int {
        guard width > 0 else { return 1 }
        return max(1, Int(((width + columnSpacing) / (minimumCardWidth + columnSpacing)).rounded(.down)))
    }

    /// Each card's width: an equal share of the row, never wider than `maximumCardWidth`.
    static func cardWidth(width: CGFloat, columns: Int) -> CGFloat {
        guard width > 0, columns > 0 else { return maximumCardWidth }
        let share = (width - columnSpacing * CGFloat(columns - 1)) / CGFloat(columns)
        return min(maximumCardWidth, share.rounded(.down))
    }

    /// `items` in rows of `columns`, in order.
    static func rows(_ items: [MenuCard], columns: Int) -> [Row] {
        let size = max(1, columns)
        let indexed = Array(items.enumerated())
        return stride(from: 0, to: indexed.count, by: size).map { start in
            let slice = indexed[start..<min(start + size, indexed.count)]
            return Row(id: slice.first?.element.id ?? "\(start)", cards: slice.map { ($0.offset, $0.element) })
        }
    }
}

/// One row of All Meals' cards on an iPad, built off the main actor like `MenuCardRow`
/// (decision 701).
struct MenuCardGridRow: View {
    let row: AllMealsLayout.Row
    let cardWidth: CGFloat
    let canAdd: Bool
    let photoURLs: [URL?]
    let photoWidth: CGFloat

    nonisolated init(row: AllMealsLayout.Row, cardWidth: CGFloat, canAdd: Bool, photoURLs: [URL?], photoWidth: CGFloat)
    {
        self.row = row
        self.cardWidth = cardWidth
        self.canAdd = canAdd
        self.photoURLs = photoURLs
        self.photoWidth = photoWidth
    }

    var body: some View {
        HStack(alignment: .top, spacing: AllMealsLayout.columnSpacing) {
            ForEach(row.cards, id: \.card.id) { item in
                MenuCardRow(
                    index: item.index, card: item.card, size: .fullWidth, canAdd: canAdd, photoURLs: photoURLs,
                    photoWidth: photoWidth
                )
                .frame(width: cardWidth)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The sticky chips above All Meals.
struct MenuChipBar: View {
    let list: MenuRecipeList
    let options: MenuFilterOptions
    @Binding var isSearching: Bool
    @Binding var searchText: String
    @State private var isShowingFilters = false
    @FocusState private var isSearchFocused: Bool

    var body: some View {
        VStack(spacing: DS.Space.s) {
            chips
            if isSearching {
                HStack(spacing: DS.Space.s) {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(Color.secondary)
                    TextField("Search meals", text: $searchText)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.search)
                        .focused($isSearchFocused)
                    if !searchText.isEmpty {
                        Button("Clear", systemImage: "xmark.circle.fill") {
                            searchText = ""
                        }
                        .labelStyle(.iconOnly)
                        .foregroundStyle(Color.secondary)
                    }
                }
                .padding(.horizontal, DS.Space.m)
                .padding(.vertical, DS.Space.s)
                .background(DS.Colors.surface, in: .capsule)
                .padding(.horizontal, DS.Space.l)
            }
        }
        .dsSheet(isPresented: $isShowingFilters) {
            MenuFilterSheet(query: list.query, options: options) { query in
                Task { await list.setQuery(query) }
            }
        }
    }

    private var chips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: DS.Space.s) {
                searchChip
                filterChip
                sortMenu
                valueMenu(
                    title: String(localized: "Main Protein"), options: options.proteins,
                    selection: list.query.protein
                ) { value in
                    apply { $0.protein = value }
                }
                timeMenu
                valueMenu(
                    title: String(localized: "Cuisine"), options: options.cuisines, selection: list.query.cuisine
                ) { value in
                    apply { $0.cuisine = value }
                }
            }
            .padding(.horizontal, DS.Space.l)
        }
        .scrollIndicators(.hidden)
        .carouselInSafeArea()
    }

    private var searchChip: some View {
        Button {
            isSearching.toggle()
            if isSearching {
                // Focus once, when search opens. Focusing on appear brought the keyboard back
                // whenever the menu scrolled the field off screen and back (it's a lazy stack).
                Task { @MainActor in isSearchFocused = true }
            } else {
                searchText = ""
            }
        } label: {
            chipLabel(
                title: nil, systemImage: "magnifyingglass",
                isSelected: isSearching || !list.query.normalizedSearch.isEmpty)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Search meals")
    }

    private var filterChip: some View {
        Button {
            isShowingFilters = true
        } label: {
            chipLabel(
                title: list.query.filterCount > 0 ? "\(list.query.filterCount)" : nil,
                systemImage: "line.3.horizontal.decrease", isSelected: list.query.filterCount > 0)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Filters")
        .accessibilityValue(
            list.query.filterCount == 1
                ? Text("1 filter") : Text("\(list.query.filterCount) filters"))
    }

    private var sortMenu: some View {
        Menu {
            Picker("Sort By", selection: sortBinding) {
                ForEach(options.sorts) { option in
                    Text(option.label).tag(option.value)
                }
            }
        } label: {
            chipLabel(
                title: String(localized: "Sort by: \(sortLabel)"), systemImage: "chevron.down", isSelected: false,
                isTrailingImage: true)
        }
        .accessibilityLabel("Sort by")
        .accessibilityValue(sortLabel)
    }

    private var timeMenu: some View {
        Menu {
            Button("Any Time") { apply { $0.maxMinutes = nil } }
            ForEach(options.maxMinutes, id: \.self) { minutes in
                Button("\(minutes) Min or Less") { apply { $0.maxMinutes = minutes } }
            }
        } label: {
            chipLabel(
                title: list.query.maxMinutes.map { String(localized: "\($0) min or less") }
                    ?? String(localized: "Time"), systemImage: "chevron.down",
                isSelected: list.query.maxMinutes != nil, isTrailingImage: true)
        }
    }

    /// A chip with nothing to choose isn't shown, rather than shown grayed out: a new household
    /// has no proteins or cuisines yet, and gray chips read as broken.
    @ViewBuilder
    private func valueMenu(
        title: String, options: [MenuFilterOption], selection: String?, apply: @escaping (String?) -> Void
    ) -> some View {
        if !options.isEmpty {
            valueMenuChip(title: title, options: options, selection: selection, apply: apply)
        }
    }

    private func valueMenuChip(
        title: String, options: [MenuFilterOption], selection: String?, apply: @escaping (String?) -> Void
    ) -> some View {
        Menu {
            Button(String(localized: "Any \(title)")) { apply(nil) }
            ForEach(options) { option in
                Button {
                    apply(option.value)
                } label: {
                    if let count = option.count {
                        Text("\(option.label) (\(count))")
                    } else {
                        Text(option.label)
                    }
                }
            }
        } label: {
            chipLabel(
                title: selection.map { MenuFilterOptions.label(for: $0, in: options) } ?? title,
                systemImage: "chevron.down", isSelected: selection != nil, isTrailingImage: true)
        }
        .accessibilityLabel(title)
    }

    private var sortLabel: String {
        options.sorts.first { $0.value == list.query.sort }?.label ?? list.query.sort.title
    }

    private var sortBinding: Binding<MenuSort> {
        Binding(
            get: { list.query.sort },
            set: { value in apply { $0.sort = value } })
    }

    private func apply(_ change: (inout MenuRecipeQuery) -> Void) {
        var query = list.query
        change(&query)
        Task { await list.setQuery(query) }
    }

    private func chipLabel(
        title: String?, systemImage: String, isSelected: Bool, isTrailingImage: Bool = false
    ) -> some View {
        HStack(spacing: DS.Space.xxs) {
            if !isTrailingImage {
                Image(systemName: systemImage)
            }
            if let title {
                Text(title)
                    .lineLimit(1)
            }
            if isTrailingImage {
                Image(systemName: systemImage)
                    .font(.caption2.weight(.semibold))
            }
        }
        .font(.subheadline.weight(.medium))
        .foregroundStyle(isSelected ? AnyShapeStyle(DS.Colors.onAccent) : AnyShapeStyle(Color.primary))
        .padding(.horizontal, DS.Space.m)
        .frame(minHeight: DS.ButtonSize.small.height)
        .background(
            isSelected ? AnyShapeStyle(DS.Colors.accent) : AnyShapeStyle(DS.Colors.surface), in: .capsule)
    }
}

/// Every filter in one sheet, applied on Done.
struct MenuFilterSheet: View {
    let query: MenuRecipeQuery
    let options: MenuFilterOptions
    let apply: (MenuRecipeQuery) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var draft: MenuRecipeQuery

    init(query: MenuRecipeQuery, options: MenuFilterOptions, apply: @escaping (MenuRecipeQuery) -> Void) {
        self.query = query
        self.options = options
        self.apply = apply
        _draft = State(initialValue: query)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Sort By") {
                    Picker("Sort By", selection: $draft.sort) {
                        ForEach(options.sorts) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                }
                Section("Filters") {
                    picker(title: "Main Protein", options: options.proteins, selection: $draft.protein)
                    picker(title: "Cuisine", options: options.cuisines, selection: $draft.cuisine)
                    picker(title: "Tag", options: options.tags, selection: $draft.tag)
                    Picker("Cook Time", selection: $draft.maxMinutes) {
                        Text("Any").tag(Int?.none)
                        ForEach(options.maxMinutes, id: \.self) { minutes in
                            Text("\(minutes) min or less").tag(Int?.some(minutes))
                        }
                    }
                    Picker("Show", selection: $draft.addons) {
                        Text("Mains and Add-Ons").tag(Bool?.none)
                        Text("Mains Only").tag(Bool?.some(false))
                        Text("Add-Ons Only").tag(Bool?.some(true))
                    }
                }
            }
            .navigationTitle("Filters")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Reset") {
                        draft = MenuRecipeQuery(search: draft.search, sort: draft.sort)
                    }
                    .disabled(draft.filterCount == 0)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        apply(draft)
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func picker(title: LocalizedStringKey, options: [MenuFilterOption], selection: Binding<String?>)
        -> some View
    {
        Picker(title, selection: selection) {
            Text("Any").tag(String?.none)
            ForEach(options) { option in
                Text(option.label).tag(String?.some(option.value))
            }
        }
        .disabled(options.isEmpty)
    }
}

/// A section's "Show More": All Meals with that section's filters, on its own screen.
struct AllMealsView: View {
    let route: AllMealsRoute

    @Environment(MenuStore.self) private var menu
    @State private var list: MenuRecipeList?

    var body: some View {
        ScrollView {
            if let list {
                AllMealsSection(list: list, title: nil, canAdd: menu.selectedTiming != .past)
                    .padding(.vertical, DS.Space.s)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .padding(.top, DS.Space.huge)
            }
        }
        .navigationTitle(route.title)
        .recipeUndoToast()
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if list == nil {
                list = menu.makeList(query: route.query)
            }
        }
    }
}

#Preview("All Meals") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ScrollView {
            AllMealsSection(list: MenuRecipeList.preview(session: session, items: MenuPreviewData.cards))
                .padding(.vertical)
        }
        .navigationTitle("Menu")
    }
    .menuPreviewEnvironment()
}

#Preview("Filters sheet") {
    MenuFilterSheet(
        query: MenuRecipeQuery(protein: "chicken", maxMinutes: 30), options: MenuPreviewData.filters
    ) { _ in }
}
