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

/// The review step: every field the parser filled, editable. The photo comes first, then the
/// name, the servings and times, the ingredients one sheet at a time, and the steps.
struct RecipeDraftReviewForm: View {
    @Bindable var store: RecipeDraftStore

    @Environment(RecipeLibrary.self) private var library
    @State private var photo = RecipePhotoModel()
    @State private var ingredientSheet: IngredientSheetTarget<UUID>?
    @State private var reorder: ReorderTarget?
    @State private var openTime: TimeField?
    @FocusState private var focus: Field?

    private enum Field: Hashable {
        case name
        case headline
        case step(Int)
    }

    private enum TimeField {
        case prep
        case cook
    }

    private var warnings: [String] { store.draft.listedWarnings }

    var body: some View {
        ScrollViewReader { proxy in
            form
                .onChange(of: focus) { _, field in
                    if case .step(let index) = field {
                        withAnimation { proxy.scrollTo(StepRowID(index: index), anchor: .center) }
                    }
                }
        }
        .keyboardDismissal()
        .recipePhotoPickers(
            photo, imageURL: $store.draft.imageURL, searchText: store.draft.name,
            source: RecipePhotoSource(
                upload: { try await store.uploadPhoto($0) },
                search: { try await store.searchPhotos($0, page: $1) },
                useFound: { try await store.useFoundPhoto($0) })
        )
        .sheet(item: $ingredientSheet) { target in
            ingredientEditor(target)
        }
        .sheet(item: $reorder) { target in
            reorderSheet(target)
        }
        .task { await library.loadIngredientNames() }
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
            RecipePhotoSection(model: photo, imageURL: $store.draft.imageURL)
            Section("Recipe") {
                TextField("Recipe name", text: $store.draft.name)
                    .font(.title3.weight(.semibold))
                    .textInputAutocapitalization(.words)
                    .submitLabel(store.draft.fromURL ? .done : .next)
                    .focused($focus, equals: .name)
                    .onSubmit { if !store.draft.fromURL { focus = .headline } }
                    .frame(minHeight: 44)
                if !store.draft.fromURL {
                    TextField(
                        "Short description (optional)", text: Binding($store.draft.headline, replacingNilWith: ""),
                        axis: .vertical
                    )
                    .lineLimit(1...4)
                    .focused($focus, equals: .headline)
                    .frame(minHeight: 44)
                }
            }
            detailsSection
            ingredientsSection
            if !store.draft.fromURL {
                stepsSection
            }
            NutritionSection(values: $store.draft.nutrition)
        }
    }

    // MARK: Servings and times

    private var detailsSection: some View {
        Section {
            RecipeServingsField(servings: $store.draft.servings)
            if !store.draft.fromURL {
                RecipeTimeField(title: "Prep time", minutes: $store.draft.prepTime, isOpen: isOpen(.prep))
                RecipeTimeField(title: "Cook time", minutes: $store.draft.cookTime, isOpen: isOpen(.cook))
            }
        } header: {
            Text("Servings and Time")
        } footer: {
            if !store.draft.fromURL, store.draft.totalTime > 0 {
                Text("Total: \(RecipeFormat.minutes(store.draft.totalTime))")
            }
        }
    }

    /// Only one time picker open at a time.
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
            let lines = store.draft.ingredients
            ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                VStack(alignment: .leading, spacing: 2) {
                    if let group = line.group, !group.isEmpty, startsGroup(at: index) {
                        Text(group)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.top, 4)
                    }
                    RecipeListRow(
                        title: line.name,
                        detail: IngredientFields.detail(
                            quantity: line.quantity ?? "", unit: line.unit ?? "count", note: line.note ?? ""),
                        caption: originalLine(line),
                        canMoveUp: index > 0, canMoveDown: index < lines.count - 1,
                        onEdit: { ingredientSheet = .edit(line.id) },
                        onMove: { store.draft.ingredients.move(at: index, by: $0) },
                        onDelete: { store.draft.ingredients.removeAll { $0.id == line.id } })
                }
            }
            Button {
                Keyboard.dismiss()
                ingredientSheet = .new
            } label: {
                Label("Add Ingredient", systemImage: "plus")
                    .frame(minHeight: 44)
            }
        } header: {
            SectionHeaderWithMenu(title: "Ingredients") {
                Button("Reorder & Delete Ingredients", systemImage: "arrow.up.arrow.down") {
                    reorder = .ingredients
                }
                .disabled(store.draft.ingredients.isEmpty)
            }
        }
    }

    private func originalLine(_ line: RecipeDraftIngredient) -> String? {
        guard let raw = line.rawText, !raw.isEmpty, raw != line.name else { return nil }
        return String(localized: "Original line: \(raw)")
    }

    @ViewBuilder
    private func ingredientEditor(_ target: IngredientSheetTarget<UUID>) -> some View {
        switch target {
        case .new:
            IngredientEditorSheet(mode: .new, units: RecipeUnit.codes, names: library.ingredientNames) { fields in
                store.draft.ingredients.append(RecipeDraftIngredient(fields: fields))
            }
        case .edit(let id):
            if let line = store.draft.ingredients.first(where: { $0.id == id }) {
                IngredientEditorSheet(
                    mode: .edit, initial: line.fields, units: units(for: line.unit ?? "count"),
                    names: library.ingredientNames
                ) { fields in
                    guard let i = store.draft.ingredients.firstIndex(where: { $0.id == id }) else { return }
                    store.draft.ingredients[i].apply(fields)
                }
            }
        }
    }

    // MARK: Steps

    private var stepsSection: some View {
        Section {
            ForEach(store.draft.steps.indices, id: \.self) { index in
                StepEntryRow(
                    index: index, text: $store.draft.steps[index], count: store.draft.steps.count,
                    focus: $focus, field: .step(index),
                    onMove: { store.draft.steps.move(at: index, by: $0) },
                    onDelete: { store.draft.steps.remove(at: index) }
                )
                .id(StepRowID(index: index))
            }
            Button {
                store.draft.steps.append("")
                let index = store.draft.steps.count - 1
                DispatchQueue.main.async { focus = .step(index) }
            } label: {
                Label("Add Step", systemImage: "plus")
                    .frame(minHeight: 44)
            }
        } header: {
            SectionHeaderWithMenu(title: "Steps") {
                Button("Reorder & Delete Steps", systemImage: "arrow.up.arrow.down") {
                    reorder = .steps
                }
                .disabled(store.draft.steps.isEmpty)
            }
        }
    }

    @ViewBuilder
    private func reorderSheet(_ target: ReorderTarget) -> some View {
        switch target {
        case .ingredients:
            ReorderDeleteSheet(
                title: "Reorder & Delete Ingredients",
                items: store.draft.ingredients.enumerated().map { index, line in
                    ReorderItem(
                        id: index, title: line.name,
                        detail: IngredientFields.detail(
                            quantity: line.quantity ?? "", unit: line.unit ?? "count", note: line.note ?? ""))
                }
            ) { places in
                store.draft.ingredients = store.draft.ingredients.reordered(by: places)
            }
        case .steps:
            ReorderDeleteSheet(
                title: "Reorder & Delete Steps",
                items: store.draft.steps.enumerated().map { index, step in
                    ReorderItem(id: index, title: String(localized: "Step \(index + 1)"), detail: step)
                }
            ) { places in
                store.draft.steps = store.draft.steps.reordered(by: places)
            }
        }
    }

    /// Whether the line at `index` is the first under its heading.
    private func startsGroup(at index: Int) -> Bool {
        let lines = store.draft.ingredients
        return index == 0 || lines[index - 1].group != lines[index].group
    }

    private func units(for unit: String) -> [String] {
        RecipeUnit.codes.contains(unit) ? RecipeUnit.codes : [unit] + RecipeUnit.codes
    }
}

