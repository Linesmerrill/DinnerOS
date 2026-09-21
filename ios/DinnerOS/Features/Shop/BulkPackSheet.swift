import SwiftUI

/// "The smallest pack was 4 lb, this week needs 10 oz."
///
/// One card per bulk pack, with the two real answers: plan another meal this week that uses
/// it, or portion and freeze the rest (docs/shopping-providers.md#bulk-packs). Doing neither
/// is fine — the member closes the sheet and nothing changes, which is exactly what happens
/// today.
struct BulkPackSheet: View {
    let packs: [ShoppingBulkPack]
    let week: ISOWeek
    let handoffID: String

    @Environment(ShoppingStore.self) private var shopping
    @Environment(PantryStore.self) private var pantry
    @Environment(MealPlanner.self) private var planner
    @Environment(\.dismiss) private var dismiss

    @State private var busyLineIDs: Set<String> = []
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            List {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                ForEach(packs) { pack in
                    Section {
                        BulkPackCard(
                            pack: pack,
                            isBusy: busyLineIDs.contains(pack.lineID),
                            plan: { suggestion in Task { await plan(suggestion, for: pack) } },
                            freeze: { Task { await freeze(pack) } })
                    }
                }
            }
            .navigationTitle("More Than This Week Needs")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }

    /// Plans the suggested meal through the ordinary planner, so it behaves exactly like a
    /// meal added from the library — same toast, same undo, same events.
    private func plan(_ suggestion: ShoppingBulkPackSuggestion, for pack: ShoppingBulkPack) async {
        busyLineIDs.insert(pack.lineID)
        defer { busyLineIDs.remove(pack.lineID) }
        errorMessage = nil
        let entry = await planner.add(
            recipeID: suggestion.recipeID, name: suggestion.recipeName, to: week,
            day: PlanDay(rawValue: suggestion.day), servings: suggestion.servings)
        guard entry != nil else {
            errorMessage = planner.errorMessage ?? String(localized: "That meal couldn't be added. Try again.")
            return
        }
        shopping.settleBulkPack(pack)
        if packs.count == 1 { dismiss() }
    }

    /// Records the whole surplus in the freezer, in one portion. The member can split it
    /// afterwards in the pantry; guessing a portion count for them would be worse than
    /// recording the amount honestly.
    private func freeze(_ pack: ShoppingBulkPack) async {
        busyLineIDs.insert(pack.lineID)
        defer { busyLineIDs.remove(pack.lineID) }
        errorMessage = nil
        do {
            try await pantry.freeze(
                request: FreezePantryItemRequest(
                    ingredientID: pack.ingredientID, name: pack.name,
                    quantity: pack.surplus, unit: pack.unit,
                    source: FreezePantryItemRequest.FreezeSource(
                        provider: shopping.provider, handoffID: handoffID, lineID: pack.lineID)))
            shopping.settleBulkPack(pack)
            if packs.count == 1 { dismiss() }
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

private struct BulkPackCard: View {
    let pack: ShoppingBulkPack
    let isBusy: Bool
    let plan: (ShoppingBulkPackSuggestion) -> Void
    let freeze: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(pack.name)
                .font(.headline)
            Text(pack.surplusText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            if !pack.suggestions.isEmpty {
                Text("Cook it again this week")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                ForEach(pack.suggestions) { suggestion in
                    Button {
                        plan(suggestion)
                    } label: {
                        BulkPackSuggestionLabel(suggestion: suggestion)
                    }
                    .buttonStyle(.bordered)
                    .disabled(isBusy)
                }
            }
            if pack.freezable {
                Button {
                    freeze()
                } label: {
                    Label(freezeTitle, systemImage: "snowflake")
                }
                .buttonStyle(.borderedProminent)
                .disabled(isBusy)
                Text("We'll remind you to take it out on the day a meal needs it.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if isBusy {
                ProgressView()
            }
        }
        .padding(.vertical, 4)
    }

    private var freezeTitle: String {
        String(localized: "Freeze the Rest")
    }
}

private struct BulkPackSuggestionLabel: View {
    let suggestion: ShoppingBulkPackSuggestion

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
