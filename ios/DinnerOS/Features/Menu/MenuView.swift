import SwiftUI

/// The Menu tab: the week strip, the week's meals, the server's carousels, and All Meals, with
/// the day-by-day week one toggle away.
///
/// `PlanStore` owns the shown week; the strip changes it and the menu follows, so By Day, the
/// grocery list, Autopilot, and the recipe screen all act on the same week.
struct MenuView: View {
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(HouseholdStore.self) private var households
    @Environment(RecipeLibrary.self) private var library
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(MealPlanner.self) private var planner
    @Environment(PairingsStore.self) private var pairings
    @Environment(MealSwapStore.self) private var swaps
    @Environment(EventReporter.self) private var events
    @Environment(AuthSession.self) private var session
    /// Optional so previews needn't supply one.
    @Environment(PlusStore.self) private var plus: PlusStore?
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    /// Optional so previews needn't supply them.
    @Environment(TabRouter.self) private var tabs: TabRouter?
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOverEnabled

    @State private var mode: MenuMode = .menu
    @State private var autopilotFlow = WeekAutopilotFlow()
    @State private var isAddingRecipes = false
    /// The "Add a Recipe" sheet: the choices (write, import, Shared Recipes, a companion).
    @State private var isAddingOwnRecipe = false
    /// The new-member welcome and what was picked on it (acted on once it closes, so two
    /// sheets never overlap).
    @State private var showsWelcome = false
    @State private var welcomeChoice: Welcome.Choice?
    private let welcomeStorage: any WelcomeStorage = UserDefaultsWelcomeStorage()
    @State private var actionError: String?
    /// The week menu's card, and where its button is (`DSMenuPopover`).
    @State private var showsWeekMenu = false
    @State private var weekMenuAnchor: CGRect = .zero
    /// Whether the Get Started card is up. It offers Shared Recipes itself, so the Menu's own
    /// Shared Recipes row waits until it's gone.
    @State private var isGetStartedShown = false

    /// Whether the week strip is showing its pills, and where the menu is scrolled to.
    @State private var collapse = MenuChromeCollapse()
    @State private var scrollOffset: CGFloat = 0
    /// Whether the reader is dragging or coasting the menu. Only then can the strip change form.
    @State private var readerScrolling = false
    /// The menu's tallest visible height, so a narrowed All Meals can fill it (see below).
    @State private var viewportHeight: CGFloat = 0
    /// The menu's width, for All Meals' iPad rows. Kept here, not in All Meals, which the lazy
    /// stack makes again as it scrolls (`AllMealsLayout.cardsWidth`).
    @State private var menuWidth: CGFloat = 0
    /// The meal search is open (All Meals' search chip), and the keyboard is up. Together they
    /// make room for the results above the keyboard (Hark's review item 7, build 122).
    @State private var isSearchOpen = false
    @State private var isKeyboardUp = false

    /// VoiceOver always gets the pills. Collapsing would take away an element a reader may be
    /// focused on, or be sitting next to, and the screen space it buys is not what someone
    /// moving element by element is short of.
    private var isStripExpanded: Bool {
        voiceOverEnabled || (collapse.isExpanded && !isSearchKeyboardUp)
    }

    /// Typing a meal search: the strip folds to its one line and results show as short rows, so
    /// a whole result fits between the chips and the keyboard, even on the Duo's cover screen.
    private var isSearchKeyboardUp: Bool { isSearchOpen && isKeyboardUp }

    /// Changes the strip's form, animated unless Reduce Motion is on (#458).
    private func changeStrip(_ change: () -> Void) {
        withAnimation(MenuScroll.collapseAnimation(reduceMotion: reduceMotion)) {
            change()
        }
    }

    private var household: Household? {
        households.current?.household
    }

    /// Whether this member still has the welcome to see for this household.
    private var welcomeDue: Bool {
        guard let user = session.currentUser, let household else { return false }
        return Welcome.shouldShow(
            accountCreated: user.createdAt,
            seen: welcomeStorage.hasSeen(userID: user.id, householdID: household.id))
    }

