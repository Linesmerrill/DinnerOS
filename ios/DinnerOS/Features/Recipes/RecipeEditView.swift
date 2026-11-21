import SwiftUI

/// Edits a recipe's ingredients and steps for the household. Renaming an ingredient ("Ramen
/// Noodles" to "Lo Mein Noodles") offers to rename it in the steps too, says how long the new
/// one usually cooks, and puts the time picked on a wheel into the step that cooks it.
struct RecipeEditView: View {
    let recipe: Recipe
    let onSave: (Recipe) -> Void

    @Environment(RecipeLibrary.self) private var library
    @Environment(\.dismiss) private var dismiss

    @State private var draft: RecipeEditDraft
    @State private var swap: SwapState?
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var photo = RecipePhotoModel()
    @State private var ingredientSheet: IngredientSheetTarget<RecipeEditDraft.Line.ID>?
    @State private var reorder: ReorderTarget?
    @State private var openTime: TimeField?
    @FocusState private var focusedStep: Int?

    private enum TimeField {
        case prep
        case cook
    }

    /// A rename shown in the "Update the steps?" card.
    struct SwapState: Equatable {
        let lineID: RecipeEditDraft.Line.ID
        let oldName: String
        let newName: String
        /// The steps before the rename, for Undo and for writing in a picked time.
        let stepsBefore: [String]
        var preview: RecipeSwapPreview
        var minutes: Int
        var isApplying = false
    }

    init(recipe: Recipe, servings: Int?, onSave: @escaping (Recipe) -> Void) {
        self.recipe = recipe
        self.onSave = onSave
        _draft = State(initialValue: RecipeEditDraft(recipe: recipe, servings: servings))
    }

