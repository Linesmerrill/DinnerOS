import SwiftUI

/// The recipe screen's bottom bar, in one layout everywhere: a row of equal-width controls at the
/// design system's button height (`RecipePlanBarLayout`).
///
/// Before the recipe is in the shown week: Pick a Day and Add to Week (the serving size is
/// chosen on the Ingredients tab). Once it is: the day and a −/+ servings stepper over the same `PlanStore` the cards use. Plus moves to the
/// recipe's next serving size up, minus to the next one down, and minus at the smallest size
/// removes the meal with an undo toast. When the same recipe is planned more than once, the
/// stepper acts on the most recently added meal.
///
/// Start Cooking, when the bar has it, is always the filled button: beside the others on iPad,
/// in a row of its own above them on iPhone Duo's cover screen, and alone once the week is set.
/// When the row is too narrow for everything on one line, the controls stack instead of
/// cutting their labels short.
struct RecipePlanBar: View {
    let summary: RecipeSummary
    let recipe: Recipe?
    let entries: [PlanEntry]
    @Binding var servings: Int?
    let selections: [PlanCustomizationRequest.Selection]
    /// Opens the cooking screen.
    var startCooking: (() -> Void)?
    /// Shows Start Cooking in this bar even while planning. On iPad the top bar shares its
    /// space with the tab bar, and iOS folded Start Cooking into an overflow menu there.
    var showsStartCooking = false
    /// Puts Start Cooking in its own row above the planning controls rather than beside them:
    /// iPhone Duo's cover screen is too narrow for both in one row.
    var stacksStartCooking = false

    @Environment(PlanStore.self) private var plans
    @Environment(MenuStore.self) private var menu
    @Environment(MealPlanner.self) private var planner
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var isChoosingWeek = false
    /// The day Add to Week puts the meal on, picked before it's added.
    @State private var pickedDay: PlanDay?

    private var entry: PlanEntry? { entries.last }

    private var options: [Int] { recipe?.servingOptions ?? [] }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// A past week can't be planned; the sheet offers the weeks that can.
    private var isPastWeek: Bool {
        menu.selectedTiming == .past
    }

    private var isBusy: Bool {
        planner.busyRecipeIDs.contains(summary.id) || entry.map { planner.busyEntryIDs.contains($0.id) } == true
    }

    private var layout: RecipePlanBarLayout? {
        RecipePlanBarLayout.make(
            isPlanned: entry != nil, canEdit: canEdit, isDraft: plans.isDraft, isPastWeek: isPastWeek,
            hasStartCooking: startCooking != nil, startCookingInBar: showsStartCooking,
            stacksStartCooking: stacksStartCooking)
    }

    var body: some View {
        if let layout {
            DSBottomBar {
                if layout.cookRowOnTop, let startCooking {
                    cookButton(startCooking)
                }
                if !layout.row.isEmpty {
                    row(layout.row)
                }
                if layout.showsFinalizedNote {
                    Text("This week is finalized. Reopen it to change the meals.")
                        .font(DS.Typography.caption)
                        .foregroundStyle(DS.Colors.secondaryText)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            // Rows fade into the bar instead of being cut mid-line at its edge.
            .overlay(alignment: .top) {
                LinearGradient(
                    colors: [DS.Colors.barBackground.opacity(0), DS.Colors.barBackground],
                    startPoint: .top, endPoint: .bottom
                )
                .frame(height: DS.Space.l)
                .offset(y: -DS.Space.l)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
            .sheet(isPresented: $isChoosingWeek) {
                AddEntrySheet(recipeID: summary.id, recipeName: summary.name, fixedWeek: nil)
            }
        }
    }

    /// One line when everything fits at its full label; then one line without the menus'
    /// icons (a small phone); otherwise one control per line. Labels are never cut short.
    @ViewBuilder
    private func row(_ controls: [RecipePlanBarLayout.Control]) -> some View {
        let stacked = VStack(spacing: DS.Space.s) {
            ForEach(controls, id: \.self) { control($0) }
        }
        if dynamicTypeSize.isAccessibilitySize || controls.count == 1 {
            stacked
        } else {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: DS.Space.m) {
                    ForEach(controls, id: \.self) { control($0) }
                }
                HStack(spacing: DS.Space.s) {
                    ForEach(controls, id: \.self) { control($0, compact: true) }
                }
                stacked
            }
        }
    }

    @ViewBuilder
    private func control(_ control: RecipePlanBarLayout.Control, compact: Bool = false) -> some View {
        switch control {
        case .day:
            dayMenu(compact: compact)
        case .stepper:
            if let entry { stepper(entry) }
        case .add(let primary):
            addButton(primary: primary)
        case .startCooking:
            if let startCooking { cookButton(startCooking) }
        }
    }

    private func cookButton(_ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Label("Start Cooking", systemImage: "frying.pan")
        }
        .buttonStyle(.dsPrimary(fullWidth: true))
    }

