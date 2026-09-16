import SwiftUI

/// What Autopilot has learned from swaps, meals left out, accepted suggestions, views, and
/// cooks — each shown the way it appears in a suggestion's reasons — with a way to reset it.
struct AutopilotLearningView: View {
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households

    @State private var learning: AutopilotLearning?
    @State private var loadError: String?
    @State private var actionError: String?
    @State private var isConfirmingReset = false
    @State private var isResetting = false

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    var body: some View {
        Group {
            if let learning {
                list(learning)
            } else if let loadError {
                ContentUnavailableView {
                    Label("Couldn't Load What Autopilot Learned", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(loadError)
                } actions: {
                    Button("Try Again") {
                        Task { await load() }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else {
                ProgressView("Loading…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("What Autopilot Learned")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(
            "Reset what Autopilot learned?", isPresented: $isConfirmingReset, titleVisibility: .visible
        ) {
            Button("Reset Learning", role: .destructive, action: reset)
        } message: {
            Text("Your ratings, meal history, and preferences stay. Autopilot starts learning again from now.")
        }
    }

    @ViewBuilder
    private func list(_ learning: AutopilotLearning) -> some View {
        List {
            if let actionError {
                Section {
                    FormErrorLabel(message: actionError)
                }
            }
            Section {
                if learning.adjustments.isEmpty {
                    Text(
                        "Nothing yet. As you swap, skip, and cook suggested meals, what Autopilot notices shows up here."
                    )
                    .foregroundStyle(.secondary)
                } else {
                    ForEach(learning.adjustments) { adjustment in
                        LearnedAdjustmentRow(adjustment: adjustment)
                    }
                }
            } footer: {
                Text(
                    "Learning nudges suggestions a little. What you set in preferences always comes first, and allergies and exclusions are never overridden."
                )
            }
            if canEdit {
                Section {
                    Button("Reset Learning", systemImage: "arrow.counterclockwise", role: .destructive) {
                        isConfirmingReset = true
                    }
                    .disabled(isResetting)
                } footer: {
                    if let resetAt = learning.resetAt {
                        Text("Last reset \(resetAt.formatted(date: .abbreviated, time: .omitted)).")
                    }
                }
            }
        }
    }

    private func load() async {
        do {
            learning = try await autopilot.learning()
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func reset() {
        isResetting = true
        actionError = nil
        Task {
            defer { isResetting = false }
            do {
                learning = try await autopilot.resetLearning()
            } catch is CancellationError {
                return
            } catch {
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

private struct LearnedAdjustmentRow: View {
    let adjustment: AutopilotLearnedAdjustment

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(adjustment.label)
                Text(adjustment.text)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: adjustment.isAway ? "arrow.down.circle" : "arrow.up.circle")
                .foregroundStyle(adjustment.isAway ? Color.orange : Color.green)
                .accessibilityLabel(adjustment.isAway ? "Less often" : "More often")
        }
        .accessibilityElement(children: .combine)
    }
}
