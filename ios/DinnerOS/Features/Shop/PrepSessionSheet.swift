import SwiftUI

/// "You just got the groceries." One card at a time: cut the pork into five, keep ten ounces
/// out for Thursday, freeze the rest (docs/shopping-providers.md#the-prep-plan).
///
/// A checklist, not a wall of text: one card fills the screen, **Done** and **Skip** move to
/// the next, and a household that closes it half way finds it where it left it. Doing nothing
/// is still fine — nothing is recorded until a card is answered.
struct PrepSessionSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(MealPlanner.self) private var planner
    @Environment(\.dismiss) private var dismiss

    @State private var index = 0
    @State private var portions: Int?
    @State private var isBusy = false
    @State private var errorMessage: String?

    private var cards: [PrepCard] { shopping.prepSession?.cards ?? [] }
    private var card: PrepCard? { index < cards.count ? cards[index] : nil }
    private var canEdit: Bool { shopping.canConfirm }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Put the Groceries Away")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .onAppear { startAtFirstUnanswered() }
    }

    @ViewBuilder
    private var content: some View {
        if let card {
            List {
                if let errorMessage {
                    Section { FormErrorLabel(message: errorMessage) }
                }
                PrepCardSections(
                    card: card, chosenPortions: chosenPortions(for: card), isBusy: isBusy,
                    canEdit: canEdit,
                    setPortions: { portions = $0 },
                    plan: { suggestion in Task { await plan(suggestion, on: card) } })
                if canEdit {
                    Section {
                        Button {
                            Task { await finish(card) }
                        } label: {
                            Label(card.freezable ? "Freeze the Rest" : "Done", systemImage: "checkmark.circle")
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(isBusy)
                        Button("Skip This One") { Task { await skip(card) } }
                            .disabled(isBusy)
                    } footer: {
                        if !card.reminderText.isEmpty {
                            Text(card.reminderText)
                        }
                    }
                }
            }
            .safeAreaInset(edge: .top, spacing: 0) { progress }
        } else {
            summary
        }
    }

    private var progress: some View {
        VStack(spacing: 4) {
            Text("Item \(index + 1) of \(cards.count)")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
            ProgressView(value: Double(index + 1), total: Double(max(cards.count, 1)))
                .progressViewStyle(.linear)
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .background(.bar)
        .accessibilityElement(children: .combine)
    }

    /// What the member sees when there is nothing left — and when there never was. An empty
    /// week is a good outcome, not a blank screen.
    @ViewBuilder
    private var summary: some View {
        let session = shopping.prepSession
        ContentUnavailableView {
            Label(
                session?.state == .nothingToPrep ? "Nothing to Prep" : "All Put Away",
                systemImage: session?.state == .nothingToPrep ? "checkmark.seal" : "snowflake")
        } description: {
            Text(session?.headline ?? String(localized: "Nothing to prep this week."))
        } actions: {
            if let session, session.skipped > 0 {
                Button("Go Back to the Skipped Ones") { index = firstSkipped(in: session) ?? 0 }
                    .buttonStyle(.bordered)
            }
            Button("Close") { dismiss() }
                .buttonStyle(.borderedProminent)
        }
    }

    // MARK: - Actions

    /// Resumes where the household left off: the first card nobody has answered.
    private func startAtFirstUnanswered() {
        index = cards.firstIndex { !$0.isAnswered } ?? cards.count
        portions = nil
    }

    private func firstSkipped(in session: PrepSession) -> Int? {
        session.cards.firstIndex { $0.status == .skipped }
    }

    /// The count in effect: the member's choice, or the suggestion the API made.
    private func chosenPortions(for card: PrepCard) -> Int {
        portions ?? card.portions?.portions ?? 1
    }

    private func finish(_ card: PrepCard) async {
        await answer {
            try await shopping.completePrepCard(card, portions: card.freezable ? chosenPortions(for: card) : nil)
        }
    }

    private func skip(_ card: PrepCard) async {
        await answer { try await shopping.skipPrepCard(card) }
    }

    private func answer(_ work: () async throws -> PrepCard) async {
        isBusy = true
        defer { isBusy = false }
        errorMessage = nil
        do {
            _ = try await work()
            advance()
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }

    /// Moves to the next card nobody has answered, or to the summary.
    private func advance() {
        portions = nil
        let next = cards.indices.first { $0 > index && !cards[$0].isAnswered }
        index = next ?? cards.firstIndex { !$0.isAnswered } ?? cards.count
    }

    /// Plans the suggested meal through the ordinary planner, so it behaves exactly like a
    /// meal added from the library — same toast, same undo, same events.
    private func plan(_ suggestion: PrepSuggestion, on card: PrepCard) async {
        isBusy = true
        defer { isBusy = false }
        errorMessage = nil
        let entry = await planner.add(
            recipeID: suggestion.recipeID, name: suggestion.recipeName, to: shopping.week,
            day: PlanDay(rawValue: suggestion.day), servings: suggestion.servings)
        if entry == nil {
            errorMessage = planner.errorMessage ?? String(localized: "That meal couldn't be added. Try again.")
        }
    }
}

/// One card's rows: what the week needs, what's left, and how to cut it up.
private struct PrepCardSections: View {
    let card: PrepCard
    let chosenPortions: Int
    let isBusy: Bool
    let canEdit: Bool
    let setPortions: (Int) -> Void
    let plan: (PrepSuggestion) -> Void

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 6) {
                Text(card.name)
                    .font(.title3.weight(.semibold))
                Text(card.surplusText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 2)
            if card.frozen {
                Label("Already in the freezer", systemImage: "snowflake")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        if !card.meals.isEmpty {
            Section("This week's meals") {
                ForEach(card.meals) { meal in
                    PrepMealRow(meal: meal)
                }
            }
        }
        if let portions = card.portions {
            Section {
                Text(card.instruction)
                    .font(.subheadline)
                if canEdit {
                    Stepper(value: Binding(get: { chosenPortions }, set: setPortions), in: 1...portionLimit) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(chosenPortions) portions")
                            Text(sizeAndThaw(portions))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .disabled(isBusy)
                }
            } header: {
                Text("Portion the rest")
            } footer: {
                Text(basisText(portions))
            }
        }
        if !card.suggestions.isEmpty {
            Section("Or cook it again this week") {
                ForEach(card.suggestions) { suggestion in
                    Button {
                        plan(suggestion)
                    } label: {
                        PrepSuggestionLabel(suggestion: suggestion)
                    }
                    .disabled(isBusy || !canEdit)
                }
            }
        }
    }

    private var portionLimit: Int {
        max(card.portions?.options.last?.portions ?? 1, 1)
    }

    /// The size and thaw time for the chosen count — never another count's. Each option
    /// carries its own estimate for exactly this reason.
    private func sizeAndThaw(_ plan: PrepPortionPlan) -> String {
        guard let option = plan.option(chosenPortions) else {
            return plan.portionSizeText
        }
        return String(localized: "about \(option.sizeText) each · \(option.thaw.summary) to thaw")
    }

    private func basisText(_ plan: PrepPortionPlan) -> String {
        switch plan.basis {
        case .meal:
            return String(
                localized:
                    "A portion is about one meal's worth: your \(plan.meals) meals this week use \(plan.typicalMealText) each."
            )
        case .week:
            return String(localized: "A portion is about what this week uses, \(plan.typicalMealText).")
        }
    }
}

private struct PrepMealRow: View {
    let meal: PrepMeal

    var body: some View {
        HStack {
            Text(meal.recipeName)
            Spacer()
            if let day = meal.planDay {
                Text(day.name())
                    .foregroundStyle(.secondary)
            }
        }
        .font(.subheadline)
        // A meal that has already been and gone is still why the amount was bought, but the
        // card must not talk about it as though it were still coming.
        .foregroundStyle(meal.past ? AnyShapeStyle(.tertiary) : AnyShapeStyle(.primary))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            meal.past
                ? Text("\(meal.recipeName), already cooked")
                : Text(meal.planDay.map { "\(meal.recipeName), \($0.name())" } ?? meal.recipeName))
    }
}

private struct PrepSuggestionLabel: View {
    let suggestion: PrepSuggestion

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(suggestion.recipeName)
            if let detail {
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var detail: String? {
        var parts: [String] = []
        if let day = PlanDay(rawValue: suggestion.day) {
            parts.append(day.name())
        }
        if let minutes = suggestion.cookMinutes {
            parts.append(String(localized: "\(minutes) min"))
        }
        parts += suggestion.reasons.prefix(1)
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

/// "The smallest pack was bigger than this week needs." Opens the prep checklist. Shown on
/// the Shop tab and in the Pantry, because putting the groceries away belongs to both.
struct PrepSessionBanner: View {
    let session: PrepSession
    let open: () -> Void

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label("Put the Groceries Away", systemImage: "arrow.up.bin")
                    .font(.headline)
                Text(summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Button("Start", action: open)
                    .buttonStyle(.bordered)
            }
            .padding(.vertical, 4)
        }
    }

    private var summary: String {
        guard let first = session.cards.first(where: { !$0.isAnswered }) else { return session.headline }
        if session.pending == 1 {
            return first.instruction
        }
        return session.headline + " " + String(localized: "Starting with \(first.name).")
    }
}
