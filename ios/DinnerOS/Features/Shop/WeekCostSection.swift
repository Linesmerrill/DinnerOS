import SwiftUI

/// The sheets the cost card opens. `ShopView` owns the selection and presents them, because
/// the card itself is not somewhere a presentation can live (decision 510): it is a `Section`
/// that hides itself between `WeekCostCard.isVisible` values, and every week-cost load changes
/// one.
enum WeekCostSheet: String, Identifiable, CaseIterable {
    case orderTotal, prices, importScreenshots, mealKit

    var id: String { rawValue }
}

/// Whether the Shop tab's cost card is on screen.
///
/// A pure rule so the reason the card can't host its own sheet is testable: `loadWeekCost()`
/// reads the week's handoffs and its cost in two round trips, so on a week with neither loaded
/// yet this answers `false`, then `true` — the card, and anything attached to it, is torn down
/// and built again in between.
nonisolated enum WeekCostCard {
    static func isVisible(isCostAvailable: Bool, hasCostContent: Bool, hasHandoffs: Bool) -> Bool {
        isCostAvailable && (hasCostContent || hasHandoffs)
    }
}

/// The Shop tab's cost card for the shown week: what was spent, used, and stocked, cost per
/// meal against the meal kit, and the ways to add prices. Shown once the week has a handoff or
/// a spend, and only on an API with the cost endpoints.
///
/// It opens sheets through `open` rather than presenting them; see `WeekCostSheet`.
struct WeekCostSection: View {
    let open: (WeekCostSheet) -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    private var isVisible: Bool {
        WeekCostCard.isVisible(
            isCostAvailable: shopping.isCostAvailable, hasCostContent: shopping.weekCost?.hasContent == true,
            hasHandoffs: !shopping.weekHandoffs.isEmpty)
    }

    var body: some View {
        if isVisible {
            Section {
                if let cost = shopping.weekCost {
                    NavigationLink {
                        SavingsView()
                    } label: {
                        WeekCostSummary(cost: cost)
                    }
                    if cost.mealKit == nil {
                        mealKitPrompt
                    }
                }
                if shopping.canEdit {
                    Button(orderTotalTitle, systemImage: "sum") { open(.orderTotal) }
                }
                if shopping.canConfirm, !shopping.priceableLines.isEmpty {
                    Button("Add Prices", systemImage: "tag") { open(.prices) }
                    Button("Import Prices from Order Screenshots…", systemImage: "photo.on.rectangle") {
                        open(.importScreenshots)
                    }
                }
            } header: {
                Text("This Week's Cost")
            }
        }
    }

    private var orderTotalTitle: String {
        guard let total = shopping.weekCost?.orderTotalCents else { return String(localized: "Add Order Total") }
        return String(localized: "Order Total: \(MoneyText.format(total))")
    }

