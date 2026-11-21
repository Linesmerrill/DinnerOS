import SwiftUI

/// A recipe's cuisine: "None" until one is picked or guessed from the title, then "Mexican".
/// Tapping the row opens a searchable list that picks one cuisine, or several when asked.
struct RecipeCuisineField: View {
    let values: [String]
    let options: [AutopilotOption]
    let onPick: ([String]) -> Void

    @State private var picking = false

    private var summary: String { RecipeCuisines.summary(values, in: options) }

    var body: some View {
        Button {
            Keyboard.dismiss()
            picking = true
        } label: {
            HStack {
                Text("Cuisine")
                    .foregroundStyle(.primary)
                Spacer()
                Text(summary)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.trailing)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
            .frame(minHeight: DS.Size.tapTarget)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Cuisine")
        .accessibilityValue(summary)
        .accessibilityHint("Opens the list of cuisines")
        .accessibilityIdentifier("RecipeCuisine")
        .dsSheet(isPresented: $picking) {
            RecipeCuisinePicker(values: values, options: RecipeCuisines.choices(options, keeping: values)) {
                onPick($0)
            }
        }
    }
}

/// The cuisines to pick from, with search. One at a time by default: tapping one picks it and
/// closes the list. "Pick More Than One" keeps it open for up to five.
struct RecipeCuisinePicker: View {
    let options: [AutopilotOption]
    let onPick: ([String]) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selection: [String]
    @State private var several: Bool
    @State private var query = ""

    init(values: [String], options: [AutopilotOption], onPick: @escaping ([String]) -> Void) {
        self.options = options
        self.onPick = onPick
        _selection = State(initialValue: values)
        _several = State(initialValue: values.count > 1)
    }

    private var shown: [AutopilotOption] { RecipeCuisines.filter(options, query: query) }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Toggle("Pick More Than One", isOn: $several)
                        .accessibilityIdentifier("CuisinePickSeveral")
                }
                Section {
                    if query.isEmpty {
                        row(label: String(localized: "None"), id: "none", checked: selection.isEmpty) {
                            selection = []
                            finishIfSingle()
                        }
                    }
                    ForEach(shown) { option in
                        row(label: option.label, id: option.value, checked: selection.contains(option.value)) {
                            toggle(option.value)
                        }
                    }
                } footer: {
                    if several {
                        Text("Pick up to \(RecipeCuisines.maxPerRecipe).")
                    }
                }
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search cuisines")
            .overlay {
                if shown.isEmpty, !query.isEmpty {
                    ContentUnavailableView.search(text: query)
                }
            }
            .navigationTitle("Cuisine")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .accessibilityIdentifier("CuisineCancel")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        onPick(selection)
                        dismiss()
                    }
                    .accessibilityIdentifier("CuisineDone")
                }
            }
        }
    }

    private func row(label: String, id: String, checked: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                Text(label)
                    .foregroundStyle(.primary)
                Spacer()
                if checked {
                    Image(systemName: "checkmark")
                        .foregroundStyle(Color.accentColor)
                        .fontWeight(.semibold)
                }
            }
            .frame(minHeight: DS.Size.tapTarget)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(checked ? .isSelected : [])
        .accessibilityIdentifier("CuisineOption-\(id)")
    }

    private func toggle(_ value: String) {
        if !several {
            selection = [value]
            finishIfSingle()
            return
        }
        if let i = selection.firstIndex(of: value) {
            selection.remove(at: i)
        } else if selection.count < RecipeCuisines.maxPerRecipe {
            selection.append(value)
        }
    }

    private func finishIfSingle() {
        guard !several else { return }
        onPick(selection)
        dismiss()
    }
}