    /// Remembers the welcome was seen, however it closed, then acts on what was picked.
    private func finishWelcome() {
        if let user = session.currentUser, let household {
            welcomeStorage.markSeen(userID: user.id, householdID: household.id)
        }
        let choice = welcomeChoice
        welcomeChoice = nil
        showWaitingChecklist()
        switch choice {
        case .browseShared: tabs?.menuPath.append(DiscoverRoute())
        case .addRecipe: isAddingOwnRecipe = true
        case .startPlanning, nil: break
        }
    }

    /// Shows a waiting checklist unless the welcome is up or about to be.
    private func showWaitingChecklist() {
        guard let plus, plus.checklistWaiting, !showsWelcome, !welcomeDue else { return }
        plus.checklistWaiting = false
        Task {
            // Let a closing sheet finish first.
            try? await Task.sleep(for: .milliseconds(600))
            plus.showsChecklist = true
        }
    }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// Typing or pasting a recipe writes to the library, not the plan.
    private var canAddOwnRecipe: Bool {
        households.access?.can(.recipesEdit) == true
    }

    private var canAddMeals: Bool {
        canEdit && plans.isDraft && menu.selectedTiming != .past
    }

    var body: some View {
        followingTheWeek(content)
            .tabHeader(mode == .menu ? String(localized: "Menu") : String(localized: "By Day"))
            .safeAreaInset(edge: .top, spacing: 0) {
                WeekStrip(isExpanded: isStripExpanded) {
                    changeStrip { collapse.expand(offset: scrollOffset) }
                }
            }
            // By Day scrolls itself and reports no offset, so leave it showing the strip — and
            // come back to the menu showing it too, rather than in whatever form you left it.
            .onChange(of: mode) { _, _ in
                changeStrip { collapse.expand(offset: scrollOffset) }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if MenuFormat.showsShopBar(timing: menu.selectedTiming) {
                    MenuBottomBar()
                }
            }
            .overlay(alignment: .bottom) { toast }
            .recipeUndoToast()
            // Over the bottom bar too, so a tap anywhere outside the menu closes it. The rows are
            // read only while it's open, so Autopilot's state doesn't redraw the Menu otherwise.
            .dsMenuPopover(
                isPresented: $showsWeekMenu,
                anchor: weekMenuAnchor,
                items: showsWeekMenu ? weekMenuItems : []
            )
            .toolbar { toolbar }
            .notificationsToolbar()
            .modifier(MenuDestinations())
            .modifier(
                AddRecipeChooserSheet(
                    isPresented: $isAddingOwnRecipe, canAdd: canAddOwnRecipe,
                    onSaved: { recipe in
                        // The new recipe opens, and the menu picks it up behind it.
                        tabs?.menuPath = NavigationPath([
                            RecipeSummary.placeholder(
                                id: recipe.id, name: recipe.name, imageURLString: recipe.imageURLString)
                        ])
                        Task { await menu.reload() }
                    },
                    onBrowseShared: {
                        tabs?.menuPath = NavigationPath([DiscoverRoute()])
                    })
            )
            // The free month checklist's Add a Recipe.
            .onChange(of: tabs?.addRecipeRequested ?? false, initial: true) { _, requested in
                guard requested else { return }
                tabs?.addRecipeRequested = false
                if canAddOwnRecipe {
                    isAddingOwnRecipe = true
                } else {
                    tabs?.menuPath = NavigationPath([DiscoverRoute()])
                }
            }
            // Also when the week start day changes: the server moved meals between weeks.
            .task(id: household?.weekScope) {
                guard let household else { return }
                // A toast and its undo name this household's plan entries, so they don't
                // carry over to the next one.
                planner.activate(householdID: household.id)
                // The editor and quick add read serving sizes through the library's cache.
                async let recipes: Void = library.activate(householdID: household.id)
                async let menuLoad: Void = menu.activate(
                    householdID: household.id, timeZone: household.planningTimeZone,
                    weekStartsOn: household.weekStartsOn)
                await plans.activate(
                    householdID: household.id, timeZone: household.planningTimeZone,
                    weekStartsOn: household.weekStartsOn)
                await menuLoad
                await recipes
            }
            .dsSheet(isPresented: $isAddingRecipes) {
                AddRecipesSheet(week: plans.week)
            }
            // The free month checklist's Plan a Dinner.
            .onChange(of: tabs?.addMealsRequested ?? false, initial: true) { _, requested in
                guard requested else { return }
                tabs?.addMealsRequested = false
                if canAddMeals { isAddingRecipes = true }
            }
            .alert("Couldn’t Change the Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
            .alert("Couldn’t Change Your Meals", isPresented: plannerError) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(planner.errorMessage ?? "")
            }
            // "Try Something Similar" is offered from a meal's long-press menu in both Your
            // Meals and By Day, so the sheet belongs to the screen that holds both.
            .dsSheet(item: swapTarget) { target in
                MealSwapSheet(target: target)
            }
            .alert("Couldn’t Swap That Meal", isPresented: swapError) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(swaps.errorMessage ?? "")
            }
            .modifier(WeekAutopilotModifier(flow: autopilotFlow, canEdit: canEdit, holdsOnboarding: welcomeDue))
            .task(id: household?.id) {
                if welcomeDue { showsWelcome = true }
            }
            // A new household's free month checklist, after the welcome.
            .onChange(of: plus?.checklistWaiting ?? false, initial: true) { _, _ in
                showWaitingChecklist()
            }
            .modifier(MenuAutoRefresh())
            .dsSheet(isPresented: $showsWelcome, onDismiss: finishWelcome) {
                WelcomeView(
                    hasRecipes: !library.items.isEmpty,
                    canAddRecipe: households.access?.can(.recipesEdit) == true
                ) { choice in
                    welcomeChoice = choice
                    showsWelcome = false
                }
            }
    }

    /// The menu, its suggestions, and the role follow the week the strip shows. Kept out of
    /// `body` so the compiler checks it quickly.
    private func followingTheWeek(_ content: some View) -> some View {
        content
            // The strip and every other screen change `PlanStore`'s week; the menu follows it.
            .onChange(of: plans.week) { _, week in
                Task { await menu.select(week: week) }
            }
            // The week's suggestions follow the same week, and reload when its meals change:
            // a meal added or removed changes what's offered with it.
            .task(id: PairingsKey(householdID: household?.id, week: plans.week)) {
                guard let householdID = household?.id else { return }
                await pairings.showWeek(plans.week, householdID: householdID)
            }
            .onChange(of: plans.plan?.entries.map(\.id) ?? []) { _, _ in
                Task { await pairings.reload() }
            }
            .onChange(of: pairings.isForbidden) { _, isForbidden in
                // The role changed elsewhere; reload it so the rest of the app matches rather
                // than leaving the suggestions hidden until the next launch.
                if isForbidden {
                    Task { await households.load() }
                }
            }
    }

    @ViewBuilder
    private var content: some View {
        switch mode {
        case .menu:
            menuContent
        case .byDay:
            WeekView(flow: autopilotFlow)
                .tabBarEdge()
        }
    }

    /// Anchors All Meals so a new filter can bring its first page back into view.
    private static let allMealsAnchor = "all-meals"

    private var menuContent: some View {
        ScrollViewReader { proxy in
            ScrollView {
                // A plain stack, not a lazy one. Its rows are a dozen sections of very different
                // heights (Your Meals, a one-card carousel, the Shared Recipes row, All Meals),
                // and a lazy stack guesses the ones it hasn't laid out from the ones it has.
                // Scrolling toward All Meals on an iPad in landscape, each guess moved the
                // content, which changed what was laid out and so the next guess: the main
                // thread laid the Menu out again every frame and never got free (QA, build 122;
                // the same with every card, photo, shimmer and All Meals change ruled out one at
                // a time). Every section here is measured, so nothing is guessed. All Meals, the
                // one long list, stays lazy inside its own section.
                VStack(alignment: .leading, spacing: DS.Space.xxl) {
                    if let refreshError = menu.refreshError {
                        FormErrorLabel(message: refreshError)
                            .padding(.horizontal, DS.Space.l)
                    }
                    // Above the week, not under All Meals: a household with nothing in its
                    // library has nothing to plan, so getting recipes in comes first (#530).
                    // A new household's free month checklist, until it's done or hidden.
                    ReferralMenuCard()
                    GetStartedSection(
                        onAddOwnRecipe: { isAddingOwnRecipe = true },
                        onShownChange: { isGetStartedShown = $0 })
                    YourMealsSection(flow: autopilotFlow)
                    // Shown whenever the Get Started card isn't, so recipes other households
                    // shared are never hidden in a menu, and never offered twice.
                    if MenuEntryPoints.showsSharedRecipesRow(getStartedShown: isGetStartedShown) {
                        SharedRecipesEntry()
                    }
                    PairingSuggestionsSection()
                    sections
                    if menu.selectedTiming != .past {
                        AllMealsSection(
                            list: menu.allMeals, canAdd: canAddMeals, containerWidth: menuWidth,
                            isKeyboardUp: isKeyboardUp, onSearchChange: { isSearchOpen = $0 }
                        )
                        // A search or filter can leave All Meals one card long at the bottom of
                        // the menu. Closing the keyboard then left too little page to stay
                        // scrolled there, and the results slid away (seen on iPad). Filling a
                        // screen keeps them in place.
                        .frame(
                            minHeight: menu.allMeals.query.isNarrowed ? viewportHeight : nil,
                            alignment: .top
                        )
                        .id(Self.allMealsAnchor)
                    }
                }
                .padding(.top, DS.Space.m)
                .padding(.bottom, DS.Space.xxl)
            }
            // Room for the floating Done above the keyboard, in the bar's color, so Done never
            // sits on a result and no photo is cut by the keyboard bar's edge. Inside the tab
            // bar scrim, so its fade lands on this strip and not on the first result.
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if isSearchKeyboardUp {
                    Rectangle()
                        .fill(DS.Colors.barBackground)
                        .frame(height: DS.Size.keyboardBar)
                        .accessibilityHidden(true)
                }
            }
            .tabBarEdge()
            // A drag on the list puts the meal search's keyboard away, and Done above it does
            // when the results are too short to drag. The keyboard covers the tab bar.
            .scrollDismissesKeyboard(.immediately)
            .toolbar {
                ToolbarItemGroup(placement: .keyboard) {
                    Spacer()
                    // Filled, so it reads on its own and not against the photo under the
                    // keyboard's floating bar (white on yellow, Hark's review item 10).
                    Button("Done") { Keyboard.dismiss() }
                        .buttonStyle(.dsPrimary(.small))
                }
            }
            // The scroll view's width doesn't depend on what's in it, so this can't loop.
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                menuWidth = $0
            }
            // How far the menu is scrolled decides the week strip's form, and nothing else
            // reads it; `MenuChromeCollapse` holds the rule.
            .onScrollGeometryChange(for: MenuScrollReading.self) { geometry in
                // Read together: the action only runs on a change, and the height alone rarely
                // changes after the first frame.
                MenuScrollReading(
                    offset: geometry.contentOffset.y + geometry.contentInsets.top,
                    height: geometry.containerSize.height)
            } action: { _, reading in
                let offset = reading.offset
                // The tallest the menu has been, not its height right now: with the keyboard up
                // the menu is short, and a minimum taken then let the page shrink back out from
                // under the results when the keyboard closed (iPad, landscape).
                if reading.height > viewportHeight { viewportHeight = reading.height }
                scrollOffset = offset
                let reader = readerScrolling
                changeStrip { collapse.update(offset: offset, readerScrolling: reader) }
            }
            .onScrollPhaseChange { _, phase in
                readerScrolling = MenuScroll.isReaderScrolling(phase)
            }
            // Closing the keyboard during a search could leave the menu scrolled back to the
            // top on iPad in landscape, away from the results. Bring All Meals back once the
            // keyboard is gone. The strip's collapse is left alone: holding it here made the
            // strip open and lose its week.
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
                isKeyboardUp = true
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
                isKeyboardUp = false
            }
            // Results as rows: bring the first one up under the chips, clear of the keyboard.
            .onChange(of: isSearchKeyboardUp) { _, typing in
                guard typing else { return }
                changeStrip { collapse.hold() }
                withAnimation(MenuScroll.animation(reduceMotion: reduceMotion)) {
                    proxy.scrollTo(Self.allMealsAnchor, anchor: .top)
                }
                Task {
                    try? await Task.sleep(for: MenuScroll.jumpSettle)
                    collapse.release(offset: scrollOffset)
                }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidHideNotification)) { _ in
                guard menu.allMeals.query.isNarrowed else { return }
                withAnimation(MenuScroll.animation(reduceMotion: reduceMotion)) {
                    proxy.scrollTo(Self.allMealsAnchor, anchor: .top)
                }
            }
            // Changing a filter reloads All Meals from its first page. Without this the
            // screen stays scrolled past that page and looks empty until you scroll back.
            .onChange(of: menu.allMeals.query) { _, _ in
                // The jump is not the reader scrolling, so the strip must not read it as a
                // gesture and collapse in the same instant they asked for different meals.
                changeStrip { collapse.hold() }
                withAnimation(MenuScroll.animation(reduceMotion: reduceMotion)) {
                    proxy.scrollTo(Self.allMealsAnchor, anchor: .top)
                }
                Task {
                    try? await Task.sleep(for: MenuScroll.jumpSettle)
                    collapse.release(offset: scrollOffset)
                }
            }
        }
        .refreshable {
            let menu = menu
            let plans = plans
            let autopilot = autopilot
            await PlanRefresh.uncancellable {
                async let menuReload: Void = menu.reload()
                async let week: Void = plans.reload()
                await autopilot.reloadWeek()
                await week
                await menuReload
            }
        }
    }

    /// The week's meals marked made, this device's answers included before they reach the server.
    private var madeRecipeIDs: Set<String> {
        Set((plans.plan?.entries ?? []).filter { events.outcome(for: $0) == .cooked }.map(\.recipe.id))
    }

    @ViewBuilder
    private var sections: some View {
        switch menu.phase {
        case .idle, .loading:
            VStack(alignment: .leading, spacing: DS.Space.m) {
                MenuSectionHeader(title: String(localized: "Loading meals…"))
                    .padding(.horizontal, DS.Space.l)
                MenuCardPlaceholders()
                    .padding(.horizontal, DS.Space.l)
            }
        case .failed(let message):
            MenuSectionError(message: message) {
                Task { await menu.retry() }
            }
            .padding(.horizontal, DS.Space.l)
        case .loaded:
            // The server chooses the sections and their order; new ones need no change here.
            let made = madeRecipeIDs
            ForEach((menu.menu?.sections ?? []).map { $0.droppingMade(made) }.filter { !$0.items.isEmpty }) {
                section in
                MenuSectionView(section: section, canAdd: canAddMeals && section.kind != .history)
            }
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

    /// The meal being re-picked, which presents the sheet. Dismissing it clears the store, so
    /// a swipe down leaves nothing half-open.
    private var swapTarget: Binding<MealSwapStore.Target?> {
        Binding(
            get: { swaps.target },
            set: { target in
                if target == nil { swaps.cancel() }
            })
    }

    private var swapError: Binding<Bool> {
        Binding(
            get: { swaps.errorMessage != nil },
            set: { presented in
                if !presented { swaps.errorMessage = nil }
            })
    }

    private var plannerError: Binding<Bool> {
        Binding(
            get: { planner.errorMessage != nil },
            set: { presented in
                if !presented { planner.errorMessage = nil }
            })
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if #available(iOS 26.0, *) {
            modeItem
                // The segmented control draws its own capsule. The bar's glass around it drew a
                // second, offset outline and clipped the thumb flat on an iPad (Hark's review
                // item 7, builds 121 and 122).
                .sharedBackgroundVisibility(.hidden)
        } else {
            modeItem
        }
        // Every tab's toolbar reads bell, +, more (`TabHeader`): the bell comes from
        // `.notificationsToolbar()`, outside this one.
        ToolbarItemGroup(placement: .topBarTrailing) {
            if plans.isSaving {
                ProgressView()
            }
            if canEdit {
                Button("Add Recipes", systemImage: TabHeader.addSystemImage) {
                    isAddingRecipes = true
                }
                .disabled(!canAddMeals)
            }
            weekMenu
        }
    }

    /// Menu or By Day. On an iPad the principal item gets a row of its own under the tab bar;
    /// leading puts the switch in the tab bar's row instead, a whole row back for the meals.
    private var modeItem: some ToolbarContent {
        ToolbarItem(placement: horizontalSizeClass == .regular ? .topBarLeading : .principal) {
            // The system's segmented control. A custom SwiftUI capsule control here (build 121,
            // Hark's review item 7) froze the Menu on an iPad in landscape (QA, build 122).
            Picker("View", selection: $mode) {
                ForEach(MenuMode.allCases) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .fixedSize()
        }
    }

    /// The week's status for planners, and Autopilot for everyone: an opaque card under the
    /// button (`DSMenuPopover`), like the recipe page's. The system menu's glass let the photos
    /// under it show through, about 3:1 in dark mode, and on the iPhone Duo's cover screen it
    /// spread across the button rail (Hark's review items 10 and 11, build 122).
    private var weekMenu: some View {
        DSMenuButton(isPresented: $showsWeekMenu, anchor: $weekMenuAnchor, accessibilityLabel: "Week Menu") {
            Image(systemName: TabHeader.moreSystemImage)
        }
    }

    private var weekMenuItems: [DSMenuItem] {
        var items: [DSMenuItem] = []
        if canEdit, let plan = plans.plan {
            if plan.status == .draft {
                items.append(
                    DSMenuItem(
                        title: String(localized: "Finalize Week"), systemImage: "lock",
                        isDisabled: plan.entries.isEmpty
                    ) { perform { try await plans.setStatus(.finalized) } })
            } else {
                items.append(
                    DSMenuItem(title: String(localized: "Reopen Week"), systemImage: "lock.open") {
                        perform { try await plans.setStatus(.draft) }
                    })
            }
        }
        items += WeekAutopilotMenuItems.items(
            flow: autopilotFlow, canEdit: canEdit, autopilot: autopilot, plans: plans)
        var recipes = [
            DSMenuItem(title: SharedRecipes.title, systemImage: SharedRecipes.systemImage, group: "") {
                tabs?.menuPath.append(DiscoverRoute())
            }
        ]
        if canAddOwnRecipe {
            recipes.append(
                DSMenuItem(title: String(localized: "Add a Recipe"), systemImage: "square.and.pencil") {
                    isAddingOwnRecipe = true
                })
        }
        items += recipes
        items.append(
            DSMenuItem(title: String(localized: "Past Weeks"), systemImage: "clock.arrow.circlepath", group: "") {
                tabs?.menuPath.append(PastWeeksRoute())
            })
        items.append(
            DSMenuItem(title: String(localized: "Companions"), systemImage: "takeoutbag.and.cup.and.straw") {
                tabs?.menuPath.append(CompanionsRoute())
            })
        return items
    }

    /// The household and week the suggestions belong to.
    private struct PairingsKey: Equatable {
        let householdID: String?
        let week: ISOWeek
    }

    private func perform(_ change: @escaping () async throws -> Void) {
        Task {
            do {
                try await change()
            } catch is CancellationError {
                return
            } catch {
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// How the Menu moves itself.
nonisolated enum MenuScroll {
    /// The slide to All Meals after a filter changes, or `nil` under Reduce Motion — the jump
    /// still happens, because landing on the new first page is the point; only the sliding is
    /// the part someone asked us not to do.
    static func animation(reduceMotion: Bool) -> Animation? {
        reduceMotion ? nil : .easeInOut(duration: 0.2)
    }

    /// The week strip's change between its pills and its one-line form, or `nil` under Reduce
    /// Motion — the strip still changes, because the space is the point; the sliding is the part
    /// someone turned the setting off for, exactly as for the jump above (#458).
    static func collapseAnimation(reduceMotion: Bool) -> Animation? {
        reduceMotion ? nil : .easeInOut(duration: 0.22)
    }

    /// How long the jump to All Meals is given to settle before scroll offsets decide the
    /// strip's form again. Longer than the jump's own animation, so every offset it produces is
    /// absorbed rather than read as a gesture.
    static let jumpSettle = Duration.milliseconds(350)

    /// A finger on the list, or the coast after it. Not idle, and not an animated jump.
    static func isReaderScrolling(_ phase: ScrollPhase) -> Bool {
        switch phase {
        case .tracking, .interacting, .decelerating: true
        default: false
        }
    }
}

#Preview("Menu") {
    NavigationStack {
        MenuView()
    }
    .menuPreviewEnvironment()
}

#Preview("Menu, dark") {
    NavigationStack {
        MenuView()
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}

#Preview("Past week") {
    NavigationStack {
        MenuView()
    }
    .menuPreviewEnvironment(menu: MenuPreviewData.pastMenu)
}

#Preview("Accessibility size") {
    NavigationStack {
        MenuView()
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility2)
}

/// Reloads the shown week every `PlanRefresh.interval` while the Menu is on screen and the app is
/// in front: a kitchen iPad left on the Menu for days still sees what a phone changed.
private struct MenuAutoRefresh: ViewModifier {
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content.task(id: scenePhase == .active) {
            guard scenePhase == .active else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: PlanRefresh.interval)
                guard !Task.isCancelled else { return }
                async let week: Void = plans.reload()
                await menu.reload()
                await week
            }
        }
    }
}

