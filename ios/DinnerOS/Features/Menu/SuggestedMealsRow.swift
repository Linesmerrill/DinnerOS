import SwiftUI

/// Autopilot's picks for the open days of a week, shown right under the planned meals as dashed
/// cards: "here's what we'd cook". Press and hold one to shuffle it or leave that day open;
/// Add puts the rest in the week. Why each was picked comes from the server (`reasons`).
struct SuggestedMealsRow: View {
    let proposal: AutopilotProposal
    let isEditable: Bool
    let review: () -> Void

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var failure: String?
    @State private var isAdding = false

    private var slots: [AutopilotSlot] { proposal.slots.sorted { $0.date < $1.date } }
    private var included: Int { slots.filter { !autopilot.excludedSlotIDs.contains($0.id) }.count }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Label("Suggested for You", systemImage: "sparkles")
                        .font(.headline)
                    Text(
                        isEditable
                            ? "From what you like and what's in your pantry. Press and hold one to shuffle it."
                            : "From what you like and what's in your pantry."
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 8)
                if isEditable {
                    Button {
                        add()
                    } label: {
                        if isAdding {
                            ProgressView()
                        } else {
                            Text(included == 1 ? "Add 1" : "Add \(included)")
                                .font(.subheadline.weight(.semibold))
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(included == 0 || isAdding)
                    .accessibilityLabel(Text("Add \(included) suggested meals to the week"))
                }
            }
            .padding(.horizontal, 16)
            cards
            if isEditable, slots.contains(where: { !$0.pairings.isEmpty }) {
                Button("Review Sides and Extras", action: review)
                    .font(.subheadline)
                    .padding(.horizontal, 16)
            }
        }
        .alert(
            "Couldn't Change That",
            isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(failure ?? "")
        }
    }

    @ViewBuilder
    private var cards: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(spacing: 16) {
                ForEach(slots) { slot in card(slot) }
            }
            .padding(.horizontal, 16)
        } else {
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 12) {
                    ForEach(slots) { slot in
                        card(slot)
                            .containerRelativeFrame(.horizontal) { width, _ in
                                width < 600 ? width * 0.62 : min(width * 0.5, max(280, (width - 48) / 4))
                            }
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, 16)
            }
            .scrollIndicators(.hidden)
            .scrollTargetBehavior(.viewAligned)
        }
    }

    private func card(_ slot: AutopilotSlot) -> some View {
        let skipped = autopilot.excludedSlotIDs.contains(slot.id)
        let swapping = autopilot.swappingSlotIDs.contains(slot.id)
        return SuggestedMealCard(slot: slot, isSkipped: skipped, isShuffling: swapping)
            .contextMenu {
                if isEditable {
                    Button("Shuffle", systemImage: "shuffle") { shuffle(slot) }
                        .disabled(swapping)
                    if skipped {
                        Button("Add Back", systemImage: "plus.circle") {
                            autopilot.setSlot(slot.id, included: true)
                        }
                    } else {
                        Button("Leave This Day Open", systemImage: "minus.circle") {
                            autopilot.setSlot(slot.id, included: false)
                        }
                    }
                }
            }
            .accessibilityAction(named: Text("Shuffle")) { shuffle(slot) }
    }

    private func shuffle(_ slot: AutopilotSlot) {
        Task {
            do {
                try await autopilot.swap(slotID: slot.id)
            } catch is CancellationError {
                return
            } catch {
                failure = HouseholdStore.message(for: error)
            }
        }
    }

    private func add() {
        isAdding = true
        Task {
            defer { isAdding = false }
            do {
                try await autopilot.accept()
            } catch is CancellationError {
                return
            } catch {
                failure = HouseholdStore.message(for: error)
            }
        }
    }
}

/// One suggested dinner: the photo, the day, the recipe, and why it was picked, drawn with a
/// dashed outline so it reads as a suggestion rather than a planned meal.
struct SuggestedMealCard: View {
    let slot: AutopilotSlot
    let isSkipped: Bool
    let isShuffling: Bool

    private var summary: RecipeSummary {
        RecipeSummary.placeholder(
            id: slot.recipe.id, name: slot.recipe.name, imageURLString: slot.recipe.imageURLString)
    }

    var body: some View {
        NavigationLink(value: summary) {
            VStack(alignment: .leading, spacing: 8) {
                RecipePhoto(url: slot.recipe.imageURL, aspectRatio: 4.0 / 3.0, pointWidth: 280, cornerRadius: 14)
                    .overlay(alignment: .topLeading) {
                        Label(slot.day.name(), systemImage: "sparkles")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(.regularMaterial, in: Capsule())
                            .padding(8)
                    }
                    .overlay {
                        if isShuffling {
                            RoundedRectangle(cornerRadius: 14).fill(.black.opacity(0.35))
                            ProgressView().tint(.white)
                        }
                    }
                Text(slot.recipe.name)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.primary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                if isSkipped {
                    Text("Left open")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else if !slot.reasonText.isEmpty {
                    Text(slot.reasonText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                }
            }
            .padding(8)
            .background(
                RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .strokeBorder(
                        Color.accentColor.opacity(isSkipped ? 0.3 : 0.8),
                        style: StrokeStyle(lineWidth: 2, dash: [7, 5]))
            )
            .opacity(isSkipped ? 0.45 : 1)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            Text(
                isSkipped
                    ? "Suggested for \(slot.day.name()): \(slot.recipe.name), left open"
                    : "Suggested for \(slot.day.name()): \(slot.recipe.name). \(slot.reasonText)")
        )
        .accessibilityHint(Text("Opens the recipe. Actions: shuffle, or leave this day open."))
    }
}
