import SwiftUI

/// The Recipes tab: the current household's recipe library, searchable, sortable, and
/// paged by cursor as the user scrolls.
struct RecipesView: View {
    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households

    @State private var searchText = ""

    /// Long enough to skip requests while typing, short enough to feel live.
    private static let searchDebounce = Duration.milliseconds(350)

    private var householdID: String? {
        households.current?.household.id
    }

    var body: some View {
        content
            .navigationTitle("Recipes")
            .navigationDestination(for: RecipeSummary.self) { summary in
                RecipeDetailView(summary: summary)
            }
            .navigationDestination(for: HouseholdRatingsRoute.self) { route in
                HouseholdRatingsView(route: route)
            }
            .searchable(text: $searchText, prompt: "Search recipes")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    filterMenu
                }
            }
            .task(id: householdID) {
                guard let householdID else { return }
                if library.householdID == householdID {
                    searchText = library.filters.search
                }
                await library.activate(householdID: householdID)
            }
            .task(id: searchText) {
                guard searchText != library.filters.search || library.phase == .idle else { return }
                // A newer keystroke cancels this task during the sleep.
                do {
                    try await Task.sleep(for: Self.searchDebounce)
                } catch {
                    return
                }
                await library.setSearch(searchText)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch library.phase {
        case .idle, .loading:
            ProgressView("Loading recipes…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Recipes", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await library.retry() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            if let refreshError = library.refreshError {
                FormErrorLabel(message: refreshError)
            }
            ForEach(library.items) { summary in
                NavigationLink(value: summary) {
                    RecipeRow(summary: summary)
                }
            }
            pageFooter
        }
        .listStyle(.plain)
        .overlay {
            if library.items.isEmpty {
                emptyState
            }
        }
        .refreshable {
            await library.refresh()
        }
    }

    @ViewBuilder
    private var pageFooter: some View {
        if let loadMoreError = library.loadMoreError {
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: loadMoreError)
                Button("Try Again") {
                    Task { await library.retry() }
                }
                .buttonStyle(.bordered)
            }
            .padding(.vertical, 4)
        } else if library.hasMore {
            ProgressView()
                .frame(maxWidth: .infinity)
                .listRowSeparator(.hidden)
                // Re-runs for each new cursor while the footer stays on screen.
                .task(id: library.nextCursor) {
                    await library.loadMore()
                }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let filters = library.filters
        if !filters.normalizedSearch.isEmpty {
            ContentUnavailableView.search(text: filters.normalizedSearch)
        } else if filters.isNarrowed {
            ContentUnavailableView {
                Label("No \(filters.kind.title)", systemImage: "line.3.horizontal.decrease.circle")
            } description: {
                Text("None of this household's recipes match this filter.")
            } actions: {
                Button("Show All Recipes") {
                    update(\.kind, to: .all)
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

    private var filterMenu: some View {
        Menu {
            Section("Sort By") {
                Picker("Sort By", selection: binding(\.sort)) {
                    ForEach(RecipeSort.allCases) { sort in
                        Text(sort.title).tag(sort)
                    }
                }
            }
            Section("Show") {
                Picker("Show", selection: binding(\.kind)) {
                    ForEach(RecipeKind.allCases) { kind in
                        Text(kind.title).tag(kind)
                    }
                }
            }
        } label: {
            Label(
                "Sort and Filter",
                systemImage: library.filters.kind == .all
                    ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
        }
        .accessibilityValue("\(library.filters.sort.title), \(library.filters.kind.title)")
    }

    private func binding<Value>(_ keyPath: WritableKeyPath<RecipeListFilters, Value>) -> Binding<Value> {
        Binding(
            get: { library.filters[keyPath: keyPath] },
            set: { update(keyPath, to: $0) })
    }

    private func update<Value>(_ keyPath: WritableKeyPath<RecipeListFilters, Value>, to value: Value) {
        var filters = library.filters
        filters[keyPath: keyPath] = value
        Task { await library.setFilters(filters) }
    }
}

#Preview("Loaded") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        RecipesView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
}

#Preview("Empty") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        RecipesView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipeLibrary.preview(session: session))
}
