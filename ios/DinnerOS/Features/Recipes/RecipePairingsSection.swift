import SwiftUI

/// "Tastes even better with": the add-ons and grocery items that go with this recipe.
///
/// Checking a card adds it to the week through the pairings API. A pairing always belongs to
/// a planned meal, so when the recipe isn't in the week yet it is planned first and the
/// pairing accepted against the new entry (#312). Items already in the week arrive with
/// `inPlan` and show checked; unchecking removes the add-on's entry or the grocery line.
struct RecipePairingsSection: View {
    /// The carousel's response: its items, and the recipe's plan entry for this week.
    let pairings: RecipePairings
    /// The recipe this screen is showing, planned or not.
    let recipe: RecipeSummary
    let reload: () async -> Void

    @Environment(PairingsStore.self) private var store
    @Environment(PlanStore.self) private var plans
    @Environment(MealPlanner.self) private var planner
    @Environment(HouseholdStore.self) private var households
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var actionError: String?
    @State private var ruleMessage: String?

    private var items: [Pairing] { pairings.items }

    /// Hiding controls is a convenience; the API enforces `plan.edit` on every change.
    private var canEdit: Bool {
        households.access?.can(.planEdit) == true && plans.isDraft && !store.isForbidden
    }

    private var cardWidth: CGFloat {
        dynamicTypeSize.isAccessibilitySize ? 220 : 150
    }

    var body: some View {
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
                Text("Tastes even better with")
                    .font(.title3.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                ScrollView(.horizontal) {
                    LazyHStack(alignment: .top, spacing: 12) {
                        ForEach(items) { pairing in
                            card(pairing)
                        }
                    }
                    .scrollTargetLayout()
                }
                .scrollIndicators(.hidden)
                .scrollTargetBehavior(.viewAligned)
                if let ruleMessage {
                    Label(ruleMessage, systemImage: "checkmark.circle")
                        .font(.footnote)
                        .foregroundStyle(.tint)
                        .transition(.opacity)
                }
                footnote
            }
            .animation(.default, value: ruleMessage)
            .alert("Couldn't Change Your Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
        }
    }

    @ViewBuilder
    private var footnote: some View {
        if !canEdit {
            Text("Your role can see these but not add them.")
                .font(.footnote)
                .foregroundStyle(Color.secondary)
        } else if pairings.entryID == nil {
            Text("Adding one of these also adds \(recipe.name) to your week.")
                .font(.footnote)
                .foregroundStyle(Color.secondary)
        } else {
            Text("Added to your week with this meal.")
                .font(.footnote)
                .foregroundStyle(Color.secondary)
        }
    }

    // MARK: Cards

    private func card(_ pairing: Pairing) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            photo(pairing)
                .overlay(alignment: .topTrailing) {
                    if canEdit {
                        checkbox(pairing)
                            .padding(4)
                    }
                }
            Text(pairing.name)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if let detail = detailText(pairing) {
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
            }
            if let reason = PairingFormat.reason(pairing, vocabulary: autopilot.vocabulary) {
                Text(reason)
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
                    .lineLimit(3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if canEdit, pairing.offersRule {
                Button("Always add this") { makeRule(pairing) }
                    .buttonStyle(.borderless)
                    .font(.caption.weight(.semibold))
                    .disabled(isBusy(pairing))
                    .accessibilityHint("Makes this a household rule")
            }
        }
        .frame(width: cardWidth, alignment: .leading)
        .accessibilityElement(children: .contain)
    }

    /// An add-on has its own photo; a grocery item has none, so it gets a basket glyph.
    @ViewBuilder
    private func photo(_ pairing: Pairing) -> some View {
        switch pairing.target {
        case .recipe(let addon):
            RecipePhoto(url: addon.imageURL, pointWidth: cardWidth, cornerRadius: 14)
        case .groceryItem:
            RoundedRectangle(cornerRadius: 14)
                .fill(Color(.secondarySystemBackground))
                .aspectRatio(4.0 / 3.0, contentMode: .fit)
                .overlay {
                    Image(systemName: "basket")
                        .font(.title)
                        .foregroundStyle(Color.secondary)
                }
                .accessibilityHidden(true)
        }
    }