    private func addButton(primary: Bool) -> some View {
        Button {
            add()
        } label: {
            if isBusy {
                ProgressView()
            } else {
                Text(isPastWeek ? "Add to Week…" : "Add to Week")
            }
        }
        .modifier(PlanBarButtonStyle(primary: primary))
        .disabled(isBusy || (!plans.isDraft && !isPastWeek) || options.isEmpty)
        .contextMenu {
            Button("Add to Another Week…", systemImage: "calendar") {
                isChoosingWeek = true
            }
        }
    }

    /// The day, before and after the meal is in the week: before, the day Add to Week puts it
    /// on; after, the day it's on, with a way to take it off the week.
    private func dayMenu(compact: Bool) -> some View {
        let day = entry.map(\.day) ?? pickedDay
        return Menu {
            Section(
                day?.title(in: plans.week, weekStartsOn: plans.weekStartsOn) ?? String(localized: "No Day Yet")
            ) {
                ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { option in
                    Button(option.name()) { pick(option) }
                        .disabled(day == option)
                }
                Button("No Day Yet") { pick(nil) }
                    .disabled(day == nil)
            }
            if let entry {
                Button("Remove from Week", systemImage: "trash", role: .destructive) {
                    Task { await planner.remove(entry) }
                }
            }
        } label: {
            PlanBarMenuLabel(text: RecipePlanBarLayout.dayLabel(day?.name()), systemImage: compact ? nil : "calendar")
        }
        .buttonStyle(.dsOutline(fullWidth: true))
        .disabled(entry != nil && !plans.isDraft)
        .accessibilityLabel("Day")
        .accessibilityValue(day?.name() ?? String(localized: "No day yet"))
    }

    private func pick(_ day: PlanDay?) {
        if let entry {
            Task { await planner.move(entry, to: day) }
        } else {
            pickedDay = day
        }
    }

    private func stepper(_ entry: PlanEntry) -> some View {
        PlanBarStepper(
            label: MenuFormat.servings(entry.servings),
            spokenLabel: MenuFormat.planCount(entries.count, servings: entry.servings),
            isBusy: isBusy,
            canIncrease: ServingSizes.step(from: entry.servings, options: options, by: 1) != nil,
            decrease: { change(entry, by: -1) },
            increase: { change(entry, by: 1) }
        )
        .disabled(!plans.isDraft)
    }

    private func add() {
        guard !isPastWeek else {
            isChoosingWeek = true
            return
        }
        Task {
            await planner.add(
                recipeID: summary.id, name: summary.name, to: plans.week, day: pickedDay, servings: servings,
                selections: selections)
        }
    }

    private func change(_ entry: PlanEntry, by delta: Int) {
        Task {
            await planner.changeServings(entry, by: delta)
            if let updated = planner.entries(recipeID: summary.id).last {
                servings = updated.servings
            }
        }
    }
}

/// What the recipe screen's bottom bar holds, worked out from the plan and the screen, so every
/// case uses the same row of controls (build 121).
nonisolated struct RecipePlanBarLayout: Equatable {
    enum Control: Hashable {
        /// The day it goes on, or is on: "Pick a Day" until there is one.
        case day
        /// − servings +, for the planned meal.
        case stepper
        /// Add to Week: filled when it's the main action, tinted beside Start Cooking.
        case add(primary: Bool)
        /// Always the filled button.
        case startCooking
    }

    /// Start Cooking in a full-width row above `row` (iPhone Duo's cover screen).
    var cookRowOnTop = false
    /// The controls on one line, each the same width and height.
    var row: [Control] = []
    /// "This week is finalized." under the controls.
    var showsFinalizedNote = false

    /// The day menu's label: the day, or "Pick a Day" until there is one.
    static func dayLabel(_ dayName: String?) -> String {
        dayName ?? String(localized: "Pick a Day")
    }

    /// `nil` when the bar has nothing to show.
    static func make(
        isPlanned: Bool, canEdit: Bool, isDraft: Bool, isPastWeek: Bool, hasStartCooking: Bool,
        startCookingInBar: Bool, stacksStartCooking: Bool
    ) -> RecipePlanBarLayout? {
        let weekIsSet = !isDraft && !isPastWeek
        // The week is set: nothing to plan here, so the bar is for cooking.
        if hasStartCooking, isPlanned, weekIsSet {
            return RecipePlanBarLayout(row: [.startCooking])
        }
        let cookInBar = hasStartCooking && startCookingInBar
        if canEdit {
            var layout = RecipePlanBarLayout(showsFinalizedNote: weekIsSet)
            if isPlanned {
                layout.row = [.day, .stepper]
            } else {
                layout.row = [.day, .add(primary: !cookInBar)]
            }
            if cookInBar {
                if stacksStartCooking {
                    layout.cookRowOnTop = true
                } else {
                    layout.row.append(.startCooking)
                }
            }
            return layout
        }
        if cookInBar {
            return RecipePlanBarLayout(row: [.startCooking])
        }
        return nil
    }
}

