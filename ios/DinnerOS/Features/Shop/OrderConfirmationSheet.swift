import SwiftUI

/// "Did you order these?": records the handed-off lines a member ordered as pantry purchases.
/// Walmart doesn't report orders, so the member says what was bought.
///
/// Grouped by meal (`OrderConfirmationGroups`), so each item reads as the thing it's for. A
/// meal whose items are all checked folds down to one row.
struct OrderConfirmationSheet: View {
    let handoff: ShoppingHandoff

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    /// Optional so previews and other hosts needn't supply one; without it meals have no photo.
    @Environment(PlanStore.self) private var plans: PlanStore?
    @Environment(\.appConfiguration) private var configuration

    @State private var draft: OrderConfirmationDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    /// Complete meals the member opened again.
    @State private var expanded: Set<String> = []

    init(handoff: ShoppingHandoff) {
        self.handoff = handoff
        _draft = State(initialValue: OrderConfirmationDraft(handoff: handoff))
    }

    private var groups: OrderConfirmationGroups {
        let plan = plans?.plan
        return OrderConfirmationGroups(
            lines: draft.lines, entries: plan?.week == handoff.week ? plan?.entries : nil,
            weekStartsOn: plans?.weekStartsOn ?? PlanDay.defaultWeekStart)
    }

    var body: some View {
        let groups = groups
        NavigationStack {
            List {
                Section {
                    Text(
                        "Walmart doesn't tell \(configuration.displayName) what you ordered. Uncheck anything you didn't buy, and the rest goes in your pantry. Prices are optional; add them now or later from Shop."
                    )
                    .foregroundStyle(.secondary)
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                if !groups.shared.isEmpty {
                    Section {
                        ForEach(groups.shared) { shared in
                            row(shared.line, detail: String(localized: "For \(shared.mealNames.formatted())"))
                        }
                    } header: {
                        Text("For Several Meals")
                    }
                }
                ForEach(groups.meals) { meal in
                    mealSection(meal)
                }
                if !groups.extras.isEmpty {
                    Section {
                        ForEach(groups.extras) { line in
                            row(line, detail: nil)
                        }
                    } header: {
                        Text("Extras")
                    }
                }
            }
            .navigationTitle("Did You Order These?")
            .navigationBarTitleDisplayMode(.inline)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                actions
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    @ViewBuilder
    private func mealSection(_ meal: OrderConfirmationGroups.Meal) -> some View {
        let checked = meal.lines.filter { draft.isSelected($0) }.count
        let isComplete = checked == meal.lines.count
        let isOpen = !isComplete || expanded.contains(meal.id)
        Section {
            Button {
                guard isComplete else { return }
                if expanded.contains(meal.id) {
                    expanded.remove(meal.id)
                } else {
                    expanded.insert(meal.id)
                }
            } label: {
                MealHeader(meal: meal, checked: checked, isComplete: isComplete, isOpen: isOpen)
            }
            .buttonStyle(.plain)
            .accessibilityHint(isComplete ? (isOpen ? "Hides its items" : "Shows its items") : "")
            if isOpen {
                ForEach(meal.lines) { line in
                    row(line, detail: nil)
                }
            }
        }
    }

    private func row(_ line: ShoppingHandoffLine, detail: String?) -> some View {
        OrderLineRow(
            line: line, detail: detail, isSelected: draft.isSelected(line),
            packages: Binding(
                get: { draft.packages(for: line) },
                set: { draft.setPackages($0, for: line) }),
            priceText: Binding(
                get: { draft.priceText(for: line) },
                set: { draft.setPriceText($0, for: line) }),
            priceError: draft.priceError(for: line),
            toggle: { draft.toggle(line) })
    }

    private var actions: some View {
        VStack(spacing: 4) {
            Button {
                if let request = draft.confirmRequest {
                    confirm(request)
                }
            } label: {
                Group {
                    if isSaving {
                        ProgressView()
                    } else {
                        Text(draft.confirmTitle)
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(draft.confirmRequest == nil)
            .accessibilityHint(
                draft.isAllSelected
                    ? "Adds everything to the pantry."
                    : "Adds the checked items to the pantry. The rest weren't ordered.")

            Button("Not Yet") {
                shopping.dismissConfirmation()
            }
            .controlSize(.large)
            .disabled(isSaving)
        }
        .padding(.horizontal)
        .padding(.top, 12)
        .padding(.bottom, 4)
        .background(.bar)
    }

    private func confirm(_ request: ConfirmShoppingOrderRequest) {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                // Success clears the store's prompt, which closes this sheet.
                try await shopping.confirm(request, for: handoff)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// A meal's photo, name and day, and how many of its items are checked.
private struct MealHeader: View {
    let meal: OrderConfirmationGroups.Meal
    let checked: Int
    let isComplete: Bool
    let isOpen: Bool

    var body: some View {
        HStack(spacing: 12) {
            RecipePhoto(url: meal.imageURL, aspectRatio: 1, pointWidth: 48, cornerRadius: 10)
                .frame(width: 48, height: 48)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(meal.name)
                    .font(.headline)
                if let day = meal.day {
                    Text(day.name())
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            if isComplete {
                Label("\(checked) of \(meal.lines.count) ordered", systemImage: "checkmark.circle.fill")
                    .labelStyle(.titleAndIcon)
                    .font(.footnote)
                    .foregroundStyle(.tint)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(isOpen ? 90 : 0))
                    .accessibilityHidden(true)
            } else {
                Text("\(checked) of \(meal.lines.count)")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

private struct OrderLineRow: View {
    let line: ShoppingHandoffLine
    /// For example "For Tacos and Pasta".
    let detail: String?
    let isSelected: Bool
    @Binding var packages: Int
    @Binding var priceText: String
    let priceError: String?
    let toggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button(action: toggle) {
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.name)
                        Text(line.product.displayName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        if let detail {
                            Text(detail)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        if line.confirmation?.status == .skipped {
                            Text("Marked not ordered")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 0)
                }
                // Inside a list Button, hierarchical styles resolve against the tint.
                .foregroundStyle(Color.primary)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(isSelected ? .isSelected : [])
            .accessibilityHint(isSelected ? "Leaves it out" : "Marks it ordered")

            if isSelected {
                Stepper(value: $packages, in: ShoppingLimits.packages) {
                    Text(ShoppingText.packages(packages))
                        .font(.subheadline)
                }
                .padding(.leading, 36)
                .accessibilityLabel("Packages of \(line.name) ordered")
                .accessibilityValue(ShoppingText.packages(packages))
                LabeledContent("Price") {
                    TextField("Price", text: $priceText, prompt: Text("Optional"))
                        .keyboardType(.decimalPad)
                        .multilineTextAlignment(.trailing)
                        .frame(maxWidth: 120)
                }
                .font(.subheadline)
                .padding(.leading, 36)
                .accessibilityLabel("Price paid for \(line.name)")
                if let priceError {
                    FormErrorLabel(message: priceError)
                        .font(.footnote)
                        .padding(.leading, 36)
                }
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    OrderConfirmationSheet(handoff: ShopPreviewData.handoff)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}
