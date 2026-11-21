import SwiftUI

/// A planned recipe: thumbnail, name, servings, note, and whether it was cooked or
/// skipped.
struct PlanEntryRow: View {
    let entry: PlanEntry
    var outcome: EventReporter.EntryOutcome?

    @Environment(MenuStore.self) private var menu
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 64.0

    var body: some View {
        HStack(alignment: .top, spacing: DS.Space.m) {
            if !dynamicTypeSize.isAccessibilitySize {
                RecipeImage(url: entry.recipe.imageURL, title: entry.recipe.name)
                    .frame(width: min(thumbnailWidth, 96))
                    .clipShape(.rect(cornerRadius: DS.Radius.small))
            }
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(entry.recipe.name)
                    .font(.headline)
                HStack(spacing: DS.Space.xs) {
                    Text(MenuFormat.servings(entry.servings))
                    if menu.isAddOn(entry) {
                        AddOnLabel()
                    }
                    if entry.isFromAutopilot {
                        AutopilotEntryBadge()
                    }
                }
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
        .padding(.vertical, DS.Space.hairline)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var outcomeLabel: some View {
        switch outcome {
        case .cooked:
            Label("Made", systemImage: "checkmark.circle.fill")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tint)
        case .skipped(let reason):
            Label(
                reason.map { String(localized: "Not made: \($0.title)") } ?? String(localized: "Not made"),
                systemImage: "xmark.circle.fill"
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
    // The row asks the menu whether an entry is an add-on.
    .menuPreviewEnvironment()
}
