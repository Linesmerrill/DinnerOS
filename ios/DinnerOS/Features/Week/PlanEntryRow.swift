import SwiftUI

/// A planned recipe: thumbnail, name, servings, and note.
struct PlanEntryRow: View {
    let entry: PlanEntry

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
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 2)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    List(PlanPreviewData.plan.entries) { entry in
        PlanEntryRow(entry: entry)
    }
}
