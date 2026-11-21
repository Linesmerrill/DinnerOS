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
    /// Weeks Autopilot was already asked to suggest for this session, so viewing a week twice
    /// doesn't plan it twice.
    @State private var suggestedWeeks: Set<String> = []
    /// Weeks whose review card the member swiped away; hidden at once while the API catches up.
    @State private var dismissedPicks: Set<String> = []

    private var entries: [PlanEntry] {
        plans.plan?.entriesInDayOrder(weekStartsOn: plans.weekStartsOn) ?? []
    }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// The same photo each `MealCard` will show, for warming the next few cards.
    private var mealPhotoURLs: [URL?] {
        entries.map { entry in
            menu.card(forRecipeID: entry.recipe.id)?.recipe.imageURL
                ?? entry.recipe.imageURLString.flatMap { URL(string: $0) }
        }
    }

    private var isEditable: Bool {
        canEdit && plans.isDraft && menu.selectedTiming != .past
    }

    /// The pending proposal for the shown week, from Autopilot or, before it loads, the menu.
    /// `nil` when there's nothing left to review: no proposal, one with no meals left in it
    /// ("Autopilot suggested 0 meals" asked about nothing), or one the member dismissed.
    private var pendingProposalMeals: Int? {
        guard !dismissedPicks.contains(plans.week.description) else { return nil }
        if let proposal = autopilot.pendingProposal, proposal.week == plans.week.description {
            return proposal.slots.isEmpty ? nil : proposal.slots.count
        }
        if let proposal = menu.menu?.proposal, proposal.isPending, menu.menu?.week == plans.week.description {
            return proposal.plannedMeals > 0 ? proposal.plannedMeals : nil
        }
        return nil
    }

    private var picksDismiss: (() -> Void)? {
        guard isEditable else { return nil }
        return { dismissPicks() }
    }

    /// Dismisses the week's pending picks: the plan doesn't change, and the card goes away.
    private func dismissPicks() {
        let week = plans.week
        withAnimation(.snappy) { _ = dismissedPicks.insert(week.description) }
        Task {
            if autopilot.week == week { try? await autopilot.dismissProposal() }
            await menu.reloadWeek(week)
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            MenuSectionHeader(title: String(localized: "Your Meals"), subtitle: subtitle)
                .padding(.horizontal, DS.Space.l)
            content
        }
        .sheet(item: $editingEntry) { entry in
            PlanEntryEditor(entry: entry, week: plans.week)
        }
        .task(id: suggestionKey) { await suggestIfOpen() }
        .accessibilityElement(children: .contain)
    }

    /// The pending proposal for the shown week, once Autopilot has loaded it.
    private var inlineProposal: AutopilotProposal? {
        guard let proposal = autopilot.pendingProposal, proposal.week == plans.week.description,
            !proposal.slots.isEmpty
        else { return nil }
        return proposal
    }

    private var suggestionKey: String {
        "\(plans.week)-\(entries.count)-\(autopilot.proposal?.id ?? "none")-\(autopilot.isLoadingWeek)"
    }

    /// A coming week with open nights and nothing suggested yet: ask Autopilot for picks, so
    /// the week shows what it would cook instead of a lone meal. Only once per week per launch,
    /// and never over a proposal the household already accepted or dismissed.
    private func suggestIfOpen() async {
        let week = plans.week
        guard isEditable, menu.selectedTiming != .past, plans.phase == .loaded, autopilot.isConfigured,
            !autopilot.isGenerating, !autopilot.isLoadingWeek, autopilot.week == week, autopilot.proposal == nil,
            autopilot.weekError == nil, !suggestedWeeks.contains(week.description),
            entries.count < (autopilot.profile?.schedule.mealsPerWeek ?? 4)
        else { return }
        suggestedWeeks.insert(week.description)
        try? await autopilot.generate(week: week, prompted: false)
    }

    private var subtitle: String? {
        MenuFormat.yourMealsHeaderSubtitle(
            counts: menu.mealCounts(of: entries), isPast: menu.selectedTiming == .past)
    }

    @ViewBuilder
    private var content: some View {
        switch plans.phase {
        case .idle, .loading:
            MenuCardPlaceholders(count: 2, width: 260)
                .padding(.horizontal, DS.Space.l)
        case .failed(let message):
            MenuSectionError(message: message) {
                Task { await plans.reload() }
            }
            .padding(.horizontal, DS.Space.l)
        case .loaded:
            if inlineProposal == nil, let meals = pendingProposalMeals {
                ReviewPicksCard(meals: meals, review: { flow.sheet = .review }, dismiss: picksDismiss)
                    .padding(.horizontal, DS.Space.l)
                    .transition(.move(edge: .leading).combined(with: .opacity))
            }
            if entries.isEmpty {
                emptyState(suggestionsPending: pendingProposalMeals != nil || autopilot.isGenerating)
                    .padding(.horizontal, DS.Space.l)
            } else {
                mealCards
            }
            if let proposal = inlineProposal {
                SuggestedMealsRow(proposal: proposal, isEditable: isEditable) { flow.sheet = .review }
                    .padding(.top, entries.isEmpty ? 0 : 8)
            } else if autopilot.isGenerating, autopilot.week == plans.week {
                Label("Autopilot is picking meals for the open nights…", systemImage: "sparkles")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, DS.Space.l)
            }
        }
    }

    @ViewBuilder
    private var mealCards: some View {
        if dynamicTypeSize.isAccessibilitySize {
            // Paging cards get unreadable at accessibility sizes; they stack instead.
            VStack(spacing: DS.Space.xl) {
                ForEach(entries) { entry in
                    card(entry)
                }
            }
            .padding(.horizontal, DS.Space.l)
        } else {
            ScrollView(.horizontal) {
                // Not lazy: a week has a handful of meals, and a lazy row takes its height from
                // the cards it has laid out so far, so a later card's Cooked/Skip and rating rows
                // spilled under the next section.
                HStack(alignment: .top, spacing: DS.Space.m) {
                    ForEach(Array(entries.enumerated()), id: \.element.id) { index, entry in
                        card(entry)
                            .containerRelativeFrame(.horizontal) { width, _ in
                                // A phone shows one big card; wider screens show several at a
                                // readable size instead of one card the width of an iPad.
                                width < 600 ? width * 0.84 : min(width * 0.84, max(360, (width - 48) / 3))
                            }
                            .prefetchesPhotos(after: index, in: mealPhotoURLs, pointWidth: MealCard.photoWidth)
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, DS.Space.l)
            }
            .scrollIndicators(.hidden)
            .carouselInSafeArea()
            .scrollTargetBehavior(.viewAligned)
        }
    }

    private func card(_ entry: PlanEntry) -> some View {
        MealCard(entry: entry, isEditable: isEditable, card: menu.card(forRecipeID: entry.recipe.id)) {
            editingEntry = entry
        }
    }

    /// The week has nothing in it yet.
    ///
    /// When Autopilot has already suggested meals, the card above is the thing to press, so this
    /// keeps only the hint that meals can be added by hand. Offering a second Autopilot button
    /// beside the first one asked the household to choose between two spellings of the same action.
    @ViewBuilder
    private func emptyState(suggestionsPending: Bool) -> some View {
        if suggestionsPending {
            if isEditable {
                Text(MenuFormat.addMealsYourselfHint)
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        } else {
            fullEmptyState
        }
    }

    @ViewBuilder
    private var fullEmptyState: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            Label(MenuFormat.emptyWeekTitle(isPast: menu.selectedTiming == .past), systemImage: "calendar")
                .font(DS.Typography.headline)
            if isEditable {
                Text("Let Autopilot suggest this week’s dinners, or pick meals yourself.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                Button {
                    flow.plan(autopilot: autopilot, plans: plans)
                } label: {
                    Label("Plan with Autopilot", systemImage: "sparkles")
                }
                .buttonStyle(.dsPrimary(.small))
                .disabled(autopilot.isGenerating)
                Text(MenuFormat.addMealsYourselfHint)
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
            } else if menu.selectedTiming != .past, !plans.isDraft {
                Text("This week is finalized.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(DS.Space.l)
        .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
    }
}

/// "Autopilot suggested 4 meals" with a button that opens the review sheet.
/// Swipe left or tap the close button to dismiss the picks without changing the plan.
struct ReviewPicksCard: View {
    let meals: Int
    let review: () -> Void
    var dismiss: (() -> Void)? = nil

    @State private var dragX: CGFloat = 0

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.sm) {
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
            Button("Review Autopilot’s Picks", action: review)
                .buttonStyle(.dsPrimary(.small))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(DS.Space.l)
        .background(.tint.opacity(0.12), in: .rect(cornerRadius: DS.Radius.large))
        .overlay(alignment: .topTrailing) {
            if let dismiss {
                Button("Dismiss", systemImage: "xmark", action: dismiss)
                    .labelStyle(.iconOnly)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(minWidth: 44, minHeight: 44)
                    .accessibilityLabel("Dismiss Autopilot’s Picks")
            }
        }
        .offset(x: dragX)
        .gesture(
            DragGesture(minimumDistance: 20)
                .onChanged { value in
                    guard dismiss != nil, abs(value.translation.width) > abs(value.translation.height) else { return }
                    dragX = min(0, value.translation.width)
                }
                .onEnded { value in
                    if let dismiss, value.translation.width < -100 {
                        dismiss()
                    } else {
                        withAnimation(.snappy) { dragX = 0 }
                    }
                },
            including: dismiss == nil ? .subviews : .all
        )
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
    @Environment(MenuStore.self) private var menu
    @Environment(MealPlanner.self) private var planner
    @Environment(EventReporter.self) private var events
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications
    @Environment(MealSwapStore.self) private var swaps

    private var outcome: EventReporter.EntryOutcome? { events.outcome(for: entry) }

    /// The stars show the menu card's `myRating`, so they need one. Without it the row would ask
    /// again about a meal the member has already rated.
    private var feedbackRecipe: RecipeSummary? { card?.recipe }
    private var isBusy: Bool { planner.busyEntryIDs.contains(entry.id) }

    private var summary: RecipeSummary {
        card?.recipe
            ?? RecipeSummary.placeholder(
                id: entry.recipe.id, name: entry.recipe.name, imageURLString: entry.recipe.imageURLString)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.sm) {
            NavigationLink(value: summary) {
                VStack(alignment: .leading, spacing: DS.Space.s) {
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
            // What happened comes before how it was: one is a fact about the night, the other an
            // opinion about the food. A meal counts as made unless someone says it wasn't
            // (decision 662), so the row only offers that, once the night is over.
            MealOutcomeRow(entry: entry)
            if let feedbackRecipe {
                switch MealRating.row(outcome: outcome, nightHasPassed: MealNight.hasPassed(date: entry.date)) {
                case .shown:
                    MealRatingRow(recipe: feedbackRecipe)
                case .heldSpace:
                    // Not asked yet, but the row's height is kept: cards of different heights in
                    // this carousel kept the Menu's lazy stack re-measuring on every frame, and
                    // an iPad in landscape froze while scrolling (QA, build 121).
                    MealRatingRow(recipe: feedbackRecipe)
                        .hidden()
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
        }
        .contextMenu { menuItems }
        .accessibilityElement(children: .contain)
    }

    /// A paging card is a little narrower than the screen; `RecipePhoto` refines this once the
    /// card is laid out.
    static let photoWidth: CGFloat = 340

    private var photo: some View {
        RecipePhoto(url: summary.imageURL, pointWidth: MealCard.photoWidth, title: summary.name)
            .overlay(alignment: .topLeading) {
                HStack(spacing: DS.Space.xs) {
                    if entry.day == nil && !isAddOn {
                        pickADay
                    } else {
                        MenuChip(text: dayLabel, systemImage: "calendar", isProminent: true)
                            .background(.regularMaterial, in: .capsule)
                            .accessibilityHidden(true)
                    }
                    if isAddOn {
                        AddOnLabel()
                            .background(.regularMaterial, in: .capsule)
                            .accessibilityHidden(true)
                    }
                }
                .padding(DS.Space.s)
            }
            .overlay(alignment: .topTrailing) {
                if entry.isFromAutopilot {
                    Image(systemName: "sparkles")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tint)
                        .padding(DS.Space.xs)
                        .background(.regularMaterial, in: .circle)
                        .padding(DS.Space.s)
                        .accessibilityLabel("Added by Autopilot")
                }
            }
            .overlay(alignment: .bottomLeading) {
                HStack(spacing: DS.Space.xs) {
                    if let minutes = summary.displayMinutes, minutes > 0 {
                        TimeBadge(minutes: minutes, isQuick: summary.timeBand == .quick)
                    }
                    outcomeBadge
                }
                .padding(DS.Space.s)
                .accessibilityHidden(true)
            }
    }

    /// Garlic bread planned with the pasta is an add-on, not a sixth dinner. The entry says so
    /// on a current server; the menu's card for the recipe answers on an older one.
    private var isAddOn: Bool {
        entry.recipe.isAddon || card?.recipe.isAddon == true
    }

    @ViewBuilder
    private var outcomeBadge: some View {
        switch outcome {
        case .cooked:
            MenuChip(text: String(localized: "Made"), systemImage: "checkmark.circle.fill", isProminent: true)
                .background(.regularMaterial, in: .capsule)
        case .skipped(let reason):
            MenuChip(
                text: reason.map { String(localized: "Not made: \($0.title)") } ?? String(localized: "Not made"),
                systemImage: "xmark.circle.fill"
            )
            .background(.regularMaterial, in: .capsule)
        case nil:
            EmptyView()
        }
    }

    /// The name only: the day, the time, and whether it's an add-on are badges on the photo,
    /// and a customization or note would make this card a different height from its neighbours.
    /// Both still reach VoiceOver through `accessibilityLabel`.
    private var text: some View {
        CardTitle(
            name: entry.recipe.name,
            reservesHeight: MenuCardMetrics.reservesTitleHeight(hasRowBelow: true)
        )
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
    }

    private var dayLabel: String {
        entry.day?.name() ?? String(localized: "Pick a day")
    }

    /// A meal without a day (another meal took its night) asks for one, and picks it right here.
    @ViewBuilder
    private var pickADay: some View {
        let chip = Label("Pick a Day", systemImage: "exclamationmark.circle.fill")
            .labelStyle(.titleAndIcon)
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, DS.Space.s)
            .padding(.vertical, DS.Space.xxs)
            .foregroundStyle(.black)
            .background(Color.orange, in: .capsule)
        if isEditable {
            Menu {
                ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { day in
                    Button(day.name()) {
                        Task { await planner.move(entry, to: day) }
                    }
                }
            } label: {
                chip
            }
            .accessibilityLabel("Pick a day for \(entry.recipe.name)")
        } else {
            chip
                .accessibilityHidden(true)
        }
    }

    private var accessibilityLabel: String {
        var parts = [entry.recipe.name, dayLabel]
        parts += MenuFormat.spokenFacts(minutes: summary.displayMinutes, calories: nil, proteinGrams: nil)
        if summary.timeBand == .quick {
            parts.append(String(localized: "quick"))
        }
        if isAddOn {
            parts.append(String(localized: "add-on"))
        }
        parts.append(MenuFormat.servings(entry.servings))
        if let customized = entry.customizations.first(where: { !$0.label.isEmpty }) {
            parts.append(String(localized: "Customized: \(customized.label)"))
        }
        if !entry.note.isEmpty {
            parts.append(entry.note)
        }
        switch outcome {
        case .cooked: parts.append(String(localized: "Made"))
        case .skipped: parts.append(String(localized: "Not made"))
        case nil: break
        }
        return parts.joined(separator: ", ")
    }

    @ViewBuilder
    private var menuItems: some View {
        Section {
            Button("I Made It", systemImage: "checkmark.circle") {
                markCooked()
            }
            .disabled(outcome == .cooked)
            Menu("Didn’t Make It", systemImage: "xmark.circle") {
                ForEach(SkipReason.allCases) { reason in
                    Button(reason.title) { markNotMade(reason) }
                }
                Button("No Reason") { markNotMade(nil) }
            }
        }
        // Open on a past week too: the meal that didn't get made is usually last week's.
        if planner.canMoveToAnotherWeek(entry, isCooked: outcome == .cooked) {
            Section {
                MoveToWeekMenu(entry: entry)
            }
        }
        if isEditable {
            Section {
                if swaps.canSwap(entry, isDraft: plans.isDraft, isCooked: outcome == .cooked) {
                    Button("Try Something Similar", systemImage: "arrow.triangle.2.circlepath") {
                        swaps.start(entry, week: plans.week)
                    }
                }
                Menu("Move To…", systemImage: "arrow.up.and.down.text.horizontal") {
                    ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { day in
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
        sendNow()
    }

    /// Records that the meal wasn't made and sends it right away: the API gives back what it
    /// took from the pantry, if it was already counted as made.
    private func markNotMade(_ reason: SkipReason?) {
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
}

/// "Move to Another Week" with the weeks a meal can go to, for a meal that didn't get made:
/// "Next Week (Oct 18 – 24)", and so on.
struct MoveToWeekMenu: View {
    let entry: PlanEntry

    @Environment(PlanStore.self) private var plans
    @Environment(MealPlanner.self) private var planner

    var body: some View {
        Menu("Move to Another Week", systemImage: "calendar.badge.clock") {
            ForEach(planner.weeksToMoveTo, id: \.self) { week in
                Button(title(week)) {
                    Task { await planner.move(entry, toWeek: week) }
                }
            }
        }
    }

    private func title(_ week: ISOWeek) -> String {
        let range = week.rangeLabel(weekStartsOn: plans.weekStartsOn)
        guard let name = MenuFormat.relativeWeekName(week, current: plans.currentWeek) else { return range }
        return "\(name) (\(range))"
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

/// When a planned meal asks "How was it?" (build 121, Hark's review item 12).
nonisolated enum MealRating {
    /// Only once it's been cooked: marked made, or its night is over and nobody said it wasn't
    /// (a meal counts as made unless someone says otherwise, decision 662). A meal still ahead,
    /// or one that wasn't made, has nothing to rate.
    static func shows(outcome: MealOutcome?, nightHasPassed: Bool) -> Bool {
        switch outcome {
        case .cooked: true
        case .skipped: false
        case nil: nightHasPassed
        }
    }

    /// The rating row on a planned meal's card: the stars, or the same space left empty, so
    /// every card in Your Meals is one height whether it asks or not.
    enum Row: Equatable {
        case shown
        case heldSpace
    }

    static func row(outcome: MealOutcome?, nightHasPassed: Bool) -> Row {
        shows(outcome: outcome, nightHasPassed: nightHasPassed) ? .shown : .heldSpace
    }
}