/// Which ingredient sheet is open: a new one, or one being edited.
enum IngredientSheetTarget<ID: Hashable>: Identifiable, Hashable {
    case new
    case edit(ID)

    var id: Self { self }
}

/// Which list Reorder & Delete is showing.
enum ReorderTarget: Identifiable {
    case ingredients
    case steps

    var id: Self { self }
}

struct StepRowID: Hashable {
    let index: Int
}

extension RecipeDraftIngredient {
    init(fields: IngredientFields) {
        self.init()
        apply(fields)
    }

    /// The line as the New Ingredient sheet shows it.
    var fields: IngredientFields {
        IngredientFields(quantity: quantity ?? "", unit: unit ?? "count", name: name, note: note ?? "")
    }

    /// Saves what the sheet changed. The original line from the page stays for reference.
    mutating func apply(_ fields: IngredientFields) {
        name = fields.name
        quantity = fields.quantity.isEmpty ? nil : fields.quantity
        unit = fields.unit
        note = fields.note.isEmpty ? nil : fields.note
    }
}

/// A section title with a menu of list actions on the right, like Reorder & Delete.
struct SectionHeaderWithMenu<Actions: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder let actions: () -> Actions

    var body: some View {
        HStack {
            Text(title)
            Spacer()
            Menu {
                actions()
            } label: {
                Image(systemName: "ellipsis.circle")
                    .font(.title3)
                    .frame(width: 44, height: 32)
                    .contentShape(.rect)
            }
            .accessibilityLabel("\(Text(title)) Options")
        }
    }
}

/// One step: its number, the text typed right in the row, and a menu to move or delete it.
struct StepEntryRow<Field: Hashable>: View {
    let index: Int
    @Binding var text: String
    let count: Int
    var focus: FocusState<Field?>.Binding
    let field: Field
    let onMove: (Int) -> Void
    let onDelete: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Text("\(index + 1)")
                .font(.headline.monospacedDigit())
                .foregroundStyle(.secondary)
                .frame(minWidth: 22, minHeight: 32, alignment: .leading)
                .accessibilityHidden(true)
            TextField("What to do", text: $text, axis: .vertical)
                .lineLimit(2...12)
                .focused(focus, equals: field)
                .padding(.vertical, 6)
                .accessibilityLabel("Step \(index + 1)")
            Menu {
                Button("Edit", systemImage: "pencil") { focus.wrappedValue = field }
                Button("Move Up", systemImage: "arrow.up") { onMove(-1) }
                    .disabled(index == 0)
                Button("Move Down", systemImage: "arrow.down") { onMove(1) }
                    .disabled(index >= count - 1)
                Button("Delete", systemImage: "trash", role: .destructive, action: onDelete)
            } label: {
                Image(systemName: "ellipsis")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("More for step \(index + 1)")
        }
        .swipeActions(edge: .trailing) {
            Button("Delete", systemImage: "trash", role: .destructive, action: onDelete)
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
