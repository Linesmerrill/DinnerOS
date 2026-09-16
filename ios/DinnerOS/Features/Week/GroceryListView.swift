import SwiftUI

/// A week's grocery list, by aisle, with local check-off and plain-text sharing. Specialty
/// ingredients add a Choose action to lines without a choice, and a "Make This Week" section
/// for house-made batches with the ingredients bought only for them.
struct GroceryListView: View {
    let week: ISOWeek

    @Environment(PlanStore.self) private var plans
    @Environment(PantryStore.self) private var pantry
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(\.openShop) private var openShop
    @State private var model: GroceryListModel?

    /// Hiding "Add to pantry?" and specialty actions is a convenience; the API enforces `pantry.edit`.
    private var canEditPantry: Bool {
        households.access?.can(.pantryEdit) == true
    }

    /// A change the list tried was refused for the member's role.
    private var wasForbidden: Bool {
        model?.purchaseFailure?.isForbidden == true || model?.specialtyFailure?.isForbidden == true
    }

    var body: some View {
        Group {
            if let model {
                GroceryListContent(model: model)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Grocery List")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let openShop, households.access?.can(.shoppingEdit) == true {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Open in Walmart", systemImage: "cart") { openShop(week) }
                }
            }
        }
        .task {
            if model == nil {
                model = plans.makeGroceryList(
                    week: week, purchases: pantry, specialties: specialties, canAddToPantry: canEditPantry)
            }
            // Lines confirmed as ordered on the Shop tab are checked off while this list was away.
            model?.reloadChecks()
            await model?.load()
        }
        .onChange(of: canEditPantry) { _, canEdit in
            model?.setCanAddToPantry(canEdit)
        }
        .onChange(of: specialties.revision) {
            // The setup screen changed a choice or recorded a batch.
            Task { await model?.specialtiesDidChange() }
        }
        .onChange(of: wasForbidden) { _, isForbidden in
            // The role changed elsewhere; reload it so the rest of the app matches.
            if isForbidden {
                Task { await households.load() }
            }
        }
    }
}

struct GroceryListContent: View {
    let model: GroceryListModel

    @Environment(EventReporter.self) private var events
    @Environment(PairingsStore.self) private var pairings
    @Environment(HouseholdStore.self) private var households

    /// Hiding the action is a convenience; the API enforces `plan.edit`.
    private var canEditPlan: Bool {
        households.access?.can(.planEdit) == true && model.list?.status == .draft
    }

    @State private var removeError: String?

    /// Items whose contributing recipes are shown.
    @State private var expanded: Set<String> = []
    /// The line whose quick picker is open.
    @State private var picker: GrocerySpecialty?
    @State private var showsSpecialtySetup = false
    @State private var confirmingBatch: GroceryBatch?

