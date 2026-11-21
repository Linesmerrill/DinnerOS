import SwiftUI

/// A recipe: an edge-to-edge hero photo, the title and headline, a stats row, tag chips, and
/// the Customize, Description, Ingredients and Nutrition sections under a sticky picker, with
/// a bottom bar that adds it to the week or changes the meal that's already there.
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
    @Environment(GrocerySkipStore.self) private var grocerySkips
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var recipe: Recipe?
    @State private var loadError: String?
    @State private var servings: Int?
    @State private var tab: RecipeDetailTab = .description
    /// Until the member picks a tab, the screen follows the first one this recipe has.
    @State private var hasPickedTab = false
    @State private var customizations: [CustomizationGroup] = []
    /// The chosen option per customizable ingredient, saved to the plan entry when there is one.
    @State private var selections: [String: String] = [:]
    @State private var pairings = RecipePairings.empty
    @State private var stepsExpanded = false
    /// The steps as the server renders them for `servings`, with the household's specialty
    /// choices applied. `nil` until they load, or when the server doesn't render them.
    @State private var instructions: RecipeInstructions?
    /// Opens the specialty ingredient setup, from a step that names something unchosen.
    @State private var showsSpecialtySetup = false
    @State private var heroHeight: CGFloat = 320
    @State private var showsNavigationBar = false
    /// The large title's bottom edge in the scroll view, so the bar's title shows only once the
    /// large one has gone under the bar: one title at a time.
    @State private var titleBottom: CGFloat = .infinity
    /// The scroll view's own top safe area: where the bar ends.
    @State private var scrollTop: CGFloat = 0
    /// The one-line title's row under the bar's buttons.
    @ScaledMetric(relativeTo: .headline) private var compactTitleHeight: CGFloat = 36
    @State private var showsMore = false
    /// The More menu's rows' height, so its sheet on the Duo fits them with no empty space.
    @State private var moreMenuHeight: CGFloat = 3 * DS.Size.tapTarget
    /// What the More menu asked for, run once its popover has closed.
    @State private var moreAction: RecipeMoreAction?
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.verticalSizeClass) private var verticalSizeClass
    /// The page's safe area, which shows iPhone Duo's cover-screen button column.
    @State private var safeArea = EdgeInsets()
    /// Where the tab picker stops as the page scrolls: just under the bar.
    @State private var pinTop: CGFloat = 0
    /// Ingredients being left out or put back, by line ID.
    @State private var leavingOut: Set<String> = []
    @State private var leaveOutError: String?
    @State private var showsCooking = false
    @State private var showsEditor = false
    @State private var deleting: RecipeSummary?
    @State private var showsNutrition = false
    @Environment(\.dismiss) private var dismiss

    /// Changing a recipe's ingredients and steps needs `recipes.edit`; the API enforces it.
    private var canEditRecipe: Bool {
        households.access?.can(.recipesEdit) == true
    }

    /// Leaving an ingredient out changes what the week's list buys, so it needs `plan.edit`, like
    /// skipping one from the list. Hiding it is a convenience; the API enforces it.
    private var canLeaveOut: Bool {
        households.access?.can(.planEdit) == true && !grocerySkips.isForbidden
    }

    /// The meal in the shown week this screen acts on: the most recently added, when the same
    /// recipe is planned more than once.
    private var plannedEntries: [PlanEntry] {
        planner.entries(recipeID: summary.id)
    }

    private var card: MenuCard? {
        menu.card(forRecipeID: summary.id)
    }

    private var name: String { recipe?.name ?? summary.name }

    /// Customize is offered only when there is something to swap or pair, so a recipe with
    /// neither doesn't show an empty tab.
    private var availableTabs: [RecipeDetailTab] {
        RecipeDetailTab.available(hasCustomizations: !customizations.isEmpty, hasPairings: !pairings.items.isEmpty)
    }

    /// The selection, corrected when the extras load and the tabs change under it.
    private var shownTab: RecipeDetailTab {
        RecipeDetailTab.resolve(tab, in: availableTabs)
    }

    private var tabSelection: Binding<RecipeDetailTab> {
        Binding(
            get: { shownTab },
            set: { newValue in
                tab = newValue
                hasPickedTab = true
            })
    }

    private var startCookingPlacement: StartCookingPlacement {
        StartCookingPlacement.resolve(horizontal: horizontalSizeClass, vertical: verticalSizeClass, safeArea: safeArea)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                hero
                if let recipe, recipe.imageURL != nil, let credit = recipe.imageCredit {
                    PhotoCreditLine(credit: credit)
                        .padding(.horizontal, DS.Space.l)
                        .padding(.top, DS.Space.xs)
                }
                // A plain stack, not a lazy one: the page has a handful of sections, and on iOS 26
                // a LazyVStack here kept re-placing its items and never settled when a recipe
                // with steps was opened right after it was saved (100% CPU, no state changing).
                VStack(alignment: .leading, spacing: DS.Space.xl) {
                    titleBlock
                        .recipeColumn()
                        .padding(.top, DS.Space.l)
                    if recipe?.editedAt != nil {
                        Label("You edited this recipe", systemImage: "pencil")
                            .font(DS.Typography.footnote)
                            .foregroundStyle(DS.Colors.secondaryText)
                            .recipeColumn()
                    }
                    if let recipe, let url = recipe.sourceURL,
                        recipe.source == RecipeSourceKey.fromPage || recipe.steps.isEmpty
                    {
                        ImportedRecipeBanner(url: url, showsNote: recipe.source == RecipeSourceKey.fromPage)
                            .recipeColumn()
                    }
                    if let recipe, recipe.imageURLString == nil, canEditRecipe,
                        recipe.source == RecipeSourceKey.typed || recipe.source == RecipeSourceKey.fromPage
                    {
                        SuggestedPhotoBanner(recipe: recipe) { saved in show(saved) }
                            .recipeColumn()
                    }
                    if let notes = recipe?.myRating?.notesForNextTime, !notes.isEmpty {
                        RecipeNotesForNextTime(notes: notes)
                            .recipeColumn()
                    }
                    RecipeDetailTabPicker(tabs: availableTabs, selection: tabSelection)
                        .stickyBelowTop(pinLine)
                        .zIndex(1)
                    sectionContent
                        .recipeColumn()
                        .padding(.top, -DS.Space.s)
                    if let recipe, !recipe.steps.isEmpty {
                        CookingStepsSection(
                            steps: recipe.steps, instructions: instructions,
                            chooseSpecialty: { _ in showsSpecialtySetup = true }, isExpanded: $stepsExpanded
                        )
                        .recipeColumn()
                    }
                    // Picking recipes to share one by one happens here, on the recipe.
                    // Any recipe the household has, imports included, can be shared (decision 685).
                    if let recipe, households.current?.household.catalogSharing == .chosen,
                        households.access?.can(.recipesEdit) == true
                    {
                        RecipeSharingRow(recipe: recipe)
                            .recipeColumn()
                    }
                }
                .padding(.bottom, DS.Space.xxl)
            }
        }
        .ignoresSafeArea(edges: .top)
        // Where the bar ends, measured on the scroll view itself so the solid bar and the pinned
        // picker use the same line (on iPad the page's own inset left a 100 pt gap).
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.safeAreaInsets.top
        } action: { top in
            scrollTop = top
        }
        // Once the photo has scrolled away, a solid bar and, under the buttons, the recipe's
        // name on one line. Nothing that scrolls under it shows through or gets cut mid-line;
        // the pinned picker sits flush below it.
        .overlay(alignment: .top) {
            if showsNavigationBar {
                VStack(spacing: 0) {
                    Color.clear.frame(height: barBottom)
                    Text(name)
                        .font(DS.Typography.headline)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .padding(.horizontal, DS.Space.huge)
                        .frame(maxWidth: .infinity)
                        .frame(height: compactTitleHeight)
                        .opacity(showsCompactTitle ? 1 : 0)
                        .accessibilityAddTraits(.isHeader)
                        .accessibilityHidden(!showsCompactTitle)
                }
                .background(DS.Colors.barBackground)
                .ignoresSafeArea(edges: .top)
                .allowsHitTesting(false)
            }
        }
        // The page has nothing to type in.
        .ignoresSafeArea(.keyboard)
        .background(DS.Colors.background)
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: RecipeDetailHeader.Scroll.self) { geometry in
            RecipeDetailHeader.Scroll(offset: geometry.contentOffset.y, top: geometry.contentInsets.top)
        } action: { _, scroll in
            let offset = scroll.offset
            if pinTop != scroll.top { pinTop = scroll.top }
            // A gap between showing and hiding the bar, so a page resting near the line
            // can't flip it on and off: the bar changes the layout, which moves the line.
            let shows = RecipeDetailHeader.showsBar(offset: offset, heroHeight: heroHeight, showing: showsNavigationBar)
            if shows != showsNavigationBar { showsNavigationBar = shows }
        }
        // The page draws its own one-line title under the buttons (above), on every device.
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(showsNavigationBar ? .visible : .hidden, for: .navigationBar)
        .modifier(RecipeScrollEdge())
        .toolbar {
            // One group, so Start Cooking and the ⋯ menu share a pill.
            ToolbarItemGroup(placement: .topBarTrailing) {
                // On iPad, Start Cooking sits in the bottom bar instead: the tab bar shares the
                // top bar there, and iOS folded these buttons into an overflow menu. On iPhone
                // Duo's cover screen too, where it scrolled away (StartCookingPlacement).
                if recipe?.steps.isEmpty == false, startCookingPlacement == .topBar {
                    Button {
                        showsCooking = true
                    } label: {
                        // An HStack, not a Label: a toolbar shows a Label's icon alone.
                        HStack(spacing: DS.Space.xs) {
                            Image(systemName: "frying.pan")
                            Text("Start Cooking")
                        }
                        .padding(.horizontal, DS.Space.xxs)
                    }
                    .buttonStyle(.dsPrimary(.small))
                    .accessibilityLabel("Start Cooking")
                }
                if hasMoreMenu {
                    moreButton
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            RecipePlanBar(
                summary: summary, recipe: recipe, entries: plannedEntries, servings: $servings,
                selections: customizationSelections,
                startCooking: recipe?.steps.isEmpty == false ? { showsCooking = true } : nil,
                showsStartCooking: startCookingPlacement != .topBar,
                stacksStartCooking: startCookingPlacement == .bottomRow)
        }
        .onGeometryChange(for: EdgeInsets.self) {
            $0.safeAreaInsets
        } action: {
            safeArea = $0
        }
        .overlay(alignment: .bottom) { toast }
        .recipeDeletion($deleting, isPlanned: { _ in !plannedEntries.isEmpty }) { dismiss() }
        .fullScreenCover(isPresented: $showsCooking) {
            CookingView(start: cookMeal, meals: weekMeals)
        }
        .sheet(isPresented: $showsEditor) {
            if let recipe {
                RecipeEditView(recipe: recipe, servings: servings) { saved in
                    show(saved)
                    Task { await loadInstructions() }
                }
            }
        }
        .sheet(isPresented: $showsNutrition) {
            if let recipe {
                NutritionEditorSheet(recipe: recipe) { saved in show(saved) }
            }
        }
        // Attached to the screen's root, never inside a section (decision 510).
        .sheet(isPresented: $showsSpecialtySetup, onDismiss: { Task { await loadInstructions() } }) {
            SpecialtyIngredientsSheet()
        }
        .alert("Couldn’t Change That", isPresented: Binding(presenting: $leaveOutError)) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(leaveOutError ?? "")
        }
        // Something was left out or put back elsewhere (the grocery list, the Shop tab, the
        // review screen): the ingredient list and the steps read it from the instructions.
        .onChange(of: grocerySkips.revision) {
            Task { await loadInstructions() }
        }
        // Keyed by the household, so a recipe opened from a widget before the app has loaded
        // the household (a cold start) loads once it has.
        .task(id: library.householdID) {
            guard library.householdID != nil else { return }
            await load(reload: false)
            await loadInstructions()
        }
        .task(id: "\(summary.id)|\(library.householdID ?? "")") { await loadExtras() }
        // The rendered steps depend on the serving size and on choices that can change
        // elsewhere, so they reload rather than being cached with the recipe.
        .task(id: InstructionsKey(recipeID: summary.id, servings: servings)) { await loadInstructions() }
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
        .onChange(of: availableTabs) { _, tabs in
            // The extras arrive after the recipe, so Customize appears a moment later.
            guard !hasPickedTab, let first = tabs.first else { return }
            tab = first
        }
    }

    // MARK: Hero

    private var hero: some View {
        // Read once per update, not on every layout pass of the header.
        let badge = heroBadge
        let url = photoURL
        let title = name
        return GeometryReader { geometry in
            let minY = geometry.frame(in: .scrollView).minY
            let stretch = max(minY, 0)
            RecipePhoto(
                url: url, aspectRatio: nil, pointWidth: geometry.size.width,
                cornerRadius: 0,
                // The hero fills the screen: it's the one photo worth the full-width original.
                maxBucket: nil,
                title: title, showsTitle: false,
                // Without a photo, the icon fades as it nears the top, so it never shows
                // behind the buttons there.
                placeholderFadesNearTop: true
            )
            .frame(width: geometry.size.width, height: geometry.size.height + stretch)
            .clipped()
            .overlay(alignment: .top) {
                // A wash of the page's own background behind the status bar and the buttons, so
                // the clock and the bar's glyphs (dark in light mode, light in dark) read over any
                // photo.
                LinearGradient(
                    colors: [DS.Colors.background.opacity(RecipeDetailHeader.scrimOpacity), .clear],
                    startPoint: .top, endPoint: .bottom
                )
                .frame(height: max(RecipeDetailHeader.scrimHeight, barBottom + DS.Space.huge))
                .allowsHitTesting(false)
            }
            .overlay(alignment: .bottomLeading) {
                if let badge {
                    MenuChip(text: badge.text, systemImage: badge.code.systemImage, isProminent: true)
                        .background(.regularMaterial, in: .capsule)
                        .padding(DS.Space.l)
                }
            }
            .offset(y: -stretch)
        }
        .frame(height: heroHeight)
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.size.width
        } action: { width in
            // About 45% of the screen's height, from the width the screen gives us.
            heroHeight = RecipeDetailHeader.heroHeight(width: width, isWide: isWideScreen)
        }
    }

    /// An iPad, or a phone on its side: the photo stops at `DS.Size.heroMaxHeight`.
    private var isWideScreen: Bool {
        horizontalSizeClass == .regular || verticalSizeClass == .compact
    }

    /// Where the bar ends, in the scroll view's own coordinates.
    private var barBottom: CGFloat {
        scrollTop > 0 ? scrollTop : max(pinTop, safeArea.top)
    }

    /// Where the picker pins: under the bar and its one-line title.
    private var pinLine: CGFloat {
        barBottom + compactTitleHeight
    }

    /// The one-line title, once the large one has gone under the bar: one title at a time.
    private var showsCompactTitle: Bool {
        showsNavigationBar && titleBottom < pinLine
    }

    /// "Autopilot pick" or "Make Again" from the card this recipe was opened from.
    private var heroBadge: MenuBadge? {
        let badges = card?.badges ?? []
        return badges.first { $0.code == .autopilotPick } ?? badges.first { $0.code == .makeAgain } ?? badges.first
    }

    private var shareURL: URL? {
        recipe?.sourceURL
    }

    /// Nutrition can be typed in on a recipe the household added.
    private func canAddNutrition(_ recipe: Recipe) -> Bool {
        canEditRecipe && (recipe.source == RecipeSourceKey.typed || recipe.source == RecipeSourceKey.fromPage)
    }

    /// The More menu: an opaque popover rather than a system menu, whose glass showed Start
    /// Cooking and the photo through it. It stays beside its button, inside the page.
    private var moreButton: some View {
        Button {
            showsMore = true
        } label: {
            Image(systemName: TabHeader.moreSystemImage)
                .modifier(HeroToolbarButton(needsBackground: !showsNavigationBar))
        }
        .accessibilityLabel("More")
        .popover(isPresented: $showsMore) {
            RecipeMoreMenu(
                shareURL: shareURL, name: name, canEdit: recipe != nil && canEditRecipe,
                measured: { moreMenuHeight = $0 }
            ) { action in
                moreAction = action
                showsMore = false
            }
            // On the Duo's cover screen the button sits in the side rail, and a popover there
            // spread across the rail; a short sheet stays over the page.
            .presentationCompactAdaptation(startCookingPlacement == .bottomRow ? .sheet : .popover)
            .presentationDetents([.height(moreMenuHeight + DS.Space.xxl)])
            .presentationBackground(DS.Colors.background)
        }
        .onChange(of: showsMore) { _, showing in
            guard !showing, let action = moreAction else { return }
            moreAction = nil
            switch action {
            case .edit: showsEditor = true
            case .delete: deleting = summary
            }
        }
    }

    /// The ⋯ menu shows only when it has something in it, so there's never an empty button.
    private var hasMoreMenu: Bool {
        shareURL != nil || (recipe != nil && canEditRecipe)
    }

    // MARK: Title and stats

    private var titleBlock: some View {
        VStack(alignment: .leading, spacing: DS.Space.sm) {
            Text(name)
                .font(DS.Typography.largeTitle)
                .foregroundStyle(Color.primary)
                .accessibilityAddTraits(.isHeader)
                .onGeometryChange(for: CGFloat.self) { proxy in
                    proxy.frame(in: .scrollView).maxY
                } action: { bottom in
                    titleBottom = bottom
                }
            if let headline = recipe?.headline ?? summary.headline, !headline.isEmpty {
                Text(headline)
                    .font(DS.Typography.subtitle)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
            RecipeStatsRow(stats: stats)
                .padding(.top, DS.Space.xxs)
            if let cuisine = cuisineText {
                Label(cuisine, systemImage: "globe")
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
                    .accessibilityLabel("Cuisine: \(cuisine)")
                    .accessibilityIdentifier("RecipeCuisineShown")
            }
            if !tags.isEmpty {
                ChipFlowLayout(spacing: DS.Space.xs) {
                    ForEach(tags, id: \.self) { tag in
                        RecipeTagChip(text: tag)
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

    /// The recipe's cuisine as the server names it ("Mexican"), or nil.
    private var cuisineText: String? {
        guard let labels = recipe?.cuisineLabels, !labels.isEmpty else { return nil }
        return labels.formatted(.list(type: .and))
    }

    /// Imported tags carry the source's own slugs ("latin-american-faves", "seo"); show the
    /// ones a person would recognize, spelled out.
    private var tags: [String] {
        // The cuisine has its own line when the server names it.
        let cuisines = (recipe?.cuisineLabels.isEmpty ?? true) ? (recipe?.cuisines ?? []) : []
        let values = (recipe?.tags ?? summary.tags) + cuisines
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
            VStack(alignment: .leading, spacing: DS.Space.s) {
                FormErrorLabel(message: loadError)
                Button("Try Again") {
                    Task { await load(reload: true) }
                }
                .buttonStyle(.dsSecondary(.small))
            }
        } else if let recipe {
            switch shownTab {
            case .customization:
                RecipeCustomizationSection(
                    customizations: customizations, selections: $selections, choose: choose(group:choice:),
                    pairings: pairings, summary: summary, reloadPairings: { await loadPairings() })
            case .description:
                RecipeDescriptionSection(recipe: recipe)
            case .ingredients:
                RecipeIngredientsSection(
                    recipe: recipe, servings: $servings, instructions: instructions, canEdit: canLeaveOut,
                    canSkipOnce: plannedWeek != nil, working: leavingOut, leaveOut: leaveOut(_:choice:),
                    putBack: putBack(_:))
            case .nutrition:
                RecipeNutritionSection(recipe: recipe, onAdd: canAddNutrition(recipe) ? { showsNutrition = true } : nil)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity)
                .padding(.vertical, DS.Space.huge)
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

    // MARK: Cooking

    /// The loaded recipe's photo, which may be none after a member removed it; the list's
    /// until the recipe loads.
    private var photoURL: URL? { recipe.map(\.imageURL) ?? summary.imageURL }

    /// This recipe, as the cooking screen's first dish, at the size on screen.
    private var cookMeal: CookMeal {
        let entry = plannedEntries.last
        return CookMeal(
            id: entry?.id ?? summary.id, recipeID: summary.id, name: name,
            imageURL: photoURL, dayText: entry?.day?.name(), servings: servings,
            swaps: entry?.customizations ?? [], week: plannedWeek)
    }

    /// The shown week when this recipe is planned in it: what "just this once" applies to.
    private var plannedWeek: ISOWeek? {
        plannedEntries.isEmpty ? nil : plans.week
    }

    /// The week's planned dishes, for the cooking screen's switcher, in day order.
    private var weekMeals: [CookMeal] {
        let first = households.current?.household.weekStartsOn ?? PlanDay.isoWeekStart
        var seen = Set<String>()
        return (plans.plan?.entriesInDayOrder(weekStartsOn: first) ?? []).compactMap { entry in
            guard seen.insert(entry.recipe.id).inserted else { return nil }
            let isThis = entry.recipe.id == summary.id
            return CookMeal(
                id: entry.id, recipeID: entry.recipe.id, name: entry.recipe.name, imageURL: entry.recipe.imageURL,
                dayText: entry.day?.name(), servings: isThis ? servings : entry.servings,
                swaps: entry.customizations, week: plans.week)
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

    /// The rendered steps for the chosen serving size. A server that doesn't render them, or
    /// a failure, leaves the recipe's own steps on screen rather than an error: nobody should
    /// lose the instructions because a substitution couldn't be looked up.
    private func loadInstructions() async {
        guard recipe != nil else { return }
        instructions =
            (try? await library.instructions(
                recipeID: summary.id, servings: servings, swaps: plannedEntries.last?.customizations ?? [],
                week: plannedWeek))
            ?? instructions
    }

    /// Leaves an ingredient out of this week's meal, this recipe every time, or every recipe,
    /// then reloads the steps so it's struck through here and in them.
    private func leaveOut(_ state: RecipeIngredientState, choice: IngredientLeaveOut) {
        guard let request = state.leaveOutRequest(choice, recipeID: summary.id, week: plannedWeek) else { return }
        changeLeftOut(state) {
            try await grocerySkips.skip(request, householdID: households.current?.household.id)
        }
    }

    /// Resumes the skip that leaves an ingredient out.
    private func putBack(_ state: RecipeIngredientState) {
        guard let leftOut = state.leftOut else { return }
        changeLeftOut(state) {
            try await grocerySkips.resume(skipID: leftOut.skipID, householdID: households.current?.household.id)
        }
    }

    private func changeLeftOut(_ state: RecipeIngredientState, _ change: @escaping () async throws -> Void) {
        guard !leavingOut.contains(state.id) else { return }
        leavingOut.insert(state.id)
        Task {
            defer { leavingOut.remove(state.id) }
            do {
                try await change()
                await loadInstructions()
            } catch is CancellationError {
                return
            } catch {
                leaveOutError = HouseholdStore.message(for: error)
            }
        }
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
    case customization
    case description
    case ingredients
    case nutrition

    var id: String { rawValue }

    /// "Customize" rather than "Customization": four labels have to fit one segmented control
    /// on a phone, and it reads as the same thing the section heading already says.
    var title: LocalizedStringKey {
        switch self {
        case .customization: "Customize"
        case .description: "Description"
        case .ingredients: "Ingredients"
        case .nutrition: "Nutrition"
        }
    }

    /// The segment's identifier, for UI tests: "recipeTab.ingredients".
    var accessibilityID: String {
        switch self {
        case .customization: "recipeTab.customize"
        case .description: "recipeTab.description"
        case .ingredients: "recipeTab.ingredients"
        case .nutrition: "recipeTab.nutrition"
        }
    }

    /// The tabs this recipe has something to show in, in the order they appear.
    static func available(hasCustomizations: Bool, hasPairings: Bool) -> [RecipeDetailTab] {
        allCases.filter { $0 != .customization || hasCustomizations || hasPairings }
    }

    /// Keeps a selection valid when the extras load and Customize appears or goes away.
    static func resolve(_ selection: RecipeDetailTab, in available: [RecipeDetailTab]) -> RecipeDetailTab {
        available.contains(selection) ? selection : (available.first ?? .description)
    }
}

/// The segmented control that sticks under the bar while the recipe scrolls: opaque, flush
/// with the bar, with a short fade under it so rows slip away rather than being cut. Its own
/// buttons, so each segment has an identifier ("recipeTab.ingredients") and reads as selected.
struct RecipeDetailTabPicker: View {
    let tabs: [RecipeDetailTab]
    @Binding var selection: RecipeDetailTab

    @ScaledMetric(relativeTo: .subheadline) private var height: CGFloat = 32

    var body: some View {
        HStack(spacing: DS.Space.hairline) {
            ForEach(tabs) { tab in
                let isSelected = tab == selection
                Button {
                    selection = tab
                } label: {
                    Text(tab.title)
                        .font(DS.Typography.subheadline.weight(isSelected ? .semibold : .regular))
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .foregroundStyle(Color.primary)
                        .frame(maxWidth: .infinity, minHeight: height)
                        .background {
                            if isSelected {
                                Capsule().fill(DS.Colors.background)
                                    .overlay(Capsule().strokeBorder(Color(.separator)))
                            }
                        }
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier(tab.accessibilityID)
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
        }
        .padding(DS.Space.hairline)
        .background(DS.Colors.fill, in: .capsule)
        .frame(maxWidth: DS.Size.readableWidth)
        .padding(.horizontal, DS.Space.l)
        .padding(.vertical, DS.Space.sm)
        .frame(maxWidth: .infinity)
        .background(DS.Colors.barBackground)
        .overlay(alignment: .bottom) { Divider() }
        .overlay(alignment: .bottom) {
            LinearGradient(
                colors: [DS.Colors.barBackground, DS.Colors.barBackground.opacity(0)], startPoint: .top,
                endPoint: .bottom
            )
            .frame(height: DS.Space.m)
            .offset(y: DS.Space.m)
            .allowsHitTesting(false)
        }
    }
}

/// What the More menu runs once it has closed.
enum RecipeMoreAction {
    case edit, delete
}

/// The More menu's rows, on an opaque background: Share, Edit Recipe, and Delete Recipe in red.
private struct RecipeMoreMenu: View {
    let shareURL: URL?
    let name: String
    let canEdit: Bool
    /// Its rows' height, for sizing the sheet it becomes on the Duo.
    var measured: (CGFloat) -> Void = { _ in }
    let choose: (RecipeMoreAction) -> Void

    /// Wide enough for "Delete Recipe" at large text without wrapping.
    static let width: CGFloat = 240

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let shareURL {
                ShareLink(item: shareURL, subject: Text(name)) {
                    row("Share", systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.plain)
            }
            if canEdit {
                if shareURL != nil { Divider() }
                Button {
                    choose(.edit)
                } label: {
                    row("Edit Recipe", systemImage: "pencil")
                }
                .buttonStyle(.plain)
                Divider()
                Button(role: .destructive) {
                    choose(.delete)
                } label: {
                    row("Delete Recipe", systemImage: "trash", tint: DS.Colors.destructive)
                }
                .buttonStyle(.plain)
            }
        }
        .frame(minWidth: Self.width)
        .padding(.vertical, DS.Space.xs)
        .fixedSize(horizontal: false, vertical: true)
        .onGeometryChange(for: CGFloat.self) {
            $0.size.height
        } action: {
            measured($0)
        }
        .frame(maxHeight: .infinity, alignment: .top)
    }

    private func row(_ title: LocalizedStringKey, systemImage: String, tint: Color = .primary) -> some View {
        DSIconRow(systemImage: systemImage, tint: tint) {
            Text(title).foregroundStyle(tint)
        }
        .font(DS.Typography.body)
        .padding(.horizontal, DS.Space.l)
        .frame(minHeight: DS.Size.tapTarget)
        .contentShape(.rect)
    }
}

extension View {
    /// The recipe page's text column: the screen margin, and on a wide screen the readable
    /// width, centered, with the content kept to its leading edge.
    fileprivate func recipeColumn() -> some View {
        padding(.horizontal, DS.Space.l)
            .frame(maxWidth: DS.Size.readableWidth + 2 * DS.Space.l, alignment: .leading)
            .frame(maxWidth: .infinity)
    }
}

/// The recipe screen's four numbers: time, calories, protein, and difficulty.
///
/// One row when every value fits on one line; otherwise two rows of two (a narrow phone, the
/// Duo's cover screen, large text). A value never wraps or breaks mid-word.
struct RecipeStatsRow: View {
    let stats: [MenuFormat.Stat]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if !stats.isEmpty {
            Group {
                if dynamicTypeSize.isAccessibilitySize {
                    grid(columns: RecipeStatsLayout.fallbackColumns(count: stats.count))
                } else {
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .top, spacing: DS.Space.m) {
                            ForEach(stats) { stat in
                                cell(stat).frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        grid(columns: RecipeStatsLayout.fallbackColumns(count: stats.count))
                    }
                }
            }
            .padding(.vertical, DS.Space.s)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func grid(columns: Int) -> some View {
        Grid(alignment: .topLeading, horizontalSpacing: DS.Space.m, verticalSpacing: DS.Space.m) {
            ForEach(Array(stride(from: 0, to: stats.count, by: columns)), id: \.self) { start in
                GridRow {
                    ForEach(stats[start..<min(start + columns, stats.count)]) { stat in
                        cell(stat).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
    }

    private func cell(_ stat: MenuFormat.Stat) -> some View {
        VStack(alignment: .leading, spacing: DS.Space.xxs) {
            Text(stat.title)
                .font(DS.Typography.caption)
                .foregroundStyle(DS.Colors.secondaryText)
                .lineLimit(1)
            HStack(spacing: DS.Space.xxs) {
                Image(systemName: stat.systemImage)
                    .foregroundStyle(DS.Colors.badge)
                    .accessibilityHidden(true)
                Text(stat.value)
                    .foregroundStyle(Color.primary)
            }
            .font(DS.Typography.subheadline.weight(.semibold))
            .lineLimit(1)
            // Its whole width, so a word like "Medium" is never hyphenated.
            .fixedSize(horizontal: true, vertical: false)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(stat.title)
        .accessibilityValue(stat.accessibilityValue)
    }
}

/// How the recipe screen's stats wrap when one row is too narrow.
enum RecipeStatsLayout {
    /// Two columns, so four stats make two even rows; a single stat keeps one.
    static func fallbackColumns(count: Int) -> Int {
        min(max(count, 1), 2)
    }
}

/// A tag under the recipe's title: primary text on a gray capsule, so it reads in light and dark
/// (the plain menu chip's gray on gray was too faint here).
struct RecipeTagChip: View {
    let text: String

    var body: some View {
        Text(text)
            .font(DS.Typography.caption.weight(.semibold))
            .foregroundStyle(Color.primary)
            .padding(.horizontal, DS.Space.s)
            .padding(.vertical, DS.Space.xxs)
            .background(DS.Colors.fill, in: .capsule)
    }
}

/// No system scroll edge at the top: its blur showed the large title and chips as a ghost under
/// the bar. The page draws its own solid bar instead.
private struct RecipeScrollEdge: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content.scrollEdgeEffectHidden(true, for: .top)
        } else {
            content
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

/// Reloads the rendered steps when the recipe or the serving size changes.
private struct InstructionsKey: Equatable {
    let recipeID: String
    let servings: Int?
}

private struct ViewedKey: Equatable {
    let recipeID: String
    let isActive: Bool
}

/// Already in the shown week: the bottom bar is the −/+ servings stepper, not Add.
#Preview("Recipe, in your week") {
    NavigationStack {
        RecipeDetailView(summary: MenuPreviewData.cards[0].recipe)
    }
    .menuPreviewEnvironment()
}

/// Not in the week: the bottom bar offers servings and Add to Week.
#Preview("Recipe, not in your week") {
    NavigationStack {
        RecipeDetailView(summary: MenuPreviewData.cards[1].recipe)
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
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

/// When the recipe page's navigation bar shows over the header photo.
enum RecipeDetailHeader {
    /// What the page reads from its scroll view: how far it's scrolled, and how far down the bar
    /// reaches. Read together because the action only runs when the value changes, and the inset
    /// alone never does after the first frame.
    struct Scroll: Equatable {
        var offset: CGFloat
        var top: CGFloat
    }

    /// How far to push a pinned row down so it stays `top` points below the scroll view's top:
    /// nothing while it's still below that line.
    nonisolated static func stickyOffset(minY: CGFloat, top: CGFloat) -> CGFloat {
        max(0, top - minY)
    }

    /// The background wash at the top of the photo, behind the status bar and the buttons.
    static let scrimHeight: CGFloat = 140
    static let scrimOpacity: Double = 0.8

    /// The photo's height for a screen `width` wide: about 45% of a phone's height, and never
    /// taller than `DS.Size.heroMaxHeight` on an iPad or a phone on its side.
    static func heroHeight(width: CGFloat, isWide: Bool) -> CGFloat {
        let natural = max(240, min(width * 1.1, 420))
        return isWide ? min(natural, DS.Size.heroMaxHeight) : natural
    }

    /// Shows once the page is scrolled close to the end of the header, and hides again only
    /// after scrolling 40 points back, so it can't flicker at the line.
    static func showsBar(offset: CGFloat, heroHeight: CGFloat, showing: Bool) -> Bool {
        let line = heroHeight - 110
        return showing ? offset > line - 40 : offset > line
    }
}

extension View {
    /// Keeps the view at `top` points from the scroll view's top once it scrolls up to there,
    /// like a pinned section header. It only moves the drawing, so it never changes the layout.
    func stickyBelowTop(_ top: CGFloat) -> some View {
        visualEffect { content, proxy in
            let minY = proxy.frame(in: .scrollView).minY
            return content.offset(y: RecipeDetailHeader.stickyOffset(minY: minY, top: top))
        }
    }
}