    var body: some View {
        NavigationStack {
            ScrollViewReader { proxy in
                Form {
                    if let swap {
                        swapSection(swap)
                    }
                    if draft.servingSizes.count > 1 {
                        Section {
                            Picker("Amounts for", selection: $draft.servings) {
                                ForEach(draft.servingSizes, id: \.self) { size in
                                    Text("\(size) servings").tag(size)
                                }
                            }
                            .pickerStyle(.segmented)
                        } footer: {
                            Text("Change an amount here and the other sizes change to match.")
                        }
                    }
                    if let problem = draft.problem, draft.hasChanges {
                        Section {
                            Label(problem, systemImage: "exclamationmark.circle")
                                .foregroundStyle(.secondary)
                        } footer: {
                            Text("Save turns on once that’s fixed.")
                        }
                    }
                    if draft.canChangePhoto {
                        Section("Name") {
                            TextField("Recipe name", text: $draft.name)
                                .font(.title3.weight(.semibold))
                                .textInputAutocapitalization(.words)
                                .submitLabel(.done)
                                .frame(minHeight: DS.Size.tapTarget)
                        }
                        RecipePhotoSection(model: photo, imageURL: $draft.imageURL)
                        timesSection
                    }
                    Section {
                        RecipeCuisineField(values: draft.cuisines, options: library.cuisineOptions) {
                            draft.cuisines = $0
                        }
                    }
                    ingredientsSection
                    stepsSection
                    if draft.canChangePhoto {
                        NutritionSection(values: $draft.nutrition)
                    }
                    Section {
                    } footer: {
                        Text(
                            "Your changes are just for your household. If the recipe is imported again, your version stays."
                        )
                    }
                }
                .onChange(of: focusedStep) { _, index in
                    if let index { withAnimation { proxy.scrollTo(StepRowID(index: index), anchor: .center) } }
                }
            }
            .keyboardDismissal()
            .recipePhotoPickers(
                photo, imageURL: $draft.imageURL, searchText: recipe.name,
                source: RecipePhotoSource(
                    upload: { try await library.uploadPhoto($0) },
                    search: { try await library.searchPhotos($0, page: $1) },
                    useFound: { try await library.useFoundPhoto($0) },
                    searchAvailable: library.photoSearchAvailable)
            )
            .dsSheet(item: $ingredientSheet) { target in ingredientEditor(target) }
            .dsSheet(item: $reorder) { target in reorderSheet(target) }
            .task { await library.loadIngredientNames() }
            .task { await library.loadPhotoSearchStatus() }
            .task { await library.loadCuisineOptions() }
            .onAppear {
                // A photo picked earlier keeps its credit while the recipe is edited.
                if photo.credit == nil, draft.imageURL == recipe.imageURLString { photo.credit = recipe.imageCredit }
            }
            .navigationTitle("Edit Recipe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save") { save() }
                            .disabled(!draft.hasChanges || draft.problem != nil)
                    }
                }
            }
            .alert("Couldn’t Save That", isPresented: Binding(presenting: $errorMessage)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
            .interactiveDismissDisabled(draft.hasChanges)
        }
    }

    // MARK: Times

    private var timesSection: some View {
        Section {
            RecipeTimeField(title: "Prep time", minutes: $draft.prepTime, isOpen: isOpen(.prep))
            RecipeTimeField(title: "Cook time", minutes: $draft.cookTime, isOpen: isOpen(.cook))
        } header: {
            Text("Time")
        } footer: {
            if draft.totalTime > 0 {
                Text("Total: \(RecipeFormat.minutes(draft.totalTime))")
            }
        }
    }

    private func isOpen(_ field: TimeField) -> Binding<Bool> {
        Binding(
            get: { openTime == field },
            set: { open in
                Keyboard.dismiss()
                openTime = open ? field : nil
            })
    }

    // MARK: Ingredients

    private var ingredientsSection: some View {
        Section {
            let lines = draft.lines
            ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                RecipeListRow(
                    title: line.name,
                    detail: IngredientFields.detail(
                        quantity: draft.quantity(of: line), unit: line.unit, note: line.note),
                    canMoveUp: index > 0, canMoveDown: index < lines.count - 1,
                    onEdit: { ingredientSheet = .edit(line.id) },
                    onMove: { draft.lines.move(at: index, by: $0) },
                    onDelete: { draft.lines.removeAll { $0.id == line.id } })
            }
            Button {
                Keyboard.dismiss()
                ingredientSheet = .new
            } label: {
                Label("Add Ingredient", systemImage: "plus")
                    .frame(minHeight: DS.Size.tapTarget)
            }
        } header: {
            SectionHeaderWithMenu(title: "Ingredients") {
                Button("Reorder & Delete Ingredients", systemImage: "arrow.up.arrow.down") {
                    reorder = .ingredients
                }
                .disabled(draft.lines.isEmpty)
            }
        } footer: {
            Text("Rename an ingredient to swap it. We’ll offer to change the steps too.")
        }
    }

    @ViewBuilder
    private func ingredientEditor(_ target: IngredientSheetTarget<RecipeEditDraft.Line.ID>) -> some View {
        switch target {
        case .new:
            IngredientEditorSheet(mode: .new, units: RecipeUnit.codes, names: library.ingredientNames) { fields in
                draft.addLine(fields)
            }
        case .edit(let id):
            if let fields = draft.fields(of: id) {
                IngredientEditorSheet(
                    mode: .edit, initial: fields, units: units(for: fields.unit), names: library.ingredientNames
                ) { fields in
                    draft.apply(fields, to: id)
                    checkSwap(id)
                }
            }
        }
    }

    /// The unit codes to offer, with the line's own when it's one the list doesn't have.
    private func units(for unit: String) -> [String] {
        RecipeUnit.codes.contains(unit) ? RecipeUnit.codes : [unit] + RecipeUnit.codes
    }

    // MARK: Steps

    private var stepsSection: some View {
        Section {
            ForEach(draft.steps.indices, id: \.self) { index in
                StepEntryRow(
                    index: index, text: $draft.steps[index], count: draft.steps.count,
                    focus: $focusedStep, field: index,
                    onMove: { draft.steps.move(at: index, by: $0) },
                    onDelete: { draft.steps.remove(at: index) }
                )
                .id(StepRowID(index: index))
            }
            Button {
                draft.steps.append("")
                let index = draft.steps.count - 1
                DispatchQueue.main.async { focusedStep = index }
            } label: {
                Label("Add Step", systemImage: "plus")
                    .frame(minHeight: DS.Size.tapTarget)
            }
        } header: {
            SectionHeaderWithMenu(title: "Steps") {
                Button("Reorder & Delete Steps", systemImage: "arrow.up.arrow.down") {
                    reorder = .steps
                }
                .disabled(draft.steps.isEmpty)
            }
        }
    }

    @ViewBuilder
    private func reorderSheet(_ target: ReorderTarget) -> some View {
        switch target {
        case .ingredients:
            ReorderDeleteSheet(
                title: "Reorder & Delete Ingredients",
                items: draft.lines.enumerated().map { index, line in
                    ReorderItem(
                        id: index, title: line.name,
                        detail: IngredientFields.detail(
                            quantity: draft.quantity(of: line), unit: line.unit, note: line.note))
                }
            ) { places in
                draft.lines = draft.lines.reordered(by: places)
            }
        case .steps:
            ReorderDeleteSheet(
                title: "Reorder & Delete Steps",
                items: draft.steps.enumerated().map { index, step in
                    ReorderItem(id: index, title: String(localized: "Step \(index + 1)"), detail: step)
                }
            ) { places in
                draft.steps = draft.steps.reordered(by: places)
            }
        }
    }

    // MARK: Swapping

    @ViewBuilder
    private func swapSection(_ swap: SwapState) -> some View {
        Section {
            let changed = swap.preview.changedCount
            Text(
                changed == 0
                    ? "No steps name \(swap.oldName)."
                    : changed == 1
                        ? "1 step now says \(swap.newName) instead of \(swap.oldName)."
                        : "\(changed) steps now say \(swap.newName) instead of \(swap.oldName).")
            if let cookTime = swap.preview.cookTime {
                Label(cookTime.text, systemImage: "timer")
                    .foregroundStyle(.tint)
            }
            if let time = swap.preview.times.first {
                Text("Step \(time.step + 1) says \(time.text). How long should it be?")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Picker("Minutes", selection: Binding(get: { swap.minutes }, set: { self.swap?.minutes = $0 })) {
                    ForEach(1...60, id: \.self) { minutes in
                        Text(minutes == 1 ? "1 minute" : "\(minutes) minutes").tag(minutes)
                    }
                }
                .pickerStyle(.wheel)
                .frame(height: 120)
                Button {
                    applyMinutes()
                } label: {
                    if swap.isApplying {
                        ProgressView()
                    } else {
                        Text("Use \(swap.minutes) Minutes in Step \(time.step + 1)")
                    }
                }
                .disabled(swap.isApplying)
            } else if swap.preview.cookTime != nil {
                Text("No step gives it a time. You can add one to a step below.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            HStack {
                Button("Undo Step Changes", role: .destructive) { undoSwap() }
                    .disabled(changed == 0 && swap.stepsBefore == draft.steps)
                Spacer()
                Button("Done") { finishSwap() }
                    .fontWeight(.semibold)
            }
            .buttonStyle(.borderless)
        } header: {
            Text("Swapped for \(swap.newName)")
        }
    }

    /// A renamed line: rename it in the steps and look up how long the new one cooks.
    private func checkSwap(_ id: RecipeEditDraft.Line.ID) {
        guard let names = draft.pendingSwap(for: id), swap?.lineID != id || swap?.newName != names.new else { return }
        let before = draft.steps
        Task {
            do {
                let preview = try await library.swapPreview(
                    RecipeSwapRequest(oldName: names.old, newName: names.new, steps: before))
                draft.steps = preview.steps.map(\.text)
                draft.finishSwap(for: id)
                swap = SwapState(
                    lineID: id, oldName: names.old, newName: names.new, stepsBefore: before, preview: preview,
                    minutes: preview.cookTime?.suggestedMinutes ?? preview.times.first?.minutes ?? 5)
            } catch is CancellationError {
                return
            } catch {
                // The rename still saves; only the step help is missing.
                draft.finishSwap(for: id)
            }
        }
    }

    /// Writes the picked minutes into the step that cooks the new ingredient.
    private func applyMinutes() {
        guard var current = swap else { return }
        current.isApplying = true
        swap = current
        Task {
            defer { swap?.isApplying = false }
            do {
                let preview = try await library.swapPreview(
                    RecipeSwapRequest(
                        oldName: current.oldName, newName: current.newName, steps: current.stepsBefore,
                        minutes: current.minutes))
                draft.steps = preview.steps.map(\.text)
                swap?.preview = preview
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }

    private func undoSwap() {
        guard let swap else { return }
        draft.steps = swap.stepsBefore
        self.swap = nil
    }

    private func finishSwap() {
        swap = nil
    }

    // MARK: Saving

    private func save() {
        Keyboard.dismiss()
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                let saved = try await library.editRecipe(recipeID: recipe.id, request: draft.request)
                onSave(saved)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}
