import SwiftUI

/// Opens one pantry item's detail, for example from a notification.
struct PantryItemRoute: Hashable {
    let itemID: String
}

extension PantryEstimateLevel {
    var color: Color {
        switch self {
        case .plenty: .green
        case .low: .orange
        case .empty: .red
        }
    }
}

/// A ring filled to the share of an item estimated to be left.
struct PantryEstimateRing: View {
    let estimate: PantryEstimate
    let diameter: CGFloat

    var body: some View {
        let lineWidth = max(2, diameter * 0.2)
        ZStack {
            Circle()
                .stroke(.quaternary, lineWidth: lineWidth)
            Circle()
                .trim(from: 0, to: CGFloat(min(max(estimate.percentRemaining, 0), 100)) / 100)
                .stroke(
                    PantryUsageFormat.level(estimate).color,
                    style: StrokeStyle(lineWidth: lineWidth, lineCap: .round)
                )
                .rotationEffect(.degrees(-90))
        }
        .frame(width: diameter, height: diameter)
        .accessibilityHidden(true)
    }
}

/// "◔ ~31% left" under a pantry row's name, with a warning glyph when the estimate had to
/// skip a cooked recipe — otherwise an item that couldn't be counted reads exactly like one
/// that was, which is the one thing the percentage must not imply.
struct PantryEstimateLabel: View {
    let estimate: PantryEstimate

    @ScaledMetric(relativeTo: .footnote) private var ringSize = 12.0

    var body: some View {
        HStack(spacing: 5) {
            PantryEstimateRing(estimate: estimate, diameter: ringSize)
            Text(PantryUsageFormat.remainingShort(estimate))
            if PantryUsageFormat.skippedRecipesShort(estimate) != nil {
                Image(systemName: "exclamationmark.circle")
                    .foregroundStyle(.orange)
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenLabel)
    }

    private var spokenLabel: String {
        let remaining = PantryUsageFormat.remainingSpoken(estimate)
        guard let skipped = PantryUsageFormat.skippedRecipesShort(estimate) else { return remaining }
        return "\(remaining), \(skipped)"
    }
}

/// The usage estimate and recent purchases for one item. Shared by the edit sheet and the
/// read-only detail.
struct PantryUsageSections: View {
    let item: PantryItem

    @Environment(PantryStore.self) private var pantry
    @ScaledMetric(relativeTo: .headline) private var ringSize = 34.0

    @State private var purchases: [PantryPurchase] = []
    @State private var purchasesPhase = LoadPhase.loading

    private enum LoadPhase: Equatable {
        case loading
        case loaded
        case failed(String)
    }

    var body: some View {
        estimateSection
        purchasesSection
    }

