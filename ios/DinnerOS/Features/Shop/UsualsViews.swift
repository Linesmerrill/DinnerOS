import SwiftUI

/// Add Item: puts something no recipe asks for on the week's list ("Milk", "Paper towels"). It
/// stays open so several can go in one after another; the household's usuals not on the list
/// yet are a tap away.
struct AddItemSheet: View {
    let week: ISOWeek
    let householdID: String

    @Environment(UsualsStore.self) private var usuals
    @Environment(\.dismiss) private var dismiss

    @State private var name = ""
    @State private var count = 1
    @State private var isAdding = false
    @State private var status: String?
    @State private var errorMessage: String?
    @FocusState private var nameFocused: Bool

    private var cleanName: String { NewGroceryItem(name: name).cleanName }

    private var quickAdds: [UsualItem] {
        usuals.usuals(for: week, householdID: householdID)?.offered ?? []
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Item", text: $name, prompt: Text("Milk"))
                        .textInputAutocapitalization(.words)
                        .submitLabel(.done)
                        .focused($nameFocused)
                        .onSubmit { add(NewGroceryItem(name: name, count: count)) }
                        .onChange(of: name) { _, newName in
                            if newName.count > NewGroceryItem.maxNameLength {
                                name = String(newName.prefix(NewGroceryItem.maxNameLength))
                            }
                        }
                    Stepper(value: $count, in: 1...NewGroceryItem.maxCount) {
                        LabeledContent("How Many") {
                            Text(count, format: .number)
                                .monospacedDigit()
                        }
                    }
                } footer: {
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    } else if let status {
                        Text(status)
                    }
                }
                if !quickAdds.isEmpty {
                    Section("Your Usuals") {
                        ForEach(quickAdds) { usual in
                            Button {
                                add(NewGroceryItem(name: usual.name, count: usual.count))
                            } label: {
                                HStack {
                                    Text(UsualsText.nameAndCount(usual.name, count: usual.count))
                                        .foregroundStyle(Color.primary)
                                    Spacer(minLength: 8)
                                    Image(systemName: "plus.circle")
                                        .foregroundStyle(.tint)
                                        .accessibilityHidden(true)
                                }
                                .contentShape(.rect)
                            }
                            .accessibilityHint("Adds it to this week's list.")
                            .disabled(isAdding)
                        }
                    }
                }
            }
            .navigationTitle("Add Item")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { add(NewGroceryItem(name: name, count: count)) }
                        .disabled(cleanName.isEmpty || isAdding)
                }
            }
            .onAppear { nameFocused = true }
        }
    }

    private func add(_ item: NewGroceryItem) {
        let item = NewGroceryItem(name: item.cleanName, count: item.count)
        guard !item.name.isEmpty, !isAdding else { return }
        isAdding = true
        errorMessage = nil
        Task {
            defer { isAdding = false }
            do {
                switch try await usuals.add([item], week: week, householdID: householdID) {
                case .added:
                    status = String(localized: "Added \(UsualsText.nameAndCount(item.name, count: item.count)).")
                case .queued:
                    status = String(localized: "Saved. It goes on the list when you're back online.")
                }
                if item.name == cleanName {
                    name = ""
                    count = 1
                }
                nameFocused = true
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// "Add your usuals?": the household's usuals not on the week's list, every one checked.
struct UsualsPromptSheet: View {
    let request: UsualsStore.PromptRequest

    @Environment(UsualsStore.self) private var usuals
    @Environment(\.dismiss) private var dismiss

    @State private var unchecked: Set<String> = []
    @State private var isAdding = false
    @State private var errorMessage: String?

    private var weekUsuals: WeekUsuals? {
        usuals.usuals(for: request.week, householdID: request.householdID)
    }

    private var offered: [UsualItem] { weekUsuals?.offered ?? [] }
    private var chosen: [UsualItem] { offered.filter { !unchecked.contains($0.id) } }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(offered) { usual in
                        Button {
                            if unchecked.contains(usual.id) {
                                unchecked.remove(usual.id)
                            } else {
                                unchecked.insert(usual.id)
                            }
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: unchecked.contains(usual.id) ? "circle" : "checkmark.circle.fill")
                                    .font(.title3)
                                    .foregroundStyle(unchecked.contains(usual.id) ? Color.secondary : Color.accentColor)
                                    .accessibilityHidden(true)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(UsualsText.nameAndCount(usual.name, count: usual.count))
                                        .foregroundStyle(Color.primary)
                                    Text(UsualsText.why(usual, window: weekUsuals?.countedWeeks ?? 6))
                                        .font(.footnote)
                                        .foregroundStyle(Color.secondary)
                                }
                                Spacer(minLength: 0)
                            }
                            .contentShape(.rect)
                        }
                        .accessibilityAddTraits(unchecked.contains(usual.id) ? [] : .isSelected)
                    }
                } footer: {
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    } else {
                        Text("Uncheck anything you don't need this week.")
                    }
                }
            }
            .navigationTitle("Add Your Usuals?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not Now") {
                        Task { await usuals.dismiss(week: request.week, householdID: request.householdID) }
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(chosen.count == 1 ? "Add 1" : "Add \(chosen.count)", action: add)
                        .disabled(chosen.isEmpty || isAdding)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func add() {
        isAdding = true
        errorMessage = nil
        Task {
            defer { isAdding = false }
            do {
                try await usuals.add(
                    chosen.map { NewGroceryItem(name: $0.name, count: $0.count) }, week: request.week,
                    householdID: request.householdID, fromUsuals: true)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

extension View {
    /// Presents "Add your usuals?" when `origin` asked for it.
    func usualsPrompt(_ usuals: UsualsStore?, origin: UsualsStore.PromptRequest.Origin) -> some View {
        sheet(
            item: Binding(
                get: { usuals?.prompt?.origin == origin ? usuals?.prompt : nil },
                set: { if $0 == nil { usuals?.promptClosed() } })
        ) { request in
            UsualsPromptSheet(request: request)
        }
    }
}

/// Shop ⋯ → Your Usuals: what DinnerOS offers each week, with pins and "not a usual".
struct YourUsualsView: View {
    let week: ISOWeek
    let householdID: String
    let canEdit: Bool

    @Environment(UsualsStore.self) private var usuals

    @State private var newPin = ""
    @State private var working: Set<String> = []
    @State private var errorMessage: String?

    private var weekUsuals: WeekUsuals? { usuals.usuals(for: week, householdID: householdID) }

    var body: some View {
        List {
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
            }
            if let weekUsuals {
                Section {
                    if weekUsuals.items.isEmpty {
                        Text("Nothing yet. Items you add to your list most weeks show up here.")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(weekUsuals.items) { usual in
                        row(usual, window: weekUsuals.countedWeeks)
                    }
                } footer: {
                    Text(
                        "Items on your list \(weekUsuals.minWeeks) of the last \(weekUsuals.window) weeks, plus ones you pin."
                    )
                }
                if canEdit {
                    Section("Pin an Item") {
                        HStack {
                            TextField("Item", text: $newPin, prompt: Text("Coffee"))
                                .textInputAutocapitalization(.words)
                                .submitLabel(.done)
                                .onSubmit(pinNew)
                            Button("Pin", action: pinNew)
                                .disabled(NewGroceryItem(name: newPin).cleanName.isEmpty || working.contains("new"))
                        }
                    }
                }
                if !weekUsuals.notUsual.isEmpty {
                    Section {
                        ForEach(weekUsuals.notUsual) { pref in
                            HStack {
                                Text(pref.name)
                                Spacer(minLength: 8)
                                if canEdit {
                                    Button("Put Back") {
                                        change(
                                            pref.ingredientKey,
                                            SetUsualRequest(ingredientKey: pref.ingredientKey, name: pref.name))
                                    }
                                    .buttonStyle(.borderless)
                                    .disabled(working.contains(pref.ingredientKey))
                                }
                            }
                        }
                    } header: {
                        Text("Not Usuals")
                    } footer: {
                        Text("These aren't offered, however often you add them.")
                    }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
            }
        }
        .navigationTitle("Your Usuals")
        .navigationBarTitleDisplayMode(.inline)
        .task { await usuals.load(week: week, householdID: householdID) }
        .refreshable { await usuals.load(week: week, householdID: householdID) }
    }

    private func row(_ usual: UsualItem, window: Int) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(UsualsText.nameAndCount(usual.name, count: usual.count))
                Text(UsualsText.why(usual, window: window))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if usual.onList {
                Text("On List")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if usual.pinned {
                Image(systemName: "pin.fill")
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
            }
        }
        .accessibilityElement(children: .combine)
        .swipeActions(edge: .trailing) {
            if canEdit {
                Button("Not a Usual", systemImage: "nosign") {
                    change(
                        usual.id, SetUsualRequest(ingredientKey: usual.ingredientKey, name: usual.name, notUsual: true))
                }
                .tint(.orange)
            }
        }
        .swipeActions(edge: .leading) {
            if canEdit {
                if usual.pinned {
                    Button("Unpin", systemImage: "pin.slash") {
                        change(usual.id, SetUsualRequest(ingredientKey: usual.ingredientKey, name: usual.name))
                    }
                } else {
                    Button("Pin", systemImage: "pin") {
                        change(
                            usual.id,
                            SetUsualRequest(
                                ingredientKey: usual.ingredientKey, name: usual.name, count: usual.count, pinned: true))
                    }
                    .tint(.accentColor)
                }
            }
        }
        .contextMenu {
            if canEdit {
                if usual.pinned {
                    Button("Unpin", systemImage: "pin.slash") {
                        change(usual.id, SetUsualRequest(ingredientKey: usual.ingredientKey, name: usual.name))
                    }
                } else {
                    Button("Pin", systemImage: "pin") {
                        change(
                            usual.id,
                            SetUsualRequest(
                                ingredientKey: usual.ingredientKey, name: usual.name, count: usual.count, pinned: true))
                    }
                }
                Button("Not a Usual", systemImage: "nosign") {
                    change(
                        usual.id, SetUsualRequest(ingredientKey: usual.ingredientKey, name: usual.name, notUsual: true))
                }
            }
        }
    }

    private func pinNew() {
        let name = NewGroceryItem(name: newPin).cleanName
        guard !name.isEmpty else { return }
        change("new", SetUsualRequest(name: name, pinned: true)) { newPin = "" }
    }

    private func change(_ id: String, _ request: SetUsualRequest, then: @escaping () -> Void = {}) {
        guard !working.contains(id) else { return }
        working.insert(id)
        errorMessage = nil
        Task {
            defer { working.remove(id) }
            do {
                try await usuals.setUsual(request, householdID: householdID)
                then()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}
