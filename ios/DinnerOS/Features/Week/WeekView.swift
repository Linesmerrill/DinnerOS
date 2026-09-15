import SwiftUI

/// The Week tab: one ISO week of the current household's plan, grouped by day, with
/// the week's grocery list one tap away.
struct WeekView: View {
    @Environment(PlanStore.self) private var plans
    @Environment(HouseholdStore.self) private var households
    @Environment(RecipeLibrary.self) private var library
    @Environment(EventReporter.self) private var events
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications
    @Environment(AutopilotStore.self) private var autopilot

    @State private var editingEntry: PlanEntry?
    @State private var autopilotFlow = WeekAutopilotFlow()
    @State private var isAddingRecipes = false
    @State private var actionError: String?

    private var household: Household? {
        households.current?.household
    }

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
            .navigationTitle("Week")
            .navigationBarTitleDisplayMode(.inline)
            .safeAreaInset(edge: .top, spacing: 0) {
                WeekSwitcher()
            }
            .toolbar { toolbar }
            .notificationsToolbar()
            .navigationDestination(for: GroceryListRoute.self) { route in
                GroceryListView(week: route.week)
            }
            .task(id: household?.id) {
                guard let household else { return }
                // The editor reads serving sizes through the library's recipe cache.
                async let recipes: Void = library.activate(householdID: household.id)
                await plans.activate(householdID: household.id, timeZone: household.planningTimeZone)
                await recipes
            }
            .sheet(item: $editingEntry) { entry in
                PlanEntryEditor(entry: entry, week: plans.week)
            }
            .sheet(isPresented: $isAddingRecipes) {
                AddRecipesSheet(week: plans.week)
            }
            .alert("Couldn't Change the Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
            .modifier(WeekAutopilotModifier(flow: autopilotFlow, canEdit: canEdit))
    }

