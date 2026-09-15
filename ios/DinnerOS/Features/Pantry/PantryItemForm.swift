import SwiftUI

/// The shared form sections for a pantry item's status, amount, staple flag, expiry, and note.
struct PantryItemFields: View {
    @Binding var draft: PantryItemDraft

    var body: some View {
        Section("Status") {
            Picker("Status", selection: $draft.status) {
                ForEach(PantryStatus.allCases) { status in
                    Text(status.title).tag(status)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
        }

        Section {
            TextField("Amount", text: $draft.quantityText, prompt: Text("For example, 1 1/2"))
                .keyboardType(.numbersAndPunctuation)
                .autocorrectionDisabled()
                .disabled(!draft.allowsAmount)
            Picker("Unit", selection: $draft.unit) {
                ForEach(PantryUnit.options(including: draft.unit), id: \.self) { code in
                    Text(PantryUnit.pickerLabel(code)).tag(code)
                }
            }
            .disabled(!draft.allowsAmount)
            if draft.allowsAmount, let error = draft.quantityError {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Amount")
        } footer: {
            if draft.allowsAmount {
                Text("Optional. Leave it empty if you have some but didn't measure.")
            } else {
                Text("Items that are out have no amount.")
            }
        }

        Section {
            Toggle("Staple", isOn: $draft.isStaple)
        } footer: {
            Text("Staples are things you always keep, like salt and oil.")
        }

        Section {
            Toggle("Expiration Date", isOn: $draft.hasExpiry.animation())
            if draft.hasExpiry {
                DatePicker("Expires On", selection: $draft.expiryDate, displayedComponents: .date)
            }
        }

        Section("Note") {
            TextField("Note", text: $draft.note, prompt: Text("Optional"), axis: .vertical)
                .lineLimit(1...4)
            if let error = draft.noteError {
                FormErrorLabel(message: error)
            }
        }
    }
}

/// Edits one item, or deletes it.
struct PantryEditSheet: View {
    let item: PantryItem

    @Environment(PantryStore.self) private var pantry
    @Environment(\.dismiss) private var dismiss

    @State private var draft: PantryItemDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var isConfirmingDelete = false

    init(item: PantryItem) {
        self.item = item
        _draft = State(initialValue: PantryItemDraft(item: item))
    }

    private var hasChanges: Bool {
        (try? draft.changes(from: item))?.isEmpty == false
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                PantryItemFields(draft: $draft)
                Section {
                    Button("Delete from Pantry", role: .destructive) {
                        isConfirmingDelete = true
                    }
                }
            }
            .navigationTitle(item.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(!draft.isValid || !hasChanges || isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
            .confirmationDialog(
                Text("Delete \(item.displayName)?"),
                isPresented: $isConfirmingDelete,
                titleVisibility: .visible
            ) {
                Button("Delete", role: .destructive) { delete() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Grocery lists will stop treating it as at home.")
            }
        }
    }

    private func save() {
        run {
            try await pantry.update(item, changes: try draft.changes(from: item))
        }
    }

    private func delete() {
        run {
            try await pantry.delete(item)
        }
    }

    private func run(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                try await action()
                dismiss()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Adds an ingredient, suggesting catalog matches while the name is typed.
struct PantryAddSheet: View {
    @Environment(PantryStore.self) private var pantry
    @Environment(\.dismiss) private var dismiss

    @State private var name = ""
    @State private var chosen: CatalogIngredient?
    /// A name the user chose to add as typed, which hides the suggestions.
    @State private var confirmedName: String?
    @State private var draft = PantryItemDraft()
    @State private var suggestions: IngredientSuggestions?
    @State private var isSaving = false
    @State private var errorMessage: String?
    @FocusState private var isNameFocused: Bool

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The chosen catalog ingredient, as long as the name still matches it.
    private var selectedIngredient: CatalogIngredient? {
        chosen?.name == trimmedName ? chosen : nil
    }

    private var showsSuggestions: Bool {
        selectedIngredient == nil && confirmedName != trimmedName
            && trimmedName.count >= IngredientSuggestions.minimumQueryLength
    }

    private var canSave: Bool {
        !trimmedName.isEmpty && trimmedName.count <= PantryItemDraft.maxNameLength && draft.isValid && !isSaving
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    TextField("Name", text: $name, prompt: Text("For example, olive oil"))
                        .focused($isNameFocused)
                        .textInputAutocapitalization(.words)
                        .submitLabel(.done)
                        .onSubmit {
                            if selectedIngredient == nil { confirmedName = trimmedName }
                        }
                } header: {
                    Text("Ingredient")
                } footer: {
                    nameFooter
                }
                if showsSuggestions {
                    suggestionsSection
                }
                PantryItemFields(draft: $draft)
            }
            .navigationTitle("Add to Pantry")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { save() }
                        .disabled(!canSave)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
            .onAppear { isNameFocused = true }
            .task(id: name) {
                let model =
                    suggestions
                    ?? IngredientSuggestions { [pantry] query in
                        try await pantry.searchCatalog(query)
                    }
                suggestions = model
                // A newer keystroke cancels this task while it waits.
                await model.update(for: showsSuggestions ? name : "")
            }
        }
    }

    @ViewBuilder
    private var nameFooter: some View {
        if let existing = pantry.existingItem(named: trimmedName, ingredientID: selectedIngredient?.id) {
            Text("\(existing.displayName) is already in your pantry. Adding it again updates that item.")
        } else if let selectedIngredient {
            Text("From the ingredient catalog, in \(PantryCategory.title(selectedIngredient.category)).")
        }
    }

    private var suggestionsSection: some View {
        Section("Suggestions") {
            if let suggestions {
                ForEach(suggestions.results) { ingredient in
                    Button {
                        choose(ingredient)
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(ingredient.name)
                                .foregroundStyle(.primary)
                            Text(PantryCategory.title(ingredient.category))
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                        .contentShape(.rect)
                    }
                    .accessibilityHint("Adds this catalog ingredient.")
                }
                if suggestions.isSearching && suggestions.results.isEmpty {
                    HStack {
                        ProgressView()
                        Text("Searching…")
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
                if let message = suggestions.errorMessage {
                    Text(message)
                        .foregroundStyle(.secondary)
                }
            }
            let hasExactMatch =
                suggestions?.results.contains {
                    $0.name.compare(trimmedName, options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame
                } ?? false
            if !hasExactMatch {
                Button {
                    confirmedName = trimmedName
                    isNameFocused = false
                } label: {
                    Label("Add “\(trimmedName)” as Typed", systemImage: "text.cursor")
                }
            }
        }
    }

    private func choose(_ ingredient: CatalogIngredient) {
        chosen = ingredient
        name = ingredient.name
        isNameFocused = false
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                let item = try draft.newItem(name: trimmedName, ingredientID: selectedIngredient?.id)
                try await pantry.add(item)
                dismiss()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview("Edit") {
    let session = HouseholdPreviewData.session()
    PantryEditSheet(item: PantryPreviewData.items[0])
        .environment(PantryPreviewData.store(session: session))
}

#Preview("Add") {
    let session = HouseholdPreviewData.session()
    PantryAddSheet()
        .environment(PantryPreviewData.store(session: session))
}
