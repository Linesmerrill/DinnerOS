import SwiftUI

/// One recipe from the global catalog, before the household owns it.
///
/// It is deliberately thinner than `RecipeDetailView`: there are no ratings, no
/// order history, no notes, and no plan bar, because none of those exist until
/// a household adds the recipe to its own library.
struct CatalogRecipeDetailView: View {
    let route: CatalogRecipeRoute

    @Environment(DiscoverStore.self) private var discover
    @Environment(HouseholdStore.self) private var households

    @State private var recipe: CatalogRecipe?
    @State private var loadError: String?
    @State private var isAdding = false
    @State private var addedRecipeID: String?
    @State private var isChoosingReason = false
    @State private var isConfirmingHideCook = false
    @Environment(\.dismiss) private var dismiss

    private var canAdd: Bool {
        households.access?.can(.recipesEdit) == true
    }

    private var isInLibrary: Bool {
        addedRecipeID != nil || recipe?.inLibrary == true
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Space.l) {
                if let recipe {
                    header(recipe)
                    ingredients(recipe)
                    steps(recipe)
                } else if let loadError {
                    DiscoverErrorRow(message: loadError) {
                        Task { await load() }
                    }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .padding(.top, DS.Space.huge)
                }
            }
            .padding(DS.Space.l)
        }
        .navigationTitle(recipe?.name ?? route.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button("Report Recipe", systemImage: "flag") { isChoosingReason = true }
                    // Only for a recipe a household shared: a meal kit's has no cook to hide.
                    if recipe?.hidesCook == true {
                        Button("Hide Recipes From This Cook", systemImage: "hand.raised") {
                            isConfirmingHideCook = true
                        }
                    }
                } label: {
                    Label("More", systemImage: "ellipsis.circle")
                }
                .accessibilityIdentifier("catalogRecipe.more")
            }
        }
        .confirmationDialog(
            "Hide recipes from this cook?", isPresented: $isConfirmingHideCook, titleVisibility: .visible
        ) {
            Button("Hide Their Recipes", role: .destructive) {
                Task {
                    // The list shows the thank-you; this page goes away.
                    if await discover.hideCook(id: route.id) { dismiss() }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("You won’t see anything they share in Shared Recipes.")
        }
        .confirmationDialog(
            "Why are you reporting this recipe?", isPresented: $isChoosingReason, titleVisibility: .visible
        ) {
            ForEach(CatalogReportReason.allCases) { reason in
                Button(reason.title) {
                    Task {
                        // The list shows the thank-you; this page goes away.
                        if await discover.report(id: route.id, reason: reason) { dismiss() }
                    }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("You won’t see it again. We’ll take a look.")
        }
        .task { await load() }
    }

    @ViewBuilder
    private func header(_ recipe: CatalogRecipe) -> some View {
        RecipeImage(url: recipe.imageURL, title: recipe.name)
            .clipShape(.rect(cornerRadius: DS.Radius.medium))
            .frame(maxHeight: DS.Size.heroMaxHeight)
            .frame(maxWidth: .infinity)
        if recipe.imageURL != nil, let credit = recipe.imageCredit {
            PhotoCreditLine(credit: credit)
        }
        // The name is the heading; the headline ("with Lime Crema") sits under it.
        Text(recipe.name)
            .font(DS.Typography.title)
            .accessibilityAddTraits(.isHeader)
        if let headline = recipe.headline, !headline.isEmpty {
            Text(headline)
                .font(DS.Typography.subtitle)
                .foregroundStyle(DS.Colors.secondaryText)
        }
        Text(Self.details(for: recipe))
            .font(.footnote)
            .foregroundStyle(.secondary)
        if let description = recipe.description, !description.isEmpty {
            Text(description)
                .font(.body)
        }
        addButton
    }

    @ViewBuilder
    private var addButton: some View {
        if isInLibrary {
            Label("In your library", systemImage: "checkmark.circle.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
        } else if canAdd {
            Button {
                Task { await add() }
            } label: {
                if isAdding {
                    ProgressView()
                } else {
                    Label("Add to Library", systemImage: "plus.circle")
                }
            }
            .buttonStyle(.dsPrimary(fullWidth: true))
            .disabled(isAdding)
        }
    }

    @ViewBuilder
    private func ingredients(_ recipe: CatalogRecipe) -> some View {
        if !recipe.ingredients.isEmpty {
            Text("Ingredients")
                .font(.title3.bold())
                .accessibilityAddTraits(.isHeader)
            ForEach(recipe.ingredients) { line in
                HStack(alignment: .firstTextBaseline, spacing: DS.Space.s) {
                    Text(line.name)
                    Spacer(minLength: 8)
                    if let amount = line.amounts.first, let text = RecipeFormat.amount(amount) {
                        Text(text)
                            .foregroundStyle(.secondary)
                    }
                }
                .font(.body)
            }
        }
    }

    @ViewBuilder
    private func steps(_ recipe: CatalogRecipe) -> some View {
        if !recipe.steps.isEmpty {
            Text("Steps")
                .font(.title3.bold())
                .accessibilityAddTraits(.isHeader)
            ForEach(recipe.steps) { step in
                HStack(alignment: .firstTextBaseline, spacing: DS.Space.s) {
                    Text("\(step.index).")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.secondary)
                    Text(step.text)
                }
            }
        }
    }

    private func load() async {
        loadError = nil
        do {
            recipe = try await discover.recipe(id: route.id)
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func add() async {
        guard let recipe else { return }
        isAdding = true
        defer { isAdding = false }
        let summary = CatalogSummary(
            id: recipe.id, catalogKey: recipe.catalogKey, source: recipe.source, name: recipe.name,
            headline: recipe.headline, imageURLString: recipe.imageURLString, isAddon: recipe.isAddon,
            totalMinutes: recipe.totalMinutes, cookMinutes: recipe.cookMinutes, timeBand: nil, calories: nil,
            proteinGrams: nil, cuisines: recipe.cuisines, tags: recipe.tags, inLibrary: false,
            libraryRecipeID: nil, reasons: [])
        if await discover.add(summary) {
            addedRecipeID = recipe.id
        }
    }

    /// For example "30 min · Thai · Serves 2".
    static func details(for recipe: CatalogRecipe) -> String {
        var parts: [String] = []
        if let minutes = recipe.displayMinutes {
            parts.append(RecipeFormat.minutes(minutes))
        }
        parts.append(contentsOf: recipe.cuisines.prefix(2))
        if let servings = recipe.servings.first {
            parts.append(String(localized: "Serves \(servings)"))
        }
        return parts.joined(separator: " · ")
    }
}