    @ViewBuilder
    private var content: some View {
        switch plans.phase {
        case .idle, .loading:
            ProgressView("Loading the week…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load This Week", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await plans.reload() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            if let plan = plans.plan {
                planList(plan)
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            NavigationLink(value: GroceryListRoute(week: plans.week)) {
                Label("Grocery List", systemImage: "cart")
            }
            .disabled(plans.plan == nil)
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            if plans.isSaving {
                ProgressView()
            }
            if let plan = plans.plan {
                weekMenu(plan)
            }
            if canEdit, plans.plan != nil {
                Button("Add Recipes", systemImage: "plus") {
                    isAddingRecipes = true
                }
                .disabled(!plans.isDraft)
            }
        }
    }

    /// The week's status for planners, and Autopilot for everyone.
    private func weekMenu(_ plan: Plan) -> some View {
        Menu {
            if canEdit {
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
            WeekAutopilotMenuItems(flow: autopilotFlow, canEdit: canEdit)
        } label: {
            Label("Week Menu", systemImage: "ellipsis.circle")
        }
    }

    private func planList(_ plan: Plan) -> some View {
        List {
            if let refreshError = plans.refreshError {
                FormErrorLabel(message: refreshError)
            }
            if plan.status != .draft {
                FinalizedNotice(canReopen: canEdit) {
                    perform { try await plans.setStatus(.draft) }
                }
            }
            WeekAutopilotSection(flow: autopilotFlow, canEdit: canEdit)
            if plan.entries.isEmpty {
                emptyState
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            } else {
                ForEach(PlanDay.allCases) { day in
                    Section {
                        let entries = plan.entries(on: day)
                        if entries.isEmpty {
                            Text("Nothing planned")
                                .foregroundStyle(.tertiary)
                        }
                        ForEach(entries) { entry in
                            row(entry)
                        }
                    } header: {
                        DayHeader(title: day.title(in: plans.week), isToday: day == plans.today)
                    }
                }
                let unscheduled = plan.entries(on: nil)
                if !unscheduled.isEmpty {
                    Section {
                        ForEach(unscheduled) { entry in
                            row(entry)
                        }
                    } header: {
                        Text("Unscheduled")
                    } footer: {
                        Text("Planned for this week without a day.")
                    }
                }
            }
        }
        .refreshable {
            async let week: Void = plans.reload()
            await autopilot.reloadWeek()
            await week
        }
        .sensoryFeedback(.success, trigger: events.outcomes)
    }

    @ViewBuilder
    private var emptyState: some View {
        ContentUnavailableView {
            Label("Nothing Planned", systemImage: "calendar")
        } description: {
            if canEditEntries {
                Text("Add recipes from your library to plan this week's dinners.")
            } else {
                Text("Nobody has planned this week yet.")
            }
        } actions: {
            if canEditEntries {
                Button("Add Recipes") {
                    isAddingRecipes = true
                }
                .buttonStyle(.borderedProminent)
            }
        }
    }

    /// Marking a meal cooked or skipped only records an event, so any member can do it,
    /// in a finalized week too. Editing still needs `plan.edit` and a draft.
    private func row(_ entry: PlanEntry) -> some View {
        let outcome = events.outcomes[entry.id]
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
            Button("Cooked", systemImage: "checkmark") {
                markCooked(entry)
            }
            .tint(.green)
            Button("Skip", systemImage: "forward") {
                events.recipeSkipped(entry, week: plans.week, reason: nil)
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
                Button("Mark as Cooked", systemImage: "checkmark.circle") {
                    markCooked(entry)
                }
                .disabled(outcome == .cooked)
                Menu("Skip", systemImage: "forward") {
                    ForEach(SkipReason.allCases) { reason in
                        Button(reason.title) {
                            events.recipeSkipped(entry, week: plans.week, reason: reason)
                        }
                    }
                    Button("Skip Without a Reason") {
                        events.recipeSkipped(entry, week: plans.week, reason: nil)
                    }
                }
            }
            if canEditEntries {
                Section {
                    Menu("Move To…", systemImage: "arrow.up.and.down.text.horizontal") {
                        ForEach(PlanDay.allCases) { day in
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

/// Previous and next week, the shown week's dates and status, and a jump to this week.
private struct WeekSwitcher: View {
    @Environment(PlanStore.self) private var plans

    var body: some View {
        let week = plans.week
        HStack(spacing: 12) {
            Button("Previous Week", systemImage: "chevron.left") {
                Task { await plans.show(week: week.previous) }
            }
            .labelStyle(.iconOnly)
            Spacer(minLength: 0)
            VStack(spacing: 4) {
                Text(week.rangeLabel())
                    .font(.headline)
                HStack(spacing: 8) {
                    if let relative = relativeName(week) {
                        Text(relative)
                            .foregroundStyle(.secondary)
                    } else {
                        Button("This Week") {
                            Task { await plans.showCurrentWeek() }
                        }
                    }
                    if let status = plans.plan?.status {
                        PlanStatusBadge(status: status)
                    }
                }
                .font(.subheadline)
            }
            .accessibilityElement(children: .contain)
            Spacer(minLength: 0)
            Button("Next Week", systemImage: "chevron.right") {
                Task { await plans.show(week: week.next) }
            }
            .labelStyle(.iconOnly)
        }
        .font(.title3)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }

    private func relativeName(_ week: ISOWeek) -> LocalizedStringKey? {
        let current = plans.currentWeek
        switch week {
        case current: return "This Week"
        case current.next: return "Next Week"
        case current.previous: return "Last Week"
        default: return nil
        }
    }
}

struct PlanStatusBadge: View {
    let status: PlanStatus

    var body: some View {
        Label(status.title, systemImage: status == .draft ? "pencil" : "lock.fill")
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 2)
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
        HStack(spacing: 6) {
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
        VStack(alignment: .leading, spacing: 8) {
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
                    .buttonStyle(.bordered)
            }
        }
        .padding(.vertical, 4)
    }
}

#Preview("Planned") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        WeekView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
    .environment(PlanPreviewData.store(session: session))
    .environment(EventReporter.preview(session: session))
    .environment(PantryPreviewData.store(session: session))
    .environment(NotificationPreviewData.store(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}

#Preview("Empty") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        WeekView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
    .environment(PlanPreviewData.store(session: session, plan: PlanPreviewData.emptyPlan))
    .environment(EventReporter.preview(session: session))
    .environment(PantryPreviewData.store(session: session))
    .environment(NotificationPreviewData.store(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}
