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
    @State private var heroWidth: CGFloat = 0
    /// The scroll view's height: the photo never takes more than about 45% of it, so the name
    /// shows whole above the bottom bar when the page opens (the Duo's short cover screen).
    @State private var pageHeight: CGFloat = 0
    @State private var showsNavigationBar = false
    /// Whether the large title has reached the bar's edge, so the bar shows the name on one line
    /// instead: never a sliver of the large title with no name in the bar.
    @State private var showsCompactTitle = false
    /// The large title's and the tab picker's tops in the page's content, for the scroll's snap
    /// points (`RecipeHeaderSnap`).
    @State private var titleContentTop: CGFloat = 0
    /// The large title's top on screen now (the scroll view's coordinates), for the bar's fade.
    @State private var titleVisibleTop: CGFloat = .infinity
    @State private var pickerContentTop: CGFloat = 0
    /// The fade under the bar: full while the picker is far below it, none once the picker pins
    /// flush under the bar (it has its own).
    @State private var barFade: CGFloat = DS.Size.edgeFade
    /// The fade under the pinned picker: a row tall once rows scroll under it.
    @State private var pickerFade: CGFloat = RecipeDetailHeader.sectionGap
    /// Where the section's fading rows are, so a page that stops with one part way into the
    /// picker's fade can move to show it whole.
    @State private var rowFrames = DSRowFrames(space: RecipeDetailHeader.contentSpace)
    /// The page has moved once to show a half-faded row whole since it was last touched.
    @State private var rowsSettled = false
    /// Where the page is scrolled to now, read when the tab changes (a plain box, so scrolling
    /// never redraws the page for it).
    @State private var scrollState = RecipeScrollState()
    @State private var pickerHeight: CGFloat = 0
    /// The scroll view's own top safe area: where the bar ends.
    @State private var scrollTop: CGFloat = 0
    /// The picker and the sections under it, as tall as they are (`sectionsMinHeight`).
    @State private var sectionsHeight: CGFloat = 0
    /// The scroll view's height less the bottom bar: what the page shows at once.
    @State private var visibleHeight: CGFloat = 0
    /// The bottom bar's inset on the scroll view, for where the scroll can end (`RecipeHeaderSnap`).
    @State private var bottomInset: CGFloat = 0
    /// Moves the page to the end of the snap zone when a drag stops inside it (`settle`).
    @State private var scrollPosition = ScrollPosition(edge: .top)
    /// The one-line title's row under the bar's buttons.
    @ScaledMetric(relativeTo: .headline) private var compactTitleHeight: CGFloat = 36
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
    /// The More menu: open or not, and where its button is, so the card opens under it.
    @State private var showsMore = false
    @State private var moreAnchor = CGRect.zero
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
                        .onGeometryChange(for: RecipeDetailHeader.Anchor.self) { proxy in
                            RecipeDetailHeader.Anchor(proxy, in: RecipeDetailHeader.contentSpace)
                        } action: { anchor in
                            if titleContentTop != anchor.content { titleContentTop = anchor.content }
                            if titleVisibleTop != anchor.visible { titleVisibleTop = anchor.visible }
                            let shows = anchor.visible < titleLine - 0.5
                            if shows != showsCompactTitle { showsCompactTitle = shows }
                        }
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
                    // The picker and everything under it, at least a screen tall once the page
                    // scrolls, so the picker can always pin and the large title never rests half
                    // under the bar on a short tab (Hark’s review, build 122 round 5).
                    VStack(alignment: .leading, spacing: DS.Space.xl) {
                        RecipeDetailTabPicker(tabs: availableTabs, selection: tabSelection, fadeHeight: pickerFade)
                            .onGeometryChange(for: RecipeDetailHeader.Anchor.self) { proxy in
                                RecipeDetailHeader.Anchor(proxy, in: RecipeDetailHeader.contentSpace)
                            } action: { anchor in
                                let fade = RecipeDetailHeader.barFade(
                                    pickerTop: anchor.visible, pinLine: pinLine, titleTop: titleVisibleTop)
                                if fade != barFade { barFade = fade }
                            }
                            .onGeometryChange(for: CGFloat.self) { proxy in
                                proxy.size.height
                            } action: { height in
                                if height != pickerHeight { pickerHeight = height }
                            }
                            .qaCover(pinnedAt: pinLine, fade: pickerFade)
                            .stickyBelowTop(pinLine)
                            .zIndex(1)
                        // Right under the picker: a fade's height of room here left the Duo's
                        // cover screen with nothing but blank space above the bottom bar (Hark's
                        // review, build 123 item 4). The picker's fade at rest is that same room,
                        // so the first row is whole once the picker pins.
                        sectionContent
                            .recipeColumn()
                            // Its rows fade out whole before they reach the pinned picker's edge.
                            .environment(\.dsBarEdge, rowFadeEdge)
                            .environment(\.dsRowFrames, rowFrames)
                            // Once rows scroll up under the pinned picker, its fade grows to a row's
                            // height, so the top row is never cut by a hard edge (Hark's review,
                            // build 122 item 1c). Measured here: the picker's own frame moves with it
                            // once it pins.
                            .onGeometryChange(for: CGFloat.self) { proxy in
                                proxy.frame(in: .scrollView).minY
                            } action: { top in
                                let fade = RecipeDetailHeader.pickerFade(
                                    sectionTop: top, pinLine: pinLine, pickerHeight: pickerHeight)
                                if fade != pickerFade { pickerFade = fade }
                            }
                            .padding(.top, RecipeDetailHeader.sectionGap - DS.Space.xl)
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
                    .onGeometryChange(for: CGFloat.self) { proxy in
                        proxy.size.height
                    } action: { height in
                        if height != sectionsHeight { sectionsHeight = height }
                    }
                    // Where the picker sits unpinned, read from the stack that holds it: the
                    // picker's own frame follows it as it pins, which moved the snap zone's end
                    // with the scroll and left a short tab resting on half the title.
                    .onGeometryChange(for: CGFloat.self) { proxy in
                        proxy.frame(in: .named(RecipeDetailHeader.contentSpace)).minY
                    } action: { top in
                        if pickerContentTop != top { pickerContentTop = top }
                    }
                    .frame(minHeight: sectionsMinHeight, alignment: .top)
                }
                // Clear of the bottom bar's fade, so the last row comes out of it whole.
                .padding(.bottom, Self.bottomPadding)
            }
            .coordinateSpace(.named(RecipeDetailHeader.contentSpace))
        }
        .scrollTargetBehavior(RecipeHeaderSnap(zone: snapZone, bottomInset: bottomInset))
        .scrollPosition($scrollPosition)
        // A drag let go without a flick never asks the snap where to stop, and a short tab came
        // to rest with the large title half under the bar (Hark's review, build 122 round 5):
        // once the page stops, it settles at the nearer end.
        .onScrollPhaseChange { _, phase, context in
            if phase == .interacting { rowsSettled = false }
            guard phase == .idle else { return }
            let geometry = context.geometry
            let reachable = RecipeDetailHeader.reachable(
                contentHeight: geometry.contentSize.height, containerHeight: geometry.containerSize.height,
                bottomInset: geometry.contentInsets.bottom)
            if let target = RecipeDetailHeader.settle(
                offset: geometry.contentOffset.y, zone: snapZone, reachable: reachable)
            {
                withAnimation(.snappy) { scrollPosition.dsScroll(toY: target) }
            } else if !rowsSettled,
                let shift = DSRowEdgeFade.restingShift(
                    rows: rowFrames.rows(scrolled: geometry.contentOffset.y + geometry.contentInsets.top),
                    barEdge: rowFadeEdge,
                    maxForward: reachable - geometry.contentOffset.y),
                let shift = RecipeDetailHeader.keepingPinned(
                    shift, offset: geometry.contentOffset.y, zone: snapZone)
            {
                // At rest no row sits part way into the picker's fade: it moves to show the row
                // whole (Hark's review, build 122 round 5: the Allergens line rested at 1.5:1).
                // Once a scroll, also after the snap above has moved it.
                rowsSettled = true
                withAnimation(.snappy) {
                    scrollPosition.dsScroll(toY: geometry.contentOffset.y + geometry.contentInsets.top + shift)
                }
            } else if pickerFade != RecipeDetailHeader.sectionGap {
                // At rest the fade is back to its resting height, clear of the first whole row.
                withAnimation(.snappy) { pickerFade = RecipeDetailHeader.sectionGap }
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
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.size.height
        } action: { height in
            pageHeight = height
        }
        // A wash of the page's own background behind the status bar, the buttons and (on iPad)
        // the tab bar while the photo is under them, so their glyphs reach 4.5:1 and the status
        // bar takes one style over any photo. Pinned to the screen, not the photo: it scrolled
        // up with the photo and left the buttons and clock on bare photo (Hark's review, build
        // 122 round 4). The Duo's cover screen has its buttons and clock in a column beside the
        // page, so it needs none.
        .overlay(alignment: .top) {
            if !showsBar && !hasSideColumn {
                // On iPad the photo starts at the bar's edge (the bar holds the tabs): the bar is
                // the page's own solid color down to that edge and the photo starts clean below
                // it. Easing the color out over the photo left a gray band on a pale photo in
                // dark mode (Hark's review, build 122 round 5).
                if scrollTop > 0 {
                    DS.Colors.background
                        .frame(height: barBottom)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                        .ignoresSafeArea(edges: .top)
                } else {
                    RecipeHeroScrim(barBottom: barBottom, easeFrom: barBottom - DS.Space.xxl)
                        .ignoresSafeArea(edges: .top)
                }
            }
        }
        // Once the photo has scrolled away, a solid bar and, under the buttons, the recipe's
        // name on one line. Nothing that scrolls under it shows through or gets cut mid-line;
        // the pinned picker sits flush below it.
        .overlay(alignment: .top) {
            if showsBar {
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
                .qaCover(.bar)
                // The large title and the rows under it fade out at the bar's edge, never cut.
                .dsEdgeFade(.bottom, height: barFade)
                .ignoresSafeArea(edges: .top)
                .allowsHitTesting(false)
            }
        }
        // The page has nothing to type in.
        .ignoresSafeArea(.keyboard)
        .background(DS.Colors.background)
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: RecipeDetailHeader.Scroll.self) { geometry in
            RecipeDetailHeader.Scroll(
                offset: geometry.contentOffset.y, top: geometry.contentInsets.top,
                // The whole container: on iOS 27 it already leaves out the bottom bar and the tab
                // bar, and taking the inset off again left a short tab 165 points short of
                // pinning its picker on a large iPhone, resting with "No ratings yet" half under
                // the bar (Hark's review, build 121 round 2). Where it doesn't, a short tab only
                // gets a little more room at its end.
                visibleHeight: geometry.containerSize.height,
                bottom: geometry.contentInsets.bottom)
        } action: { _, scroll in
            let offset = scroll.offset
            scrollState.offset = offset
            if pinTop != scroll.top { pinTop = scroll.top }
            if visibleHeight != scroll.visibleHeight { visibleHeight = scroll.visibleHeight }
            if bottomInset != scroll.bottom { bottomInset = scroll.bottom }
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
        // Over the bottom bar too, so a tap anywhere outside the menu closes it.
        .dsMenuPopover(isPresented: $showsMore, anchor: moreAnchor, items: moreItems)
        .onChange(of: showsCooking) { if showsCooking { showsMore = false } }
        // Another tab picked while the picker is pinned starts at its top, right under the
        // picker: it opened wherever the last tab had been scrolled to, its heading under the
        // picker and its first row half faded (Hark's review, build 121 round 2: Calories on the
        // Duo, Serves 2 on Ingredients).
        .onChange(of: shownTab) {
            guard let target = RecipeDetailHeader.tabStart(offset: scrollState.offset, zone: snapZone) else { return }
            rowsSettled = true
            scrollPosition.dsScroll(toY: target)
        }
        .recipeDeletion($deleting, isPlanned: { _ in !plannedEntries.isEmpty }) { dismiss() }
        .fullScreenCover(isPresented: $showsCooking) {
            CookingView(start: cookMeal, meals: weekMeals)
        }
        .dsSheet(isPresented: $showsEditor) {
            if let recipe {
                RecipeEditView(recipe: recipe, servings: servings) { saved in
                    show(saved)
                    Task { await loadInstructions() }
                }
            }
        }
        .dsSheet(isPresented: $showsNutrition) {
            if let recipe {
                NutritionEditorSheet(recipe: recipe) { saved in show(saved) }
            }
        }
        // Attached to the screen's root, never inside a section (decision 510).
        .dsSheet(isPresented: $showsSpecialtySetup, onDismiss: { Task { await loadInstructions() } }) {
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
            heroWidth = width
        }
    }

    /// About 45% of the screen's height, from the width the screen gives us.
    private var heroHeight: CGFloat {
        RecipeDetailHeader.heroHeight(
            width: heroWidth, isWide: isWideScreen, pageHeight: pageHeight, stackedBar: hasSideColumn)
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

    /// Where the section's rows fade out under the pinned picker.
    private var rowFadeEdge: CGFloat {
        RecipeDetailHeader.rowFadeEdge(pinLine: pinLine, pickerHeight: pickerHeight)
    }

    /// Where the large title starts to go under the bar: the room above it (the photo's gap)
    /// below the bar's edge. Past that the one-line title shows in the bar.
    private var titleLine: CGFloat {
        pinLine + RecipeDetailHeader.titleGap
    }

    /// iPhone Duo's cover screen: the bar's buttons and the clock sit in a column beside the
    /// page, so nothing is drawn at the top until the name moves there.
    private var hasSideColumn: Bool {
        startCookingPlacement == .bottomRow
    }

    /// The solid bar over the page's top: once the photo has scrolled away from under the
    /// buttons, and on the Duo only once it holds the name, so it's never an empty band.
    private var showsBar: Bool {
        showsNavigationBar && (!hasSideColumn || showsCompactTitle)
    }

    /// Where a scroll may come to rest between the large title and the pinned picker
    /// (`RecipeHeaderSnap`).
    private var snapZone: ClosedRange<CGFloat>? {
        RecipeDetailHeader.snapZone(
            titleTop: titleContentTop, pickerTop: pickerContentTop, pinLine: pinLine, fade: RecipeDetailHeader.titleGap)
    }

    /// The least height for the picker and the sections under it (`RecipeDetailHeader.sectionsMinHeight`).
    private var sectionsMinHeight: CGFloat {
        RecipeDetailHeader.sectionsMinHeight(
            sectionsTop: pickerContentTop, sectionsHeight: sectionsHeight, bottomPadding: Self.bottomPadding,
            visibleHeight: visibleHeight, pinLine: pinLine, zone: snapZone)
    }

    /// Room under the last section, clear of the bottom bar's fade.
    private static let bottomPadding = DS.Size.edgeFade + DS.Space.s

    /// Whether the bottom bar has the −/+ servings stepper: then the Ingredients tab names the
    /// size without a second control for it (Hark's review, build 122 round 5).
    private var barHasServings: Bool {
        RecipePlanBarLayout.make(
            isPlanned: !plannedEntries.isEmpty, canEdit: households.access?.can(.planEdit) == true,
            isDraft: plans.isDraft, isPastWeek: menu.selectedTiming == .past,
            hasStartCooking: recipe?.steps.isEmpty == false, startCookingInBar: startCookingPlacement != .topBar,
            stacksStartCooking: startCookingPlacement == .bottomRow
        )?.row.contains(.stepper) == true
    }

    /// "Autopilot pick" or "Make Again" from the card this recipe was opened from.
    private var heroBadge: MenuBadge? {
        RecipeTagChips.heroBadge(card?.badges ?? [])
    }

    private var shareURL: URL? {
        recipe?.sourceURL
    }

    /// Nutrition can be typed in on a recipe the household added.
    private func canAddNutrition(_ recipe: Recipe) -> Bool {
        canEditRecipe && (recipe.source == RecipeSourceKey.typed || recipe.source == RecipeSourceKey.fromPage)
    }

    /// The More menu: an opaque card under the button, Delete Recipe in red, kept inside the
    /// page (Hark's review item 8). Not the system menu, whose glass showed Start Cooking and the
    /// photo through it; not a popover, which a toolbar rebuild left unable to close (build 121).
    private var moreButton: some View {
        DSMenuButton(isPresented: $showsMore, anchor: $moreAnchor) {
            Image(systemName: TabHeader.moreSystemImage)
                .modifier(HeroToolbarButton(needsBackground: !showsNavigationBar))
        }
    }

    private var moreItems: [DSMenuItem] {
        var items: [DSMenuItem] = []
        if let shareURL {
            items.append(
                DSMenuItem(title: "Share", systemImage: "square.and.arrow.up") { [name, moreAnchor] in
                    DSShareSheet.present(shareURL, subject: name, anchor: moreAnchor)
                })
        }
        if recipe != nil, canEditRecipe {
            items.append(DSMenuItem(title: "Edit Recipe", systemImage: "pencil") { showsEditor = true })
            items.append(
                DSMenuItem(title: "Delete Recipe", systemImage: "trash", role: .destructive) { deleting = summary })
        }
        return items
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
            // Ratings only for a meal that's been cooked (Hark's review item 12).
            if let recipe, hasBeenCooked {
                HouseholdRatingSummary(recipe: recipe)
            }
        }
    }

    /// Whether this recipe has been cooked, so the page asks how it was.
    private var hasBeenCooked: Bool {
        guard let recipe else { return false }
        let weekHasStarted = plans.week <= plans.currentWeek
        return RecipeRatingVisibility.hasBeenCooked(
            myRating: recipe.myRating, householdRating: recipe.householdRating, orderWeeks: recipe.orderWeeks,
            currentWeek: plans.currentWeek,
            nightHasPassed: weekHasStarted && plannedEntries.contains { MealNight.hasPassed(date: $0.date) })
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
        return RecipeTagChips.shown((recipe?.tags ?? summary.tags) + cuisines, badge: heroBadge?.text)
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
                RecipeDescriptionSection(recipe: recipe, hasBeenCooked: hasBeenCooked)
            case .ingredients:
                RecipeIngredientsSection(
                    recipe: recipe, servings: $servings, showsServingPicker: !barHasServings,
                    instructions: instructions, canEdit: canLeaveOut,
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
    /// How far the fade under it reaches (`DSEdgeFade.height(scrolledUnder:)`).
    var fadeHeight: CGFloat = DS.Size.edgeFade

    @ScaledMetric(relativeTo: .subheadline) private var height: CGFloat = 32
    /// Each tab's name at full size, set in the selected tab's heavier weight (the wider one), so
    /// the shared scale doesn't change as the selection moves.
    @State private var labelWidths: [RecipeDetailTab: CGFloat] = [:]
    /// The width each tab gets: the row shares its width equally.
    @State private var segmentWidth: CGFloat = 0

    /// Room between a tab's name and its pill's edge.
    static let labelPadding: CGFloat = DS.Space.s
    /// The smallest the names shrink to before they'd be cut.
    static let minimumLabelScale: CGFloat = 0.6

    /// One scale for every tab's name, so they all shrink together and read as one size: each
    /// name shrank on its own before, and a short one ("Nutrition") stayed bigger than the rest
    /// on a small phone (Hark's review, build 121 round 2). The scale that fits the widest name
    /// in its tab, never above full size.
    nonisolated static func sharedScale(labelWidths: [CGFloat], segmentWidth: CGFloat) -> CGFloat {
        let room = segmentWidth - 2 * labelPadding
        guard room > 0, let widest = labelWidths.max(), widest > room else { return 1 }
        return max(room / widest, minimumLabelScale)
    }

    private var labelScale: CGFloat {
        Self.sharedScale(labelWidths: tabs.compactMap { labelWidths[$0] }, segmentWidth: segmentWidth)
    }

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
                        // Its full width, shrunk by the one scale all the names share.
                        .fixedSize()
                        .scaleEffect(labelScale)
                        .foregroundStyle(Color.primary)
                        // Clear of the selected pill's rounded ends (Hark's review, build 123).
                        .padding(.horizontal, RecipeDetailTabPicker.labelPadding)
                        // Every tab the same width, whatever its name's full width.
                        .frame(minWidth: 0, maxWidth: .infinity, minHeight: height)
                        .background {
                            // The name's full width in the heavier weight, measured unseen.
                            Text(tab.title)
                                .font(DS.Typography.subheadline.weight(.semibold))
                                .lineLimit(1)
                                .fixedSize()
                                .hidden()
                                .onGeometryChange(for: CGFloat.self) { proxy in
                                    proxy.size.width
                                } action: { width in
                                    if labelWidths[tab] != width { labelWidths[tab] = width }
                                }
                        }
                        .onGeometryChange(for: CGFloat.self) { proxy in
                            proxy.size.width
                        } action: { width in
                            if tab == tabs.first, segmentWidth != width { segmentWidth = width }
                        }
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
        // Rows fade out under the pinned picker instead of being cut by a hard line. The page
        // reports the picker and this fade to the UI tests where it pins (`qaCover(pinnedAt:)`).
        .dsEdgeFade(.bottom, height: fadeHeight, reportsCover: false)
    }
}

