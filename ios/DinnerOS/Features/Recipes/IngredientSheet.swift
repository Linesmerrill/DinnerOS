import SwiftUI

/// One ingredient as the member types it in the New Ingredient sheet.
nonisolated struct IngredientFields: Equatable, Sendable {
    var quantity = ""
    var unit = "count"
    var name = ""
    var note = ""

    var isBlank: Bool { name.trimmingCharacters(in: .whitespaces).isEmpty }

    /// "2 Tbsp", "2" for a count, or "" with no amount.
    static func amount(quantity: String, unit: String) -> String {
        let shown = AmountEntry.display(quantity.trimmingCharacters(in: .whitespaces))
        guard !shown.isEmpty else { return "" }
        return unit.isEmpty || unit == "count" ? shown : "\(shown) \(RecipeUnit.label(unit))"
    }

    /// The second line of a row: "2, chopped", "2 Tbsp", "minced".
    static func detail(quantity: String, unit: String, note: String) -> String {
        [amount(quantity: quantity, unit: unit), note.trimmingCharacters(in: .whitespaces)]
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
    }
}

// MARK: - Ingredient name suggestions

/// The names offered while an ingredient is typed: the household's own first, most used first,
/// then the common list. "g" offers Garlic, Ground Beef, Ginger. Pure, so ranking is tested.
nonisolated struct IngredientNameIndex: Equatable, Sendable {
    private struct Entry: Equatable, Sendable {
        let name: String
        let lower: String
        let words: [Substring]
    }

    private let entries: [Entry]

    static let empty = IngredientNameIndex(household: [], common: [])

    init(household: [String], common: [String]) {
        var seen = Set<String>()
        var entries: [Entry] = []
        for name in household + common {
            let trimmed = name.trimmingCharacters(in: .whitespaces)
            let key = Self.key(trimmed)
            guard !key.isEmpty, seen.insert(key).inserted else { continue }
            let lower = trimmed.lowercased()
            entries.append(Entry(name: trimmed, lower: lower, words: lower.split(separator: " ")))
        }
        self.entries = entries
    }

    var isEmpty: Bool { entries.isEmpty }

    /// Up to `limit` names for what's typed. Names that start with it come first, then names
    /// with a word that does ("Fresh Basil" for "bas"). What's typed exactly isn't offered back.
    func suggestions(for typed: String, limit: Int = 6) -> [String] {
        let query = typed.trimmingCharacters(in: .whitespaces).lowercased()
        guard !query.isEmpty else { return [] }
        let typedKey = Self.key(query)
        var starts: [String] = []
        var inside: [String] = []
        for entry in entries where Self.key(entry.lower) != typedKey {
            if entry.lower.hasPrefix(query) {
                starts.append(entry.name)
                if starts.count == limit { break }
            } else if inside.count < limit, entry.words.dropFirst().contains(where: { $0.hasPrefix(query) }) {
                inside.append(entry.name)
            }
        }
        return Array((starts + inside).prefix(limit))
    }

    /// The same ingredient however it's written: case, spacing, and a plural "s" don't count.
    static func key(_ name: String) -> String {
        var words = name.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
        guard var last = words.popLast() else { return "" }
        if last.hasSuffix("oes") || last.hasSuffix("ches") || last.hasSuffix("shes") {
            last.removeLast(2)
        } else if last.hasSuffix("ies"), last.count > 4 {
            last.removeLast(3)
            last += "y"
        } else if last.hasSuffix("s"), !last.hasSuffix("ss"), last.count > 3 {
            last.removeLast()
        }
        words.append(last)
        return words.joined(separator: " ")
    }
}

// MARK: - The New Ingredient sheet

/// One ingredient at a time, like AnyList: Quantity on the amount keypad, then Item with
/// suggestions, then an optional Note. Add Another saves this one and starts the next.
struct IngredientEditorSheet: View {
    enum Mode: Equatable {
        case new
        case edit
    }

