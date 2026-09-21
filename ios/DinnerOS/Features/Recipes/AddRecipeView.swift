import SwiftUI

/// Adding a recipe by hand: paste text or a link, review what came back, save.
///
/// The review step is the point. Everything on it was read by a parser from
/// text nobody checked, so a person confirms it before it becomes one of the
/// household's recipes — and it stays the household's: a typed or pasted
/// recipe never enters the global catalog.
struct AddRecipeView: View {
    /// Called with the saved recipe so the caller can dismiss and show it.
    var onSaved: (Recipe) -> Void = { _ in }

    @Environment(HouseholdStore.self) private var households
    @Environment(RecipeLibrary.self) private var library
    @Environment(\.dismiss) private var dismiss

    @State private var store: RecipeDraftStore?
    @State private var mode: EntryMode = .text
    @State private var text = ""
    @State private var link = ""

    private enum EntryMode: String, CaseIterable, Identifiable {
        case text
        case link

        var id: String { rawValue }

        var title: LocalizedStringKey {
            switch self {
            case .text: "Paste Text"
            case .link: "Paste a Link"
            }
        }
    }

    var body: some View {
        NavigationStack {
            Group {
                if let store {
                    content(store)
                } else {
                    ProgressView()
                }
            }
            .navigationTitle("Add a Recipe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbar }
            .task {
                guard store == nil, let householdID = households.current?.household.id else { return }
                store = library.makeDraftStore(householdID: householdID)
            }
        }
    }

    @ViewBuilder
    private func content(_ store: RecipeDraftStore) -> some View {
        @Bindable var store = store
        switch store.step {
        case .entry, .parsing:
            entryForm(store)
        case .review, .saving:
            RecipeDraftReviewForm(draft: $store.draft, warnings: store.draft.warnings)
                .disabled(store.step == .saving)
        }
    }

    @ViewBuilder
    private func entryForm(_ store: RecipeDraftStore) -> some View {
        Form {
            Section {
                Picker("How", selection: $mode) {
                    ForEach(EntryMode.allCases) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                .pickerStyle(.segmented)
            }
            switch mode {
            case .text:
                Section {
                    TextEditor(text: $text)
                        .frame(minHeight: 200)
                        .accessibilityLabel("Recipe text")
                } header: {
                    Text("Paste the Recipe")
                } footer: {
                    Text("Headings like \"Ingredients\" and \"Steps\" help us split it up. You'll review it next.")
                }
            case .link:
                Section {
                    TextField("https://…", text: $link)
                        .textContentType(.URL)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } header: {
                    Text("Paste a Link")
                } footer: {
                    Text("We read the page on our server, never on your phone. You'll review what we find.")
                }
            }
            if let message = store.errorMessage {
                Section {
                    Label(message, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                Button("Type It Myself") {
                    store.startBlank()
                }
            }
        }
        .overlay {
            if store.step == .parsing {
                ProgressView("Reading…")
                    .padding()
                    .background(.regularMaterial, in: .rect(cornerRadius: 12))
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Cancel") { dismiss() }
        }
        ToolbarItem(placement: .confirmationAction) {
            if let store {
                switch store.step {
                case .entry, .parsing:
                    Button("Next") {
                        Task {
                            switch mode {
                            case .text: await store.parse(text: text)
                            case .link: await store.parse(url: link)
                            }
                        }
                    }
                    .disabled(store.isBusy || currentInput.isEmpty)
                case .review, .saving:
                    Button("Save") {
                        Task {
                            if let recipe = await store.save() {
                                onSaved(recipe)
                                dismiss()
                            }
                        }
                    }
                    .disabled(!store.canSave)
                }
            }
        }
    }

    private var currentInput: String {
        let value = mode == .text ? text : link
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// The review step: every field the parser filled, editable.
struct RecipeDraftReviewForm: View {
    @Binding var draft: RecipeDraft
    let warnings: [String]

    /// "Serves 4", or a reminder that the source didn't say.
    private var servingsLabel: String {
        draft.servings == 0
            ? String(localized: "Servings: not set") : String(localized: "Serves \(draft.servings)")
    }

    var body: some View {
        Form {
            if !warnings.isEmpty {
                Section {
                    ForEach(warnings, id: \.self) { warning in
                        Label(warning, systemImage: "exclamationmark.triangle")
                            .font(.footnote)
                    }
                } header: {
                    Text("Check These")
                }
            }
            Section("Recipe") {
                TextField("Name", text: $draft.name)
                TextField("Headline", text: Binding($draft.headline, replacingNilWith: ""))
                Stepper(
                    value: $draft.servings, in: 0...12,
                    label: { Text(servingsLabel) })
                Stepper(
                    value: Binding($draft.totalMinutes, replacingNilWith: 0), in: 0...480, step: 5,
                    label: { Text(RecipeFormat.minutes(draft.totalMinutes ?? 0)) })
            }
            Section("Ingredients") {
                ForEach($draft.ingredients) { $line in
                    VStack(alignment: .leading, spacing: 4) {
                        TextField("Ingredient", text: $line.name)
                        HStack {
                            TextField("Amount", text: Binding($line.quantity, replacingNilWith: ""))
                                .frame(maxWidth: 100)
                            Picker("Unit", selection: Binding($line.unit, replacingNilWith: "count")) {
                                ForEach(RecipeUnit.codes, id: \.self) { code in
                                    Text(RecipeUnit.label(code)).tag(code)
                                }
                            }
                            .labelsHidden()
                        }
                        if let raw = line.rawText, !raw.isEmpty, raw != line.name {
                            Text(raw)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .onDelete { draft.ingredients.remove(atOffsets: $0) }
                Button("Add Ingredient") {
                    draft.ingredients.append(RecipeDraftIngredient())
                }
            }
            Section("Steps") {
                ForEach(draft.steps.indices, id: \.self) { index in
                    TextField("Step \(index + 1)", text: $draft.steps[index], axis: .vertical)
                }
                .onDelete { draft.steps.remove(atOffsets: $0) }
                Button("Add Step") {
                    draft.steps.append("")
                }
            }
        }
    }
}

/// A binding that shows `replacement` where the value is `nil`, and writes
/// `nil` back when the field is emptied, so an untouched optional stays absent.
extension Binding {
    init(_ source: Binding<String?>, replacingNilWith replacement: String) where Value == String {
        self.init(
            get: { source.wrappedValue ?? replacement },
            set: { source.wrappedValue = $0.isEmpty ? nil : $0 })
    }

    init(_ source: Binding<Int?>, replacingNilWith replacement: Int) where Value == Int {
        self.init(
            get: { source.wrappedValue ?? replacement },
            set: { source.wrappedValue = $0 == 0 ? nil : $0 })
    }
}