    @ViewBuilder
    private var mealKitPrompt: some View {
        if households.access?.can(.householdUpdate) == true {
            Button("Compare with a Meal Kit", systemImage: "shippingbox") { open(.mealKit) }
                .font(.subheadline)
                .accessibilityHint("Opens household settings to add what you spent on meal kits.")
        } else {
            Text("Ask a household admin to add a meal kit price in Household settings to compare.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }
}

/// What a `WeekCostSheet` shows, built where it is presented rather than inside the card.
struct WeekCostSheetView: View {
    let sheet: WeekCostSheet

    @Environment(ShoppingStore.self) private var shopping

    var body: some View {
        switch sheet {
        case .orderTotal: OrderTotalSheet(currentCents: shopping.weekCost?.orderTotalCents)
        case .prices: LinePricesSheet(lines: shopping.priceableLines)
        case .importScreenshots: OrderImportSheet()
        case .mealKit:
            NavigationStack {
                HouseholdSettingsForm()
            }
        }
    }
}

/// Spent, used, stocked, cost per meal, the meal kit comparison, and the API's honest summary.
struct WeekCostSummary: View {
    let cost: WeekCost

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            row("Spent", cost.spentCents)
            row("Used this week", cost.usedCents)
            row("Stocked for later", cost.stockedCents)
            row("Cost per meal", cost.costPerMealCents)
            if let saved = cost.savedCents, cost.mealKit != nil {
                Text(WeekCostText.comparison(savedCents: saved))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(saved >= 0 ? AnyShapeStyle(Color.green) : AnyShapeStyle(Color.orange))
            }
            if !cost.summary.isEmpty {
                Text(cost.summary)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private func row(_ title: LocalizedStringKey, _ cents: Int?) -> some View {
        LabeledContent(title, value: WeekCostText.amount(cents))
            .monospacedDigit()
    }
}

/// The week's order total, fees, tax, and tip included.
struct OrderTotalSheet: View {
    let currentCents: Int?

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var text: String
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(currentCents: Int?) {
        self.currentCents = currentCents
        _text = State(initialValue: currentCents.map(MoneyText.editingText) ?? "")
    }

    private var change: FieldChange<Int>? {
        FieldChange.price(text: text, original: currentCents)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Order total (with fees, tax, and tip)", text: $text, prompt: Text("For example, 130.42"))
                        .keyboardType(.decimalPad)
                    if let error = MoneyText.error(text) {
                        FormErrorLabel(message: error)
                    }
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    }
                } header: {
                    Text("Order total (with fees, tax, and tip)")
                } footer: {
                    Text("What the whole order cost. Fees, tax, and tip count toward your cost per meal.")
                }
                if currentCents != nil {
                    Section {
                        Button("Clear Order Total", role: .destructive) { save(.clear) }
                    }
                }
            }
            .navigationTitle("Order Total")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        if let change { save(change) }
                    }
                    .disabled(change == nil || change == .keep || isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private func save(_ change: FieldChange<Int>) {
        let cents: Int?
        switch change {
        case .keep:
            dismiss()
            return
        case .clear: cents = nil
        case .set(let value): cents = value
        }
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                try await shopping.setOrderTotal(cents)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// Prices for the week's handed-off lines, typed by hand, before or after confirming.
struct LinePricesSheet: View {
    let lines: [PriceableLine]

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var texts: [PriceableLine.ID: String]
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(lines: [PriceableLine]) {
        self.lines = lines
        _texts = State(
            initialValue: Dictionary(
                lines.map { ($0.id, $0.line.priceCents.map(MoneyText.editingText) ?? "") },
                uniquingKeysWith: { first, _ in first }))
    }

    /// Only the prices that changed; `nil` while any text isn't a price.
    private var changes: [PriceableLine.ID: Int?]? {
        var result: [PriceableLine.ID: Int?] = [:]
        for priced in lines {
            guard let change = FieldChange.price(text: texts[priced.id] ?? "", original: priced.line.priceCents)
            else { return nil }
            switch change {
            case .keep: continue
            case .clear: result[priced.id] = .some(nil)
            case .set(let cents): result[priced.id] = .some(cents)
            }
        }
        return result
    }

    var body: some View {
        NavigationStack {
            List {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    ForEach(lines) { priced in
                        LinePriceRow(line: priced.line, text: binding(for: priced.id))
                    }
                } footer: {
                    Text(
                        "What you paid for each item, all packages together. Saved products remember the price per package."
                    )
                }
            }
            .navigationTitle("Add Prices")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save", action: save)
                        .disabled(changes?.isEmpty != false || isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private func binding(for id: PriceableLine.ID) -> Binding<String> {
        Binding(get: { texts[id] ?? "" }, set: { texts[id] = $0 })
    }

    private func save() {
        guard let changes, !changes.isEmpty else { return }
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                try await shopping.savePrices(changes)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

private struct LinePriceRow: View {
    let line: ShoppingHandoffLine
    @Binding var text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(line.name)
                    Text(
                        "\(line.product.displayName) · \(ShoppingText.packages(line.confirmation?.packages ?? line.packages))"
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    if let note = line.pantry?.text {
                        Text(note)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 8)
                TextField("Price", text: $text, prompt: Text("Price"))
                    .keyboardType(.decimalPad)
                    .multilineTextAlignment(.trailing)
                    .frame(maxWidth: 100)
                    .accessibilityLabel("Price paid for \(line.name)")
            }
            if let error = MoneyText.error(text) {
                FormErrorLabel(message: error)
                    .font(.footnote)
            }
        }
    }
}
