import SwiftUI

/// The shared form sections for a pantry item's status, amount, staple flag, where it's kept
/// and its best-by date, and note.
struct PantryItemFields: View {
    @Binding var draft: PantryItemDraft
    /// The food, for its recommended best-by date.
    var name: String = ""
    var category: String? = nil

    @Environment(PantryStore.self) private var pantry
    @State private var suggestion: ShelfLifeSuggestion?

    private var lookupKey: String {
        "\(name)|\(category ?? "")|\(draft.storage.rawValue)|\(draft.storedOnString())"
    }

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
            Picker("Kept In", selection: storageBinding) {
                ForEach(PantryStorage.choices, id: \.self) { storage in
                    Text(storage.title).tag(storage)
                }
            }
            .pickerStyle(.segmented)
            DatePicker("Put Away", selection: $draft.storedOn, in: ...Date.now, displayedComponents: .date)
            if draft.hasExpiry {
                DatePicker("Best By", selection: $draft.expiryDate, displayedComponents: .date)
            } else {
                LabeledContent("Best By") {
                    if let suggestion,
                        let date = PantryDate.date(from: suggestion.bestBy, timeZone: .autoupdatingCurrent)
                    {
                        Text(date.formatted(date: .abbreviated, time: .omitted))
                    } else {
                        Text(name.isEmpty ? "Add a name first" : "…")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            Toggle("Pick My Own Date", isOn: $draft.hasExpiry.animation())
        } header: {
            Text("Storage")
        } footer: {
            if !draft.hasExpiry, let suggestion {
                Text(suggestion.explanation)
            }
        }
        .task(id: lookupKey) { await lookUp() }

        Section {
            TextField("Note", text: $draft.note, prompt: Text("Optional"), axis: .vertical)
                .lineLimit(1...4)
            if let error = draft.noteError {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Note")
        } footer: {
            PantryBestByDisclaimer()
                .padding(.top, 12)
        }
    }

    /// Choosing a place marks it as the member's choice, so a suggestion won't move it.
    private var storageBinding: Binding<PantryStorage> {
        Binding(
            get: { draft.storage },
            set: {
                draft.storage = $0
                draft.storageChosen = true
            })
    }

    /// The recommended best-by date for what's typed, where it's kept, and when it went there.
    /// Until the member picks a place, the food's usual place is used ("Carrots" go in the
    /// fridge).
    private func lookUp() async {
        guard !name.trimmingCharacters(in: .whitespaces).isEmpty else {
            suggestion = nil
            return
        }
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        let found = await pantry.shelfLife(
            name: name, category: category, storage: draft.storage, storedOn: draft.storedOnString())
        guard !Task.isCancelled else { return }
        if let found, !draft.storageChosen, let usual = found.usualStorage, usual != draft.storage {
            // The next lookup, for the usual place, fills the date in.
            draft.storage = usual
            return
        }
        suggestion = found
    }
}

/// "These are recommended best-by dates. Use your best judgment."
struct PantryBestByDisclaimer: View {
    var body: some View {
        Text(
            "These are recommended best-by dates from USDA FoodKeeper. Use your best judgment."
        )
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Edits one item, restocks it, or deletes it, and shows its usage estimate and purchases.
struct PantryEditSheet: View {
    @Environment(PantryStore.self) private var pantry
    @Environment(\.dismiss) private var dismiss

    /// The item the draft started from; changes are computed against it.
    @State private var item: PantryItem
    @State private var draft: PantryItemDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var isConfirmingDelete = false
    @State private var isRestocking = false

    init(item: PantryItem) {
        _item = State(initialValue: item)
        _draft = State(initialValue: PantryItemDraft(item: item))
    }

    private var hasChanges: Bool {
        (try? draft.changes(from: item))?.isEmpty == false
    }

    /// The item as the store has it now, so the estimate stays current after a restock.
    private var current: PantryItem {
        pantry.items.first { $0.id == item.id } ?? item
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
                    Button("Restock / I Bought This", systemImage: "cart.badge.plus") {
                        isRestocking = true
                    }
                } footer: {
                    Text("Records a purchase. The estimate starts over from the amount you bought.")
                }
                PantryItemFields(draft: $draft, name: item.displayName, category: item.category)
                PantryThresholdFields(draft: $draft, householdPercent: pantry.settings?.lowThresholdPercent)
                PantryUsageSections(item: current, canRecordUse: true)
                Section {
                    Button("Delete from Pantry", role: .destructive) {
                        isConfirmingDelete = true
                    }
                }
            }
            .task {
                if pantry.settings == nil {
                    _ = try? await pantry.loadSettings()
                }
            }
            .onChange(of: draft.usesHouseholdThreshold) { _, usesHousehold in
                // Turning the override on starts from the household's threshold.
                if !usesHousehold, item.lowThresholdPercent == nil, let percent = pantry.settings?.lowThresholdPercent {
                    draft.lowThresholdPercent = percent
                }
            }
            .sheet(isPresented: $isRestocking) {
                PantryRestockSheet(item: current) { updated in
                    // Without edits in progress the form shows the restocked item. With edits,
                    // the draft keeps its base so only the member's own changes are sent.
                    if !hasChanges {
                        item = updated
                        draft = PantryItemDraft(item: updated)
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

    /// The name to date: once it's chosen from the catalog, confirmed, or not being searched.
    private var nameIsSettled: Bool {
        selectedIngredient != nil || confirmedName == trimmedName || trimmedName.count >= 3
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
                PantryItemFields(
                    draft: $draft, name: nameIsSettled ? trimmedName : "", category: selectedIngredient?.category)
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
