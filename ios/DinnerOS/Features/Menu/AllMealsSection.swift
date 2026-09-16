import SwiftUI

/// Every recipe in the household, under a sticky row of chips: search, filters, sort, protein,
/// time, and cuisine. Pages by cursor as the list scrolls.
struct AllMealsSection: View {
    let list: MenuRecipeList
    var title: String? = String(localized: "All Meals")
    var canAdd = true

    @Environment(MenuStore.self) private var menu

    @State private var isSearching = false
    @State private var searchText = ""

    /// Long enough to skip requests while typing, short enough to feel live.
    private static let searchDebounce = Duration.milliseconds(350)

    private var options: MenuFilterOptions {
        menu.filterOptions ?? .fallback
    }

    private var photoURLs: [URL?] { list.items.map(\.recipe.imageURL) }

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 20, pinnedViews: [.sectionHeaders]) {
            Section {
                content
                    .padding(.horizontal, 16)
            } header: {
                VStack(alignment: .leading, spacing: 8) {
                    if let title {
                        Text(title)
                            .font(.title3.bold())
                            .accessibilityAddTraits(.isHeader)
                            .padding(.horizontal, 16)
                    }
                    MenuChipBar(
                        list: list, options: options, isSearching: $isSearching, searchText: $searchText)
                }
                .padding(.vertical, 8)
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
    }

    @ViewBuilder
    private var content: some View {
        switch list.phase {
        case .idle, .loading:
            VStack(spacing: 20) {
                ForEach(0..<3, id: \.self) { _ in
                    VStack(alignment: .leading, spacing: 8) {
                        RecipePhoto(url: nil, aspectRatio: 16.0 / 10.0, pointWidth: 400)
                        RoundedRectangle(cornerRadius: 4).fill(.quaternary).frame(height: 14)
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
                LazyVStack(alignment: .leading, spacing: 24) {
                    if let refreshError = list.refreshError {
                        FormErrorLabel(message: refreshError)
                    }
                    ForEach(Array(list.items.enumerated()), id: \.element.id) { index, card in
                        MenuRecipeCard(card: card, size: .fullWidth, canAdd: canAdd)
                            .prefetchesPhotos(
                                after: index, in: photoURLs, pointWidth: MenuRecipeCard.fullWidthEstimate)
                    }
                    pageFooter
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
            ContentUnavailableView {
                Label("No Recipes Yet", systemImage: "book.closed")
            } description: {
                Text(
                    "Recipes arrive when an order history is imported into this household. After an import finishes, pull down to refresh."
                )
            }
        }
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
        VStack(spacing: 8) {
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    searchChip
                    filterChip
                    sortMenu
                    valueMenu(
                        title: String(localized: "Main protein"), options: options.proteins,
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
                .padding(.horizontal, 16)
            }
            .scrollIndicators(.hidden)
            if isSearching {
                HStack(spacing: 8) {
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
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(Color(.secondarySystemBackground), in: .capsule)
                .padding(.horizontal, 16)
                .onAppear { isSearchFocused = true }
            }
        }
        .sheet(isPresented: $isShowingFilters) {
            MenuFilterSheet(query: list.query, options: options) { query in
                Task { await list.setQuery(query) }
            }
        }
    }

    private var searchChip: some View {
        Button {
            isSearching.toggle()
            if !isSearching {
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
            Picker("Sort by", selection: sortBinding) {
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
            Button("Any time") { apply { $0.maxMinutes = nil } }
            ForEach(options.maxMinutes, id: \.self) { minutes in
                Button("\(minutes) min or less") { apply { $0.maxMinutes = minutes } }
            }
        } label: {
            chipLabel(
                title: list.query.maxMinutes.map { String(localized: "\($0) min or less") }
                    ?? String(localized: "Time"), systemImage: "chevron.down",
                isSelected: list.query.maxMinutes != nil, isTrailingImage: true)
        }
    }

    private func valueMenu(
        title: String, options: [MenuFilterOption], selection: String?, apply: @escaping (String?) -> Void
    ) -> some View {
        Menu {
            Button(String(localized: "Any \(title.lowercased())")) { apply(nil) }
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
        .disabled(options.isEmpty)
        .opacity(options.isEmpty ? 0.5 : 1)
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
        HStack(spacing: 4) {
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
        .foregroundStyle(isSelected ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
        .padding(.horizontal, 12)
        .frame(minHeight: 34)
        .background(
            isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.secondarySystemBackground)), in: .capsule)
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
                    Picker("Sort by", selection: $draft.sort) {
                        ForEach(options.sorts) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                }
                Section("Filters") {
                    picker(title: "Main protein", options: options.proteins, selection: $draft.protein)
                    picker(title: "Cuisine", options: options.cuisines, selection: $draft.cuisine)
                    picker(title: "Tag", options: options.tags, selection: $draft.tag)
                    Picker("Cook time", selection: $draft.maxMinutes) {
                        Text("Any").tag(Int?.none)
                        ForEach(options.maxMinutes, id: \.self) { minutes in
                            Text("\(minutes) min or less").tag(Int?.some(minutes))
                        }
                    }
                    Picker("Show", selection: $draft.addons) {
                        Text("Mains and add-ons").tag(Bool?.none)
                        Text("Mains only").tag(Bool?.some(false))
                        Text("Add-ons only").tag(Bool?.some(true))
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
                    .padding(.vertical, 8)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .padding(.top, 40)
            }
        }
        .navigationTitle(route.title)
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
