import SwiftUI

/// On a recipe the household added without a photo: the first photo a web search finds for
/// its name, with one tap to use it. Not now hides it on this phone for this recipe.
struct SuggestedPhotoBanner: View {
    let recipe: Recipe
    let onSaved: (Recipe) -> Void

    @Environment(RecipeLibrary.self) private var library
    @State private var found: RecipeFoundPhoto?
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var dismissed = false
    @State private var sourceName: String?
    @State private var sourceURL: URL?

    private var dismissKey: String { "suggestedPhotoDismissed.\(recipe.id)" }

    var body: some View {
        Group {
            if let found, !dismissed {
                HStack(spacing: 12) {
                    AsyncImage(url: URL(string: found.thumbnailURL)) { image in
                        image.resizable().scaledToFill()
                    } placeholder: {
                        Color(.secondarySystemFill)
                    }
                    .frame(width: 72, height: 72)
                    .clipShape(.rect(cornerRadius: 10))
                    .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Add this photo?")
                            .font(.subheadline.weight(.semibold))
                        if let credit = found.photoCredit(source: sourceName, sourceURL: sourceURL) {
                            PhotoCreditLine(credit: credit)
                        }
                        if let errorMessage {
                            Text(errorMessage)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        HStack(spacing: 8) {
                            Button {
                                use(found)
                            } label: {
                                if isSaving { ProgressView() } else { Text("Use Photo") }
                            }
                            .buttonStyle(.borderedProminent)
                            .disabled(isSaving)
                            Button("Not Now") { dismiss() }
                                .buttonStyle(.bordered)
                                .disabled(isSaving)
                        }
                        .font(.subheadline)
                    }
                    Spacer(minLength: 0)
                }
                .padding(12)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 14))
            }
        }
        .task(id: recipe.id) { await load() }
    }

    private func load() async {
        guard found == nil, !UserDefaults.standard.bool(forKey: dismissKey) else {
            dismissed = true
            return
        }
        // Search only when the server can (decision 704). No suggestion is fine: offline,
        // search turned off, or nothing found.
        await library.loadPhotoSearchStatus()
        guard library.photoSearchAvailable else { return }
        guard let page = try? await library.searchPhotos(recipe.name, page: 1) else { return }
        sourceName = page.sourceName
        sourceURL = page.sourceURL
        found = page.photos.first
    }

    private func use(_ photo: RecipeFoundPhoto) {
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                let pick = try await library.useFoundPhoto(photo.id)
                let saved = try await library.editRecipe(recipeID: recipe.id, request: .photo(pick.imageURL))
                onSaved(saved)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }

    private func dismiss() {
        UserDefaults.standard.set(true, forKey: dismissKey)
        withAnimation { dismissed = true }
    }
}
