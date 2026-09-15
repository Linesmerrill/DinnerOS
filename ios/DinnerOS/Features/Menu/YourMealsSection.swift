import SwiftUI

/// The week's chosen meals as large cards, with the servings control on the card. A pending
/// Autopilot proposal shows above them; a past week is read-only apart from cooked and skipped.
struct YourMealsSection: View {
    let flow: WeekAutopilotFlow

    @Environment(PlanStore.self) private var plans
    @Environment(MenuStore.self) private var menu
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var editingEntry: PlanEntry?

    private var entries: [PlanEntry] {
        plans.plan?.entries ?? []
    }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var isEditable: Bool {
        canEdit && plans.isDraft && menu.selectedTiming != .past
    }

    /// The pending proposal for the shown week, from Autopilot or, before it loads, the menu.
    private var pendingProposalMeals: Int? {
        if let proposal = autopilot.pendingProposal, proposal.week == plans.week.description {
            return proposal.slots.count
        }
        if let proposal = menu.menu?.proposal, proposal.isPending, menu.menu?.week == plans.week.description {
            return proposal.plannedMeals
        }
        return nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            MenuSectionHeader(title: String(localized: "Your Meals"), subtitle: subtitle)
                .padding(.horizontal, 16)
            content
        }
        .sheet(item: $editingEntry) { entry in
            PlanEntryEditor(entry: entry, week: plans.week)
        }
        .accessibilityElement(children: .contain)
    }

    private var subtitle: String {
        if menu.selectedTiming == .past {
            return MenuFormat.mealsPlanned(entries.count)
        }
        return MenuFormat.yourMealsSubtitle(
            total: entries.count, fromAutopilot: entries.filter(\.isFromAutopilot).count)
    }

    @ViewBuilder
    private var content: some View {
        switch plans.phase {
        case .idle, .loading:
            MenuCardPlaceholders(count: 2, width: 260)
                .padding(.horizontal, 16)
        case .failed(let message):
            MenuSectionError(message: message) {
                Task { await plans.reload() }
            }
            .padding(.horizontal, 16)
        case .loaded:
            if let meals = pendingProposalMeals {
                ReviewPicksCard(meals: meals) { flow.sheet = .review }
                    .padding(.horizontal, 16)
            }
            if entries.isEmpty {
                emptyState
                    .padding(.horizontal, 16)
            } else {
                mealCards
            }
        }
    }

    @ViewBuilder
    private var mealCards: some View {
        if dynamicTypeSize.isAccessibilitySize {
            // Paging cards get unreadable at accessibility sizes; they stack instead.
            VStack(spacing: 20) {
                ForEach(entries) { entry in
                    card(entry)
                }
            }
            .padding(.horizontal, 16)
        } else {
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 12) {
                    ForEach(entries) { entry in
                        card(entry)
                            .containerRelativeFrame(.horizontal) { width, _ in width * 0.84 }
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, 16)
            }
            .scrollIndicators(.hidden)
            .scrollTargetBehavior(.viewAligned)
        }
    }

    private func card(_ entry: PlanEntry) -> some View {
        MealCard(entry: entry, isEditable: isEditable, card: menu.card(forRecipeID: entry.recipe.id)) {
            editingEntry = entry
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(
                menu.selectedTiming == .past
                    ? String(localized: "Nothing was planned this week")
                    : String(localized: "Nothing planned yet"), systemImage: "calendar"
            )
            .font(.headline)
            if isEditable {
                Text("Let Autopilot suggest this week's dinners, or pick meals yourself.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                Button {
                    flow.plan(autopilot: autopilot, plans: plans)
                } label: {
                    Label("Plan with Autopilot", systemImage: "sparkles")
                }
                .buttonStyle(.borderedProminent)
                .disabled(autopilot.isGenerating)
                Text("or add meals below")
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
            } else if menu.selectedTiming != .past, !plans.isDraft {
                Text("This week is finalized.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18))
    }
}

/// "Autopilot suggested 4 meals" with a button that opens the review sheet.
struct ReviewPicksCard: View {
    let meals: Int
    let review: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label(
                meals == 1
                    ? String(localized: "Autopilot suggested 1 meal")
                    : String(localized: "Autopilot suggested \(meals) meals"), systemImage: "sparkles"
            )
            .font(.headline)
            .foregroundStyle(Color.primary)
            Text("Review, swap, and add them to your week.")
                .font(.subheadline)
                .foregroundStyle(Color.secondary)
            Button("Review Autopilot's Picks", action: review)
                .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background(.tint.opacity(0.12), in: .rect(cornerRadius: 18))
        .accessibilityElement(children: .contain)
    }
}

/// One planned meal: the photo, its day, servings, facts, and whether it was cooked.
struct MealCard: View {
    let entry: PlanEntry
    let isEditable: Bool
    let card: MenuCard?
    let edit: () -> Void

    @Environment(PlanStore.self) private var plans
    @Environment(MealPlanner.self) private var planner
    @Environment(EventReporter.self) private var events
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications

    private var outcome: EventReporter.EntryOutcome? { events.outcomes[entry.id] }
    private var isBusy: Bool { planner.busyEntryIDs.contains(entry.id) }

    private var summary: RecipeSummary {
        card?.recipe
            ?? RecipeSummary.placeholder(
                id: entry.recipe.id, name: entry.recipe.name, imageURLString: entry.recipe.imageURLString)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink(value: summary) {
                VStack(alignment: .leading, spacing: 8) {
                    photo
                    text
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if isEditable {
                ServingsStepper(
                    label: MenuFormat.servings(entry.servings), isBusy: isBusy,
                    decrease: { changeServings(-1) }, increase: { changeServings(1) })
            }
        }
        .contextMenu { menuItems }
        .accessibilityElement(children: .contain)
    }

    private var photo: some View {
        RecipePhoto(url: summary.imageURL, pointWidth: 340)
            .overlay(alignment: .topLeading) {
                MenuChip(text: dayLabel, systemImage: "calendar", isProminent: true)
                    .background(.regularMaterial, in: .capsule)
                    .padding(8)
            }
            .overlay(alignment: .topTrailing) {
                if entry.isFromAutopilot {
                    Image(systemName: "sparkles")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tint)
                        .padding(6)
                        .background(.regularMaterial, in: .circle)
                        .padding(8)
                        .accessibilityLabel("Added by Autopilot")
                }
            }
            .overlay(alignment: .bottomLeading) {
                outcomeBadge
                    .padding(8)
            }
    }

    @ViewBuilder
    private var outcomeBadge: some View {
        switch outcome {
        case .cooked:
            MenuChip(text: String(localized: "Cooked"), systemImage: "checkmark.circle.fill", isProminent: true)
                .background(.regularMaterial, in: .capsule)
        case .skipped(let reason):
            MenuChip(
                text: reason.map { String(localized: "Skipped: \($0.title)") } ?? String(localized: "Skipped"),
                systemImage: "forward.fill"
            )
            .background(.regularMaterial, in: .capsule)
        case nil:
            EmptyView()
        }
    }

    private var text: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(entry.recipe.name)
                .font(.headline)
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if let headline = summary.headline, !headline.isEmpty {
                Text(headline)
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                    .lineLimit(1)
            }
            FactsRow(summary: summary)
            if let customized = entry.customizations.first(where: { !$0.label.isEmpty }) {
                Label("Customized: \(customized.label)", systemImage: "arrow.triangle.swap")
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
            }
            if !entry.note.isEmpty {
                Text(entry.note)
                    .font(.caption)
                    .italic()
                    .foregroundStyle(Color.secondary)
                    .lineLimit(2)
            }
            BadgeRow(badges: card?.badges ?? [])
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
    }

    private var dayLabel: String {
        entry.day?.name() ?? String(localized: "Unscheduled")
    }

    private var accessibilityLabel: String {
        var parts = [entry.recipe.name, dayLabel]
        parts += MenuFormat.spokenFacts(
            minutes: summary.displayMinutes, calories: summary.calories, proteinGrams: summary.proteinGrams)
        parts.append(MenuFormat.servings(entry.servings))
        switch outcome {
        case .cooked: parts.append(String(localized: "Cooked"))
        case .skipped: parts.append(String(localized: "Skipped"))
        case nil: break
        }
        return parts.joined(separator: ", ")
    }

    @ViewBuilder
    private var menuItems: some View {
        Section {
            Button("Mark as Cooked", systemImage: "checkmark.circle") {
                markCooked()
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
        if isEditable {
            Section {
                Menu("Move To…", systemImage: "arrow.up.and.down.text.horizontal") {
                    ForEach(PlanDay.allCases) { day in
                        Button(day.name()) {
                            Task { await planner.move(entry, to: day) }
                        }
                        .disabled(entry.day == day)
                    }
                    Button("Unscheduled") {
                        Task { await planner.move(entry, to: nil) }
                    }
                    .disabled(entry.day == nil)
                }
                Button("Edit…", systemImage: "pencil", action: edit)
                Button("Remove", systemImage: "trash", role: .destructive) {
                    Task { await planner.remove(entry) }
                }
            }
        }
    }

    private func changeServings(_ delta: Int) {
        Task { await planner.changeServings(entry, by: delta) }
    }

    /// Records the cooked event and sends it right away: the API deducts the recipe from the
    /// pantry when the event is stored.
    private func markCooked() {
        events.recipeCooked(entry, week: plans.week)
        Task {
            await events.flush()
            await pantry.refresh()
            await notifications.refreshUnreadCount()
        }
    }
}

#Preview("Your Meals") {
    NavigationStack {
        ScrollView {
            YourMealsSection(flow: WeekAutopilotFlow())
                .padding(.vertical)
        }
    }
    .menuPreviewEnvironment()
}

#Preview("Empty, with a proposal") {
    NavigationStack {
        ScrollView {
            YourMealsSection(flow: WeekAutopilotFlow())
                .padding(.vertical)
        }
    }
    .menuPreviewEnvironment(
        plan: PlanPreviewData.emptyPlan, menu: MenuPreviewData.proposalMenu, withProposal: true)
}
