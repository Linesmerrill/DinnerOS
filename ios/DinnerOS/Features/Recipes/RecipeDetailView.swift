import SwiftUI

/// A recipe: an edge-to-edge hero photo, the title and headline, a stats row, tag chips, and
/// the Overview, Ingredients, and Nutrition sections under a sticky picker, with a bottom bar
/// that adds it to the week or changes the meal that's already there.
///
/// The summary's name and photo show at once; everything else appears when the recipe loads
/// (from the library's cache when it was opened before).
struct RecipeDetailView: View {
    let summary: RecipeSummary

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(MenuStore.self) private var menu
    @Environment(MealPlanner.self) private var planner
    @Environment(PlanStore.self) private var plans
    @Environment(EventReporter.self) private var events
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var recipe: Recipe?
    @State private var loadError: String?
    @State private var servings: Int?
    @State private var tab: RecipeDetailTab = .overview
    @State private var customizations: [CustomizationGroup] = []
    /// The chosen option per customizable ingredient, saved to the plan entry when there is one.
    @State private var selections: [String: String] = [:]
    @State private var pairings = RecipePairings.empty
    @State private var stepsExpanded = false
    @State private var heroHeight: CGFloat = 320
    @State private var showsNavigationBar = false

    /// The meal in the shown week this screen acts on: the most recently added, when the same
    /// recipe is planned more than once.
    private var plannedEntries: [PlanEntry] {
        planner.entries(recipeID: summary.id)
    }

    private var card: MenuCard? {
        menu.card(forRecipeID: summary.id)
    }

