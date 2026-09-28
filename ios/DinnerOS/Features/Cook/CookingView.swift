import SwiftUI

/// A dish on the cooking screen's switcher: one of the week's meals.
struct CookMeal: Identifiable, Hashable {
    let id: String
    let recipeID: String
    let name: String
    let imageURL: URL?
    /// "Tuesday", or `nil` for a meal with no day.
    let dayText: String?
    let servings: Int?
}

/// The screen for cooking: the ingredients as a checklist and the steps side by side on an iPad,
/// or as tabs on a phone. It only reads the recipe; nothing here changes the plan or the recipe.
/// The switcher at the top left moves between the week's dishes without losing what was ticked.
struct CookingView: View {
    let meals: [CookMeal]

    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households

    @State private var recipeID: String
    @State private var session = CookSession()
    @State private var dishes: [String: CookDish] = [:]
    @State private var loadError: String?
    @State private var tab: CookTab = .ingredients
    @State private var showsSwitcher = false

    init(start: CookMeal, meals: [CookMeal]) {
        self.meals = meals.contains { $0.recipeID == start.recipeID } ? meals : [start] + meals
        _recipeID = State(initialValue: start.recipeID)
    }

    private var meal: CookMeal? { meals.first { $0.recipeID == recipeID } }
    private var dish: CookDish? { dishes[recipeID] }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(dish?.recipe.name ?? meal?.name ?? "")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) { switcherButton }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .task(id: recipeID) { await load() }
        // The screen stays on while cooking; hands are busy.
        .onAppear { UIApplication.shared.isIdleTimerDisabled = true }
        .onDisappear { UIApplication.shared.isIdleTimerDisabled = false }
    }

    @ViewBuilder
    private var content: some View {
        if let dish {
            if sizeClass == .regular {
                sideBySide(dish)
            } else {
                tabs(dish)
            }
        } else if let loadError {
            ContentUnavailableView {
                Label("Couldn't Load the Recipe", systemImage: "wifi.exclamationmark")
            } description: {
                Text(loadError)
            } actions: {
                Button("Try Again") { Task { await load() } }
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: Layouts

    /// iPad: ingredients on the left, steps on the right, each scrolling on its own.
    private func sideBySide(_ dish: CookDish) -> some View {
        GeometryReader { geometry in
            HStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 24) {
                        CookHeader(dish: dish)
                        CookIngredientList(dish: dish, session: session)
                        CookNotesEditor(recipeID: dish.recipe.id)
                    }
                    .padding(24)
                }
                .frame(width: max(320, geometry.size.width * 0.38))
                .background(Color(.secondarySystemBackground))
                Divider()
                ScrollView {
                    CookStepList(dish: dish, session: session)
                        .padding(24)
                }
            }
        }
    }

    /// Phone: one tab at a time.
    private func tabs(_ dish: CookDish) -> some View {
        VStack(spacing: 0) {
            Picker("Show", selection: $tab) {
                ForEach(CookTab.allCases) { tab in
                    Text(tab.title).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            ScrollView {
                Group {
                    switch tab {
                    case .ingredients:
                        VStack(alignment: .leading, spacing: 20) {
                            CookHeader(dish: dish)
                            CookIngredientList(dish: dish, session: session)
                        }
                    case .steps:
                        CookStepList(dish: dish, session: session)
                    case .notes:
                        CookNotesEditor(recipeID: dish.recipe.id)
                    }
                }
                .padding(16)
            }
        }
    }

    // MARK: Switcher

    private var switcherButton: some View {
        Button {
            showsSwitcher = true
        } label: {
            Image(systemName: "rectangle.on.rectangle")
        }
        .accessibilityLabel("Switch Dish")
        .popover(isPresented: $showsSwitcher) {
            CookSwitcher(meals: meals, selected: recipeID) { picked in
                recipeID = picked.recipeID
                showsSwitcher = false
            }
            .frame(minWidth: 320, idealWidth: 360, minHeight: min(560, CGFloat(meals.count) * 80 + 90))
            .presentationCompactAdaptation(.popover)
        }
    }

    // MARK: Loading

    private func load() async {
        let id = recipeID
        guard dishes[id] == nil else { return }
        loadError = nil
        do {
            let recipe: Recipe
            if let cached = library.cachedRecipe(id: id) {
                recipe = cached
            } else {
                recipe = try await library.recipe(id: id, reload: false)
            }
            let servings =
                meals.first { $0.recipeID == id }?.servings
                ?? recipe.preferredServings(householdDefault: households.current?.household.defaultServings)
            let instructions = try? await library.instructions(recipeID: id, servings: servings)
            dishes[id] = CookDish(recipe: recipe, servings: servings, instructions: instructions)
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }
}

/// A recipe loaded for cooking, at the serving size being cooked.
struct CookDish {
    let recipe: Recipe
    let servings: Int?
    let instructions: RecipeInstructions?

    var ingredients: [CookIngredient] {
        CookChecklist.make(recipe: recipe, servings: servings ?? 0, instructions: instructions)
    }
}

enum CookTab: String, CaseIterable, Identifiable {
    case ingredients, steps, notes
    var id: String { rawValue }
    var title: LocalizedStringKey {
        switch self {
        case .ingredients: "Ingredients"
        case .steps: "Steps"
        case .notes: "Notes"
        }
    }
}

// MARK: - Header

private struct CookHeader: View {
    let dish: CookDish

    var body: some View {
        HStack(alignment: .center, spacing: 14) {
            RecipePhoto(url: dish.recipe.imageURL, aspectRatio: 1, pointWidth: 88, cornerRadius: 14)
                .frame(width: 88, height: 88)
            VStack(alignment: .leading, spacing: 4) {
                if let servings = dish.servings {
                    Text("Serves \(servings)")
                        .font(.headline)
                }
                if let minutes = dish.recipe.displayMinutes {
                    Text("\(minutes) min")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Ingredients

/// The checklist. An ingredient split across steps opens into its shares, each ticked as it
/// goes in.
struct CookIngredientList: View {
    let dish: CookDish
    let session: CookSession

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Ingredients")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.accentColor)
                .accessibilityAddTraits(.isHeader)
                .padding(.bottom, 8)
            ForEach(dish.ingredients) { ingredient in
                ingredientRow(ingredient)
                ForEach(ingredient.parts) { part in
                    partRow(part, of: ingredient)
                }
            }
        }
    }

    private func ingredientRow(_ ingredient: CookIngredient) -> some View {
        let isChecked = session.isChecked(ingredient.id, recipe: dish.recipe.id)
        return Button {
            session.toggle(ingredient, recipe: dish.recipe.id)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                CookCheckbox(isChecked: isChecked)
                VStack(alignment: .leading, spacing: 2) {
                    amountAndName(amount: ingredient.amountText, name: ingredient.name)
                        .strikethrough(isChecked || ingredient.isLeftOut)
                        .foregroundStyle(isChecked || ingredient.isLeftOut ? Color.secondary : Color.primary)
                    if ingredient.isLeftOut {
                        Text("You leave this out")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(ingredient.isLeftOut)
        .accessibilityAddTraits(isChecked ? .isSelected : [])
    }

    private func partRow(_ part: CookPart, of ingredient: CookIngredient) -> some View {
        let isChecked = session.isChecked(part.id, recipe: dish.recipe.id)
        return Button {
            session.toggle(part, of: ingredient, recipe: dish.recipe.id)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                CookCheckbox(isChecked: isChecked, small: true)
                (Text(part.amountText).fontWeight(.semibold) + Text(" in step \(part.stepIndex)"))
                    .font(.subheadline)
                    .strikethrough(isChecked)
                    .foregroundStyle(isChecked ? Color.secondary : Color.primary)
                Spacer(minLength: 0)
            }
            .padding(.leading, 36)
            .padding(.vertical, 5)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text("\(part.amountText) of \(ingredient.name) in step \(part.stepIndex)"))
        .accessibilityAddTraits(isChecked ? .isSelected : [])
    }

    private func amountAndName(amount: String?, name: String) -> Text {
        guard let amount, !amount.isEmpty else { return Text(name) }
        return Text(amount).fontWeight(.semibold) + Text(" ") + Text(name)
    }
}

private struct CookCheckbox: View {
    let isChecked: Bool
    var small = false

    var body: some View {
        Image(systemName: isChecked ? "checkmark.square.fill" : "square")
            .font(small ? .body : .title3)
            .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
            .accessibilityHidden(true)
    }
}

// MARK: - Steps

/// The steps, with their photos. Tapping a step marks it as the one you're on; the others fade
/// back so it's easy to find again after looking away.
struct CookStepList: View {
    let dish: CookDish
    let session: CookSession

    var body: some View {
        let current = session.current(recipe: dish.recipe.id)
        VStack(alignment: .leading, spacing: 12) {
            Text("Steps")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.accentColor)
                .accessibilityAddTraits(.isHeader)
            if let steps = dish.instructions?.steps, !steps.isEmpty {
                ForEach(steps) { step in
                    stepCard(index: step.index, imageURL: step.imageURL, current: current) {
                        InstructionStepText(step: step)
                            .font(.title3)
                    }
                }
            } else {
                ForEach(dish.recipe.steps) { step in
                    stepCard(index: step.index, imageURL: step.imageURL, current: current) {
                        Text(step.text)
                            .font(.title3)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        }
    }

    private func stepCard(
        index: Int, imageURL: URL?, current: Int?, @ViewBuilder text: () -> some View
    ) -> some View {
        let isCurrent = current == index
        return VStack(alignment: .leading, spacing: 8) {
            Text("Step \(index)")
                .font(.headline)
                .foregroundStyle(isCurrent ? Color.accentColor : Color.secondary)
            text()
            if let imageURL {
                RecipePhoto(url: imageURL, aspectRatio: 16.0 / 9.0, pointWidth: 520, cornerRadius: 12)
                    .frame(maxWidth: 520)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 14)
                .fill(isCurrent ? Color.accentColor.opacity(0.12) : Color.clear)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 14)
                .strokeBorder(isCurrent ? Color.accentColor : Color.clear, lineWidth: 2)
        )
        .opacity(current == nil || isCurrent ? 1 : 0.45)
        .contentShape(Rectangle())
        .onTapGesture {
            withAnimation(.easeOut(duration: 0.2)) { session.tapStep(index, recipe: dish.recipe.id) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isCurrent ? [.isButton, .isSelected] : .isButton)
        .accessibilityHint(Text(isCurrent ? "Tap to clear" : "Marks this as the step you're on"))
    }
}

// MARK: - Notes

/// The cook's own notes on the recipe, saved as they type. Nobody else sees them.
struct CookNotesEditor: View {
    let recipeID: String

    @Environment(RecipeLibrary.self) private var library
    @State private var text = ""
    @State private var saved = ""
    @State private var loaded = false
    @State private var failed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("My Notes")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.accentColor)
                .accessibilityAddTraits(.isHeader)
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text("What you changed, or what to try next time")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 8)
                        .accessibilityHidden(true)
                }
                TextEditor(text: $text)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: 120)
                    .disabled(!loaded)
                    .accessibilityLabel("My notes")
            }
            .padding(8)
            .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12))
            Text(failed ? "Couldn't save your note. It'll try again as you type." : "Only you see this.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .task(id: recipeID) {
            loaded = false
            let note = try? await library.note(recipeID: recipeID)
            text = note?.text ?? ""
            saved = text
            loaded = true
        }
        .task(id: text) {
            guard loaded, text != saved else { return }
            do {
                try await Task.sleep(for: .seconds(1))
                let note = try await library.saveNote(recipeID: recipeID, text: text)
                saved = note.text == text.trimmingCharacters(in: .whitespacesAndNewlines) ? text : saved
                failed = false
            } catch is CancellationError {
                return
            } catch {
                failed = true
            }
        }
    }
}

// MARK: - Switcher

/// The week's dishes, for cooking two at once: the main and the garlic bread.
private struct CookSwitcher: View {
    let meals: [CookMeal]
    let selected: String
    let pick: (CookMeal) -> Void

    var body: some View {
        List {
            Section("This Week") {
                ForEach(meals) { meal in
                    Button {
                        pick(meal)
                    } label: {
                        HStack(spacing: 12) {
                            RecipePhoto(url: meal.imageURL, aspectRatio: 1, pointWidth: 52, cornerRadius: 8)
                                .frame(width: 52, height: 52)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(meal.name)
                                    .foregroundStyle(Color.primary)
                                    .lineLimit(2)
                                if let day = meal.dayText {
                                    Text(day)
                                        .font(.subheadline)
                                        .foregroundStyle(.secondary)
                                }
                            }
                            Spacer(minLength: 0)
                            if meal.recipeID == selected {
                                Image(systemName: "checkmark")
                                    .foregroundStyle(Color.accentColor)
                                    .accessibilityLabel("Showing")
                            }
                        }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
    }
}
