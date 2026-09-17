import SwiftUI

// MARK: - Customization

/// Swap or double the protein, then the add-ons that go with the meal. The two belong
/// together: both change what arrives with this meal, and neither is reading material.
struct RecipeCustomizationSection: View {
    let customizations: [CustomizationGroup]
    @Binding var selections: [String: String]
    let choose: (CustomizationGroup, CustomizationChoice) -> Void
    let pairings: RecipePairings
    /// The recipe as a summary, for planning it when a pairing is added before it is.
    let summary: RecipeSummary
    let reloadPairings: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 28) {
            if !customizations.isEmpty {
                RecipeCustomizeSection(groups: customizations, selections: $selections, choose: choose)
            }
            RecipePairingsSection(pairings: pairings, recipe: summary, reload: reloadPairings)
        }
    }
}

// MARK: - Description

/// What the meal is, what it's cooked with, how the household rated it, and when it was last
/// ordered: everything about the recipe that is read rather than changed.
struct RecipeDescriptionSection: View {
    let recipe: Recipe

    @Environment(HouseholdStore.self) private var households

    var body: some View {
        VStack(alignment: .leading, spacing: 28) {
            if let description = recipe.description, !description.isEmpty {
                Text(description)
                    .foregroundStyle(Color.secondary)
            }
            if !recipe.utensils.isEmpty {
                DetailSection("Utensils") {
                    Text(recipe.utensils.formatted(.list(type: .and)))
                        .foregroundStyle(Color.secondary)
                }
            }
            // After the meal itself, where someone who just cooked it finishes reading.
            RecipeRatingSection(recipe: recipe) { _ in }
            RecipeAutopilotSection(recipeID: recipe.id)
            DetailSection("Order History") {
                if recipe.orderWeeks.isEmpty {
                    Text("Not ordered yet")
                        .foregroundStyle(Color.secondary)
                } else {
                    Text(RecipeFormat.timesOrdered(recipe.timesOrdered))
                        .foregroundStyle(Color.secondary)
                    ForEach(recipe.orderWeeks.reversed().prefix(6), id: \.self) { week in
                        Text(ISOWeek(week)?.weekOf(weekStartsOn: households.weekStartsOn) ?? week)
                    }
                }
            }
        }
    }
}

// MARK: - Ingredients

/// Allergens, the serving size, and the ingredient list with round photos.
struct RecipeIngredientsSection: View {
    let recipe: Recipe
    @Binding var servings: Int?

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var options: [Int] { recipe.servingOptions }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if !recipe.allergens.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Allergens")
                        .font(.subheadline.weight(.semibold))
                    Text(recipe.allergens.joined(separator: " · "))
                        .font(.subheadline)
                        .foregroundStyle(Color.secondary)
                }
                .accessibilityElement(children: .combine)
            }
            servingPicker
            VStack(alignment: .leading, spacing: 14) {
                ForEach(recipe.ingredientLines(servings: servings ?? options.first ?? 0)) { line in
                    IngredientRow(line: line)
                }
            }
        }
    }

    @ViewBuilder
    private var servingPicker: some View {
        if options.count > 1, let selected = servings {
            VStack(alignment: .leading, spacing: 6) {
                Text("Serving \(selected) people")
                    .font(.headline)
                let selection = Binding(get: { selected }, set: { servings = $0 })
                if dynamicTypeSize.isAccessibilitySize || options.count > 4 {
                    Picker("Servings", selection: selection) {
                        ForEach(options, id: \.self) { option in
                            Text(MenuFormat.servings(option)).tag(option)
                        }
                    }
                    .pickerStyle(.menu)
                } else {
                    Picker("Servings", selection: selection) {
                        ForEach(options, id: \.self) { option in
                            Text("\(option)").tag(option)
                        }
                    }
                    .pickerStyle(.segmented)
                }
            }
        } else if let only = options.first {
            Text("Serving \(only) people")
                .font(.headline)
        }
    }
}

/// One ingredient: a round photo, the name, the amount, and what it contains.
struct IngredientRow: View {
    let line: IngredientLine

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            IngredientPhoto(url: line.imageURL)
            VStack(alignment: .leading, spacing: 2) {
                Text(line.name)
                    .font(.headline)
                    .foregroundStyle(Color.primary)
                HStack(spacing: 6) {
                    if let amount = line.amount {
                        Text(amount)
                            .font(.subheadline)
                            .foregroundStyle(Color.secondary)
                    }
                    if line.isPantryStaple {
                        Text("pantry")
                            .font(.caption2)
                            .foregroundStyle(Color.secondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(.quaternary, in: .capsule)
                            .accessibilityLabel("pantry staple")
                    }
                }
                if !line.allergens.isEmpty {
                    Text("Contains: \(line.allergens.joined(separator: ", "))")
                        .font(.caption)
                        .foregroundStyle(Color.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Nutrition

/// Per-serving nutrition in label order.
struct RecipeNutritionSection: View {
    let recipe: Recipe

    var body: some View {
        let nutrients = MenuFormat.orderedNutrition(recipe.nutritionValues)
        VStack(alignment: .leading, spacing: 16) {
            if nutrients.isEmpty {
                ContentUnavailableView {
                    Label("No Nutrition Values", systemImage: "chart.pie")
                } description: {
                    Text("This recipe didn't come with nutrition information.")
                }
            } else {
                Text("Nutrition Values")
                    .font(.title3.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                VStack(spacing: 0) {
                    ForEach(Array(nutrients.enumerated()), id: \.offset) { index, nutrient in
                        HStack {
                            Text(nutrient.name)
                                .foregroundStyle(Color.primary)
                            Spacer(minLength: 12)
                            Text(MenuFormat.nutrientAmount(nutrient))
                                .monospacedDigit()
                                .foregroundStyle(Color.secondary)
                        }
                        .padding(.vertical, 10)
                        .accessibilityElement(children: .combine)
                        if index < nutrients.count - 1 {
                            Divider()
                        }
                    }
                }
                Text("Per serving, as published with the recipe. Values are approximate.")
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
            }
        }
    }
}

// MARK: - Steps

/// The numbered cooking steps, with their photos, behind a disclosure row.
struct CookingStepsSection: View {
    let steps: [RecipeStep]
    @Binding var isExpanded: Bool

    var body: some View {
        DisclosureGroup(isExpanded: $isExpanded) {
            VStack(alignment: .leading, spacing: 20) {
                ForEach(steps) { step in
                    RecipeStepRow(step: step)
                }
            }
            .padding(.top, 12)
        } label: {
            HStack {
                Text("Cooking Steps")
                    .font(.title3.weight(.semibold))
                Spacer(minLength: 8)
                Text("\(steps.count)")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
            .accessibilityAddTraits(.isHeader)
        }
        .tint(Color.accentColor)
    }
}

struct RecipeStepRow: View {
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
                    RecipePhoto(url: url, aspectRatio: 16.0 / 9.0, pointWidth: 340, cornerRadius: 12)
                }
            }
        }
    }
}

/// A section heading on the recipe screen.
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
                .font(.title3.weight(.semibold))
                .accessibilityAddTraits(.isHeader)
            content
        }
    }
}

#Preview("Ingredients") {
    ScrollView {
        RecipeIngredientsSection(recipe: RecipePreviewData.recipe, servings: .constant(2))
            .padding()
    }
    .menuPreviewEnvironment()
}

#Preview("Nutrition") {
    ScrollView {
        RecipeNutritionSection(recipe: RecipePreviewData.recipe)
            .padding()
    }
}