    let mode: Mode
    let units: [String]
    let names: IngredientNameIndex
    /// Saves one ingredient. A new one is saved on Add Another and on Done.
    let onSave: (IngredientFields) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var fields: IngredientFields
    @State private var amountFocused = false
    @FocusState private var focus: Field?
    @State private var added = 0
    /// Set after a suggestion is tapped, so the list doesn't open again for the name it filled.
    @State private var picked: String?

    private enum Field: Hashable {
        case item
        case note
    }

    init(
        mode: Mode, initial: IngredientFields = IngredientFields(), units: [String], names: IngredientNameIndex,
        onSave: @escaping (IngredientFields) -> Void
    ) {
        self.mode = mode
        self.units = units
        self.names = names
        self.onSave = onSave
        _fields = State(initialValue: initial)
    }

    private var suggestions: [String] {
        guard focus == .item, picked != fields.name else { return [] }
        return names.suggestions(for: fields.name)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: DS.Space.sm) {
                        AmountKeypadField(
                            text: $fields.quantity, unit: $fields.unit, units: units,
                            isFocused: $amountFocused,
                            next: {
                                focus = .item
                                return true
                            }
                        )
                        .frame(minHeight: DS.Size.tapTarget)
                        .accessibilityLabel("Quantity")
                        UnitMenu(unit: $fields.unit, units: units)
                    }
                } header: {
                    Text("Quantity")
                }
                Section {
                    TextField("Item", text: $fields.name)
                        .font(.body.weight(.semibold))
                        .textInputAutocapitalization(.words)
                        .autocorrectionDisabled()
                        .submitLabel(mode == .new ? .next : .done)
                        .focused($focus, equals: .item)
                        .frame(minHeight: DS.Size.tapTarget)
                        .onSubmit { mode == .new ? addAnother() : done() }
                    ForEach(suggestions, id: \.self) { name in
                        Button {
                            picked = name
                            fields.name = name
                            focus = .note
                        } label: {
                            Label(name, systemImage: "magnifyingglass")
                                .foregroundStyle(.primary)
                                .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                                .contentShape(.rect)
                        }
                        .accessibilityHint("Fills in the item")
                    }
                } header: {
                    Text("Item")
                }
                Section {
                    TextField("Minced, diced, softened", text: $fields.note)
                        .focused($focus, equals: .note)
                        .submitLabel(mode == .new ? .next : .done)
                        .frame(minHeight: DS.Size.tapTarget)
                        .onSubmit { mode == .new ? addAnother() : done() }
                } header: {
                    Text("Note")
                } footer: {
                    if added > 0 {
                        Text(added == 1 ? "1 ingredient added." : "\(added) ingredients added.")
                    }
                }
            }
            .keyboardDismissal()
            .navigationTitle(mode == .new ? "New Ingredient" : "Edit Ingredient")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    if mode == .new {
                        Button("Add Another") { addAnother() }
                            .disabled(fields.isBlank)
                    } else {
                        Button("Cancel") { dismiss() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", systemImage: "checkmark") { done() }
                        .disabled(mode == .edit && fields.isBlank)
                }
            }
            .onAppear {
                // A new ingredient starts on its amount, an existing one on its name.
                if mode == .new { amountFocused = true } else { focus = .item }
            }
        }
    }

    /// Saves this one and clears the sheet for the next, back on Quantity.
    private func addAnother() {
        guard !fields.isBlank else { return }
        onSave(trimmed)
        added += 1
        fields = IngredientFields(unit: "count")
        picked = nil
        focus = nil
        DispatchQueue.main.async { amountFocused = true }
    }

    private func done() {
        if !fields.isBlank { onSave(trimmed) }
        dismiss()
    }

    private var trimmed: IngredientFields {
        var out = fields
        out.name = fields.name.trimmingCharacters(in: .whitespaces)
        out.note = fields.note.trimmingCharacters(in: .whitespaces)
        return out
    }
}

// MARK: - Rows