    private var name: String { recipe?.name ?? summary.name }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                hero
                LazyVStack(alignment: .leading, spacing: 20, pinnedViews: [.sectionHeaders]) {
                    titleBlock
                        .padding(.horizontal, 16)
                        .padding(.top, 16)
                    Section {
                        sectionContent
                            .padding(.horizontal, 16)
                            .padding(.top, 4)
                    } header: {
                        RecipeDetailTabPicker(selection: $tab)
                    }
                    if let recipe, !recipe.steps.isEmpty {
                        CookingStepsSection(steps: recipe.steps, isExpanded: $stepsExpanded)
                            .padding(.horizontal, 16)
                    }
                }
                .padding(.bottom, 24)
            }
        }
        .ignoresSafeArea(edges: .top)
        .background(Color(.systemBackground))
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y > heroHeight - 110
        } action: { _, isPastHero in
            showsNavigationBar = isPastHero
        }
        .navigationTitle(showsNavigationBar ? name : "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(showsNavigationBar ? .visible : .hidden, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                ShareLink(item: shareURL ?? URL(string: "https://example.com").unsafelyUnwrapped, subject: Text(name)) {
                    Image(systemName: "square.and.arrow.up")
                        .modifier(HeroToolbarButton(needsBackground: !showsNavigationBar))
                }
                .accessibilityLabel("Share \(name)")
                .opacity(shareURL == nil ? 0 : 1)
                .disabled(shareURL == nil)
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            RecipePlanBar(
                summary: summary, recipe: recipe, entries: plannedEntries, servings: $servings,
                selections: customizationSelections)
        }
        .overlay(alignment: .bottom) { toast }
        .task { await load(reload: false) }
        .task(id: summary.id) { await loadExtras() }
        .task(id: ViewedKey(recipeID: summary.id, isActive: scenePhase == .active)) {
            // Counts as viewed after staying on screen, in the foreground, for a few
            // seconds. Leaving the screen or the app cancels the wait.
            guard scenePhase == .active else { return }
            do {
                try await Task.sleep(for: EventReporter.viewDwell)
            } catch {
                return
            }
            events.recipeViewed(recipeID: summary.id)
        }
        .onChange(of: plannedEntries.last?.customizations) { _, saved in
            applySavedCustomizations(saved)
        }
    }

    // MARK: Hero

    private var hero: some View {
        GeometryReader { geometry in
            let minY = geometry.frame(in: .scrollView).minY
            let stretch = max(minY, 0)
            RecipePhoto(
                url: recipe?.imageURL ?? summary.imageURL, aspectRatio: nil, pointWidth: geometry.size.width,
                cornerRadius: 0
            )
            .frame(width: geometry.size.width, height: geometry.size.height + stretch)
            .clipped()
            .overlay(alignment: .top) {
                // Keeps the back and share buttons legible over a bright photo.
                LinearGradient(
                    colors: [.black.opacity(0.35), .clear], startPoint: .top, endPoint: .bottom
                )
                .frame(height: 120)
                .allowsHitTesting(false)
            }
            .overlay(alignment: .bottomLeading) {
                if let badge = heroBadge {
                    MenuChip(text: badge.text, systemImage: badge.code.systemImage, isProminent: true)
                        .background(.regularMaterial, in: .capsule)
                        .padding(16)
                }
            }
            .offset(y: -stretch)
        }
        .frame(height: heroHeight)
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.size.width
        } action: { width in
            // About 45% of the screen's height, from the width the screen gives us.
            heroHeight = max(240, min(width * 1.1, 420))
        }
    }

    /// "Autopilot pick" or "Make Again" from the card this recipe was opened from.
    private var heroBadge: MenuBadge? {
        let badges = card?.badges ?? []
        return badges.first { $0.code == .autopilotPick } ?? badges.first { $0.code == .makeAgain } ?? badges.first
    }

    private var shareURL: URL? {
        recipe?.sourceURL
    }

    // MARK: Title and stats

    private var titleBlock: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(name)
                .font(.largeTitle.bold())
                .foregroundStyle(Color.primary)
            if let headline = recipe?.headline ?? summary.headline, !headline.isEmpty {
                Text(headline)
                    .font(.title3)
                    .foregroundStyle(Color.secondary)
            }
            RecipeStatsRow(stats: stats)
                .padding(.top, 4)
            if !tags.isEmpty {
                ChipFlowLayout(spacing: 6) {
                    ForEach(tags, id: \.self) { tag in
                        MenuChip(text: tag)
                    }
                }
            }
            if let recipe {
                HouseholdRatingSummary(recipe: recipe)
            }
        }
    }

    private var stats: [MenuFormat.Stat] {
        MenuFormat.stats(
            minutes: recipe?.displayMinutes ?? summary.displayMinutes,
            calories: recipe?.calories ?? summary.calories,
            proteinGrams: recipe?.proteinGrams ?? summary.proteinGrams,
            difficulty: recipe.flatMap { recipe in
                recipe.difficulty.flatMap { $0 > 0 ? RecipeFormat.difficulty($0, source: recipe.source) : nil }
            })
    }

    /// Imported tags carry the source's own slugs ("latin-american-faves", "seo"); show the
    /// ones a person would recognize, spelled out.
    private var tags: [String] {
        let values = (recipe?.tags ?? summary.tags) + (recipe?.cuisines ?? [])
        var seen = Set<String>()
        return values.compactMap { value -> String? in
            let spelled = value.replacingOccurrences(of: "-", with: " ")
                .replacingOccurrences(of: "_", with: " ")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            let key = spelled.lowercased()
            guard !key.isEmpty, key != "seo", !key.hasSuffix("faves"), !key.hasSuffix("picks") else { return nil }
            guard seen.insert(key).inserted else { return nil }
            return spelled.capitalized(with: .current)
        }
        .prefix(5)
        .map { $0 }
    }

    // MARK: Sections

    @ViewBuilder
    private var sectionContent: some View {
        if let loadError {
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: loadError)
                Button("Try Again") {
                    Task { await load(reload: true) }
                }
                .buttonStyle(.bordered)
            }
        } else if let recipe {
            switch tab {
            case .overview:
                RecipeOverviewSection(
                    recipe: recipe, customizations: customizations, selections: $selections,
                    choose: choose(group:choice:), pairings: pairings, summary: summary,
                    reloadPairings: { await loadPairings() })
            case .ingredients:
                RecipeIngredientsSection(recipe: recipe, servings: $servings)
            case .nutrition:
                RecipeNutritionSection(recipe: recipe)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity)
                .padding(.vertical, 40)
        }
    }

    @ViewBuilder
    private var toast: some View {
        if let toast = planner.toast {
            PlannerToastView(
                toast: toast,
                undo: { Task { await planner.undo() } },
                dismiss: { planner.dismissToast(toast.id) })
        }
    }

    // MARK: Loading

    private func load(reload: Bool) async {
        if !reload, let cached = library.cachedRecipe(id: summary.id) {
            show(cached)
            return
        }
        do {
            show(try await library.recipe(id: summary.id, reload: reload))
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func show(_ loaded: Recipe) {
        recipe = loaded
        if let servings, loaded.servingOptions.contains(servings) { return }
        servings =
            plannedEntries.last?.servings
            ?? loaded.preferredServings(householdDefault: households.current?.household.defaultServings)
    }

    /// Customizations and pairings are optional: a household or server without them shows nothing.
    private func loadExtras() async {
        async let pairings: Void = loadPairings()
        customizations = (try? await library.customizations(recipeID: summary.id)) ?? []
        applySavedCustomizations(plannedEntries.last?.customizations)
        await pairings
    }

    private func loadPairings() async {
        pairings = (try? await library.pairings(recipeID: summary.id, week: plans.week)) ?? .empty
    }

    /// Starts each group at the entry's saved choice, or the recipe as written.
    private func applySavedCustomizations(_ saved: [PlanEntryCustomization]?) {
        var chosen: [String: String] = [:]
        for group in customizations {
            if let match = saved?.first(where: { $0.ingredientKey == group.ingredientKey }) {
                chosen[group.ingredientKey] = match.choiceID
            } else if let original = group.originalChoice {
                chosen[group.ingredientKey] = original.id
            }
        }
        selections = chosen
    }

    /// The choices that differ from the recipe as written.
    private var customizationSelections: [PlanCustomizationRequest.Selection] {
        customizations.compactMap { group in
            guard let choiceID = selections[group.ingredientKey], choiceID != group.originalChoice?.id else {
                return nil
            }
            return PlanCustomizationRequest.Selection(ingredientKey: group.ingredientKey, choiceID: choiceID)
        }
    }

    /// Saves a choice to the planned meal at once, or keeps it until the meal is added.
    private func choose(group: CustomizationGroup, choice: CustomizationChoice) {
        let previous = selections[group.ingredientKey]
        selections[group.ingredientKey] = choice.id
        guard let entry = plannedEntries.last else { return }
        let selections = customizationSelections
        Task {
            do {
                try await plans.setCustomization(entryID: entry.id, selections: selections)
            } catch is CancellationError {
                return
            } catch {
                self.selections[group.ingredientKey] = previous
                planner.errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Which part of the recipe is showing.
enum RecipeDetailTab: String, CaseIterable, Identifiable {
    case overview
    case ingredients
    case nutrition

    var id: String { rawValue }

    var title: LocalizedStringKey {
        switch self {
        case .overview: "Overview"
        case .ingredients: "Ingredients"
        case .nutrition: "Nutrition"
        }
    }
}

/// The segmented picker that sticks under the navigation bar while the recipe scrolls.
struct RecipeDetailTabPicker: View {
    @Binding var selection: RecipeDetailTab

    var body: some View {
        Picker("Section", selection: $selection) {
            ForEach(RecipeDetailTab.allCases) { tab in
                Text(tab.title).tag(tab)
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
    }
}

/// The recipe screen's four numbers: time, calories, protein, and difficulty.
struct RecipeStatsRow: View {
    let stats: [MenuFormat.Stat]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if !stats.isEmpty {
            let columns = Array(
                repeating: GridItem(.flexible(), alignment: .topLeading),
                count: dynamicTypeSize.isAccessibilitySize ? 2 : min(stats.count, 4))
            LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
                ForEach(stats) { stat in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(stat.title)
                            .font(.caption2)
                            .foregroundStyle(Color.secondary)
                        Label(stat.value, systemImage: stat.systemImage)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(Color.primary)
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(stat.title)
                    .accessibilityValue(stat.accessibilityValue)
                }
            }
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// A circular material background for buttons floating over the hero, on systems that don't
/// already give toolbar buttons one.
private struct HeroToolbarButton: ViewModifier {
    let needsBackground: Bool

    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content
        } else if needsBackground {
            content
                .foregroundStyle(Color.white)
                .frame(width: 30, height: 30)
                .background(.ultraThinMaterial, in: .circle)
        } else {
            content
        }
    }
}

private struct ViewedKey: Equatable {
    let recipeID: String
    let isActive: Bool
}

#Preview("Recipe") {
    NavigationStack {
        RecipeDetailView(summary: MenuPreviewData.cards[0].recipe)
    }
    .menuPreviewEnvironment()
}

#Preview("Recipe, dark") {
    NavigationStack {
        RecipeDetailView(summary: MenuPreviewData.cards[1].recipe)
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
    .preferredColorScheme(.dark)
}

#Preview("Recipe, accessibility size") {
    NavigationStack {
        RecipeDetailView(summary: MenuPreviewData.cards[0].recipe)
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility2)
}
