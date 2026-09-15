import SwiftUI

/// A week's grocery list, by aisle, with local check-off and plain-text sharing.
struct GroceryListView: View {
    let week: ISOWeek

    @Environment(PlanStore.self) private var plans
    @Environment(PantryStore.self) private var pantry
    @Environment(HouseholdStore.self) private var households
    @State private var model: GroceryListModel?

    /// Hiding the "Add to pantry?" prompt is a convenience; the API enforces `pantry.edit`.
    private var canEditPantry: Bool {
        households.access?.can(.pantryEdit) == true
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
        .task {
            if model == nil {
                model = plans.makeGroceryList(week: week, purchases: pantry, canAddToPantry: canEditPantry)
            }
            await model?.load()
        }
        .onChange(of: canEditPantry) { _, canEdit in
            model?.setCanAddToPantry(canEdit)
        }
        .onChange(of: model?.purchaseFailure?.isForbidden == true) { _, isForbidden in
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

    /// Items whose contributing recipes are shown.
    @State private var expanded: Set<String> = []

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
        List {
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
            ForEach(list.categories) { category in
                Section(category.title) {
                    ForEach(category.items) { item in
                        GroceryItemRow(
                            item: item,
                            isChecked: model.isChecked(item),
                            isExpanded: expandedBinding(for: item.ingredientKey),
                            toggle: {
                                model.toggle(item)
                                events.groceryItemChecked(item, checked: model.isChecked(item), week: model.week)
                            })
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
    let toggle: () -> Void

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
                            statusLabel
                        }
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

    @ViewBuilder
    private var statusLabel: some View {
        switch item.status {
        case .pantryHint:
            Text("Pantry staple, check before buying")
                .font(.caption)
                .foregroundStyle(.secondary)
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
        switch item.status {
        case .pantryHint: parts.append(String(localized: "Pantry staple"))
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
}
