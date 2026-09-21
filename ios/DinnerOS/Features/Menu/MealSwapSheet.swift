import SwiftUI

/// "Try Something Similar": a few meals like the planned one, each with its reasons, and one
/// tap to put it in the week instead (`docs/autopilot.md#try-something-similar`).
///
/// The day, the servings, and the rest of the week don't change. Asking for others remembers
/// what was already shown, so the list moves on rather than repeating itself.
struct MealSwapSheet: View {
    let target: MealSwapStore.Target

    @Environment(MealSwapStore.self) private var swaps
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Try Something Similar")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") {
                            swaps.cancel()
                            dismiss()
                        }
                    }
                }
                .task(id: target.entryID) { await swaps.load() }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch swaps.phase {
        case .loading:
            ProgressView("Looking for something similar…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Find Anything Else", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await swaps.load() }
                }
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            Section {
                if swaps.alternatives.isEmpty {
                    ContentUnavailableView(
                        "Nothing Else Fits This Night", systemImage: "fork.knife",
                        description: Text(
                            swaps.notice
                                ?? String(
                                    localized:
                                        "Your library has nothing else for this day. Add a recipe, or browse Try Something Else."
                                ))
                    )
                    .listRowSeparator(.hidden)
                } else {
                    ForEach(swaps.alternatives) { alternative in
                        MealSwapRow(
                            alternative: alternative,
                            isApplying: swaps.applyingRecipeID == alternative.recipe.id,
                            isDisabled: swaps.applyingRecipeID != nil
                        ) {
                            Task { await swaps.apply(alternative) }
                        }
                    }
                }
            } header: {
                Text("Instead of \(target.recipeName)")
            } footer: {
                if let notice = swaps.notice, !swaps.alternatives.isEmpty {
                    Text(notice)
                }
            }
            Section {
                Button("Show Me Others", systemImage: "arrow.clockwise") {
                    Task { await swaps.showOthers() }
                }
                .disabled(swaps.applyingRecipeID != nil || swaps.alternatives.isEmpty)
            }
        }
    }
}

/// One alternative: its photo, name, facts, and why it's like the meal being replaced.
struct MealSwapRow: View {
    let alternative: MealAlternative
    let isApplying: Bool
    let isDisabled: Bool
    let choose: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 88.0

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
        layout {
            RecipeImage(url: alternative.recipe.imageURL)
                .frame(width: dynamicTypeSize.isAccessibilitySize ? nil : min(thumbnailWidth, 140))
                .clipShape(.rect(cornerRadius: 8))
            VStack(alignment: .leading, spacing: 4) {
                Text(alternative.recipe.name)
                    .font(.headline)
                Text(details)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if !alternative.reasonText.isEmpty {
                    Label(alternative.reasonText, systemImage: "sparkles")
                        .font(.caption)
                        .foregroundStyle(.tint)
                }
                Button {
                    choose()
                } label: {
                    if isApplying {
                        ProgressView()
                    } else {
                        Label("Cook This Instead", systemImage: "arrow.triangle.2.circlepath")
                    }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .controlSize(.small)
                .disabled(isDisabled)
                .accessibilityLabel("Cook \(alternative.recipe.name) instead")
            }
        }
        .padding(.vertical, 4)
    }

    /// For example "30 min · Serves 2".
    private var details: String {
        var parts: [String] = []
        if let minutes = alternative.cookMinutes {
            parts.append(RecipeFormat.minutes(minutes))
        }
        parts.append(MenuFormat.servings(alternative.servings))
        return parts.joined(separator: " · ")
    }
}
