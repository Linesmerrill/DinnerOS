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
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOverEnabled

    @State private var mode: MenuMode = .menu
    @State private var autopilotFlow = WeekAutopilotFlow()
    @State private var isAddingRecipes = false
    @State private var actionError: String?

    /// Whether the week strip is showing its pills, and where the menu is scrolled to.
    @State private var collapse = MenuChromeCollapse()
    @State private var scrollOffset: CGFloat = 0

    /// VoiceOver always gets the pills. Collapsing would take away an element a reader may be
    /// focused on, or be sitting next to, and the screen space it buys is not what someone
    /// moving element by element is short of.
    private var isStripExpanded: Bool {
        voiceOverEnabled || collapse.isExpanded
    }

    /// Changes the strip's form, animated unless Reduce Motion is on (#458).
    private func changeStrip(_ change: () -> Void) {
        withAnimation(MenuScroll.collapseAnimation(reduceMotion: reduceMotion)) {
            change()
        }
    }

    private var household: Household? {
        households.current?.household
    }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var canAddMeals: Bool {
        canEdit && plans.isDraft && menu.selectedTiming != .past
    }

    var body: some View {
        content
            .navigationTitle(mode == .menu ? "Menu" : "By Day")
            .navigationBarTitleDisplayMode(.inline)
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
            .safeAreaInset(edge: .bottom, spacing: 0) { MenuBottomBar() }
            .overlay(alignment: .bottom) { toast }
            .toolbar { toolbar }
            .notificationsToolbar()
            .navigationDestination(for: RecipeSummary.self) { summary in
                RecipeDetailView(summary: summary)
            }
            .navigationDestination(for: HouseholdRatingsRoute.self) { route in
                HouseholdRatingsView(route: route)
            }
            .navigationDestination(for: GroceryListRoute.self) { route in
                GroceryListView(week: route.week)
            }
            .navigationDestination(for: AllMealsRoute.self) { route in
                AllMealsView(route: route)
            }
            .navigationDestination(for: PastWeeksRoute.self) { _ in
                PastWeeksView()
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
            .sheet(isPresented: $isAddingRecipes) {
                AddRecipesSheet(week: plans.week)
            }
            .alert("Couldn't Change the Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
            .alert("Couldn't Change Your Meals", isPresented: plannerError) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(planner.errorMessage ?? "")
            }
            .modifier(WeekAutopilotModifier(flow: autopilotFlow, canEdit: canEdit))
    }

    @ViewBuilder
    private var content: some View {
        switch mode {
        case .menu:
            menuContent
        case .byDay:
            WeekView(flow: autopilotFlow)
        }
    }

    /// Anchors All Meals so a new filter can bring its first page back into view.
    private static let allMealsAnchor = "all-meals"

    private var menuContent: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 28) {
                    if let refreshError = menu.refreshError {
                        FormErrorLabel(message: refreshError)
                            .padding(.horizontal, 16)
                    }
                    YourMealsSection(flow: autopilotFlow)
                    PairingSuggestionsSection()
                    sections
                    if menu.selectedTiming != .past {
                        AllMealsSection(list: menu.allMeals, canAdd: canAddMeals)
                            .id(Self.allMealsAnchor)
                    }
                }
                .padding(.top, 12)
                .padding(.bottom, 24)
            }
            // How far the menu is scrolled decides the week strip's form, and nothing else
            // reads it; `MenuChromeCollapse` holds the rule.
            .onScrollGeometryChange(for: CGFloat.self) { geometry in
                geometry.contentOffset.y + geometry.contentInsets.top
            } action: { _, offset in
                scrollOffset = offset
                changeStrip { collapse.update(offset: offset) }
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
            async let menuReload: Void = menu.reload()
            async let week: Void = plans.reload()
            await autopilot.reloadWeek()
            await week
            await menuReload
        }
    }

    @ViewBuilder
    private var sections: some View {
        switch menu.phase {
        case .idle, .loading:
            VStack(alignment: .leading, spacing: 12) {
                MenuSectionHeader(title: String(localized: "Loading meals…"))
                    .padding(.horizontal, 16)
                MenuCardPlaceholders()
                    .padding(.horizontal, 16)
            }
        case .failed(let message):
            MenuSectionError(message: message) {
                Task { await menu.retry() }
            }
            .padding(.horizontal, 16)
        case .loaded:
            // The server chooses the sections and their order; new ones need no change here.
            ForEach(menu.menu?.sections ?? []) { section in
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

    private var plannerError: Binding<Bool> {
        Binding(
            get: { planner.errorMessage != nil },
            set: { presented in
                if !presented { planner.errorMessage = nil }
            })
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .principal) {
            Picker("View", selection: $mode) {
                ForEach(MenuMode.allCases) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 220)
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            if plans.isSaving {
                ProgressView()
            }
            weekMenu
            if canEdit {
                Button("Add Recipes", systemImage: "plus") {
                    isAddingRecipes = true
                }
                .disabled(!canAddMeals)
            }
        }
    }

    /// The week's status for planners, and Autopilot for everyone.
    private var weekMenu: some View {
        Menu {
            if canEdit, let plan = plans.plan {
                Section {
                    if plan.status == .draft {
                        Button("Finalize Week", systemImage: "lock") {
                            perform { try await plans.setStatus(.finalized) }
                        }
                        .disabled(plan.entries.isEmpty)
                    } else {
                        Button("Reopen Week", systemImage: "lock.open") {
                            perform { try await plans.setStatus(.draft) }
                        }
                    }
                }
            }
            WeekAutopilotMenuItems(flow: autopilotFlow, canEdit: canEdit)
            Section {
                NavigationLink(value: PastWeeksRoute()) {
                    Label("Past Weeks", systemImage: "clock.arrow.circlepath")
                }
            }
        } label: {
            Label("Week Menu", systemImage: "ellipsis.circle")
        }
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