    var body: some View {
        content
            .toolbar {
                if let text = model.plainText() {
                    ToolbarItem(placement: .topBarTrailing) {
                        ShareLink(
                            item: text,
                            subject: Text("Grocery List"),
                            preview: SharePreview(Text("Grocery List: \(model.week.rangeLabel())"))
                        ) {
                            Label("Share", systemImage: "square.and.arrow.up")
                        }
                    }
                }
                if !model.checked.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu("More", systemImage: "ellipsis.circle") {
                            Button("Uncheck All", systemImage: "circle") {
                                model.uncheckAll()
                            }
                        }
                    }
                }
            }
            .sheet(item: $picker) { specialty in
                SpecialtyQuickPicker(specialty: specialty, model: model)
            }
            .alert("Couldn't Remove That", isPresented: Binding(presenting: $removeError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(removeError ?? "")
            }
            .sheet(isPresented: $showsSpecialtySetup) {
                SpecialtyIngredientsSheet()
            }
            .confirmationDialog(
                confirmingBatch.map { String(localized: "Made \($0.specialtyName)?") } ?? "",
                isPresented: Binding(presenting: $confirmingBatch),
                titleVisibility: .visible,
                presenting: confirmingBatch
            ) { batch in
                Button("Made It") {
                    Task { await model.recordBatch(batch) }
                }
                Button("Cancel", role: .cancel) {}
            } message: { batch in
                Text(
                    "Adds \(SpecialtyFormat.batchCount(max(batch.batches, 1), yield: batch.batchYield)) of \(batch.specialtyName) to the pantry."
                )
            }
            .alert(
                model.specialtyFailure?.title ?? "",
                isPresented: Binding(
                    get: { model.specialtyFailure != nil },
                    set: { isPresented in
                        if !isPresented { model.dismissSpecialtyFailure() }
                    }),
                presenting: model.specialtyFailure
            ) { failure in
                if !failure.isForbidden {
                    Button("Try Again") {
                        Task { await model.retrySpecialtyAction(failure) }
                    }
                }
                Button("OK", role: .cancel) {}
            } message: { failure in
                Text(failure.message)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch model.phase {
        case .idle, .loading:
            ProgressView("Building the list…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Build the List", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await model.load() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            if let list = model.list {
                listView(list)
            }
        }
    }

    private func listView(_ list: GroceryList) -> some View {
        let layout = GroceryListLayout(list)
        return List {
            if let refreshError = model.refreshError {
                FormErrorLabel(message: refreshError)
            }
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text(model.week.rangeLabel())
                        .font(.headline)
                    if !list.isEmpty {
                        Text("\(model.remainingCount) of \(list.allItems.count) left to check off")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                if !list.skipped.isEmpty {
                    SkippedEntriesNotice(skipped: list.skipped)
                }
            } footer: {
                if !list.isEmpty {
                    Text("Checks are saved on this device only.")
                }
            }
            if list.isEmpty {
                ContentUnavailableView(
                    "Nothing to Buy", systemImage: "cart",
                    description: Text("Add recipes to this week to build its grocery list.")
                )
                .listRowBackground(Color.clear)
            }
            if model.canChangeSpecialties && !layout.needsChoice.isEmpty {
                Section {
                    SpecialtyChoiceNotice(count: layout.needsChoice.count) {
                        showsSpecialtySetup = true
                    }
                }
            }
            ForEach(Array(layout.toMake.enumerated()), id: \.element.id) { index, group in
                Section {
                    GroceryBatchRow(
                        batch: group.batch,
                        canMake: model.canChangeSpecialties,
                        isWorking: model.isChangingSpecialty(withID: group.batch.specialtyID)
                    ) {
                        confirmingBatch = group.batch
                    }
                    ForEach(group.ingredients) { item in
                        itemRow(item)
                    }
                } header: {
                    if index == 0 {
                        Text("Make This Week")
                    }
                } footer: {
                    if !group.ingredients.isEmpty {
                        Text("The items above are only for making \(group.batch.specialtyName).")
                    }
                }
            }
            if !layout.alreadyMade.isEmpty {
                Section("Already Made") {
                    ForEach(layout.alreadyMade) { batch in
                        GroceryMadeBatchRow(batch: batch)
                    }
                }
            }
            ForEach(layout.categories) { category in
                Section(category.title) {
                    ForEach(category.items) { item in
                        itemRow(item)
                    }
                }
            }
        }
        .refreshable {
            await model.load()
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            GroceryPurchaseBanner(model: model)
        }
    }

    private func itemRow(_ item: GroceryItem) -> some View {
        GroceryItemRow(
            item: item,
            isChecked: model.isChecked(item),
            isExpanded: expandedBinding(for: item.ingredientKey),
            canChooseSpecialty: model.canChangeSpecialties,
            isChangingSpecialty: item.specialtyDetail.map { model.isChangingSpecialty(withID: $0.id) } ?? false,
            toggle: {
                model.toggle(item)
                events.groceryItemChecked(item, checked: model.isChecked(item), week: model.week)
            },
            choose: {
                picker = item.specialtyDetail
            }
        )
        // A paired grocery item is the household's own add-on, so it can be taken back off
        // the week. Ingredients a recipe needs can't: removing the meal does that.
        .swipeActions(edge: .trailing) {
            ForEach(removableExtras(item)) { extra in
                Button("Remove", systemImage: "trash", role: .destructive) {
                    remove(extra)
                }
            }
        }
        .contextMenu {
            ForEach(removableExtras(item)) { extra in
                Button("Remove from This Week", systemImage: "trash", role: .destructive) {
                    remove(extra)
                }
            }
        }
    }

    /// The pairing extras this member may take off the list.
    private func removableExtras(_ item: GroceryItem) -> [GroceryExtra] {
        guard canEditPlan, !pairings.isForbidden else { return [] }
        return item.pairingExtras
    }

    private func remove(_ extra: GroceryExtra) {
        Task {
            do {
                try await pairings.removeGroceryItem(id: extra.id)
                await model.load()
            } catch is CancellationError {
                return
            } catch {
                removeError = HouseholdStore.message(for: error)
            }
        }
    }

    private func expandedBinding(for key: String) -> Binding<Bool> {
        Binding(
            get: { expanded.contains(key) },
            set: { isExpanded in
                if isExpanded {
                    expanded.insert(key)
                } else {
                    expanded.remove(key)
                }
            })
    }
}

private struct GroceryItemRow: View {
    let item: GroceryItem
    let isChecked: Bool
    @Binding var isExpanded: Bool
    let canChooseSpecialty: Bool
    let isChangingSpecialty: Bool
    let toggle: () -> Void
    let choose: () -> Void

    private var amount: String? {
        GroceryListText.amountText(for: item)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Button(action: toggle) {
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(isChecked ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                            .font(.title3)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.name)
                                .strikethrough(isChecked)
                                .foregroundStyle(isChecked || item.status != .toBuy ? .secondary : .primary)
                            if let amount {
                                Text(amount)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                            ForEach(Array(item.via.enumerated()), id: \.offset) { _, via in
                                Text(via.text)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                            // What put the line on the list besides a recipe, such as an
                            // accepted pairing. Read the same way as `via`.
                            ForEach(item.extras) { extra in
                                Text(extra.text)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                            statusLabel
                        }
                        // Keeps secondary text gray inside the Button.
                        .foregroundStyle(Color.primary)
                        Spacer(minLength: 0)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(accessibilityLabel)
                .accessibilityAddTraits(isChecked ? .isSelected : [])
                .accessibilityHint(isChecked ? "Unchecks the item" : "Checks off the item")

                if !item.recipes.isEmpty {
                    Button {
                        withAnimation { isExpanded.toggle() }
                    } label: {
                        Image(systemName: "chevron.down")
                            .rotationEffect(.degrees(isExpanded ? 180 : 0))
                            .foregroundStyle(.secondary)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(isExpanded ? "Hide Recipes" : "Show Recipes")
                }
            }
            if item.needsSpecialtyChoice, let specialty = item.specialtyDetail {
                specialtyPrompt(specialty)
            }
            if isExpanded {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(item.recipes) { recipe in
                        Label(recipe.name, systemImage: "fork.knife")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.leading, 36)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Used in \(item.recipes.map(\.name).formatted(.list(type: .and)))")
            }
        }
    }

    private func specialtyPrompt(_ specialty: GrocerySpecialty) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(specialty.text, systemImage: "sparkles")
                .font(.footnote)
                .foregroundStyle(.secondary)
            if canChooseSpecialty {
                if isChangingSpecialty {
                    HStack(spacing: 6) {
                        ProgressView()
                        Text("Saving your choice…")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                } else {
                    Button("Choose", action: choose)
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .accessibilityLabel("Choose an Option for \(specialty.name)")
                }
            }
        }
        .padding(.leading, 36)
    }

    @ViewBuilder
    private var statusLabel: some View {
        switch item.status {
        case .pantryHint:
            Text("Pantry staple, check before buying")
                .font(.caption)
                .foregroundStyle(.secondary)
        case .inPantry where item.isHouseMade:
            Label("In pantry (house-made)", systemImage: "house.fill")
                .font(.caption)
                .foregroundStyle(.tint)
        case .inPantry:
            Label("In your pantry", systemImage: "checkmark.seal")
                .font(.caption)
                .foregroundStyle(.tint)
        default:
            EmptyView()
        }
    }

    private var accessibilityLabel: String {
        var parts = [item.name]
        if let amount {
            parts.append(amount)
        }
        parts += item.via.map(\.text)
        parts += item.extras.map(\.text)
        switch item.status {
        case .pantryHint: parts.append(String(localized: "Pantry staple"))
        case .inPantry where item.isHouseMade: parts.append(String(localized: "In pantry, house-made"))
        case .inPantry: parts.append(String(localized: "In your pantry"))
        default: break
        }
        return parts.joined(separator: ", ")
    }
}

private struct SkippedEntriesNotice: View {
    let skipped: [GrocerySkippedEntry]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(
                skipped.count == 1
                    ? String(localized: "1 recipe isn't included")
                    : String(localized: "\(skipped.count) recipes aren't included"),
                systemImage: "exclamationmark.triangle.fill"
            )
            .symbolRenderingMode(.multicolor)
            .font(.subheadline.weight(.semibold))
            ForEach(skipped) { entry in
                Text("\(entry.recipeName): \(entry.reason.explanation)")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        GroceryListContent(
            model: .preview(session: session, list: PlanPreviewData.groceryList, checked: ["garlic"]))
    }
    .environment(EventReporter.preview(session: session))
    .environment(HouseholdPreviewData.store(session: session))
    .environment(SpecialtyPreviewData.store(session: session))
}
