import SwiftUI

/// Searches the household's recipes and adds them to a week, several in a row.
///
/// The plus button adds a recipe at once with the preferred serving size and the day
/// chosen at the top. Tapping a recipe opens the full form (servings and note).
struct AddRecipesSheet: View {
    let week: ISOWeek

    @Environment(PlanStore.self) private var plans
    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    /// Separate from the Recipes tab's library so searching here doesn't change that list.
    @State private var picker: RecipeLibrary?
    @State private var searchText = ""
    @State private var day: PlanDay?
    /// How many times each recipe was added from this sheet.
    @State private var addedCounts: [String: Int] = [:]
    @State private var adding: Set<String> = []
    @State private var configuring: RecipeSummary?
    @State private var addError: String?
    /// Set when there was no household to search, so the sheet says so instead of showing a
    /// spinner nothing would ever replace.
    @State private var couldNotStart = false

    private static let searchDebounce = Duration.milliseconds(350)

    private var addedTotal: Int {
        addedCounts.values.reduce(0, +)
    }

    var body: some View {
        NavigationStack {
            Group {
                if let picker {
                    content(picker)
                } else if couldNotStart {
                    ContentUnavailableView {
                        Label("Couldn't Load Recipes", systemImage: "exclamationmark.triangle")
                    } description: {
                        Text("Recipes need your household. Try again in a moment.")
                    } actions: {
                        Button("Try Again") {
                            Task { await start() }
                        }
                        .buttonStyle(.borderedProminent)
                    }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .navigationTitle("Add Recipes")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(
                text: $searchText, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search recipes"
            )
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
                ToolbarItem(placement: .status) {
                    if addedTotal > 0 {
                        Text("Added \(addedTotal) to \(week.rangeLabel(weekStartsOn: plans.weekStartsOn))")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .task { await start() }
            .task(id: searchText) {
                guard let picker, searchText != picker.filters.search else { return }
                do {
                    try await Task.sleep(for: Self.searchDebounce)
                } catch {
                    return
                }
                await picker.setSearch(searchText)
            }
            .sheet(item: $configuring) { summary in
                AddEntrySheet(recipeID: summary.id, recipeName: summary.name, fixedWeek: week, initialDay: day) {
                    addedCounts[summary.id, default: 0] += 1
                }
            }
            .alert("Couldn't Add Recipe", isPresented: Binding(presenting: $addError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(addError ?? "")
            }
        }
    }

    /// Makes the sheet's own recipe list and loads it. Without a household there is nothing to
    /// search, which used to leave the sheet on a spinner forever; now it says so and offers
    /// another go.
    private func start() async {
        guard let householdID = households.current?.household.id else {
            couldNotStart = picker == nil
            return
        }
        couldNotStart = false
        let picker = picker ?? library.makeIndependentCopy()
        self.picker = picker
        await picker.activate(householdID: householdID)
    }

    @ViewBuilder
    private func content(_ picker: RecipeLibrary) -> some View {
        switch picker.phase {
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
                    Task { await picker.retry() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            List {
                Section {
                    DayPicker(week: week, selection: $day)
                } footer: {
                    Text("Tap + to add with the usual serving size, or tap a recipe to choose servings and add a note.")
                }
                Section {
                    ForEach(picker.items) { summary in
                        row(summary)
                    }
                    footer(picker)
                }
            }
            .overlay {
                if picker.items.isEmpty {
                    if picker.filters.normalizedSearch.isEmpty {
                        ContentUnavailableView("No Recipes Yet", systemImage: "book.closed")
                    } else {
                        ContentUnavailableView.search(text: picker.filters.normalizedSearch)
                    }
                }
            }
        }
    }

    private func row(_ summary: RecipeSummary) -> some View {
        HStack(spacing: 8) {
            Button {
                configuring = summary
            } label: {
                RecipeRow(summary: summary)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Choose servings and a note")
            Spacer(minLength: 0)
            if adding.contains(summary.id) {
                ProgressView()
                    .frame(minWidth: 44, minHeight: 44)
            } else {
                let count = addedCounts[summary.id, default: 0]
                Button {
                    quickAdd(summary)
                } label: {
                    Image(systemName: count > 0 ? "checkmark.circle.fill" : "plus.circle")
                        .font(.title2)
                        .frame(minWidth: 44, minHeight: 44)
                }
                .buttonStyle(.borderless)
                .accessibilityLabel("Add \(summary.name)")
                .accessibilityValue(count > 0 ? Text("Added \(count) times") : Text(""))
            }
        }
    }

    @ViewBuilder
    private func footer(_ picker: RecipeLibrary) -> some View {
        if let loadMoreError = picker.loadMoreError {
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: loadMoreError)
                Button("Try Again") {
                    Task { await picker.retry() }
                }
                .buttonStyle(.bordered)
            }
        } else if picker.hasMore {
            ProgressView()
                .frame(maxWidth: .infinity)
                .task(id: picker.nextCursor) {
                    await picker.loadMore()
                }
        }
    }

    /// Adds with the household's default size when the recipe offers it, else the next
    /// size up (the same rule as the recipe screen).
    private func quickAdd(_ summary: RecipeSummary) {
        adding.insert(summary.id)
        let day = day
        Task {
            defer { adding.remove(summary.id) }
            do {
                let recipe = try await library.recipe(id: summary.id)
                let householdDefault = households.current?.household.defaultServings
                guard let servings = recipe.preferredServings(householdDefault: householdDefault) else {
                    addError = String(localized: "\(summary.name) has no serving sizes, so it can't be planned.")
                    return
                }
                try await plans.addEntry(
                    NewPlanEntry(recipeID: summary.id, day: day, servings: servings), to: week)
                addedCounts[summary.id, default: 0] += 1
            } catch is CancellationError {
                return
            } catch {
                addError = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    AddRecipesSheet(week: PlanPreviewData.week)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(RecipePreviewData.library(session: session))
        .environment(PlanPreviewData.store(session: session))
}
