import SwiftUI

/// "Share with other households" on a recipe's page, shown when the household picks the
/// recipes it shares one by one (Household settings, Recipe Sharing). Sharing copies the
/// recipe into Shared Recipes; turning it off stops sharing it again but leaves copies others
/// already added.
struct RecipeSharingRow: View {
    let recipe: Recipe

    @Environment(HouseholdStore.self) private var households
    @Environment(RecipeLibrary.self) private var library
    @State private var isShared: Bool?
    @State private var isSaving = false
    @State private var failure: String?

    private var shown: Bool { isShared ?? recipe.sharedToCatalog }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Toggle(isOn: Binding(get: { shown }, set: { save($0) })) {
                Label("Share With Other Households", systemImage: "person.2")
            }
            .disabled(isSaving)
            Text(
                shown
                    ? "Others can find this recipe in Shared Recipes and add a copy."
                    : "Only your household can see this recipe."
            )
            .font(.footnote)
            .foregroundStyle(.secondary)
            if let failure {
                Text(failure)
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
        }
        .padding(14)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 14))
    }

    private func save(_ value: Bool) {
        let previous = shown
        isShared = value
        isSaving = true
        failure = nil
        Task {
            defer { isSaving = false }
            do {
                try await library.setSharing(recipeID: recipe.id, shared: value)
            } catch is CancellationError {
                return
            } catch {
                isShared = previous
                failure = HouseholdStore.message(for: error)
            }
        }
    }
}
