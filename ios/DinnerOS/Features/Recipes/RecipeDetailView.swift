import SwiftUI

/// A full recipe. Shows the summary's name and photo at once, then the rest when the
/// recipe loads (from the library's cache when it was opened before).
struct RecipeDetailView: View {
    let summary: RecipeSummary

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(EventReporter.self) private var events
    @Environment(\.scenePhase) private var scenePhase

    @State private var recipe: Recipe?
    @State private var loadError: String?
    @State private var servings: Int?
    @State private var isAddingToWeek = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                RecipeImage(url: recipe?.imageURL ?? summary.imageURL)
                VStack(alignment: .leading, spacing: 28) {
                    header
                    if let loadError {
                        VStack(alignment: .leading, spacing: 8) {
                            FormErrorLabel(message: loadError)
                            Button("Try Again") {
                                Task { await load(reload: true) }
                            }
                            .buttonStyle(.bordered)
                        }
                    }
                    if let recipe {
                        RecipeDetailSections(recipe: recipe, servings: $servings) { show($0) }
                    } else if loadError == nil {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                    }
                }
                .padding(.horizontal)
            }
            .padding(.bottom, 32)
        }
        .navigationTitle(summary.name)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load(reload: false) }
        .task(id: ViewedKey(recipeID: summary.id, isActive: scenePhase == .active)) {
            // Counts as viewed after staying on screen, in the foreground, for a few
            // seconds. Leaving the screen or the app cancels the wait.
            guard scenePhase == .active else { return }
            do {
                try await Task.sleep(for: EventReporter.viewDwell)
            } catch {
                return
            }
            events.recipeViewed(recipeID: summary.id)
        }
        .refreshable { await load(reload: true) }
        .toolbar {
            if households.access?.can(.planEdit) == true {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Add to Week…", systemImage: "calendar.badge.plus") {
                        isAddingToWeek = true
                    }
                }
            }
        }
        .sheet(isPresented: $isAddingToWeek) {
            AddEntrySheet(recipeID: summary.id, recipeName: summary.name, fixedWeek: nil)
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(recipe?.name ?? summary.name)
                .font(.title.bold())
            if let headline = recipe?.headline ?? summary.headline, !headline.isEmpty {
                Text(headline)
                    .font(.title3)
                    .foregroundStyle(.secondary)
            }
            if let recipe {
                RecipeFacts(recipe: recipe)
                    .padding(.top, 4)
                HouseholdRatingSummary(recipe: recipe)
            }
        }
    }

    private func load(reload: Bool) async {
        if !reload, let cached = library.cachedRecipe(id: summary.id) {
            show(cached)
            return
        }
        do {
            show(try await library.recipe(id: summary.id, reload: reload))
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func show(_ loaded: Recipe) {
        recipe = loaded
        if let servings, loaded.servingOptions.contains(servings) { return }
        servings = loaded.preferredServings(householdDefault: households.current?.household.defaultServings)
    }
}

private struct ViewedKey: Equatable {
    let recipeID: String
    let isActive: Bool
}

/// Time, difficulty, and serving sizes.
private struct RecipeFacts: View {
    let recipe: Recipe

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) { facts }
            VStack(alignment: .leading, spacing: 6) { facts }
        }
        .font(.subheadline)
        .foregroundStyle(.secondary)
    }

    @ViewBuilder
    private var facts: some View {
        if let minutes = recipe.displayMinutes {
            Label(RecipeFormat.minutes(minutes), systemImage: "clock")
                .accessibilityLabel("Cook time \(RecipeFormat.minutes(minutes))")
        }
        if let difficulty = recipe.difficulty, difficulty > 0 {
            Label(RecipeFormat.difficulty(difficulty, source: recipe.source), systemImage: "chart.bar")
        }
        let options = recipe.servingOptions
        if !options.isEmpty {
            Label(
                String(localized: "Serves \(options.map(String.init).joined(separator: " or "))"),
                systemImage: "person.2")
        }
    }
}

/// Everything below the header.
private struct RecipeDetailSections: View {
    let recipe: Recipe
    @Binding var servings: Int?
    let onRatingChange: (Recipe) -> Void

