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
    @FocusState private var focusedName: RecipeEditDraft.Line.ID?
    @State private var focusedAmount: RecipeEditDraft.Line.ID?

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
                    if draft.canChangePhoto {
                        RecipePhotoSection(imageURL: $draft.imageURL) { jpeg in
                            try await library.uploadPhoto(jpeg)
                        }
                    }
                    ingredientsSection
                    stepsSection
                    Section {
                    } footer: {
                        Text(
                            "Your changes are just for your household. If the recipe is imported again, your version stays."
                        )
                    }
                }
                .scrollDismissesKeyboard(.interactively)
                .onChange(of: focusedName) { _, id in scroll(proxy, to: id) }
                .onChange(of: focusedAmount) { _, id in scroll(proxy, to: id) }
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
            .onChange(of: focusedName) { previous, _ in
                if let previous { checkSwap(previous) }
            }
            .alert("Couldn't Save That", isPresented: Binding(presenting: $errorMessage)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
            .interactiveDismissDisabled(draft.hasChanges)
        }
    }

    // MARK: Ingredients

    private var ingredientsSection: some View {
        Section {
            ForEach($draft.lines) { $line in
                IngredientEntryRow(
                    id: line.id,
                    name: $line.name,
                    amount: Binding(
                        get: { draft.quantity(of: line) },
                        set: { draft.setQuantity($0, for: line.id) }),
                    unit: $line.unit,
                    units: units(for: line),
                    nameFocus: $focusedName,
                    amountFocus: $focusedAmount,
                    next: { next(after: line.id) })
            }
            .onDelete { draft.removeLines(at: $0) }
            Button {
                focus(draft.addLine())
            } label: {
                Label("Add Ingredient", systemImage: "plus")
                    .frame(minHeight: 44)
            }
        } header: {
            Text("Ingredients")
        } footer: {
            Text("Rename an ingredient to swap it. We'll offer to change the steps too.")
        }
    }

    /// Keeps the ingredient being typed above the keyboard.
    private func scroll(_ proxy: ScrollViewProxy, to id: RecipeEditDraft.Line.ID?) {
        guard let id else { return }
        withAnimation { proxy.scrollTo(id, anchor: .center) }
    }

    /// Next on the amount keypad: on to the next ingredient, or a new one after the last.
    private func next(after id: RecipeEditDraft.Line.ID) -> Bool {
        guard let following = draft.line(after: id) else { return false }
        focus(following)
        return true
    }

    /// Puts the cursor in an ingredient's name once its row is on screen.
    private func focus(_ id: RecipeEditDraft.Line.ID) {
        DispatchQueue.main.async {
            focusedAmount = nil
            focusedName = id
        }
    }

    /// The unit codes to offer, with the line's own when it's one the list doesn't have.
    private func units(for line: RecipeEditDraft.Line) -> [String] {
        RecipeUnit.codes.contains(line.unit) ? RecipeUnit.codes : [line.unit] + RecipeUnit.codes
    }

    // MARK: Steps

    private var stepsSection: some View {
        Section("Steps") {
            ForEach(draft.steps.indices, id: \.self) { index in
                VStack(alignment: .leading, spacing: 4) {
                    Text("Step \(index + 1)")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                    TextField("What to do", text: $draft.steps[index], axis: .vertical)
                        .lineLimit(2...12)
                        .keyboardDoneButton()
                }
                .padding(.vertical, 2)
            }
            .onDelete { draft.steps.remove(atOffsets: $0) }
            Button("Add Step", systemImage: "plus") { draft.steps.append("") }
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
        if let id = focusedName { checkSwap(id) }
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
