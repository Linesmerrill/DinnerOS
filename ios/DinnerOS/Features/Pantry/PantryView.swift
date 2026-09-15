import SwiftUI

/// The Pantry tab: what the household has at home, grouped by aisle, searchable, and quick
/// to update after cooking or shopping.
///
/// Editing is hidden without `pantry.edit`. That's a convenience only: the API checks every
/// change and answers `403` if a hidden rule applies.
struct PantryView: View {
    @Environment(PantryStore.self) private var pantry
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    @State private var searchText = ""
    @State private var statusFilter: PantryStatusFilter = .all
    @State private var editMode: EditMode = .inactive
    @State private var selection: Set<String> = []
    @State private var sheet: Sheet?
    @State private var isWorking = false
    @State private var actionError: String?
    @State private var bulkOutcome: PantryBulkOutcome?

    private enum Sheet: Identifiable {
        case add
        case edit(PantryItem)

        var id: String {
            switch self {
            case .add: "add"
            case .edit(let item): "edit-\(item.id)"
            }
        }
    }

    private var householdID: String? {
        households.current?.household.id
    }

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    private var isSelecting: Bool {
        editMode.isEditing
    }

    private var sections: [PantrySection] {
        PantryList.sections(pantry.items, search: searchText, status: statusFilter)
    }

