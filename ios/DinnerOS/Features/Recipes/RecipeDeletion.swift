import SwiftUI

extension View {
    /// Asks before deleting a recipe, deletes it, reloads the week it came off, and leaves the
    /// Undo toast to `RecipeUndoToast`.
    func recipeDeletion(
        _ summary: Binding<RecipeSummary?>, isPlanned: @escaping (RecipeSummary) -> Bool = { _ in false },
        onDeleted: @escaping () -> Void = {}
    ) -> some View {
        modifier(RecipeDeletionModifier(target: summary, isPlanned: isPlanned, onDeleted: onDeleted))
    }

    /// The "Deleted. Undo" toast for the recipe just deleted, on screens that list recipes.
    func recipeUndoToast() -> some View {
        overlay(alignment: .bottom) { RecipeUndoToast() }
    }
}

private struct RecipeDeletionModifier: ViewModifier {
    @Binding var target: RecipeSummary?
    let isPlanned: (RecipeSummary) -> Bool
    let onDeleted: () -> Void

    @Environment(RecipeLibrary.self) private var library
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @State private var errorMessage: String?

    func body(content: Content) -> some View {
        content
            .confirmationDialog(
                title, isPresented: Binding(get: { target != nil }, set: { if !$0 { target = nil } }),
                titleVisibility: .visible, presenting: target
            ) { summary in
                Button("Delete Recipe", role: .destructive) { delete(summary) }
                Button("Cancel", role: .cancel) {}
            } message: { summary in
                Text(
                    isPlanned(summary)
                        ? "It also comes off this week's menu and shopping list. You can undo this right after."
                        : "You can undo this right after.")
            }
            .alert("Couldn't Delete That", isPresented: Binding(presenting: $errorMessage)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
    }

    private var title: String {
        target.map { String(localized: "Delete \($0.name)?") } ?? ""
    }

    private func delete(_ summary: RecipeSummary) {
        Task {
            do {
                let result = try await library.deleteRecipe(summary)
                onDeleted()
                await menu.reload()
                if !result.removedFromWeeks.isEmpty { await plans.reload() }
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// "No Bake Cookies deleted. Undo" for a few seconds after a delete.
struct RecipeUndoToast: View {
    @Environment(RecipeLibrary.self) private var library
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @State private var isRestoring = false
    @State private var errorMessage: String?

    var body: some View {
        if let deleted = library.recentlyDeleted {
            HStack(spacing: 12) {
                Text(errorMessage ?? String(localized: "\(deleted.summary.name) deleted."))
                    .font(.subheadline)
                    .lineLimit(2)
                Spacer(minLength: 8)
                Button {
                    undo(deleted)
                } label: {
                    if isRestoring { ProgressView() } else { Text("Undo").fontWeight(.semibold) }
                }
                .disabled(isRestoring)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 52)
            .background(.regularMaterial, in: .capsule)
            .padding(.horizontal, 16)
            .padding(.bottom, 12)
            .transition(.move(edge: .bottom).combined(with: .opacity))
            .task(id: deleted.id) {
                try? await Task.sleep(for: .seconds(8))
                if !Task.isCancelled, library.recentlyDeleted?.id == deleted.id, !isRestoring {
                    library.dismissDeleted()
                }
            }
            .accessibilityElement(children: .contain)
        }
    }

    private func undo(_ deleted: DeletedRecipe) {
        isRestoring = true
        Task {
            defer { isRestoring = false }
            do {
                try await library.undoDelete()
                await menu.reload()
                if !deleted.removedFromWeeks.isEmpty { await plans.reload() }
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}