/// An ingredient or step in the list: tap to edit, swipe to delete, and a menu to move it.
struct RecipeListRow: View {
    let title: String
    let detail: String
    var caption: String? = nil
    var placeholder: LocalizedStringKey = "No name"
    let canMoveUp: Bool
    let canMoveDown: Bool
    let onEdit: () -> Void
    let onMove: (Int) -> Void
    let onDelete: () -> Void

    var body: some View {
        HStack(spacing: DS.Space.s) {
            Button(action: onEdit) {
                VStack(alignment: .leading, spacing: DS.Space.hairline) {
                    Group {
                        if title.isEmpty {
                            Text(placeholder).foregroundStyle(.secondary)
                        } else {
                            Text(title).foregroundStyle(.primary)
                        }
                    }
                    .font(.body)
                    if !detail.isEmpty {
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    if let caption {
                        Text(caption)
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
                .frame(maxWidth: .infinity, minHeight: DS.Size.tapTarget, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Edits it")
            Menu {
                Button("Edit", systemImage: "pencil", action: onEdit)
                Button("Move Up", systemImage: "arrow.up") { onMove(-1) }
                    .disabled(!canMoveUp)
                Button("Move Down", systemImage: "arrow.down") { onMove(1) }
                    .disabled(!canMoveDown)
                Button("Delete", systemImage: "trash", role: .destructive, action: onDelete)
            } label: {
                Image(systemName: "ellipsis")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("More for \(title.isEmpty ? String(localized: "this item") : title)")
        }
        .swipeActions(edge: .trailing) {
            Button("Delete", systemImage: "trash", role: .destructive, action: onDelete)
        }
    }
}

// MARK: - Reorder & Delete

/// An item in the Reorder & Delete list. `id` is its place in the list it came from.
struct ReorderItem: Identifiable, Equatable {
    let id: Int
    let title: String
    let detail: String
}

/// Reorder & Delete: drag handles to move, checkboxes to pick several and delete them at once,
/// with Undo. Done hands back the original places in their new order.
struct ReorderDeleteSheet: View {
    let title: LocalizedStringKey
    let onDone: ([Int]) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var items: [ReorderItem]
    @State private var selection = Set<Int>()
    @State private var undo: [ReorderItem]?

    init(title: LocalizedStringKey, items: [ReorderItem], onDone: @escaping ([Int]) -> Void) {
        self.title = title
        self.onDone = onDone
        _items = State(initialValue: items)
    }

    var body: some View {
        NavigationStack {
            List(selection: $selection) {
                ForEach(items) { item in
                    VStack(alignment: .leading, spacing: DS.Space.hairline) {
                        Text(item.title.isEmpty ? String(localized: "No name") : item.title)
                        if !item.detail.isEmpty {
                            Text(item.detail)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .lineLimit(2)
                    .tag(item.id)
                }
                .onMove { items.move(fromOffsets: $0, toOffset: $1) }
            }
            .environment(\.editMode, .constant(.active))
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        onDone(items.map(\.id))
                        dismiss()
                    }
                    .fontWeight(.semibold)
                }
                ToolbarItemGroup(placement: .bottomBar) {
                    if let undo {
                        Button("Undo") {
                            items = undo
                            self.undo = nil
                        }
                    }
                    Spacer()
                    Button(role: .destructive) {
                        undo = items
                        items.removeAll { selection.contains($0.id) }
                        selection = []
                    } label: {
                        Text(selection.isEmpty ? "Delete" : "Delete \(selection.count)")
                    }
                    .disabled(selection.isEmpty)
                }
            }
        }
    }
}

extension Array {
    /// The items kept by Reorder & Delete, in their new order.
    func reordered(by places: [Int]) -> [Element] {
        places.compactMap { indices.contains($0) ? self[$0] : nil }
    }

    /// Moves the item at `index` by `offset` places, staying in the list.
    mutating func move(at index: Int, by offset: Int) {
        let target = index + offset
        guard indices.contains(index), indices.contains(target) else { return }
        swapAt(index, target)
    }
}
