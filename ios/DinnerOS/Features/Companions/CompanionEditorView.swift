import SwiftUI

/// Quick setup for a companion: a name, then everything else optional. Opened from Add a
/// Recipe ("Add a Companion") and from the Companions list, as a sheet.
struct CompanionEditorView: View {
    let householdID: String
    private let existing: Companion?
    private let onSaved: (Companion) -> Void

    @Environment(CompanionsStore.self) private var companions: CompanionsStore?
    @Environment(AutopilotStore.self) private var autopilot: AutopilotStore?
    @Environment(\.dismiss) private var dismiss

    @State private var draft: CompanionDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var confirmsDelete = false

    /// A new companion.
    init(householdID: String, onSaved: @escaping (Companion) -> Void) {
        self.householdID = householdID
        existing = nil
        self.onSaved = onSaved
        _draft = State(initialValue: CompanionDraft())
    }

    /// A new companion, for a caller that only needs to know it was saved.
    init(householdID: String, onSaved: @escaping () -> Void) {
        self.init(householdID: householdID, onSaved: { (_: Companion) in onSaved() })
    }

    /// Changes a saved companion.
    init(householdID: String, companion: Companion, onSaved: @escaping (Companion) -> Void = { _ in }) {
        self.householdID = householdID
        existing = companion
        self.onSaved = onSaved
        _draft = State(initialValue: CompanionDraft(companion))
    }

    private var frequencies: [CompanionOption] { companions?.frequencies ?? CompanionOption.fallbackFrequencies }
    private var modes: [CompanionOption] { companions?.modes ?? CompanionOption.fallbackModes }