/// What the More menu runs once it has closed.
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

/// Which tags show as chips under the recipe's title.
nonisolated enum RecipeTagChips {
    /// Imported tags carry the source's own slugs ("latin-american-faves", "seo"): the ones a
    /// person would recognize, spelled out, each once, up to five. A tag that says the same as the
    /// photo's badge (`badge`, "Quick") is left out, so it shows once (build 122 round 5).
    static func shown(_ values: [String], badge: String? = nil) -> [String] {
        var seen = Set<String>()
        if let badge { seen.insert(badge.lowercased()) }
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

    /// The badge on the photo: "Autopilot pick", then "Make Again", then any other, but never
    /// "Quick": the page already says how long it takes and Autopilot calls it quick under
    /// Description, so the photo said it a second time (Hark's review, build 122 round 5).
    static func heroBadge(_ badges: [MenuBadge]) -> MenuBadge? {
        let shown = badges.filter { $0.code != .quick }
        return shown.first { $0.code == .autopilotPick } ?? shown.first { $0.code == .makeAgain } ?? shown.first
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

/// Where the recipe page is scrolled to, kept outside SwiftUI's state so reading it as the page
/// scrolls redraws nothing.
@MainActor final class RecipeScrollState {
    var offset: CGFloat = 0
}

/// When the recipe page's navigation bar shows over the header photo.
enum RecipeDetailHeader {
    /// What the page reads from its scroll view: how far it's scrolled, and how far down the bar
    /// reaches. Read together because the action only runs when the value changes, and the inset
    /// alone never does after the first frame.
    struct Scroll: Equatable {
        var offset: CGFloat
        var top: CGFloat
        /// What the page shows at once, at most: the scroll view's container height.
        var visibleHeight: CGFloat = 0
        var bottom: CGFloat = 0
    }

    /// How far to push a pinned row down so it stays `top` points below the scroll view's top:
    /// nothing while it's still below that line.
    nonisolated static func stickyOffset(minY: CGFloat, top: CGFloat) -> CGFloat {
        max(0, top - minY)
    }

    /// The page's content, for the title's and the picker's places in it.
    static let contentSpace = "RecipeDetailContent"

    /// Where a view sits: in the scroll view as it's scrolled now, and in the page's content.
    nonisolated struct Anchor: Equatable, Sendable {
        var visible: CGFloat
        var content: CGFloat

        init(visible: CGFloat, content: CGFloat) {
            self.visible = visible
            self.content = content
        }

        init(_ proxy: GeometryProxy, in space: String) {
            visible = proxy.frame(in: .scrollView).minY
            content = proxy.frame(in: .named(space)).minY
        }
    }

    /// The fade under the bar: the room between the bar's edge and the picker, up to
    /// `DS.Size.edgeFade`, so it never draws over the picker as the picker comes up to pin.
    ///
    /// Near the large title (`titleTop`, on screen) it's no taller than the title's distance
    /// from the bar's edge either way: resting whole just under the bar, the title keeps the
    /// photo's gap clear above it and the photo is wholly under the bar. A fade the full height
    /// there showed a strip of the photo through it between the bar and the title on the Duo
    /// (Hark's review, build 121 round 2). As the title goes under the bar the fade grows again.
    static func barFade(pickerTop: CGFloat, pinLine: CGFloat, titleTop: CGFloat = .infinity) -> CGFloat {
        min(max(pickerTop - pinLine, 0), DS.Size.edgeFade, abs(titleTop - pinLine))
    }

    /// Where a newly picked tab starts: with the picker just pinned (the zone's end) when the
    /// page was scrolled past that, so the tab's first row shows whole under it. `nil` when the
    /// picker isn't pinned: the page stays put.
    nonisolated static func tabStart(offset: CGFloat, zone: ClosedRange<CGFloat>?) -> CGFloat? {
        guard let zone, offset > zone.upperBound + 0.5 else { return nil }
        return zone.upperBound
    }

    /// The room between the photo and the large title (the title's top padding): where the
    /// title rests just under the bar, so the photo ends right at the bar's edge.
    nonisolated static let titleGap: CGFloat = DS.Space.l

    /// A resting move (`DSRowEdgeFade.restingShift`) that never takes a page whose picker has
    /// pinned (`offset` at or past the zone's end) back to where the picker comes loose: the
    /// page moved back past it and rested with the large title's last line under the bar
    /// (Hark's review, build 121 round 2). `nil` when nothing is left to move.
    nonisolated static func keepingPinned(_ shift: CGFloat, offset: CGFloat, zone: ClosedRange<CGFloat>?)
        -> CGFloat?
    {
        guard let zone, offset >= zone.upperBound - 0.5 else { return shift }
        let kept = max(offset + shift, zone.upperBound) - offset
        return abs(kept) > 0.5 ? kept : nil
    }

    /// The room between the picker and the section under it: the section starts right under the
    /// picker's own padding (Hark's review, build 123 item 4).
    nonisolated static let sectionGap: CGFloat = DS.Space.s

    /// The fade under the pinned picker, from where the section under it (`sectionTop`,
    /// `sectionGap` below the picker when nothing has scrolled under it) is now: that gap at
    /// rest, so it never covers the first row, growing to a row's height
    /// (`DS.Size.rowEdgeFade`) once rows scroll under.
    nonisolated static func pickerFade(sectionTop: CGFloat, pinLine: CGFloat, pickerHeight: CGFloat) -> CGFloat {
        let scrolledUnder = pinLine + pickerHeight + sectionGap - sectionTop
        return min(sectionGap + max(scrolledUnder, 0), DS.Size.rowEdgeFade)
    }

    /// The edge the section's rows fade out at (`dsBarEdge`), placed so a row resting `sectionGap`
    /// under the pinned picker is whole and one gone under the picker's own padding is gone.
    nonisolated static func rowFadeEdge(pinLine: CGFloat, pickerHeight: CGFloat) -> CGFloat {
        pinLine + pickerHeight + sectionGap - DS.Size.edgeFade
    }

    /// The scroll offsets between "the large title sits whole just under the bar" and "the
    /// picker has pinned": a scroll that ends in there settles at one end, so the page never
    /// rests on a sliver of the large title (Hark's review item 6). `nil` when there's no room.
    static func snapZone(titleTop: CGFloat, pickerTop: CGFloat, pinLine: CGFloat, fade: CGFloat)
        -> ClosedRange<CGFloat>?
    {
        let start = titleTop - pinLine - fade
        let end = pickerTop - pinLine
        guard start > 0, end > start else { return nil }
        return start...end
    }

    /// The least height for the picker and the sections under it (starting `sectionsTop` down the
    /// page's content and `sectionsHeight` tall as they are), so that a page that scrolls into
    /// `zone` can always scroll on until the picker pins at `pinLine`: then a short tab (Nutrition
    /// with no values) never comes to rest with the large title half under the bar. 0 when the
    /// page already reaches that far, or never scrolls into the zone (it fits on the screen).
    static func sectionsMinHeight(
        sectionsTop: CGFloat, sectionsHeight: CGFloat, bottomPadding: CGFloat, visibleHeight: CGFloat,
        pinLine: CGFloat, zone: ClosedRange<CGFloat>?
    ) -> CGFloat {
        guard let zone, visibleHeight > 0, sectionsHeight > 0 else { return 0 }
        let reachable = sectionsTop + sectionsHeight + bottomPadding - visibleHeight
        guard reachable > zone.lowerBound, reachable < zone.upperBound else { return 0 }
        // With the picker pinned, the sections fill the page from the picker's top to the end.
        return (visibleHeight - pinLine - bottomPadding).rounded(.up)
    }

    /// The zone, ending no further than the page can scroll: a page too short to pin the picker
    /// settles at its own end.
    nonisolated static func clamped(_ zone: ClosedRange<CGFloat>, reachable: CGFloat) -> ClosedRange<CGFloat> {
        zone.lowerBound...max(zone.lowerBound, min(zone.upperBound, reachable))
    }

    /// Where a page that stopped at `offset` should move to: the nearer end of the zone when it
    /// stopped inside it, nil when it's already outside or at an end.
    nonisolated static func settle(offset: CGFloat, zone: ClosedRange<CGFloat>?, reachable: CGFloat) -> CGFloat? {
        guard let zone else { return nil }
        let target = snapped(offset, zone: clamped(zone, reachable: reachable), velocity: 0)
        return abs(target - offset) > 0.5 ? target : nil
    }

    /// The farthest a page scrolls: its content and the bottom bar's inset, less the screen.
    nonisolated static func reachable(contentHeight: CGFloat, containerHeight: CGFloat, bottomInset: CGFloat)
        -> CGFloat
    {
        contentHeight + bottomInset - containerHeight
    }

    /// Where a scroll ending at `target` settles: unchanged outside `zone`; inside it, at the end
    /// it was heading for (`velocity` is positive scrolling down the page), or the nearer end
    /// when it was let go without a flick.
    nonisolated static func snapped(_ target: CGFloat, zone: ClosedRange<CGFloat>, velocity: CGFloat) -> CGFloat {
        guard target > zone.lowerBound, target < zone.upperBound else { return target }
        if velocity > 0 { return zone.upperBound }
        if velocity < 0 { return zone.lowerBound }
        return target - zone.lowerBound < zone.upperBound - target ? zone.lowerBound : zone.upperBound
    }

    /// The photo's height for a screen `width` wide: about 45% of a phone's height, and never
    /// taller than `DS.Size.heroMaxHeight` on an iPad or a phone on its side.
    ///
    /// On a short screen it also stays within `heroPageShare` of the page's `pageHeight`, so the
    /// recipe's name isn't left under the bottom bar when the page opens (build 122).
    ///
    /// Under a two-row bottom bar (`stackedBar`, the Duo's cover screen) it takes less again, so
    /// the open tab's first lines show above the bar's fade (Hark's review, build 123 item 4).
    static func heroHeight(width: CGFloat, isWide: Bool, pageHeight: CGFloat = 0, stackedBar: Bool = false)
        -> CGFloat
    {
        let floor = stackedBar ? stackedBarMinHero : 240
        let share = stackedBar ? stackedBarPageShare : heroPageShare
        var natural = max(floor, min(width * 1.1, 420))
        if pageHeight > 0 { natural = max(floor, min(natural, pageHeight * share)) }
        return isWide ? min(natural, DS.Size.heroMaxHeight) : natural
    }

    /// The most of the page's height the photo takes on a phone.
    static let heroPageShare: CGFloat = 0.45
    /// The same under a two-row bottom bar, and the least it gets there.
    static let stackedBarPageShare: CGFloat = 0.35
    static let stackedBarMinHero: CGFloat = 160

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

/// Settles a scroll that ends between the large title and the pinned picker at one or the other
/// (`RecipeDetailHeader.snapped`).
nonisolated struct RecipeHeaderSnap: ScrollTargetBehavior {
    let zone: ClosedRange<CGFloat>?
    /// The bottom bar's inset: the scroll ends that much further down. Left out, a page given
    /// room to pin its picker was still held short of it (build 122 round 5).
    var bottomInset: CGFloat = 0

    func updateTarget(_ target: inout ScrollTarget, context: TargetContext) {
        guard let zone else { return }
        let reachable = RecipeDetailHeader.reachable(
            contentHeight: context.contentSize.height, containerHeight: context.containerSize.height,
            bottomInset: bottomInset)
        target.rect.origin.y = RecipeDetailHeader.snapped(
            target.rect.origin.y, zone: RecipeDetailHeader.clamped(zone, reachable: reachable),
            velocity: context.velocity.dy)
    }
}

/// The wash at the top of the recipe's photo: `DS.Colors.heroScrim` down to the middle of the
/// bar's buttons, then easing out to `DS.Size.heroScrimFade` below the bar on a cosine curve, flat
/// at both ends, so it never ends in a visible band (Hark's review, build 122 round 4).
struct RecipeHeroScrim: View {
    let barBottom: CGFloat
    /// Where the ease starts: about the middle of the bar's buttons (half a button above the bar's
    /// edge), so they still sit on a near-full wash.
    var easeFrom: CGFloat? = nil
    /// The wash's color at full strength: `DS.Colors.heroScrim` over a photo under the buttons.
    var color: Color = DS.Colors.heroScrim

    var body: some View {
        let height = barBottom + DS.Size.heroScrimFade
        LinearGradient(
            stops: Self.stops(barBottom: barBottom, easeFrom: easeFrom).map {
                .init(color: color.opacity($0.opacity), location: $0.location)
            },
            startPoint: .top, endPoint: .bottom
        )
        .frame(height: height)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    /// How many steps the eased fade is drawn with: enough that no step shows.
    static let fadeSteps = 12

    /// The wash's opacity (of `heroScrim`) by location, 0 at the top and 1 at the wash's end:
    /// full down to `easeFrom` (the bar's edge if not given), then a cosine ease to clear.
    static func stops(barBottom: CGFloat, easeFrom: CGFloat? = nil) -> [(location: CGFloat, opacity: Double)] {
        let height = barBottom + DS.Size.heroScrimFade
        guard height > 0 else { return [(0, 1), (1, 0)] }
        let start = min(max(easeFrom ?? barBottom, 0), max(barBottom, 0)) / height
        var stops: [(location: CGFloat, opacity: Double)] = [(0, 1)]
        for step in 0...fadeSteps {
            let t = Double(step) / Double(fadeSteps)
            stops.append((start + (1 - start) * CGFloat(t), (1 + cos(.pi * t)) / 2))
        }
        return stops
    }
}