    var body: some View {
        content
            .navigationTitle("Pantry")
            .toolbar { toolbar }
            .environment(\.editMode, $editMode)
            .task(id: householdID) {
                endSelection()
                guard let householdID else { return }
                await pantry.activate(householdID: householdID)
            }
            .onChange(of: canEdit) { _, canEdit in
                if !canEdit {
                    endSelection()
                    sheet = nil
                }
            }
            .sheet(item: $sheet) { sheet in
                switch sheet {
                case .add: PantryAddSheet()
                case .edit(let item): PantryEditSheet(item: item)
                }
            }
            .alert(
                "Couldn't Update the Pantry",
                isPresented: Binding(presenting: $actionError),
                presenting: actionError
            ) { _ in
                Button("OK") {}
            } message: { message in
                Text(message)
            }
            .alert(
                "Some Items Were Already Gone",
                isPresented: Binding(presenting: $bulkOutcome),
                presenting: bulkOutcome
            ) { _ in
                Button("OK") {}
            } message: { outcome in
                Text(outcome.summary)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch pantry.phase {
        case .idle, .loading:
            ProgressView("Loading pantry…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load the Pantry", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await pantry.retry() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        let visibleSections = sections
        return List(selection: $selection) {
            if let refreshError = pantry.refreshError {
                FormErrorLabel(message: refreshError)
            }
            ForEach(visibleSections) { section in
                Section(section.title) {
                    ForEach(section.items) { item in
                        row(for: item)
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .searchable(text: $searchText, prompt: "Search pantry")
        .safeAreaInset(edge: .top) {
            if !pantry.items.isEmpty {
                statusPicker
            }
        }
        .overlay {
            if visibleSections.isEmpty {
                emptyState
            }
        }
        .refreshable {
            await pantry.refresh()
        }
        .disabled(isWorking)
        .overlay {
            if isWorking {
                ProgressView()
                    .controlSize(.large)
            }
        }
    }

    private var statusPicker: some View {
        Picker("Show", selection: $statusFilter) {
            ForEach(PantryStatusFilter.allCases) { filter in
                Text(filter.title).tag(filter)
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }

    @ViewBuilder
    private func row(for item: PantryItem) -> some View {
        if canEdit && !isSelecting {
            Button {
                sheet = .edit(item)
            } label: {
                PantryItemRow(item: item)
            }
            .accessibilityHint("Edit this item.")
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                if item.status != .out {
                    Button("Out", systemImage: "xmark.circle") { mark(item, .out) }
                        .tint(.red)
                }
                if item.status != .low {
                    Button("Low", systemImage: "battery.25percent") { mark(item, .low) }
                        .tint(.orange)
                }
            }
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                if item.status != .inStock {
                    Button("In Stock", systemImage: "checkmark.circle") { mark(item, .inStock) }
                        .tint(.green)
                }
            }
            .tag(item.id)
        } else {
            PantryItemRow(item: item)
                .tag(item.id)
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let search = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        if pantry.items.isEmpty {
            ContentUnavailableView {
                Label("Your Pantry Is Empty", systemImage: "cabinet")
            } description: {
                if canEdit {
                    Text("Add what you keep at home so grocery lists know what you already have.")
                } else {
                    Text("No one in this household has added pantry items yet.")
                }
            } actions: {
                if canEdit {
                    Button("Add Common Staples") { addStaples() }
                        .buttonStyle(.borderedProminent)
                    Button("Add an Item") { sheet = .add }
                }
            }
        } else if !search.isEmpty {
            ContentUnavailableView.search(text: search)
        } else {
            ContentUnavailableView {
                if statusFilter == .low {
                    Label("Nothing Is Running Low", systemImage: "checkmark.circle")
                } else {
                    Label("Nothing Is Out", systemImage: "checkmark.circle")
                }
            } actions: {
                Button("Show All Items") { statusFilter = .all }
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if canEdit && pantry.phase == .loaded {
            if isSelecting {
                ToolbarItem(placement: .topBarLeading) {
                    Button(allVisibleSelected ? "Deselect All" : "Select All") { toggleSelectAll() }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { endSelection() }
                }
                ToolbarItemGroup(placement: .bottomBar) {
                    bulkButton(.inStock)
                    Spacer()
                    bulkButton(.low)
                    Spacer()
                    bulkButton(.out)
                }
            } else {
                if !pantry.items.isEmpty {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("Select") {
                            withAnimation { editMode = .active }
                        }
                        .accessibilityHint("Select several items to change their status together.")
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Add Item", systemImage: "plus") { sheet = .add }
                }
            }
        }
    }

    private func bulkButton(_ status: PantryStatus) -> some View {
        Button(status.title) { applyToSelection(status) }
            .disabled(selection.isEmpty || isWorking)
            .accessibilityLabel("Mark Selected Items \(status.title)")
    }

    // MARK: - Selection

    private var visibleIDs: [String] {
        sections.flatMap { $0.items.map(\.id) }
    }

    private var allVisibleSelected: Bool {
        let visible = visibleIDs
        return !visible.isEmpty && visible.allSatisfy(selection.contains)
    }

    private func toggleSelectAll() {
        selection = allVisibleSelected ? [] : Set(visibleIDs)
    }

    private func endSelection() {
        selection = []
        withAnimation { editMode = .inactive }
    }

    // MARK: - Actions

    private func mark(_ item: PantryItem, _ status: PantryStatus) {
        perform { try await pantry.setStatus(of: item, to: status) }
    }

    private func applyToSelection(_ status: PantryStatus) {
        let ids = pantry.items.map(\.id).filter(selection.contains)
        perform {
            let outcome = try await pantry.setStatus(ofItemsWithIDs: ids, to: status)
            endSelection()
            if outcome.missingCount > 0 {
                bulkOutcome = outcome
            }
        }
    }

    private func addStaples() {
        perform { try await pantry.addDefaultStaples() }
    }

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            isWorking = true
            defer { isWorking = false }
            do {
                try await action()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                // After a sign-out the root view replaces this screen, so no error is shown.
                guard session.currentUser != nil else { return }
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Sample data for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are
/// type-checked in Release builds too.
enum PantryPreviewData {
    static let items: [PantryItem] = [
        item(id: "p1", name: "Olive Oil", category: "pantry", quantity: "3/2", value: 1.5, unit: "cup", staple: true),
        item(id: "p2", name: "Carrots", category: "produce", status: .low),
        item(id: "p3", name: "Za'atar", category: "spices", status: .out),
        item(
            id: "p4", name: "Greek Yogurt", category: "dairy-eggs", quantity: "2", value: 2, unit: "cup",
            expiresOn: PantryDate.string(from: .now.addingTimeInterval(2 * 86_400), timeZone: .autoupdatingCurrent)),
    ]

    static func store(session: AuthSession) -> PantryStore {
        .preview(session: session, items: items, householdID: HouseholdPreviewData.household.id)
    }

    private static func item(
        id: String, name: String, category: String, quantity: String? = nil, value: Double? = nil,
        unit: String? = nil, status: PantryStatus = .inStock, staple: Bool = false, expiresOn: String? = nil
    ) -> PantryItem {
        PantryItem(
            id: id, householdID: HouseholdPreviewData.household.id, ingredientID: nil, key: name.lowercased(),
            displayName: name, category: category, quantity: quantity, quantityValue: value, unit: unit,
            status: status, isStaple: staple, expiresOn: expiresOn, note: "", updatedBy: "user-ada",
            createdAt: .now, updatedAt: .now)
    }
}

#Preview("Loaded") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        PantryView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(PantryPreviewData.store(session: session))
}

#Preview("Empty") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        PantryView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(PantryStore.preview(session: session))
}