    private var cuisineOptions: [AutopilotOption] {
        let options = autopilot?.vocabulary?.cuisines ?? []
        return options.isEmpty ? CompanionCuisines.fallback : options
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $draft.name, prompt: Text("Tortilla chips"))
                        .textInputAutocapitalization(.sentences)
                        .accessibilityIdentifier("CompanionName")
                } header: {
                    Text("Name")
                } footer: {
                    if let recipe = draft.linkedRecipe {
                        Text("Made from your \(recipe.name) recipe. Adding it plans that recipe with the meal.")
                    }
                }
                if draft.linkedRecipe == nil {
                    storeSection
                }
                goesWithSection
                Section {
                    Picker("How Often", selection: $draft.frequency) {
                        ForEach(frequencies) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } header: {
                    Text("How Often")
                } footer: {
                    if draft.frequency == Companion.onlyWithMeals, !draft.goesWithSomething {
                        Text("Pick what it goes with, or how often you buy it.")
                    }
                }
                Section("When It's Due") {
                    Picker("When It's Due", selection: $draft.mode) {
                        ForEach(modes) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                if existing != nil {
                    Section {
                        Button("Delete Companion", role: .destructive) {
                            confirmsDelete = true
                        }
                        .accessibilityIdentifier("CompanionDelete")
                    }
                }
            }
            .navigationTitle(existing == nil ? "New Companion" : "Edit Companion")
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
                            .disabled(!draft.canSave)
                            .accessibilityIdentifier("CompanionSave")
                    }
                }
            }
            .confirmationDialog(
                "Delete \(draft.name)?", isPresented: $confirmsDelete, titleVisibility: .visible
            ) {
                Button("Delete", role: .destructive) { delete() }
                Button("Cancel", role: .cancel) {}
            } message: {
                if draft.linkedRecipe != nil {
                    Text("Your recipe stays. It just won't be suggested as a companion.")
                }
            }
            .interactiveDismissDisabled(isSaving)
            .task {
                if autopilot?.vocabulary == nil {
                    await autopilot?.activate(householdID: householdID)
                }
                if companions?.householdID != householdID {
                    await companions?.load(householdID: householdID)
                }
            }
        }
    }

    private var storeSection: some View {
        Section {
            TextField("What to Buy", text: $draft.storeItem, prompt: Text("Same as the name"))
                .textInputAutocapitalization(.sentences)
                .accessibilityIdentifier("CompanionStoreItem")
            Stepper(value: $draft.quantity, in: 0...99) {
                if draft.quantity == 0 {
                    Text("How Many: Any")
                } else {
                    Text("How Many: \(draft.quantity)")
                }
            }
            TextField("Product Link", text: $draft.productLink, prompt: Text("Paste a link to the product"))
                .keyboardType(.URL)
                .textContentType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } header: {
            Text("At the Store")
        } footer: {
            Text("All optional. It goes on the list under its name unless you say what to buy.")
        }
    }

    private var goesWithSection: some View {
        Section {
            ValueChipGroup(
                options: cuisineOptions, selection: $draft.cuisines, maxCount: 12, initialCount: 12)
            NavigationLink {
                CompanionRecipePicker(selection: $draft.recipes)
            } label: {
                LabeledContent("Recipes", value: recipesSummary)
            }
            .accessibilityIdentifier("CompanionRecipes")
        } header: {
            Text("Goes With")
        } footer: {
            Text("Pick cuisines or recipes. It's suggested when one of them is on the menu.")
        }
    }

    private var recipesSummary: String {
        switch draft.recipes.count {
        case 0: String(localized: "None")
        case 1: draft.recipes[0].name
        default: String(localized: "\(draft.recipes.count) recipes")
        }
    }

    private func save() {
        guard let companions, draft.canSave else { return }
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                let saved = try await companions.save(draft, id: existing?.id, householdID: householdID)
                onSaved(saved)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }

    private func delete() {
        guard let companions, let existing else { return }
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                try await companions.delete(id: existing.id, householdID: householdID)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Cuisines to offer before the Autopilot vocabulary loads, or from an older server.
enum CompanionCuisines {
    static let fallback: [AutopilotOption] = [
        ("american", "American"), ("chinese", "Chinese"), ("french", "French"), ("greek", "Greek"),
        ("indian", "Indian"), ("italian", "Italian"), ("japanese", "Japanese"), ("korean", "Korean"),
        ("mediterranean", "Mediterranean"), ("mexican", "Mexican"), ("middle eastern", "Middle Eastern"),
        ("thai", "Thai"), ("vietnamese", "Vietnamese"),
    ].map { AutopilotOption(value: $0.0, label: String(localized: String.LocalizationValue($0.1)), description: nil, recipeCount: nil) }
}

/// Picks the main meals a companion goes with, over its own copy of the library so the
/// Menu's search doesn't change.
struct CompanionRecipePicker: View {
    @Binding var selection: [CompanionRecipe]

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households

    @State private var picker: RecipeLibrary?
    @State private var searchText = ""

    private static let searchDebounce = Duration.milliseconds(350)

    var body: some View {
        Group {
            if let picker {
                content(picker)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Recipes")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $searchText, prompt: "Search recipes")
        .task {
            guard let householdID = households.current?.household.id else { return }
            let picker = picker ?? library.makeIndependentCopy()
            self.picker = picker
            await picker.activate(householdID: householdID)
        }
        .task(id: searchText) {
            guard let picker, searchText != picker.filters.search else { return }
            do {
                try await Task.sleep(for: Self.searchDebounce)
            } catch {
                return
            }
            await picker.setSearch(searchText)
        }
    }

    @ViewBuilder
    private func content(_ picker: RecipeLibrary) -> some View {
        let meals = picker.items.filter { !$0.isAddon }
        List {
            ForEach(meals) { summary in
                let isSelected = selection.contains { $0.id == summary.id }
                Button {
                    var draft = CompanionDraft()
                    draft.recipes = selection
                    draft.toggleRecipe(CompanionRecipe(id: summary.id, name: summary.name))
                    selection = draft.recipes
                } label: {
                    HStack {
                        Text(summary.name)
                            .foregroundStyle(Color.primary)
                        Spacer()
                        if isSelected {
                            Image(systemName: "checkmark")
                                .foregroundStyle(Color.accentColor)
                        }
                    }
                }
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
            if picker.hasMore {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .task(id: picker.nextCursor) {
                        await picker.loadMore()
                    }
            }
        }
        .overlay {
            if meals.isEmpty, !picker.hasMore, picker.phase == .loaded {
                ContentUnavailableView("No Recipes", systemImage: "fork.knife", description: Text("Add a recipe first."))
            }
        }
    }
}

#Preview("New") {
    let session = HouseholdPreviewData.session()
    CompanionEditorView(householdID: "household-preview", onSaved: { (_: Companion) in })
        .environment(CompanionsStore.preview(session: session))
        .environment(AutopilotPreviewData.store(session: session))
        .environment(HouseholdPreviewData.store(session: session))
        .environment(RecipePreviewData.library(session: session))
}
