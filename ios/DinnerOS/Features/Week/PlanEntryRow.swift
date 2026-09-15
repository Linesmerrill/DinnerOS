import SwiftUI

/// A planned recipe: thumbnail, name, servings, note, and whether it was cooked or
/// skipped.
struct PlanEntryRow: View {
    let entry: PlanEntry
    var outcome: EventReporter.EntryOutcome?

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 64.0

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            if !dynamicTypeSize.isAccessibilitySize {
                RecipeImage(url: entry.recipe.imageURL)
                    .frame(width: min(thumbnailWidth, 96))
                    .clipShape(.rect(cornerRadius: 6))
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.recipe.name)
                    .font(.headline)
                Text("\(entry.servings) servings")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if !entry.note.isEmpty {
                    Text(entry.note)
                        .font(.subheadline)
                        .italic()
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                        .accessibilityLabel("Note: \(entry.note)")
                }
                outcomeLabel
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 2)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var outcomeLabel: some View {
        switch outcome {
        case .cooked:
            Label("Cooked", systemImage: "checkmark.circle.fill")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tint)
        case .skipped(let reason):
            Label(
                reason.map { String(localized: "Skipped: \($0.title)") } ?? String(localized: "Skipped"),
                systemImage: "forward.fill"
            )
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
        case nil:
            EmptyView()
        }
    }
}

#Preview {
    List(Array(PlanPreviewData.plan.entries.enumerated()), id: \.element.id) { index, entry in
        PlanEntryRow(entry: entry, outcome: index == 0 ? .cooked : index == 1 ? .skipped(.noTime) : nil)
    }
}
