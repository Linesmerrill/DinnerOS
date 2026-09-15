import SwiftUI

/// Customizes a copy of an option (saved as the household's and chosen), or edits one of the
/// household's own options.
struct SpecialtyOptionEditor: View {
    enum Mode {
        /// Copies the option with `basedOnOptionId`, then chooses the copy.
        case customize(SpecialtyOption)
        case edit(SpecialtyOption)
    }

    let specialty: SpecialtyIngredient
    let mode: Mode

    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var draft: SpecialtyOptionDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var showsProblem = false
    /// A copy saved whose choice failed; saving again chooses it instead of adding another.
    @State private var savedOptionID: String?

    init(specialty: SpecialtyIngredient, mode: Mode) {
        self.specialty = specialty
        self.mode = mode
        switch mode {
        case .customize(let option): _draft = State(initialValue: SpecialtyOptionDraft(copying: option))
        case .edit(let option): _draft = State(initialValue: SpecialtyOptionDraft(editing: option))
        }
    }

    private var isCustomizing: Bool {
        if case .customize = mode { return true }
        return false
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                if showsProblem, let problem = draft.problem?.errorDescription {
                    Section {
                        FormErrorLabel(message: problem)
                    }
                }
                Section {
                    TextField("Name", text: $draft.name)
                    TextField("Notes", text: $draft.notes, prompt: Text("Notes (optional)"), axis: .vertical)
                        .lineLimit(2...6)
                } header: {
                    Text(draft.type.title)
                } footer: {
                    if isCustomizing {
                        Text("A copy for your household, chosen when you save. The original stays available.")
                    }
                }
                if draft.isStoreAlternative {
                    Section {
                        amountFields(label: "Replaces", quantity: $draft.perQuantityText, unit: $draft.perUnit)
                    } header: {
                        Text("Replaces")
                    } footer: {
                        Text("How much \(specialty.name) the ingredients below replace, for example 1 tbsp.")
                    }
                } else {
                    Section {
                        amountFields(label: "Makes", quantity: $draft.yieldQuantityText, unit: $draft.yieldUnit)
                        LabeledContent("Keeps (Days)") {
                            TextField("Days", value: $draft.shelfLifeDays, format: .number)
                                .keyboardType(.numberPad)
                                .multilineTextAlignment(.trailing)
                        }
                    } header: {
                        Text("Batch")
                    } footer: {
                        Text("How much one batch makes, and how many days a jar keeps.")
                    }
                }
                ingredientsSection
                if !draft.isStoreAlternative {
                    stepsSection
                }
            }
            .navigationTitle(isCustomizing ? String(localized: "Customize") : String(localized: "Edit Option"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(isCustomizing ? String(localized: "Save & Use") : String(localized: "Save")) { save() }
                        .disabled(isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private var ingredientsSection: some View {
        Section {
            ForEach($draft.ingredients) { $ingredient in
                VStack(alignment: .leading, spacing: 6) {
                    TextField("Ingredient", text: $ingredient.name, prompt: Text("Ingredient name"))
                    amountFields(label: "Amount", quantity: $ingredient.quantityText, unit: $ingredient.unit)
                }
                .padding(.vertical, 2)
            }
            .onDelete { offsets in
                draft.ingredients.remove(atOffsets: offsets)
            }
            if draft.canAddIngredient {
                Button("Add Ingredient", systemImage: "plus") {
                    draft.addIngredient()
                }
            }
        } header: {
            Text("Ingredients")
        } footer: {
            if draft.isStoreAlternative {
                Text("Every ingredient needs an amount. Swipe left to remove one.")
            } else {
                Text("Leave an amount empty for “to taste.” Swipe left to remove an ingredient.")
            }
        }
    }

    private var stepsSection: some View {
        Section {
            ForEach($draft.steps) { $step in
                TextField("Step", text: $step.text, prompt: Text("Describe this step"), axis: .vertical)
            }
            .onDelete { offsets in
                draft.steps.remove(atOffsets: offsets)
            }
            if draft.canAddStep {
                Button("Add Step", systemImage: "plus") {
                    draft.addStep()
                }
            }
        } header: {
            Text("Steps")
        }
    }

    private func amountFields(
        label: LocalizedStringKey, quantity: Binding<String>, unit: Binding<String>
    ) -> some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6)) : AnyLayout(HStackLayout(spacing: 8))
        return layout {
            TextField(label, text: quantity, prompt: Text("Amount"))
                .keyboardType(.numbersAndPunctuation)
                .autocorrectionDisabled()
            Picker("Unit", selection: unit) {
                ForEach(PantryUnit.options(including: unit.wrappedValue), id: \.self) { code in
                    Text(PantryUnit.pickerLabel(code)).tag(code)
                }
            }
            .pickerStyle(.menu)
            .labelsHidden()
            .fixedSize()
        }
    }

    private func save() {
        guard let request = try? draft.request() else {
            withAnimation { showsProblem = true }
            return
        }
        showsProblem = false
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                switch mode {
                case .customize:
                    if let savedOptionID {
                        try await specialties.choose(optionID: savedOptionID, forSpecialtyWithID: specialty.id)
                    } else {
                        try await specialties.addOption(request, toSpecialtyWithID: specialty.id)
                    }
                case .edit(let option):
                    try await specialties.updateOption(request, optionID: option.id, specialtyID: specialty.id)
                }
                dismiss()
            } catch is CancellationError {
                // Nothing to report.
            } catch let error as SpecialtyOptionNotChosenError {
                savedOptionID = error.optionID
                errorMessage = error.errorDescription
                if error.isForbidden {
                    await households.load()
                }
            } catch {
                errorMessage = HouseholdStore.message(for: error)
                if (error as? APIError)?.status == 403 {
                    await households.load()
                }
            }
        }
    }
}

#Preview("Customize Batch") {
    let session = HouseholdPreviewData.session()
    SpecialtyOptionEditor(
        specialty: SpecialtyPreviewData.southwest, mode: .customize(SpecialtyPreviewData.blendBatch)
    )
    .environment(HouseholdPreviewData.store(session: session))
    .environment(SpecialtyPreviewData.store(session: session))
}