    @ViewBuilder
    private var estimateSection: some View {
        if let estimate = item.estimate {
            Section {
                HStack(spacing: 14) {
                    PantryEstimateRing(estimate: estimate, diameter: ringSize)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(PantryUsageFormat.remainingSpoken(estimate))
                            .font(.headline)
                        Text(PantryUsageFormat.remainingAmount(estimate))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                Text(estimate.summary)
                    .font(.subheadline)
                LabeledContent("Recipes", value: PantryUsageFormat.recipeUse(estimate))
                LabeledContent {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(PantryUsageFormat.otherUse(estimate))
                        Text(PantryUsageFormat.dailyRate(estimate))
                            .font(.caption)
                    }
                } label: {
                    Text("Other Use")
                }
                LabeledContent {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(PantryUsageFormat.threshold(estimate.lowThresholdPercent))
                        Text(PantryUsageFormat.thresholdSource(estimate.thresholdSource))
                            .font(.caption)
                    }
                } label: {
                    Text("Low Alert")
                }
                if let skipped = PantryUsageFormat.skippedRecipes(estimate) {
                    Label(skipped, systemImage: "exclamationmark.circle")
                        .foregroundStyle(.orange)
                }
                if item.isEstimatedLow {
                    Label(
                        "The estimate marked this item low. Setting a status yourself replaces it.",
                        systemImage: "chart.line.downtrend.xyaxis"
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
            } header: {
                Text("Usage Estimate")
            } footer: {
                Text("Units that don't convert aren't guessed. Correct the amount any time; your number wins.")
            }
        } else {
            Section("Usage Estimate") {
                Text("Record an amount or restock this item to estimate what's left.")
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var purchasesSection: some View {
        Section("Recent Purchases") {
            switch purchasesPhase {
            case .loading:
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Loading purchases…")
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            case .failed(let message):
                VStack(alignment: .leading, spacing: 8) {
                    FormErrorLabel(message: message)
                    Button("Try Again") {
                        Task { await loadPurchases() }
                    }
                }
            case .loaded:
                if purchases.isEmpty {
                    Text("No purchases recorded yet.")
                        .foregroundStyle(.secondary)
                }
                ForEach(purchases) { purchase in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(PantryUsageFormat.purchaseAmount(purchase))
                        Text(
                            "\(PantryUsageFormat.purchaseSource(purchase.source)), \(purchase.purchasedAt.formatted(date: .abbreviated, time: .omitted))"
                        )
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
        // A restock starts a new cycle, which reloads the history.
        .task(id: "\(item.id) \(item.estimate?.cycleID ?? "") \(item.updatedAt.timeIntervalSince1970)") {
            await loadPurchases()
        }
    }

    private func loadPurchases() async {
        if purchasesPhase != .loaded {
            purchasesPhase = .loading
        }
        do {
            purchases = try await pantry.purchases(ofItemWithID: item.id)
            purchasesPhase = .loaded
        } catch is CancellationError {
            // The next appearance loads again.
        } catch {
            purchasesPhase = .failed(HouseholdStore.message(for: error))
        }
    }
}

/// One item's status, amount, estimate, and purchases, read-only apart from Edit and Restock
/// for members with `pantry.edit`. Opened from a notification, or from the Pantry tab by
/// members who can't edit.
struct PantryItemDetailView: View {
    let itemID: String

    @Environment(PantryStore.self) private var pantry
    @Environment(HouseholdStore.self) private var households

    @State private var sheet: Sheet?

    private enum Sheet: Identifiable {
        case edit(PantryItem)
        case restock(PantryItem)

        var id: String {
            switch self {
            case .edit(let item): "edit-\(item.id)"
            case .restock(let item): "restock-\(item.id)"
            }
        }
    }

    private var item: PantryItem? {
        pantry.items.first { $0.id == itemID }
    }

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    var body: some View {
        content
            .navigationTitle(item?.displayName ?? String(localized: "Pantry Item"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if canEdit, let item {
                    ToolbarItem(placement: .primaryAction) {
                        Button("Edit") { sheet = .edit(item) }
                    }
                }
            }
            .task(id: households.current?.household.id) {
                guard let householdID = households.current?.household.id else { return }
                await pantry.activate(householdID: householdID)
                // A notification can name an item added after the pantry last loaded.
                if pantry.phase == .loaded, item == nil {
                    await pantry.refresh()
                }
            }
            .sheet(item: $sheet) { sheet in
                switch sheet {
                case .edit(let item): PantryEditSheet(item: item)
                case .restock(let item): PantryRestockSheet(item: item)
                }
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
            if let item {
                detail(item)
            } else {
                ContentUnavailableView(
                    "No Longer in the Pantry", systemImage: "cabinet",
                    description: Text("Someone may have removed this item."))
            }
        }
    }

    private func detail(_ item: PantryItem) -> some View {
        List {
            if let refreshError = pantry.refreshError {
                FormErrorLabel(message: refreshError)
            }
            Section {
                LabeledContent("Status") {
                    PantryStatusPill(status: item.status, isEstimated: item.isEstimatedLow)
                }
                LabeledContent("Amount", value: PantryFormat.amount(item) ?? String(localized: "Not measured"))
                if let size = item.unitSize {
                    LabeledContent("Package Size", value: PantryUsageFormat.unitSize(size))
                }
                if canEdit {
                    Button("Restock / I Bought This", systemImage: "cart.badge.plus") {
                        sheet = .restock(item)
                    }
                }
            }
            PantryUsageSections(item: item)
        }
        .refreshable {
            await pantry.refresh()
        }
    }
}

/// Records that the household bought more of an item ("Restock / I bought this").
struct PantryRestockSheet: View {
    let item: PantryItem
    /// Receives the item as it is after the purchase.
    let onRecorded: (PantryItem) -> Void

    @Environment(PantryStore.self) private var pantry
    @Environment(\.dismiss) private var dismiss

    /// Created once, so trying again after a failure reuses its `clientPurchaseId`.
    @State private var draft: PantryPurchaseDraft
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(item: PantryItem, onRecorded: @escaping (PantryItem) -> Void = { _ in }) {
        self.item = item
        self.onRecorded = onRecorded
        _draft = State(initialValue: PantryPurchaseDraft(item: item))
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    TextField("Amount", text: $draft.quantityText, prompt: Text("For example, 2"))
                        .keyboardType(.numbersAndPunctuation)
                        .autocorrectionDisabled()
                    Picker("Unit", selection: $draft.unit) {
                        ForEach(PantryUnit.options(including: draft.unit), id: \.self) { code in
                            Text(PantryUnit.pickerLabel(code)).tag(code)
                        }
                    }
                    if let error = draft.quantityError {
                        FormErrorLabel(message: error)
                    }
                } header: {
                    Text("How Much You Bought")
                } footer: {
                    Text(
                        "The estimate starts over from this amount. Leave it empty to mark the item in stock without tracking it."
                    )
                }
                if draft.isDiscreteUnit {
                    Section {
                        Toggle("Package Size", isOn: $draft.hasUnitSize.animation())
                        if draft.hasUnitSize {
                            TextField("Each Holds", text: $draft.unitSizeText, prompt: Text("For example, 8"))
                                .keyboardType(.numbersAndPunctuation)
                                .autocorrectionDisabled()
                            Picker("Size Unit", selection: $draft.unitSizeUnit) {
                                ForEach(PantryPurchaseDraft.sizeUnits, id: \.self) { code in
                                    Text(PantryUnit.pickerLabel(code)).tag(code)
                                }
                            }
                            if let error = draft.unitSizeError {
                                FormErrorLabel(message: error)
                            }
                        }
                    } footer: {
                        Text(
                            "Optional. When each one holds a known amount, such as 8 oz, recipes measured that way count too."
                        )
                    }
                }
            }
            .navigationTitle("Restock \(item.displayName)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { save() }
                        .disabled(!draft.isValid || isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                let response = try await pantry.recordPurchase(try draft.manualPurchase(itemID: item.id))
                onRecorded(response.item)
                dismiss()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// An item's own low-stock threshold, or the household's.
struct PantryThresholdFields: View {
    @Binding var draft: PantryItemDraft
    /// The household's threshold, when loaded.
    let householdPercent: Int?

    var body: some View {
        Section {
            Toggle("Use Household Setting", isOn: $draft.usesHouseholdThreshold.animation())
            if draft.usesHouseholdThreshold {
                if let householdPercent {
                    LabeledContent("Mark Low At", value: PantryUsageFormat.threshold(householdPercent))
                }
            } else {
                Stepper(value: $draft.lowThresholdPercent, in: PantrySettings.thresholdRange, step: 5) {
                    LabeledContent("Mark Low At", value: PantryUsageFormat.threshold(draft.lowThresholdPercent))
                }
            }
        } header: {
            Text("Low-Stock Alert")
        } footer: {
            Text(
                "When this much of what you bought is used, the estimate marks the item low and notifies the household."
            )
        }
    }
}

/// The household's low-stock threshold, shown as percent used. Read-only without `pantry.edit`.
struct PantryThresholdSheet: View {
    @Environment(PantryStore.self) private var pantry
    @Environment(HouseholdStore.self) private var households
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @Environment(\.dismiss) private var dismiss

    @State private var percent = PantrySettings.defaultLowThresholdPercent
    @State private var loaded: PantrySettings?
    @State private var loadError: String?
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    var body: some View {
        NavigationStack {
            Form {
                content
            }
            .navigationTitle("Low-Stock Alerts")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if canEdit {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") { save() }
                            .disabled(loaded == nil || loaded?.lowThresholdPercent == percent || isSaving)
                    }
                } else {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
            .task { await load() }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let loaded {
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            Section {
                if canEdit {
                    Stepper(value: $percent, in: PantrySettings.thresholdRange, step: 5) {
                        LabeledContent("Mark Low At", value: PantryUsageFormat.threshold(percent))
                    }
                    Slider(
                        value: Binding(get: { Double(percent) }, set: { percent = Int($0.rounded()) }),
                        in: 1...100, step: 1
                    ) {
                        Text("Percent Used")
                    } minimumValueLabel: {
                        Text(PantryUsageFormat.threshold(1))
                    } maximumValueLabel: {
                        Text(PantryUsageFormat.threshold(100))
                    }
                    .accessibilityValue(PantryUsageFormat.threshold(percent))
                    if percent != loaded.defaultLowThresholdPercent {
                        Button("Use Default (\(PantryUsageFormat.threshold(loaded.defaultLowThresholdPercent)))") {
                            percent = loaded.defaultLowThresholdPercent
                        }
                    }
                } else {
                    LabeledContent("Mark Low At", value: PantryUsageFormat.threshold(loaded.lowThresholdPercent))
                }
            } footer: {
                Text(
                    "When an item with a recorded amount has used this much of what was bought, it's marked Estimated Low and the household is notified once per restock. An item can have its own threshold."
                )
            }
        } else if let loadError {
            Section {
                FormErrorLabel(message: loadError)
                Button("Try Again") {
                    Task { await load() }
                }
            }
        } else {
            Section {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Loading…")
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private func load() async {
        if loaded == nil, let cached = pantry.settings {
            show(cached)
        }
        loadError = nil
        do {
            show(try await pantry.loadSettings())
        } catch is CancellationError {
            // The next appearance loads again.
        } catch {
            if loaded == nil {
                loadError = HouseholdStore.message(for: error)
            }
        }
    }

    private func show(_ settings: PantrySettings) {
        if loaded == nil || percent == loaded?.lowThresholdPercent {
            percent = settings.lowThresholdPercent
        }
        loaded = settings
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                try await pantry.updateSettings(lowThresholdPercent: percent)
                dismiss()
                // Setting a low-stock alert is asking to be told, so it's a moment to ask
                // whether that may reach a locked phone. Only asked once per device.
                await push?.requestAuthorizationIfNeeded()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview("Detail") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        PantryItemDetailView(itemID: "p1")
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(PantryPreviewData.store(session: session))
}

#Preview("Restock") {
    let session = HouseholdPreviewData.session()
    PantryRestockSheet(item: PantryPreviewData.items[0])
        .environment(PantryPreviewData.store(session: session))
}

#Preview("Threshold") {
    let session = HouseholdPreviewData.session()
    PantryThresholdSheet()
        .environment(HouseholdPreviewData.store(session: session))
        .environment(PantryPreviewData.store(session: session))
}
