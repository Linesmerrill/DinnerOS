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
    /// The meal's protein swaps, so the steps name what's being cooked.
    var swaps: [PlanEntryCustomization] = []
    /// The week it's planned in, so what's skipped just this once is left out of the steps.
    var week: ISOWeek? = nil
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
    @Environment(ShoppingStore.self) private var shopping

    @State private var recipeID: String
    @State private var session = CookSession()
    @State private var dishes: [String: CookDish] = [:]
    @State private var loadError: String?
    @State private var tab: CookTab = .ingredients
    @State private var showsSwitcher = false
    @State private var timers = CookTimers()
    /// Shares ticks, the current step, and timers with the household's other devices.
    @State private var sync: CookSync?
    /// A time tapped in a step, waiting to be adjusted and started.
    @State private var timerRequest: CookTimerRequest?
    @State private var confirmsLeaving = false
    @State private var showsLayout = false
    @AppStorage("cook.ingredientLayout") private var layout: CookIngredientLayout = .byStep
    /// The walkthrough shows once, the first time the cooking screen opens.
    @AppStorage("cook.hasSeenWelcome") private var hasSeenWelcome = false
    @State private var showsWelcome = false

    init(start: CookMeal, meals: [CookMeal]) {
        self.meals = meals.contains { $0.recipeID == start.recipeID } ? meals : [start] + meals
        _recipeID = State(initialValue: start.recipeID)
    }

    private var meal: CookMeal? { meals.first { $0.recipeID == recipeID } }
    private var dish: CookDish? { dishes[recipeID] }

    var body: some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    // No title in the bar: a dish's name didn't fit beside the buttons and was cut
                    // short ("Street Cart-St..."). It heads the page instead, on up to two lines.
                    ToolbarItem(placement: .topBarLeading) { switcherButton }
                    ToolbarItem(placement: .topBarLeading) { layoutButton }
                    ToolbarItem(placement: .topBarLeading) {
                        Button {
                            showsWelcome = true
                        } label: {
                            Image(systemName: "questionmark.circle")
                        }
                        .accessibilityLabel("How the Cooking Screen Works")
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") {
                            if timers.runningCount > 0 { confirmsLeaving = true } else { dismiss() }
                        }
                    }
                }
                .environment(
                    \.openURL,
                    OpenURLAction { url in
                        guard let request = CookTimerText.request(from: url) else { return .systemAction }
                        timerRequest = request
                        return .handled
                    }
                )
                .confirmationDialog(
                    timers.runningCount == 1
                        ? "A timer is still running." : "\(timers.runningCount) timers are still running.",
                    isPresented: $confirmsLeaving, titleVisibility: .visible
                ) {
                    Button("Stop Timers and Leave", role: .destructive) {
                        timers.removeAll()
                        dismiss()
                    }
                    Button("Keep Cooking", role: .cancel) {}
                }
        }
        .offlineBanner()
        .task(id: recipeID) {
            if sync == nil { sync = CookSync(library: library, session: session, timers: timers) }
            sync?.follow(recipe: recipeID)
            await load()
        }
        .onAppear { if !hasSeenWelcome { showsWelcome = true } }
        .sheet(isPresented: $showsWelcome, onDismiss: { hasSeenWelcome = true }) {
            CookWelcome { showsWelcome = false }
        }
        // The saved products, so an ingredient can be flagged from here.
        .task { if shopping.preferences.isEmpty { await shopping.loadPreferences() } }
        // The screen stays on while cooking; hands are busy.
        .onAppear { UIApplication.shared.isIdleTimerDisabled = true }
        .onDisappear {
            UIApplication.shared.isIdleTimerDisabled = false
            sync?.stop()
        }
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
                Label("Couldn’t Load the Recipe", systemImage: "wifi.exclamationmark")
            } description: {
                Text(loadError)
            } actions: {
                Button("Try Again") { Task { await load() } }
                    .buttonStyle(.dsTextLink)
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
                    VStack(alignment: .leading, spacing: DS.Space.xxl) {
                        CookHeader(dish: dish)
                        CookIngredientList(dish: dish, session: session, layout: layout)
                        CookNotesEditor(recipeID: dish.recipe.id)
                    }
                    .padding(DS.Space.xxl)
                }
                .frame(width: max(320, geometry.size.width * 0.38))
                .background(DS.Colors.surface)
                Divider()
                ScrollView {
                    stepList(dish)
                        .padding(DS.Space.xxl)
                }
                .contentMargins(.bottom, CGFloat(timers.timers.count) * 72, for: .scrollContent)
                .overlay(alignment: .bottomTrailing) {
                    CookTimerDock(timers: timers, stacked: true)
                }
            }
        }
    }

    private func stepList(_ dish: CookDish) -> some View {
        CookStepList(
            dish: dish, session: session, timerRequest: $timerRequest,
            timerLabel: { timerLabel(step: $0, dish: dish) },
            startTimer: { request, seconds in
                timers.start(label: request.subject ?? timerLabel(step: request.step, dish: dish), seconds: seconds)
                timerRequest = nil
            })
    }

    /// "Step 2", with the dish's name when the week has more than one to cook.
    private func timerLabel(step: Int, dish: CookDish) -> String {
        meals.count > 1 ? String(localized: "Step \(step), \(dish.recipe.name)") : String(localized: "Step \(step)")
    }

    /// Phone: the dish's name, then one tab at a time.
    private func tabs(_ dish: CookDish) -> some View {
        VStack(spacing: 0) {
            CookTitle(name: dish.recipe.name)
                .padding(.horizontal, DS.Space.l)
                .padding(.top, DS.Space.xs)
            Picker("Show", selection: $tab) {
                ForEach(CookTab.allCases) { tab in
                    Text(tab.title).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, DS.Space.l)
            .padding(.vertical, DS.Space.s)
            ScrollView {
                Group {
                    switch tab {
                    case .ingredients:
                        VStack(alignment: .leading, spacing: DS.Space.xl) {
                            CookHeader(dish: dish, showsName: false)
                            CookIngredientList(dish: dish, session: session, layout: layout)
                        }
                    case .steps:
                        stepList(dish)
                    case .notes:
                        CookNotesEditor(recipeID: dish.recipe.id)
                    }
                }
                .padding(DS.Space.l)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                CookTimerDock(timers: timers, stacked: false)
            }
        }
    }

    // MARK: Layout

    private var layoutButton: some View {
        Button {
            showsLayout = true
        } label: {
            Image(systemName: "list.bullet.indent")
        }
        .accessibilityLabel("Ingredient Layout")
        .popover(isPresented: $showsLayout) {
            CookLayoutPicker(layout: $layout)
                .presentationCompactAdaptation(.popover)
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
            let meal = meals.first { $0.recipeID == id }
            let servings =
                meal?.servings
                ?? recipe.preferredServings(householdDefault: households.current?.household.defaultServings)
            let instructions = try? await library.instructions(
                recipeID: id, servings: servings, swaps: meal?.swaps ?? [], week: meal?.week)
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

    /// The server's checklist when it sent one; the app's own reading of the steps otherwise.
    var ingredients: [CookIngredient] {
        if let checklist = instructions?.checklist { return checklist.cookIngredients(recipe: recipe) }
        return CookChecklist.make(recipe: recipe, servings: servings ?? 0, instructions: instructions)
    }

    var stepGroups: [CookStepGroup] {
        if let checklist = instructions?.checklist { return checklist.cookStepGroups }
        return CookChecklist.byStep(recipe: recipe, servings: servings ?? 0, instructions: instructions)
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

/// The dish's name at the top of the cooking screen: wraps to two lines rather than being cut.
private struct CookTitle: View {
    let name: String

    var body: some View {
        Text(name)
            .font(DS.Typography.title)
            .lineLimit(2)
            .minimumScaleFactor(0.85)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

private struct CookHeader: View {
    let dish: CookDish
    /// The iPad's ingredient column names the dish here; a phone names it above the tabs.
    var showsName = true

    @ScaledMetric(relativeTo: .body) private var photoSize: CGFloat = 88

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            if showsName {
                CookTitle(name: dish.recipe.name)
            }
            HStack(alignment: .center, spacing: DS.Space.ml) {
                RecipePhoto(
                    url: dish.recipe.imageURL, aspectRatio: 1, pointWidth: photoSize, cornerRadius: DS.Radius.large,
                    title: dish.recipe.name
                )
                .frame(width: photoSize, height: photoSize)
                VStack(alignment: .leading, spacing: DS.Space.xxs) {
                    if let servings = dish.servings {
                        Text("Serves \(servings)")
                            .font(DS.Typography.headline)
                    }
                    if let minutes = dish.recipe.displayMinutes {
                        Text(RecipeFormat.minutes(minutes))
                            .font(DS.Typography.subheadline)
                            .foregroundStyle(DS.Colors.secondaryText)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }
}

// MARK: - Ingredients

/// The checklist. An ingredient split across steps opens into its shares, each ticked as it
/// goes in.
///
/// Every row has the same columns, in both layouts: the check, the amount right-aligned in a
/// fixed column in one quiet style, then the name. Headings and rows share one leading edge.
struct CookIngredientList: View {
    let dish: CookDish
    let session: CookSession
    var layout: CookIngredientLayout = .all

    /// The amount column, wide enough for "1½ Tbsp"; it grows with the text size.
    @ScaledMetric(relativeTo: .subheadline) private var amountWidth: CGFloat = 64

    /// Where a row's name starts: after the check and the amount column.
    private var nameInset: CGFloat { CookCircle.size + DS.Space.m + amountWidth + DS.Space.m }
    /// Where a row's shares start: under the amount column, so their text has room.
    private var partInset: CGFloat { CookCircle.size / 2 }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Ingredients")
                .font(DS.Typography.sectionTitle)
                .foregroundStyle(DS.Colors.accent)
                .accessibilityAddTraits(.isHeader)
                .padding(.bottom, DS.Space.s)
            if let notes = dish.instructions?.saveForLater, !notes.isEmpty {
                CookSaveForLaterCard(notes: notes)
                    .padding(.bottom, DS.Space.s)
            }
            switch layout {
            case .all:
                ForEach(dish.ingredients) { ingredient in
                    ingredientRow(ingredient)
                    ForEach(ingredient.parts) { part in
                        partRow(
                            part, of: ingredient,
                            isLast: part.id == ingredient.parts.last?.id && ingredient.components.isEmpty)
                    }
                    ForEach(Array(ingredient.components.enumerated()), id: \.offset) { index, component in
                        componentRow(
                            component, id: ingredient.componentID(index),
                            isLast: index == ingredient.components.count - 1)
                    }
                    Divider().padding(.leading, nameInset)
                }
            case .byStep:
                let current = session.current(recipe: dish.recipe.id)
                ForEach(dish.stepGroups) { group in
                    stepGroup(group, isCurrent: group.index == current)
                }
            }
        }
    }

    /// One step's ingredients under a bold heading with a green underline; the step you're on
    /// gets a tinted background that reaches past the rows, so the text keeps its edge.
    private func stepGroup(_ group: CookStepGroup, isCurrent: Bool) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(CookStepGroupTitle.text(group.index))
                .font(DS.Typography.sectionTitle)
                .padding(.bottom, DS.Space.xxs)
                .overlay(alignment: .bottom) {
                    Capsule().fill(DS.Colors.accent).frame(height: 3).offset(y: 2)
                }
                .padding(.top, DS.Space.l)
                .padding(.bottom, DS.Space.xs)
                .accessibilityAddTraits(.isHeader)
            ForEach(group.items) { item in
                stepItemRow(item)
                ForEach(Array(item.parts.enumerated()), id: \.offset) { index, part in
                    stepPartRow(part, index: index, of: item, isLast: index == item.parts.count - 1)
                }
                Divider().padding(.leading, nameInset)
            }
        }
        .padding(.bottom, DS.Space.xxs)
        .background {
            RoundedRectangle(cornerRadius: DS.Radius.large)
                .fill(isCurrent ? DS.Colors.accentFill : Color.clear)
                .padding(.horizontal, -DS.Space.s)
        }
    }

    /// The check, the amount in its column, and the name with what's under it.
    private func row(
        isChecked: Bool, isQuiet: Bool, amount: String?, name: String, ingredientKey: String?,
        @ViewBuilder details: () -> some View
    ) -> some View {
        HStack(alignment: .center, spacing: DS.Space.m) {
            CookCircle(isChecked: isChecked)
            Text(amount ?? "")
                .font(DS.Typography.subheadline)
                .monospacedDigit()
                .foregroundStyle(DS.Colors.secondaryText)
                .strikethrough(isQuiet)
                .lineLimit(2)
                .multilineTextAlignment(.trailing)
                .frame(width: amountWidth, alignment: .trailing)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                HStack(spacing: DS.Space.xs) {
                    Text(name)
                        .font(DS.Typography.body.weight(.semibold))
                        .strikethrough(isQuiet)
                        .foregroundStyle(isQuiet ? DS.Colors.secondaryText : Color.primary)
                    CookFlagBadge(ingredientKey: ingredientKey)
                }
                details()
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, DS.Space.s)
        .contentShape(Rectangle())
    }

    private func stepItemRow(_ item: CookStepItem) -> some View {
        let isChecked = session.isChecked(item.id, recipe: dish.recipe.id)
        let quiet = isChecked || item.isLeftOut
        let save = dish.instructions?.saveForLater(ingredientKey: item.ingredientKey)
        return Button {
            session.toggle(item, recipe: dish.recipe.id)
        } label: {
            row(
                isChecked: isChecked, isQuiet: quiet, amount: item.amountText, name: item.name,
                ingredientKey: item.ingredientKey
            ) {
                if let prep = item.prep {
                    Text(prep)
                        .font(DS.Typography.subheadline)
                        .italic()
                        .foregroundStyle(DS.Colors.secondaryText)
                }
                if let save, !item.isLeftOut {
                    CookSaveBadge(text: save.short)
                }
            }
        }
        .buttonStyle(.plain)
        .disabled(item.isLeftOut)
        .modifier(CookFlagMenu(ingredientKey: item.ingredientKey, name: item.name))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            Text(
                [item.amountText, item.name, item.prep, item.isLeftOut ? nil : save?.short].compactMap { $0 }
                    .joined(separator: " "))
        )
        .accessibilityAddTraits(isChecked ? [.isButton, .isSelected] : .isButton)
    }

    /// A share of an ingredient ("1 Tbsp for step 1, melted"), on a branch under its row.
    private func shareRow(isChecked: Bool, isLast: Bool, text: Text) -> some View {
        HStack(spacing: DS.Space.sm) {
            CookBranch(isLast: isLast)
                .frame(width: DS.Space.l)
            CookCircle(isChecked: isChecked, small: true)
            text
                .font(DS.Typography.subheadline)
                .strikethrough(isChecked)
                .foregroundStyle(isChecked ? DS.Colors.secondaryText : Color.primary)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(.leading, partInset)
        .frame(minHeight: DS.Size.tapTarget)
        .contentShape(Rectangle())
    }

    private func stepPartRow(_ part: String, index: Int, of item: CookStepItem, isLast: Bool) -> some View {
        let isChecked = session.isChecked(item.partID(index), recipe: dish.recipe.id)
        return Button {
            session.togglePart(index, of: item, recipe: dish.recipe.id)
        } label: {
            shareRow(isChecked: isChecked, isLast: isLast, text: Text(part))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isChecked ? [.isButton, .isSelected] : .isButton)
    }

    private func ingredientRow(_ ingredient: CookIngredient) -> some View {
        let isChecked = session.isChecked(ingredient.id, recipe: dish.recipe.id)
        let save = dish.instructions?.saveForLater(ingredientKey: ingredient.ingredientKey)
        return Button {
            session.toggle(ingredient, recipe: dish.recipe.id)
        } label: {
            row(
                isChecked: isChecked, isQuiet: isChecked || ingredient.isLeftOut, amount: ingredient.amountText,
                name: ingredient.name, ingredientKey: ingredient.ingredientKey
            ) {
                if ingredient.isLeftOut {
                    Text("You skip this")
                        .font(DS.Typography.caption)
                        .foregroundStyle(DS.Colors.secondaryText)
                } else if let save {
                    CookSaveBadge(text: save.short)
                }
            }
        }
        .buttonStyle(.plain)
        .disabled(ingredient.isLeftOut)
        .modifier(CookFlagMenu(ingredientKey: ingredient.ingredientKey, name: ingredient.name))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            Text([ingredient.amountText, ingredient.name].compactMap { $0 }.joined(separator: " "))
        )
        .accessibilityAddTraits(isChecked ? [.isButton, .isSelected] : .isButton)
    }

    private func partRow(_ part: CookPart, of ingredient: CookIngredient, isLast: Bool) -> some View {
        let isChecked = session.isChecked(part.id, recipe: dish.recipe.id)
        return Button {
            session.toggle(part, of: ingredient, recipe: dish.recipe.id)
        } label: {
            shareRow(isChecked: isChecked, isLast: isLast, text: Text("\(part.amountText) for step \(part.stepIndex)"))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text("\(part.amountText) of \(ingredient.name) for step \(part.stepIndex)"))
        .accessibilityAddTraits(isChecked ? [.isButton, .isSelected] : .isButton)
    }

    /// One thing a home-made sauce or blend is mixed from: "1 tsp Chili Powder".
    private func componentRow(_ component: String, id: String, isLast: Bool) -> some View {
        let isChecked = session.isChecked(id, recipe: dish.recipe.id)
        return Button {
            session.toggle(id: id, recipe: dish.recipe.id)
        } label: {
            shareRow(isChecked: isChecked, isLast: isLast, text: Text(component))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isChecked ? [.isButton, .isSelected] : .isButton)
    }
}

/// A by-step group's heading: "Have Ready" for what to gather first, then "Step 1", "Step 2".
enum CookStepGroupTitle {
    static func text(_ index: Int) -> String {
        index == 0 ? String(localized: "Have Ready") : String(localized: "Step \(index)")
    }
}

/// What to keep for the week's later meals, at the top of the checklist: the mushrooms bought
/// for two dinners shouldn't all go into the first.
struct CookSaveForLaterCard: View {
    let notes: [InstructionSaveForLater]

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.s) {
            Label("Save Some for Later", systemImage: "refrigerator")
                .font(.headline)
                .foregroundStyle(Color.orange)
                .accessibilityAddTraits(.isHeader)
            ForEach(notes) { note in
                (Text(note.name).fontWeight(.semibold) + Text(": ") + Text(note.text))
                    .font(.subheadline)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(DS.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: DS.Radius.medium).fill(Color.orange.opacity(0.12)))
        .overlay(RoundedRectangle(cornerRadius: DS.Radius.medium).strokeBorder(Color.orange.opacity(0.35)))
        .accessibilityElement(children: .combine)
    }
}

/// "Save 4 oz for Beef Stir Fry" under a checklist row.
private struct CookSaveBadge: View {
    let text: String

    var body: some View {
        Label(text, systemImage: "refrigerator")
            .font(.caption.weight(.semibold))
            .foregroundStyle(Color.orange)
            .labelStyle(.titleAndIcon)
    }
}

/// The tree line from an ingredient to its parts: down the side, then across to the part. The
/// last part's line stops at its row.
private struct CookBranch: View {
    let isLast: Bool

    var body: some View {
        GeometryReader { geometry in
            Path { path in
                let x: CGFloat = 4, mid = geometry.size.height / 2
                path.move(to: CGPoint(x: x, y: 0))
                path.addLine(to: CGPoint(x: x, y: isLast ? mid : geometry.size.height))
                path.move(to: CGPoint(x: x, y: mid))
                path.addLine(to: CGPoint(x: geometry.size.width, y: mid))
            }
            .stroke(Color.accentColor.opacity(0.55), style: StrokeStyle(lineWidth: 1.5, lineCap: .round))
        }
        .accessibilityHidden(true)
    }
}

/// Press and hold an ingredient: "Pick a different product next time", for the saved product
/// that didn't work out. Shown only when there is a saved product to flag.
private struct CookFlagMenu: ViewModifier {
    let ingredientKey: String?
    let name: String

    @Environment(ShoppingStore.self) private var shopping
    @State private var failed = false

    func body(content: Content) -> some View {
        if let key = ingredientKey, shopping.hasSavedProduct(ingredientKey: key) {
            let flagged = shopping.isChangeRequested(ingredientKey: key)
            content
                .contextMenu {
                    Button(
                        flagged ? "Keep This Product" : "Pick a Different Product Next Time",
                        systemImage: flagged ? "flag.slash" : "flag"
                    ) {
                        Task {
                            do {
                                try await shopping.setChangeRequest(ingredientKey: key, flagged: !flagged)
                            } catch {
                                failed = true
                            }
                        }
                    }
                }
                .alert("Couldn’t Save That", isPresented: $failed) {
                    Button("OK", role: .cancel) {}
                } message: {
                    Text("Check your connection and try again.")
                }
        } else {
            content
        }
    }
}

/// A small flag beside an ingredient whose product was flagged.
private struct CookFlagBadge: View {
    let ingredientKey: String?
    @Environment(ShoppingStore.self) private var shopping

    var body: some View {
        if let key = ingredientKey, shopping.isChangeRequested(ingredientKey: key) {
            Image(systemName: "flag.fill")
                .font(.caption)
                .foregroundStyle(.orange)
                .accessibilityLabel("Flagged to change next time")
        }
    }
}

/// A rounded square check, filled with the accent when done: one style for every row.
private struct CookCircle: View {
    static let size: CGFloat = 26
    static let smallSize: CGFloat = 20

    let isChecked: Bool
    var small = false

    var body: some View {
        let size = small ? Self.smallSize : Self.size
        let shape = RoundedRectangle(cornerRadius: DS.Radius.small * size / Self.size, style: .continuous)
        ZStack {
            shape.strokeBorder(isChecked ? DS.Colors.accent : DS.Colors.secondaryText, lineWidth: 1.5)
            if isChecked {
                shape.fill(DS.Colors.accent)
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.45, weight: .bold))
                    .foregroundStyle(DS.Colors.onAccent)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

// MARK: - Steps

/// The steps, with their photos. Tapping a step marks it as the one you're on; the others fade
/// back so it's easy to find again after looking away.
struct CookStepList: View {
    let dish: CookDish
    let session: CookSession
    @Binding var timerRequest: CookTimerRequest?
    var timerLabel: (Int) -> String = { String(localized: "Step \($0)") }
    var startTimer: (CookTimerRequest, Int) -> Void = { _, _ in }

    var body: some View {
        let current = session.current(recipe: dish.recipe.id)
        VStack(alignment: .leading, spacing: DS.Space.m) {
            Text("Steps")
                .font(DS.Typography.sectionTitle)
                .foregroundStyle(DS.Colors.accent)
                .accessibilityAddTraits(.isHeader)
            if let steps = dish.instructions?.steps, !steps.isEmpty {
                ForEach(steps) { step in
                    stepCard(index: step.index, imageURL: step.imageURL, current: current) {
                        VStack(alignment: .leading, spacing: DS.Space.sm) {
                            InstructionStepText(step: step, showsTimers: true)
                                .font(.title3)
                            // How to make a specialty, or what to use instead.
                            ForEach(step.notes) { note in
                                InstructionNoteLabel(note: note)
                            }
                        }
                    }
                }
            } else {
                ForEach(dish.recipe.steps) { step in
                    stepCard(index: step.index, imageURL: step.imageURL, current: current) {
                        CookTimerText.text(step.text, step: step.index)
                            .font(.title3)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            if let instructions = dish.instructions {
                SafeTemperaturesFooter(instructions: instructions)
            }
        }
    }

    private func stepCard(
        index: Int, imageURL: URL?, current: Int?, @ViewBuilder text: () -> some View
    ) -> some View {
        let isCurrent = current == index
        return VStack(alignment: .leading, spacing: DS.Space.s) {
            Text("Step \(index)")
                .font(DS.Typography.headline)
                .foregroundStyle(isCurrent ? DS.Colors.accent : DS.Colors.secondaryText)
            text()
            if let imageURL {
                RecipePhoto(url: imageURL, aspectRatio: 16.0 / 9.0, pointWidth: 520, cornerRadius: DS.Radius.medium)
                    .frame(maxWidth: 520)
            }
        }
        // The highlight reaches past the text, so every step keeps the headings' leading edge.
        .padding(.vertical, DS.Space.ml)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: DS.Radius.large)
                .fill(isCurrent ? DS.Colors.accentFill : Color.clear)
                .padding(.horizontal, -DS.Space.ml)
        }
        .overlay {
            RoundedRectangle(cornerRadius: DS.Radius.large)
                .strokeBorder(isCurrent ? DS.Colors.accent : Color.clear, lineWidth: 2)
                .padding(.horizontal, -DS.Space.ml)
        }
        .opacity(current == nil || isCurrent ? 1 : 0.45)
        .contentShape(Rectangle())
        // Simultaneous, so a time in the text still opens its timer (and marks this step).
        .simultaneousGesture(
            TapGesture().onEnded {
                withAnimation(.easeOut(duration: 0.2)) { session.tapStep(index, recipe: dish.recipe.id) }
            }
        )
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isCurrent ? [.isButton, .isSelected] : .isButton)
        .accessibilityHint(Text(isCurrent ? "Tap to clear" : "Marks this as the step you’re on"))
        .popover(
            item: Binding(
                get: { timerRequest?.step == index ? timerRequest : nil },
                set: { if $0 == nil, timerRequest?.step == index { timerRequest = nil } })
        ) { request in
            CookTimerSetup(request: request, label: request.subject ?? timerLabel(index)) { seconds in
                startTimer(request, seconds)
            }
            // A popover on a phone can open off the top of the screen; a short sheet always fits.
            .presentationCompactAdaptation(.sheet)
            .presentationDetents([.height(320)])
        }
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
        VStack(alignment: .leading, spacing: DS.Space.s) {
            Text("My Notes")
                .font(DS.Typography.sectionTitle)
                .foregroundStyle(DS.Colors.accent)
                .accessibilityAddTraits(.isHeader)
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text("What you changed, or what to try next time")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 5)
                        .padding(.vertical, DS.Space.s)
                        .accessibilityHidden(true)
                }
                TextEditor(text: $text)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: 120)
                    .disabled(!loaded)
                    .accessibilityLabel("My notes")
            }
            .padding(DS.Space.s)
            .background(DS.Colors.subtleFill, in: RoundedRectangle(cornerRadius: DS.Radius.medium))
            Text(failed ? "Couldn’t save your note. It’ll try again as you type." : "Only you see this.")
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
                        HStack(spacing: DS.Space.m) {
                            RecipePhoto(
                                url: meal.imageURL, aspectRatio: 1, pointWidth: 52, cornerRadius: DS.Radius.small
                            )
                            .frame(width: 52, height: 52)
                            VStack(alignment: .leading, spacing: DS.Space.hairline) {
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