    var body: some View {
        if let description = recipe.description, !description.isEmpty {
            Text(description)
                .foregroundStyle(.secondary)
        }
        ingredientsSection
        if !recipe.steps.isEmpty {
            DetailSection("Steps") {
                ForEach(recipe.steps) { step in
                    RecipeStepRow(step: step)
                }
            }
        }
        // After the steps, where someone who just cooked it finishes reading.
        RecipeRatingSection(recipe: recipe, onChange: onRatingChange)
        RecipeAutopilotSection(recipeID: recipe.id)
        if !recipe.nutritionPerServing.isEmpty {
            DetailSection("Nutrition per Serving") {
                Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 8) {
                    ForEach(Array(recipe.nutritionPerServing.enumerated()), id: \.offset) { _, nutrient in
                        GridRow {
                            Text(nutrient.name)
                            Text(
                                "\(nutrient.amount.formatted(.number.precision(.fractionLength(0...1)))) \(nutrient.unit)"
                            )
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                            .gridColumnAlignment(.trailing)
                        }
                    }
                }
            }
        }
        if !recipe.allergens.isEmpty {
            DetailSection("Allergens") {
                Text(recipe.allergens.formatted(.list(type: .and)))
            }
        }
        if !recipe.utensils.isEmpty {
            DetailSection("Utensils") {
                Text(recipe.utensils.formatted(.list(type: .and)))
            }
        }
        DetailSection("Order History") {
            if recipe.orderWeeks.isEmpty {
                Text("Not ordered yet")
                    .foregroundStyle(.secondary)
            } else {
                Text(RecipeFormat.timesOrdered(recipe.timesOrdered))
                    .foregroundStyle(.secondary)
                ForEach(recipe.orderWeeks.reversed(), id: \.self) { week in
                    Text(ISOWeek(week)?.weekOf() ?? week)
                }
            }
        }
    }

    private var ingredientsSection: some View {
        DetailSection("Ingredients") {
            let options = recipe.servingOptions
            if options.count > 1, let selected = servings {
                ServingsPicker(options: options, selection: Binding(get: { selected }, set: { servings = $0 }))
            } else if let only = options.first {
                Text("\(only) servings")
                    .foregroundStyle(.secondary)
            }
            IngredientList(lines: recipe.ingredientLines(servings: servings ?? options.first ?? 0))
        }
    }
}

private struct ServingsPicker: View {
    let options: [Int]
    @Binding var selection: Int

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize || options.count > 4 {
            Picker("Servings", selection: $selection) {
                ForEach(options, id: \.self) { Text("\($0) servings").tag($0) }
            }
            .pickerStyle(.menu)
        } else {
            HStack {
                Text("Servings")
                Picker("Servings", selection: $selection) {
                    ForEach(options, id: \.self) { Text("\($0)").tag($0) }
                }
                .pickerStyle(.segmented)
            }
        }
    }
}

/// Amounts in one column and names in another; stacked at accessibility sizes.
private struct IngredientList: View {
    let lines: [IngredientLine]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(lines) { line in
                    VStack(alignment: .leading, spacing: 2) {
                        if let amount = line.amount {
                            Text(amount)
                                .foregroundStyle(.secondary)
                        }
                        name(line)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        } else {
            Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 12, verticalSpacing: 10) {
                ForEach(lines) { line in
                    GridRow {
                        Text(line.amount ?? "")
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                            .gridColumnAlignment(.trailing)
                        name(line)
                    }
                }
            }
        }
    }

    private func name(_ line: IngredientLine) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(line.name)
            if line.isPantryStaple {
                Text("pantry")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(.quaternary, in: .capsule)
                    .accessibilityLabel("pantry staple")
            }
        }
    }
}

private struct RecipeStepRow: View {
    let step: RecipeStep

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(step.index.formatted())
                .font(.headline)
                .foregroundStyle(.tint)
                .frame(minWidth: 20, alignment: .trailing)
                .accessibilityLabel("Step \(step.index)")
            VStack(alignment: .leading, spacing: 10) {
                Text(step.text)
                    .fixedSize(horizontal: false, vertical: true)
                if let url = step.imageURL {
                    RecipeImage(url: url)
                        .clipShape(.rect(cornerRadius: 8))
                }
            }
        }
    }
}

struct DetailSection<Content: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder let content: Content

    init(_ title: LocalizedStringKey, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .font(.title2.bold())
                .accessibilityAddTraits(.isHeader)
            content
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        RecipeDetailView(summary: RecipePreviewData.summaries[0])
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
    .environment(PlanPreviewData.store(session: session))
    .environment(EventReporter.preview(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}
