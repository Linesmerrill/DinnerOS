import SwiftUI

/// "Goes well with your meals": the week's open add-on suggestions, one quiet row each, with
/// Add and Dismiss.
///
/// One section under Your Meals rather than a row on every card (#313): the suggestions are
/// about the week, several meals can have one, and a card-level affordance competed with the
/// servings stepper the cards already carry. The section hides itself when nothing is open,
/// so a household without pairings never sees it.
struct PairingSuggestionsSection: View {
    @Environment(PairingsStore.self) private var pairings
    @Environment(PlanStore.self) private var plans
    @Environment(MenuStore.self) private var menu
    @Environment(HouseholdStore.self) private var households
    @Environment(AutopilotStore.self) private var autopilot

    @State private var actionError: String?

    /// Hiding controls is a convenience; the API enforces `plan.edit`.
    private var canEdit: Bool {
        households.access?.can(.planEdit) == true && plans.isDraft && menu.selectedTiming != .past
            && !pairings.isForbidden
    }

    private var suggestions: [(meal: MealPairings, pairing: Pairing)] {
        pairings.openSuggestions
    }

    var body: some View {
        if canEdit, !suggestions.isEmpty {
            VStack(alignment: .leading, spacing: DS.Space.m) {
                MenuSectionHeader(
                    title: String(localized: "Goes well with your meals"),
                    subtitle: String(localized: "Sides and extras for this week's dinners")
                )
                .padding(.horizontal, DS.Space.l)
                VStack(spacing: 0) {
                    ForEach(Array(suggestions.enumerated()), id: \.offset) { index, item in
                        row(meal: item.meal, pairing: item.pairing)
                        if index < suggestions.count - 1 {
                            Divider()
                                .padding(.leading, DS.Space.l)
                        }
                    }
                }
                .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
                .padding(.horizontal, DS.Space.l)
            }
            .alert("Couldn't Change Your Week", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
            .accessibilityElement(children: .contain)
        }
    }

    private func row(meal: MealPairings, pairing: Pairing) -> some View {
        let isBusy = pairings.isBusy(entryID: meal.entryID, key: pairing.key)
        return VStack(alignment: .leading, spacing: DS.Space.s) {
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(PairingFormat.addPrompt(pairing))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.primary)
                Text(withMeal(meal))
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
                if let reason = PairingFormat.reason(pairing, vocabulary: autopilot.vocabulary) {
                    Text(reason)
                        .font(.caption)
                        .foregroundStyle(Color.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            HStack(spacing: DS.Space.m) {
                Button("Add") {
                    perform { try await pairings.accept(entryID: meal.entryID, key: pairing.key) }
                }
                .buttonStyle(.dsPrimary(.small))
                Button("Not This Week") {
                    perform { try await pairings.dismiss(entryID: meal.entryID, key: pairing.key) }
                }
                .buttonStyle(.dsSecondary(.small))
                if isBusy {
                    ProgressView()
                        .controlSize(.small)
                }
                Spacer(minLength: 0)
            }
            .disabled(isBusy)
        }
        .padding(DS.Space.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(PairingFormat.addPrompt(pairing)) \(withMeal(meal))")
        .accessibilityIdentifier(pairing.isCompanion ? "CompanionSuggestion" : "PairingSuggestion")
    }

    /// "with Chicken Noodle Soup on Thursday", or without the day when it's unscheduled.
    private func withMeal(_ meal: MealPairings) -> String {
        guard let day = meal.day else {
            return String(localized: "with \(meal.recipe.name)")
        }
        return String(localized: "with \(meal.recipe.name) on \(day.name())")
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

#Preview("Suggestions") {
    NavigationStack {
        ScrollView {
            PairingSuggestionsSection()
                .padding(.vertical)
        }
    }
    .menuPreviewEnvironment(pairings: MenuPreviewData.weekPairings)
}
