import SwiftUI

/// The household's rating of a recipe and the signed-in user's own rating: stars, tags,
/// an optional comment, and Save or Remove.
struct RecipeRatingSection: View {
    let recipe: Recipe
    /// Receives the recipe after a rating change, with the new household average when
    /// the reload succeeded.
    let onChange: (Recipe) -> Void

    @Environment(RecipeLibrary.self) private var library

    @State private var draft: RatingDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var savedCount = 0

    init(recipe: Recipe, onChange: @escaping (Recipe) -> Void) {
        self.recipe = recipe
        self.onChange = onChange
        _draft = State(initialValue: RatingDraft(rating: recipe.myRating))
    }

    var body: some View {
        DetailSection("Your Rating") {
            StarRatingControl(score: $draft.score)
            TagChips(draft: $draft)
            commentField
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
            }
            actions
        }
        .disabled(isSaving)
        .sensoryFeedback(.success, trigger: savedCount)
        .onChange(of: recipe.myRating) { previous, current in
            // A reload replaced the rating; keep edits the user hasn't saved.
            if draft == RatingDraft(rating: previous) {
                draft = RatingDraft(rating: current)
            }
        }
    }

    private var commentField: some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField("Add a comment (optional)", text: $draft.comment, axis: .vertical)
                .lineLimit(2...6)
                .textFieldStyle(.roundedBorder)
            let length = draft.commentLength
            if length > RatingLimits.maxCommentLength - 100 {
                Text("\(length) of \(RatingLimits.maxCommentLength) characters")
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(draft.isCommentTooLong ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
            }
        }
    }

    private var actions: some View {
        HStack(spacing: 12) {
            Button(recipe.myRating == nil ? "Save Rating" : "Update Rating") {
                Task { await save() }
            }
            .buttonStyle(.borderedProminent)
            .disabled(!draft.canSave || !draft.differs(from: recipe.myRating))
            if recipe.myRating != nil {
                Button("Remove", role: .destructive) {
                    Task { await remove() }
                }
                .buttonStyle(.bordered)
            }
            if isSaving {
                ProgressView()
            }
        }
    }

    private func save() async {
        await change {
            let saved = try await library.saveRating(draft, recipeID: recipe.id)
            var updated = library.cachedRecipe(id: recipe.id) ?? recipe
            updated.myRating = saved
            return updated
        }
    }

    private func remove() async {
        await change {
            try await library.removeRating(recipeID: recipe.id)
            var updated = library.cachedRecipe(id: recipe.id) ?? recipe
            updated.myRating = nil
            return updated
        }
    }

    private func change(_ operation: () async throws -> Recipe) async {
        isSaving = true
        errorMessage = nil
        defer { isSaving = false }
        do {
            let updated = try await operation()
            draft = RatingDraft(rating: updated.myRating)
            onChange(updated)
            savedCount += 1
        } catch is CancellationError {
            return
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

/// "4.5 ★ from 2" with a link to every member's rating, or "No ratings yet".
struct HouseholdRatingSummary: View {
    let recipe: Recipe

    var body: some View {
        let rating = recipe.householdRating
        if let summary = RatingFormat.summary(rating) {
            NavigationLink(value: HouseholdRatingsRoute(recipeID: recipe.id, recipeName: recipe.name)) {
                HStack(spacing: 6) {
                    Text(summary)
                        .foregroundStyle(.primary)
                    Text("See household ratings")
                        .foregroundStyle(.tint)
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                }
                .font(.subheadline)
            }
            .accessibilityLabel(RatingFormat.accessibilitySummary(rating))
            .accessibilityHint("Shows every member's rating")
        } else {
            Text("No ratings yet")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }
}

/// Five tappable stars. VoiceOver reads it as one adjustable control.
struct StarRatingControl: View {
    @Binding var score: Int

    var body: some View {
        HStack(spacing: 2) {
            ForEach(RatingLimits.scores, id: \.self) { value in
                Button {
                    score = value
                } label: {
                    Image(systemName: value <= score ? "star.fill" : "star")
                        .font(.title2)
                        .foregroundStyle(value <= score ? AnyShapeStyle(.orange) : AnyShapeStyle(.secondary))
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Your rating")
        .accessibilityValue(score == 0 ? String(localized: "Not rated") : String(localized: "\(score) of 5 stars"))
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: score = min(RatingLimits.scores.upperBound, score + 1)
            case .decrement: score = max(RatingLimits.scores.lowerBound, score - 1)
            @unknown default: break
            }
        }
    }
}

/// A read-only star score, for example on a member's rating.
struct StarsLabel: View {
    let score: Int

    var body: some View {
        HStack(spacing: 1) {
            ForEach(RatingLimits.scores, id: \.self) { value in
                Image(systemName: value <= score ? "star.fill" : "star")
            }
        }
        .font(.caption)
        .foregroundStyle(.orange)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(score) of 5 stars")
    }
}

/// The allowlisted tags as toggle chips. Choosing Make Again clears Never Again, and the
/// other way around.
private struct TagChips: View {
    @Binding var draft: RatingDraft

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(RatingTag.known) { tag in
                let isSelected = draft.contains(tag)
                Button {
                    draft.toggle(tag)
                } label: {
                    TagChip(title: tag.title, isSelected: isSelected)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(isSelected ? .isSelected : [])
                .accessibilityHint(hint(for: tag, isSelected: isSelected))
            }
        }
    }

    private func hint(for tag: RatingTag, isSelected: Bool) -> String {
        guard !isSelected, let excluded = tag.excluded, draft.contains(excluded) else { return "" }
        return String(localized: "Also clears \(excluded.title)")
    }
}

struct TagChip: View {
    let title: String
    var isSelected = false

    var body: some View {
        Text(title)
            .font(.subheadline)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .foregroundStyle(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
            .background(isSelected ? AnyShapeStyle(.tint.opacity(0.15)) : AnyShapeStyle(.quaternary), in: .capsule)
            .overlay {
                Capsule().strokeBorder(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.clear))
            }
            .contentShape(.capsule)
    }
}

/// Lays subviews out in rows, wrapping to the next row when one is full.
struct FlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let rows = arrange(subviews, width: proposal.width ?? .infinity)
        let width = rows.map(\.width).max() ?? 0
        let height = rows.map(\.height).reduce(0, +) + spacing * CGFloat(max(rows.count - 1, 0))
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for row in arrange(subviews, width: bounds.width) {
            var x = bounds.minX
            for index in row.indices {
                let size = subviews[index].sizeThatFits(ProposedViewSize(width: bounds.width, height: nil))
                subviews[index].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
            y += row.height + spacing
        }
    }

    private struct Row {
        var indices: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func arrange(_ subviews: Subviews, width maxWidth: CGFloat) -> [Row] {
        var rows: [Row] = []
        var current = Row()
        for index in subviews.indices {
            let size = subviews[index].sizeThatFits(ProposedViewSize(width: maxWidth, height: nil))
            let proposedWidth = current.indices.isEmpty ? size.width : current.width + spacing + size.width
            if proposedWidth > maxWidth, !current.indices.isEmpty {
                rows.append(current)
                current = Row(indices: [index], width: size.width, height: size.height)
            } else {
                current.indices.append(index)
                current.width = proposedWidth
                current.height = max(current.height, size.height)
            }
        }
        if !current.indices.isEmpty {
            rows.append(current)
        }
        return rows
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                HouseholdRatingSummary(recipe: RecipePreviewData.recipe)
                RecipeRatingSection(recipe: RecipePreviewData.recipe) { _ in }
            }
            .padding()
        }
    }
    .environment(RecipePreviewData.library(session: session))
}