/// Filled for the main action, tinted otherwise, both at the bar's one height and full width.
private struct PlanBarButtonStyle: ViewModifier {
    let primary: Bool

    func body(content: Content) -> some View {
        if primary {
            content.buttonStyle(.dsPrimary(fullWidth: true))
        } else {
            content.buttonStyle(.dsSecondary(fullWidth: true))
        }
    }
}

/// A bar menu's label: an icon, the value, and the up-down chevrons that say it's a choice.
private struct PlanBarMenuLabel: View {
    let text: String
    /// Left out where the bar is tight.
    var systemImage: String?

    var body: some View {
        HStack(spacing: DS.Space.xs) {
            if let systemImage {
                Image(systemName: systemImage)
                    .accessibilityHidden(true)
            }
            Text(text)
            Image(systemName: "chevron.up.chevron.down")
                .font(DS.Typography.caption)
                .accessibilityHidden(true)
        }
        .foregroundStyle(Color.primary)
    }
}

/// "− 2 servings +" at the bar's button height, tinted like `.dsSecondary` so Start Cooking stays
/// the one filled button. The label never truncates: the bar stacks its controls first.
private struct PlanBarStepper: View {
    let label: String
    /// What VoiceOver reads: "1 in your week (2 servings)".
    let spokenLabel: String
    var isBusy = false
    var canIncrease = true
    let decrease: () -> Void
    let increase: () -> Void

    @ScaledMetric(relativeTo: .body) private var height = DS.ButtonSize.large.height
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        HStack(spacing: 0) {
            button(systemImage: "minus", label: String(localized: "Fewer Servings"), action: decrease)
            Group {
                if isBusy {
                    ProgressView()
                } else {
                    Text(label)
                        .font(DS.Typography.button)
                        .monospacedDigit()
                        .lineLimit(1)
                        .fixedSize()
                }
            }
            .frame(maxWidth: .infinity)
            button(systemImage: "plus", label: String(localized: "More Servings"), action: increase)
                .disabled(!canIncrease)
                .opacity(canIncrease ? 1 : 0.4)
        }
        .foregroundStyle(isEnabled ? DS.Colors.accent : DS.Colors.disabledText)
        .frame(minHeight: height)
        .background(isEnabled ? DS.Colors.accentFill : DS.Colors.disabledFill, in: .capsule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("Servings"))
        .accessibilityValue(Text(spokenLabel))
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: if canIncrease { increase() }
            case .decrement: decrease()
            @unknown default: break
            }
        }
    }

    private func button(systemImage: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(DS.Typography.button)
                .frame(minWidth: DS.Size.tapTarget, minHeight: DS.Size.tapTarget)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .disabled(isBusy)
    }
}

#Preview("Not in the week") {
    NavigationStack {
        DS.Colors.background
            .safeAreaInset(edge: .bottom, spacing: 0) {
                RecipePlanBar(
                    summary: MenuPreviewData.cards[1].recipe, recipe: RecipePreviewData.recipe, entries: [],
                    servings: .constant(2), selections: [])
            }
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
}

#Preview("In the week") {
    NavigationStack {
        DS.Colors.background
            .safeAreaInset(edge: .bottom, spacing: 0) {
                RecipePlanBar(
                    summary: MenuPreviewData.cards[0].recipe, recipe: RecipePreviewData.recipe,
                    entries: Array(PlanPreviewData.plan.entries.prefix(1)), servings: .constant(4), selections: [])
            }
    }
    .menuPreviewEnvironment()
}
