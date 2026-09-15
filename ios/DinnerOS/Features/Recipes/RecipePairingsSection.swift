import SwiftUI

/// "Tastes even better with": add-ons that go with the recipe. Checking one adds it to the same
/// day as this meal. Hidden when the household has no pairings, or the API doesn't serve them yet.
struct RecipePairingsSection: View {
    let pairings: [RecipePairing]
    /// The planned meal these add-ons join; `nil` when the recipe isn't in the week yet.
    let mainEntry: PlanEntry?
    let reload: () async -> Void

    @Environment(MealPlanner.self) private var planner
    @Environment(PlanStore.self) private var plans

    var body: some View {
        if !pairings.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
                Text("Tastes even better with")
                    .font(.title3.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                ScrollView(.horizontal) {
                    LazyHStack(alignment: .top, spacing: 12) {
                        ForEach(pairings) { pairing in
                            card(pairing)
                        }
                    }
                    .scrollTargetLayout()
                }
                .scrollIndicators(.hidden)
                .scrollTargetBehavior(.viewAligned)
                Text(
                    mainEntry == nil
                        ? String(localized: "Add this recipe to your week to add its pairings.")
                        : String(localized: "Added to your week with this recipe.")
                )
                .font(.footnote)
                .foregroundStyle(Color.secondary)
            }
        }
    }

    private func card(_ pairing: RecipePairing) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            photo(pairing)
                .overlay(alignment: .topTrailing) {
                    checkbox(pairing)
                        .padding(4)
                }
            Text(pairing.name)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if let reason = pairing.reason {
                Text(reason)
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
                    .lineLimit(2)
            }
        }
        .frame(width: 150, alignment: .leading)
    }

    @ViewBuilder
    private func photo(_ pairing: RecipePairing) -> some View {
        switch pairing.target {
        case .recipe(let recipe):
            RecipePhoto(url: recipe.imageURL, pointWidth: 150, cornerRadius: 14)
        case .groceryItem:
            // Grocery items have no photo of their own.
            RecipePhoto(url: nil, pointWidth: 150, cornerRadius: 14)
        }
    }

    private func checkbox(_ pairing: RecipePairing) -> some View {
        let isChecked = isInPlan(pairing)
        return Button {
            toggle(pairing)
        } label: {
            Image(systemName: isChecked ? "checkmark.square.fill" : "square")
                .font(.title3)
                .symbolRenderingMode(.palette)
                .foregroundStyle(isChecked ? Color.white : Color.primary, isChecked ? Color.accentColor : Color.clear)
                .padding(6)
                .background(.regularMaterial, in: .rect(cornerRadius: 8))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(!canToggle(pairing))
        .accessibilityLabel(
            isChecked ? Text("Remove \(pairing.name) from your week") : Text("Add \(pairing.name) to your week")
        )
        .accessibilityAddTraits(isChecked ? [.isSelected] : [])
    }

    /// A recipe pairing follows the plan; a grocery item follows the server's answer.
    private func isInPlan(_ pairing: RecipePairing) -> Bool {
        switch pairing.target {
        case .recipe(let recipe): !planner.entries(recipeID: recipe.id).isEmpty
        case .groceryItem: pairing.inPlan
        }
    }

    /// Grocery-item pairings wait for the pairings API's accept action.
    private func canToggle(_ pairing: RecipePairing) -> Bool {
        guard case .recipe = pairing.target else { return false }
        return mainEntry != nil && planner.canEdit && plans.isDraft
    }

    private func toggle(_ pairing: RecipePairing) {
        guard case .recipe(let recipe) = pairing.target, let mainEntry else { return }
        Task {
            if let planned = planner.entries(recipeID: recipe.id).last {
                await planner.remove(planned)
            } else {
                // The add-on joins the meal it goes with, on the same day.
                await planner.add(
                    recipeID: recipe.id, name: recipe.name, to: plans.week, day: mainEntry.day,
                    servings: mainEntry.servings)
            }
            await reload()
        }
    }
}

// TODO(pairings API): grocery-item pairings need the API's accept action
// (`POST .../recipes/{recipeId}/pairings/{id}/accept`) before their checkbox can do anything;
// until then they're shown and disabled.

#Preview("Pairings") {
    ScrollView {
        RecipePairingsSection(
            pairings: MenuPreviewData.pairings, mainEntry: PlanPreviewData.plan.entries.first, reload: {}
        )
        .padding()
    }
    .menuPreviewEnvironment()
}