    /// "15 min" for an add-on, "1 package" for a grocery item.
    private func detailText(_ pairing: Pairing) -> String? {
        switch pairing.target {
        case .recipe(let addon):
            guard let minutes = addon.cookMinutes, minutes > 0 else { return nil }
            return RecipeFormat.minutes(minutes)
        case .groceryItem(let item):
            return item.amountText
        }
    }

    private func checkbox(_ pairing: Pairing) -> some View {
        let isChecked = pairing.inPlan
        return Button {
            toggle(pairing)
        } label: {
            Group {
                if isBusy(pairing) {
                    ProgressView()
                } else {
                    Image(systemName: isChecked ? "checkmark.square.fill" : "square")
                        .font(.title3)
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(
                            isChecked ? Color.white : Color.primary, isChecked ? Color.accentColor : Color.clear)
                }
            }
            .padding(6)
            .background(.regularMaterial, in: .rect(cornerRadius: 8))
            .frame(width: 44, height: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(isBusy(pairing))
        .accessibilityLabel(
            isChecked ? Text("Remove \(pairing.name) from your week") : Text("Add \(pairing.name) to your week")
        )
        .accessibilityAddTraits(isChecked ? [.isSelected] : [])
    }

    private func isBusy(_ pairing: Pairing) -> Bool {
        if let entryID = pairings.entryID, store.isBusy(entryID: entryID, key: pairing.key) {
            return true
        }
        // While the main recipe is being planned for a plan-then-accept.
        return planner.busyRecipeIDs.contains(recipe.id)
    }

    // MARK: Actions

    private func toggle(_ pairing: Pairing) {
        perform {
            if pairing.inPlan {
                try await store.remove(pairing, mainEntryID: pairings.entryID)
            } else {
                try await store.acceptForRecipe(
                    key: pairing.key, mainRecipeID: recipe.id, mainRecipeName: recipe.name,
                    mainEntryID: pairings.entryID)
            }
            await reload()
        }
    }

    private func makeRule(_ pairing: Pairing) {
        guard let entryID = pairings.entryID else {
            // A rule needs the meal it was suggested with, so plan the meal first.
            actionError = String(localized: "Add \(recipe.name) to your week first, then keep this as a rule.")
            return
        }
        perform {
            guard let result = try await store.makeRule(entryID: entryID, key: pairing.key) else { return }
            ruleMessage =
                result.status == "unchanged"
                ? String(localized: "You already have a rule for that.")
                : String(localized: "Saved: \(pairing.name) is now a rule.")
            await reload()
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

#Preview("Pairings, planned") {
    ScrollView {
        RecipePairingsSection(
            pairings: MenuPreviewData.recipePairings, recipe: MenuPreviewData.cards[1].recipe, reload: {}
        )
        .padding()
    }
    .menuPreviewEnvironment()
}

#Preview("Pairings, not planned") {
    ScrollView {
        RecipePairingsSection(
            pairings: RecipePairings(
                recipeID: "recipe-3", week: MenuPreviewData.week.description, entryID: nil,
                mealCategories: [.pasta],
                items: MenuPreviewData.pairings.map { pairing in
                    var copy = pairing
                    copy.inPlan = false
                    return copy
                }),
            recipe: MenuPreviewData.cards[1].recipe, reload: {}
        )
        .padding()
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
}

#Preview("Pairings, accessibility size") {
    ScrollView {
        RecipePairingsSection(
            pairings: MenuPreviewData.recipePairings, recipe: MenuPreviewData.cards[1].recipe, reload: {}
        )
        .padding()
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility2)
}