/// Where the menu's links go. Kept apart from the menu's body so the compiler checks it
/// quickly.
/// The Menu tab's pages. Each gets the solid band under an iPad's tab bar (`TabBarBacking`),
/// which has to go on the page, inside the stack, so the back and more buttons draw over it.
private struct MenuDestinations: ViewModifier {
    func body(content: Content) -> some View {
        content
            .navigationDestination(for: RecipeSummary.self) { summary in
                RecipeDetailView(summary: summary)
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: HouseholdRatingsRoute.self) { route in
                HouseholdRatingsView(route: route)
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: GroceryListRoute.self) { route in
                GroceryListView(week: route.week)
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: AllMealsRoute.self) { route in
                AllMealsView(route: route)
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: PastWeeksRoute.self) { _ in
                PastWeeksView()
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: CompanionsRoute.self) { _ in
                CompanionsListView()
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: DiscoverRoute.self) { _ in
                DiscoverView()
                    .tabBarBacking(.top)
            }
            .navigationDestination(for: CatalogRecipeRoute.self) { route in
                CatalogRecipeDetailView(route: route)
                    .tabBarBacking(.top)
            }
    }
}

/// What the menu reads from its scroll view: how far it's scrolled and how tall it is.
struct MenuScrollReading: Equatable {
    var offset: CGFloat
    var height: CGFloat
}
