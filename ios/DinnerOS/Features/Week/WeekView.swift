import SwiftUI

/// The Menu tab's **By Day** view: the shown week's plan grouped by day, with cooked and skip
/// swipes, day changes, and removal.
///
/// The Menu screen owns the week strip, the toolbar's week menu, and Autopilot's sheets, and
/// passes its `WeekAutopilotFlow` in, so both views act on the same week and the same flow.
struct WeekView: View {
    let flow: WeekAutopilotFlow

    @Environment(PlanStore.self) private var plans
    @Environment(HouseholdStore.self) private var households
    @Environment(EventReporter.self) private var events
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(MealSwapStore.self) private var swaps
    @Environment(MealPlanner.self) private var planner

    @State private var editingEntry: PlanEntry?
    @State private var isAddingRecipes = false
    @State private var actionError: String?

    /// Whether the role may change plans. Hiding controls is a convenience; the API
    /// enforces `plan.edit`.
    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var canEditEntries: Bool {
        canEdit && plans.isDraft
    }

    var body: some View {
        content
            .dsSheet(item: $editingEntry) { entry in
                PlanEntryEditor(entry: entry, week: plans.week)
            }
            .dsSheet(isPresented: $isAddingRecipes) {
                AddRecipesSheet(week: plans.week)
            }
            .alert("Couldn’t Change the Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        switch plans.phase {
        case .idle, .loading:
            ProgressView("Loading the week…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn’t Load This Week", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await plans.reload() }
                }
                .buttonStyle(.dsPrimary(.small))
            }
        case .loaded:
            if let plan = plans.plan {
                planList(plan)
            }
        }
    }

    private func planList(_ plan: Plan) -> some View {
        ScrollViewReader { proxy in
            dayList(plan)
                // Opens on today, with the meals still waiting for a day just above it, not on
                // days already gone: on a Saturday the week's meals were below the fold under
                // five empty days (build 122 review 4). On the week's first day the top of the
                // list already is today.
                .task(id: plans.week) {
                    guard let today = plans.today, today != plans.weekStartsOn else { return }
                    await Task.yield()
                    proxy.scrollTo(plan.entries(on: nil).isEmpty ? today.rawValue : Self.unscheduledID, anchor: .top)
                }
        }
    }

    private static let unscheduledID = "unscheduled"

    private func dayList(_ plan: Plan) -> some View {
        List {
            if let refreshError = plans.refreshError {
                FormErrorLabel(message: refreshError)
            }
            if plan.status != .draft {
                FinalizedNotice(canReopen: canEdit) {
                    perform { try await plans.setStatus(.draft) }
                }
            }
            WeekAutopilotSection(flow: flow, canEdit: canEdit)
            if plan.entries.isEmpty {
                emptyState
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            } else {
                let days = PlanDay.week(startingOn: plans.weekStartsOn)
                // Meals without a day sit just above today (the week's first day when it isn't
                // this week), so they open in view instead of under every day of the week.
                let split = plans.today.flatMap { days.firstIndex(of: $0) } ?? 0
                ForEach(days[..<split]) { day in daySection(day, in: plan) }
                unscheduledSection(plan)
                ForEach(days[split...]) { day in daySection(day, in: plan) }
            }
        }
        // A centered column on an iPad, like Pantry and Shop, not rows the width of the screen.
        .dsReadableForm()
        .refreshable {
            async let week: Void = plans.reload()
            await autopilot.reloadWeek()
            await week
        }
        .sensoryFeedback(.success, trigger: events.outcomes)
    }

    private func daySection(_ day: PlanDay, in plan: Plan) -> some View {
        Section {
            let entries = plan.entries(on: day)
            if entries.isEmpty {
                Text("Nothing planned")
                    .foregroundStyle(DS.Colors.quietText)
            }
            ForEach(entries) { entry in
                row(entry)
            }
        } header: {
            DayHeader(
                title: day.title(in: plans.week, weekStartsOn: plans.weekStartsOn),
                isToday: day == plans.today
            )
            // On the header, so today opens with its date showing above its meals.
            .id(day.rawValue)
        }
    }

    @ViewBuilder
    private func unscheduledSection(_ plan: Plan) -> some View {
        let unscheduled = plan.entries(on: nil)
        if !unscheduled.isEmpty {
            Section {
                ForEach(unscheduled) { entry in
                    row(entry)
                }
            } header: {
                Text("Pick a Day")
                    .id(Self.unscheduledID)
            } footer: {
                Text("Planned for this week without a day.")
            }
        }
    }

    /// The household said it's away this week, so an empty week is the plan.
    private var isSkippingWeek: Bool {
        autopilot.week == plans.week && autopilot.context?.skip == true
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSkippingWeek {
            ContentUnavailableView {
                Label("Skipping This Week", systemImage: "beach.umbrella")
            } description: {
                Text("Nothing planned, on purpose.")
            } actions: {
                if canEdit {
                    Button("Change This Week") {
                        flow.sheet = .context
                    }
                    .buttonStyle(.dsPrimary(.small))
                }
            }
        } else {
            ContentUnavailableView {
                Label("Nothing Planned", systemImage: "calendar")
            } description: {
                if canEditEntries {
                    Text("Add recipes from your library to plan this week’s dinners.")
                } else {
                    Text("Nobody has planned this week yet.")
                }
            } actions: {
                if canEditEntries {
                    Button("Add Recipes") {
                        isAddingRecipes = true
                    }
                    .buttonStyle(.dsPrimary(.small))
                }
            }
        }
    }

    /// Marking a meal made or not made only records an event, so any member can do it,
    /// in a finalized week too. Editing still needs `plan.edit` and a draft.
    private func row(_ entry: PlanEntry) -> some View {
        let outcome = events.outcome(for: entry)
        return Group {
            if canEditEntries {
                Button {
                    editingEntry = entry
                } label: {
                    PlanEntryRow(entry: entry, outcome: outcome)
                }
                .buttonStyle(.plain)
                .accessibilityHint("Edits the day, servings, and note")
            } else {
                PlanEntryRow(entry: entry, outcome: outcome)
            }
        }
        .swipeActions(edge: .leading) {
            Button("Made It", systemImage: "checkmark") {
                markCooked(entry)
            }
            .tint(.green)
            Button("Didn’t Make It", systemImage: "xmark") {
                markNotMade(entry, reason: nil)
            }
            .tint(.orange)
        }
        .swipeActions(edge: .trailing) {
            if canEditEntries {
                Button("Remove", systemImage: "trash", role: .destructive) {
                    perform { try await plans.deleteEntry(id: entry.id) }
                }
            }
        }
        .contextMenu {
            Section {
                Button("I Made It", systemImage: "checkmark.circle") {
                    markCooked(entry)
                }
                .disabled(outcome == .cooked)
                Menu("Didn’t Make It", systemImage: "xmark.circle") {
                    ForEach(SkipReason.allCases) { reason in
                        Button(reason.title) { markNotMade(entry, reason: reason) }
                    }
                    Button("No Reason") { markNotMade(entry, reason: nil) }
                }
            }
            if planner.canMoveToAnotherWeek(entry, isCooked: outcome == .cooked) {
                Section {
                    MoveToWeekMenu(entry: entry)
                }
            }
            if canEditEntries {
                Section {
                    if swaps.canSwap(entry, isDraft: plans.isDraft, isCooked: outcome == .cooked) {
                        Button("Try Something Similar", systemImage: "arrow.triangle.2.circlepath") {
                            swaps.start(entry, week: plans.week)
                        }
                    }
                    Menu("Move To…", systemImage: "arrow.up.and.down.text.horizontal") {
                        ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { day in
                            Button(day.name()) {
                                perform { try await plans.moveEntry(id: entry.id, to: day) }
                            }
                            .disabled(entry.day == day)
                        }
                        Button("Unscheduled") {
                            perform { try await plans.moveEntry(id: entry.id, to: nil) }
                        }
                        .disabled(entry.day == nil)
                    }
                    Button("Edit…", systemImage: "pencil") {
                        editingEntry = entry
                    }
                    Button("Remove", systemImage: "trash", role: .destructive) {
                        perform { try await plans.deleteEntry(id: entry.id) }
                    }
                }
            }
        }
    }

    /// Records the cooked event and sends it right away: the API deducts the recipe from the
    /// pantry when the event is stored, so the pantry's estimates and the unread count are
    /// refreshed after the send instead of waiting for the next batch.
    private func markCooked(_ entry: PlanEntry) {
        events.recipeCooked(entry, week: plans.week)
        sendNow()
    }

    /// The API gives back what a meal took from the pantry when it's marked not made.
    private func markNotMade(_ entry: PlanEntry, reason: SkipReason?) {
        events.recipeSkipped(entry, week: plans.week, reason: reason)
        sendNow()
    }

    private func sendNow() {
        Task {
            await events.flush()
            await pantry.refresh()
            await notifications.refreshUnreadCount()
        }
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

/// Opens the grocery list for a week.
struct GroceryListRoute: Hashable {
    let week: ISOWeek
}

struct PlanStatusBadge: View {
    let status: PlanStatus

    var body: some View {
        Label(status.title, systemImage: status == .draft ? "pencil" : "lock.fill")
            .font(.caption.weight(.semibold))
            .padding(.horizontal, DS.Space.s)
            .padding(.vertical, DS.Space.hairline)
            .foregroundStyle(status == .draft ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tint))
            .background(background, in: .capsule)
            .accessibilityLabel("Status: \(status.title)")
    }

    private var background: AnyShapeStyle {
        status == .draft ? AnyShapeStyle(.quaternary) : AnyShapeStyle(.tint.opacity(0.15))
    }
}

private struct DayHeader: View {
    let title: String
    let isToday: Bool

    var body: some View {
        HStack(spacing: DS.Space.xs) {
            Text(title)
            if isToday {
                Text("Today")
                    .foregroundStyle(.tint)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct FinalizedNotice: View {
    let canReopen: Bool
    let reopen: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.s) {
            Label("This week is finalized", systemImage: "lock.fill")
                .font(.headline)
            Text(
                canReopen
                    ? "Its recipes are locked so the grocery list stays put. Reopen the week to change it."
                    : "Its recipes are locked so the grocery list stays put."
            )
            .font(.subheadline)
            .foregroundStyle(.secondary)
            if canReopen {
                Button("Reopen Week", action: reopen)
                    .buttonStyle(.dsSecondary(.small))
            }
        }
        .padding(.vertical, DS.Space.xxs)
    }
}

#Preview("By Day") {
    NavigationStack {
        WeekView(flow: WeekAutopilotFlow())
            .navigationTitle("By Day")
    }
    .menuPreviewEnvironment()
}

#Preview("By Day, empty") {
    NavigationStack {
        WeekView(flow: WeekAutopilotFlow())
            .navigationTitle("By Day")
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
}
