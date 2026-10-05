import SwiftUI

/// "What could have been better?" after a rating under five: a few kinds of problem as chips, each
/// opening its choices, and "An Ingredient", which lists the meal's ingredients and asks what was
/// wrong with the one picked (and which part, for something made from parts). Every question is
/// optional; Skip saves nothing. Answers teach Autopilot, and "The product I bought" flags that
/// ingredient's saved product to pick a different one next time.
struct RatingFeedbackSheet: View {
    let recipeID: String
    let recipeName: String
    let score: Int
    /// The saved rating, so reopening shows the earlier answers.
    let rating: RecipeRating?

    @Environment(RecipeLibrary.self) private var library
    @Environment(ShoppingStore.self) private var shopping
    @Environment(MenuStore.self) private var menu
    @Environment(\.dismiss) private var dismiss

    @State private var questions: RatingQuestions?
    @State private var feedback: RatingFeedback?
    @State private var openCategory: String?
    @State private var openIngredient: String?
    @State private var loadError: String?
    @State private var isSaving = false
    @State private var saveError: String?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    header
                    if let questions, let feedback {
                        content(questions, feedback)
                    } else if let loadError {
                        ContentUnavailableView(
                            "Couldn't Load the Questions", systemImage: "wifi.slash", description: Text(loadError))
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                    if let saveError {
                        FormErrorLabel(message: saveError)
                    }
                }
                .padding(20)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Skip") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(feedback == nil || isSaving)
                }
            }
            .task { await load() }
        }
        .presentationDetents([.medium, .large])
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(questions?.title ?? String(localized: "What could have been better?"))
                .font(.title3.weight(.semibold))
            Text("\(recipeName) · \(String(repeating: "★", count: score))\(String(repeating: "☆", count: 5 - score))")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text("Pick anything that fits. It helps Autopilot choose better next time.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func content(_ questions: RatingQuestions, _ feedback: RatingFeedback) -> some View {
        FlowLayout(spacing: 8) {
            ForEach(questions.categories) { category in
                let count = feedback.count(in: category, ingredients: questions.ingredients)
                Button {
                    withAnimation(.snappy) {
                        openCategory = openCategory == category.code ? nil : category.code
                        openIngredient = nil
                    }
                } label: {
                    Label(count > 0 ? "\(category.title) (\(count))" : category.title, systemImage: category.symbol)
                        .font(.subheadline.weight(.medium))
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                        .background(
                            openCategory == category.code || count > 0
                                ? AnyShapeStyle(.tint.opacity(0.15)) : AnyShapeStyle(.quaternary), in: .capsule
                        )
                        .foregroundStyle(count > 0 ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(openCategory == category.code ? .isSelected : [])
            }
        }
        if let category = questions.categories.first(where: { $0.code == openCategory }) {
            VStack(alignment: .leading, spacing: 12) {
                if category.isIngredients {
                    ingredients(questions)
                } else {
                    FlowLayout(spacing: 8) {
                        ForEach(category.options) { option in
                            let picked = feedback.isPicked(option.tag)
                            Button {
                                self.feedback?.toggle(option.tag)
                            } label: {
                                TagChip(title: option.title, isSelected: picked)
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(picked ? .isSelected : [])
                        }
                    }
                }
            }
            .padding(.leading, 12)
            .overlay(alignment: .leading) {
                Capsule().fill(.tint.opacity(0.5)).frame(width: 2)
            }
            .transition(.opacity.combined(with: .move(edge: .top)))
        }
        summary(questions)
    }

    @ViewBuilder
    private func ingredients(_ questions: RatingQuestions) -> some View {
        if questions.ingredients.isEmpty {
            Text("No ingredients to ask about.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        Text("Which one?")
            .font(.subheadline)
            .foregroundStyle(.secondary)
        FlowLayout(spacing: 8) {
            ForEach(questions.ingredients) { ingredient in
                let answered = feedback?.miss(for: ingredient) != nil
                Button {
                    withAnimation(.snappy) { openIngredient = openIngredient == ingredient.id ? nil : ingredient.id }
                } label: {
                    TagChip(title: ingredient.name, isSelected: answered || openIngredient == ingredient.id)
                }
                .buttonStyle(.plain)
            }
        }
        if let ingredient = questions.ingredients.first(where: { $0.id == openIngredient }) {
            reasons(for: ingredient, questions)
        }
    }

    @ViewBuilder
    private func reasons(for ingredient: RatingQuestions.Ingredient, _ questions: RatingQuestions) -> some View {
        let miss = feedback?.miss(for: ingredient)
        Text("What was wrong with the \(ingredient.name)?")
            .font(.subheadline)
            .foregroundStyle(.secondary)
        FlowLayout(spacing: 8) {
            ForEach(questions.reasons) { reason in
                Button {
                    feedback?.setReason(reason.reason, for: ingredient)
                } label: {
                    TagChip(title: reason.title, isSelected: miss?.reason == reason.reason)
                }
                .buttonStyle(.plain)
            }
        }
        if let detail = questions.reasons.first(where: { $0.reason == miss?.reason })?.detail, !detail.isEmpty {
            Label(detail, systemImage: "info.circle")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        if !ingredient.parts.isEmpty {
            Text("Was it one part?")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            FlowLayout(spacing: 8) {
                ForEach(ingredient.parts, id: \.self) { part in
                    Button {
                        feedback?.setPart(part, for: ingredient)
                    } label: {
                        TagChip(title: part, isSelected: miss?.part == part)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    /// What's been said so far, so answers in a closed category aren't forgotten.
    @ViewBuilder
    private func summary(_ questions: RatingQuestions) -> some View {
        if let feedback, !feedback.isEmpty {
            let titles = Dictionary(
                questions.categories.flatMap(\.options).map { ($0.tag, $0.title) }, uniquingKeysWith: { a, _ in a })
            let reasonTitles = Dictionary(
                questions.reasons.map { ($0.reason, $0.title) }, uniquingKeysWith: { a, _ in a })
            let lines =
                feedback.tags.sorted().compactMap { titles[$0] }
                + feedback.misses.values.sorted { $0.name < $1.name }.map { miss in
                    let what = miss.part.isEmpty ? miss.name : "\(miss.name) (\(miss.part))"
                    let reason = reasonTitles[miss.reason] ?? miss.reason
                    return "\(what): \(reason.prefix(1).lowercased() + reason.dropFirst())"
                }
            VStack(alignment: .leading, spacing: 4) {
                Text("You said")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                ForEach(lines, id: \.self) { line in
                    Text(line).font(.subheadline)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(.quaternary.opacity(0.4), in: .rect(cornerRadius: 12))
        }
    }

    private func load() async {
        guard questions == nil else { return }
        do {
            let loaded = try await library.ratingQuestions(recipeID: recipeID)
            questions = loaded
            feedback = RatingFeedback(rating: rating, questions: loaded)
        } catch is CancellationError {
            return
        } catch {
            loadError = error.localizedDescription
        }
    }

    private func save() {
        guard let feedback else { return }
        isSaving = true
        saveError = nil
        Task {
            defer { isSaving = false }
            do {
                let saved = try await library.saveRating(feedback.draft(score: score), recipeID: recipeID)
                menu.applyRating(
                    recipeID: recipeID, mine: saved, household: library.cachedRecipe(id: recipeID)?.householdRating)
                // "The product I bought": that ingredient's saved product gets re-chosen next time.
                // An ingredient without a saved product has nothing to flag.
                for key in feedback.productKeys {
                    try? await shopping.setChangeRequest(ingredientKey: key, flagged: true)
                }
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                saveError = error.localizedDescription
            }
        }
    }
}
