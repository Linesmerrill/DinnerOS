import SwiftUI

/// "How was it?" and five stars on a meal whose night has passed: one tap rates it.
///
/// The rating screen is at the bottom of a recipe's Description tab, which is a photo, a title
/// block and a scroll away from the meal itself, and nothing on the Menu ever mentions it. This is
/// the same rating, on the card the meal is already showing on, so the answer costs one tap at the
/// moment the question makes sense.
///
/// A tap saves the score alone. An existing comment and tags are carried through untouched, so
/// changing a 4 to a 5 here never quietly discards what someone typed on the recipe screen, and
/// the fuller rating — tags, a comment, removing it — stays there.
struct MealRatingRow: View {
    /// The card's recipe, which carries the member's own rating and the household average.
    let recipe: RecipeSummary

    @Environment(RecipeLibrary.self) private var library
    @Environment(MenuStore.self) private var menu

    @State private var isSaving = false
    @State private var failed = false
    @State private var savedCount = 0

    private var score: Int { recipe.myRating?.score ?? 0 }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                Text(score == 0 ? "How was it?" : "Your rating")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                if isSaving {
                    ProgressView()
                        .controlSize(.small)
                }
            }
            StarRatingControl(score: binding)
                .disabled(isSaving)
            if failed {
                Text("Couldn't save that rating. Tap a star to try again.")
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .sensoryFeedback(.success, trigger: savedCount)
    }

    /// Reads the saved score and saves on a tap. `StarRatingControl` is the recipe screen's own
    /// control, so the stars, the hit targets, and what VoiceOver says are the same in both places.
    private var binding: Binding<Int> {
        Binding(get: { score }, set: { save($0) })
    }

    private func save(_ value: Int) {
        guard RatingLimits.scores.contains(value), value != score, !isSaving else { return }
        isSaving = true
        failed = false
        Task {
            defer { isSaving = false }
            do {
                var draft = RatingDraft(rating: recipe.myRating)
                draft.score = value
                let saved = try await library.saveRating(draft, recipeID: recipe.id)
                // The library reloads the rated recipe, so its cached copy carries the new
                // household average; without one the card keeps the average it had.
                menu.applyRating(
                    recipeID: recipe.id, mine: saved,
                    household: library.cachedRecipe(id: recipe.id)?.householdRating)
                savedCount += 1
            } catch is CancellationError {
                return
            } catch {
                failed = true
            }
        }
    }
}

#Preview("Unrated") {
    MealRatingRow(recipe: MenuPreviewData.cards[1].recipe)
        .padding()
        .menuPreviewEnvironment()
}

#Preview("Rated") {
    MealRatingRow(recipe: RecipePreviewData.summaries[0])
        .padding()
        .menuPreviewEnvironment()
}
