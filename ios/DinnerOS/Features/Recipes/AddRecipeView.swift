import SwiftUI

/// Adding a recipe by hand: paste text or a link, review what came back, save.
///
/// The review step is the point. Everything on it was read by a parser from
/// text nobody checked, so a person confirms it before it becomes one of the
/// household's recipes — and it stays the household's: a typed or pasted
/// recipe never enters the global catalog.
struct AddRecipeView: View {
    /// A link shared from Safari: the screen opens on it and reads it right away.
    var initialLink: String? = nil
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
                let store = library.makeDraftStore(householdID: householdID)
                self.store = store
                if let initialLink {
                    mode = .link
                    link = initialLink
                    await store.parse(url: initialLink)
                }
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
            RecipeDraftReviewForm(store: store)
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
                    PasteButton(payloadType: String.self) { strings in
                        if let first = strings.first { link = RecipePageLoader.link(in: first) }
                    }
                } header: {
                    Text("Paste a Link")
                } footer: {
                    Text(
                        "Any recipe site works. We save the ingredients and a link back to the page, and you check them before saving."
                    )
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
    @Bindable var store: RecipeDraftStore

    @FocusState private var nameFocus: UUID?
    @State private var amountFocus: UUID?

    private var warnings: [String] { store.draft.listedWarnings }

    /// "Serves 4", or a reminder that the source didn't say.
    private var servingsLabel: String {
        store.draft.servings == 0
            ? String(localized: "Servings: not set") : String(localized: "Serves \(store.draft.servings)")
    }

    var body: some View {
        ScrollViewReader { proxy in
            form
                .onChange(of: nameFocus) { _, id in scroll(proxy, to: id) }
                .onChange(of: amountFocus) { _, id in scroll(proxy, to: id) }
        }
    }

    /// Keeps the ingredient being typed above the keyboard.
    private func scroll(_ proxy: ScrollViewProxy, to id: UUID?) {
        guard let id else { return }
        withAnimation { proxy.scrollTo(id, anchor: .center) }
    }

    private var form: some View {
        Form {
            if store.draft.fromURL {
                Section {
                    Label(ImportedRecipeText.note, systemImage: "checklist")
                        .font(.subheadline)
                    if let link = store.draft.sourceURL, let url = URL(string: link) {
                        Link(destination: url) {
                            Label("View Recipe", systemImage: "safari")
                        }
                    }
                }
            }
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
                TextField("Name", text: $store.draft.name)
                if !store.draft.fromURL {
                    TextField("Headline", text: Binding($store.draft.headline, replacingNilWith: ""))
                }
                Stepper(
                    value: $store.draft.servings, in: 0...12,
                    label: { Text(servingsLabel) })
                if !store.draft.fromURL {
                    Stepper(
                        value: Binding($store.draft.totalMinutes, replacingNilWith: 0), in: 0...480, step: 5,
                        label: { Text(RecipeFormat.minutes(store.draft.totalMinutes ?? 0)) })
                }
            }
            RecipePhotoSection(imageURL: $store.draft.imageURL) { jpeg in
                try await store.uploadPhoto(jpeg)
            }
            ingredientsSection
            if !store.draft.fromURL {
                stepsSection
            }
        }
        .scrollDismissesKeyboard(.interactively)
    }

    private var ingredientsSection: some View {
        Section {
            ForEach($store.draft.ingredients) { $line in
                VStack(alignment: .leading, spacing: 4) {
                    if let group = line.group, !group.isEmpty, startsGroup(line) {
                        Text(group)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                    }
                    IngredientEntryRow(
                        id: line.id,
                        name: $line.name,
                        amount: Binding($line.quantity, replacingNilWith: ""),
                        unit: Binding($line.unit, replacingNilWith: "count"),
                        units: RecipeUnit.codes.contains(line.unit ?? "count")
                            ? RecipeUnit.codes : [line.unit ?? "count"] + RecipeUnit.codes,
                        nameFocus: $nameFocus,
                        amountFocus: $amountFocus,
                        next: { next(after: line.id) })
                    if let note = line.note, !note.isEmpty {
                        Text(note)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if let raw = line.rawText, !raw.isEmpty, raw != line.name {
                        Text("Original line: \(raw)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .onDelete { store.draft.ingredients.remove(atOffsets: $0) }
            Button {
                focus(store.addIngredient())
            } label: {
                Label("Add Ingredient", systemImage: "plus")
                    .frame(minHeight: 44)
            }
        } header: {
            Text("Ingredients")
        } footer: {
            Text("Swipe left on an ingredient to remove it.")
        }
    }

    private var stepsSection: some View {
        Section("Steps") {
            ForEach(store.draft.steps.indices, id: \.self) { index in
                TextField("Step \(index + 1)", text: $store.draft.steps[index], axis: .vertical)
            }
            .onDelete { store.draft.steps.remove(atOffsets: $0) }
            Button("Add Step") {
                store.draft.steps.append("")
            }
        }
    }

    /// Whether the line is the first under its heading.
    private func startsGroup(_ line: RecipeDraftIngredient) -> Bool {
        guard let i = store.draft.ingredients.firstIndex(where: { $0.id == line.id }) else { return false }
        return i == 0 || store.draft.ingredients[i - 1].group != line.group
    }

    /// Next on the amount keypad: on to the next ingredient, or a new one after the last.
    private func next(after id: UUID) -> Bool {
        guard let following = store.ingredient(after: id) else { return false }
        focus(following)
        return true
    }

    /// Puts the cursor in an ingredient's name once its row is on screen.
    private func focus(_ id: UUID) {
        DispatchQueue.main.async {
            amountFocus = nil
            nameFocus = id
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
