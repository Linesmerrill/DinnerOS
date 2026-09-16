import SwiftUI

/// The Shop tab: the week's grocery list matched to saved Walmart products, handed off as
/// add-to-cart links, then "Did you order these?" (Phase 8a).
struct ShopView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    @State private var choice: ProductChoice?
    @State private var isEditingStore = false
    @State private var isShowingSavedProducts = false

    private var household: Household? {
        households.current?.household
    }

    private var isReady: Bool {
        shopping.setupPhase == .loaded && shopping.isConfigured
    }

    var body: some View {
        content
            .navigationTitle("Shop")
            .navigationBarTitleDisplayMode(.inline)
            .safeAreaInset(edge: .top, spacing: 0) {
                if isReady {
                    ShopWeekSwitcher()
                }
            }
            .toolbar { toolbar }
            .navigationDestination(isPresented: $isShowingSavedProducts) {
                SavedProductsView()
            }
            .sheet(item: $choice) { choice in
                ChooseProductSheet(choice: choice)
            }
            .sheet(isPresented: $isEditingStore) {
                ShopStoreSettingsSheet()
            }
            .task(id: household?.id) {
                guard let household else { return }
                shopping.activate(householdID: household.id, timeZone: household.planningTimeZone)
                await shopping.load()
                // Loaded here too, so an API without the catalog hides the request row before
                // anyone taps it.
                await shopping.loadCatalog()
            }
    }

    @ViewBuilder
    private var content: some View {
        switch shopping.setupPhase {
        case .idle, .loading:
            ProgressView("Loading your store…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Shopping", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await shopping.load() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            if shopping.isConfigured {
                ShopWeekList(choose: { choice = $0 }, openStoreSetup: { isEditingStore = true })
            } else {
                ShopSetupView()
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if isReady {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button("Saved Products", systemImage: "bookmark") {
                        isShowingSavedProducts = true
                    }
                    if shopping.canEdit {
                        Button("Store Settings", systemImage: "storefront") {
                            isEditingStore = true
                        }
                    }
                } label: {
                    Label("Shop Options", systemImage: "ellipsis.circle")
                }
            }
        }
    }
}

/// The week's match: lines needing a product, ready lines with package counts, and lines
/// left out, with the handoff button pinned below.
private struct ShopWeekList: View {
    let choose: (ProductChoice) -> Void
    let openStoreSetup: () -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(PlanStore.self) private var plans
    @Environment(PantryStore.self) private var pantry
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households

    @State private var showsNotIncluded = false
    @State private var isRequestingStore = false
    /// The shown week's grocery list, only so the export section has the same text the
    /// Grocery List screen shares.
    @State private var groceryModel: GroceryListModel?
    @State private var exportController = GroceryExportController(remindersStore: EventKitRemindersStore())

    var body: some View {
        List {
            if let refreshError = shopping.refreshError {
                FormErrorLabel(message: refreshError)
            }
            if shopping.canConfirm, let handoff = shopping.openHandoff {
                OpenHandoffBanner(handoff: handoff) {
                    shopping.reviewOpenHandoff()
                }
            }
            switch shopping.proposalPhase {
            case .idle, .loading:
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Matching this week's list…")
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            case .failed(let message):
                Section {
                    FormErrorLabel(message: message)
                    Button("Try Again") {
                        Task { await shopping.reload() }
                    }
                }
            case .loaded:
                if let proposal = shopping.proposal {
                    sections(proposal)
                }
            }
            if let groceryModel {
                GroceryExportSection(controller: exportController, model: groceryModel)
            }
            Section {
                RequestStoreRow { isRequestingStore = true }
            }
        }
        .refreshable {
            await shopping.reload()
        }
        .sheet(isPresented: $isRequestingStore) {
            RequestStoreSheet(openStoreSetup: openStoreSetup)
        }
        .groceryExportPrompts(exportController)
        // The week, and the plan behind it: a serving size changed on the Menu changes every
        // quantity the export writes, and the week alone can't see that.
        .task(id: ShopListKey(week: shopping.week, planRevision: shopping.planRevision)) {
            guard
                let model = plans.makeGroceryList(
                    week: shopping.week, purchases: pantry, specialties: specialties,
                    canAddToPantry: households.access?.can(.pantryEdit) == true)
            else {
                groceryModel = nil
                return
            }
            groceryModel = model
            await model.load()
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if shopping.proposalPhase == .loaded, let proposal = shopping.proposal, !proposal.lines.isEmpty {
                ShopHandoffBar(proposal: proposal)
            }
        }
    }

    @ViewBuilder
    private func sections(_ proposal: ShoppingProposal) -> some View {
        let needsProduct = proposal.needsProduct
        let notIncluded = proposal.notIncluded
        if proposal.lines.isEmpty && needsProduct.isEmpty {
            ContentUnavailableView {
                Label("Nothing to Buy", systemImage: "cart")
            } description: {
                Text(
                    "Nothing on this week's grocery list needs buying. Plan recipes, or uncheck lines on the grocery list."
                )
            }
            .listRowBackground(Color.clear)
        }
        if !needsProduct.isEmpty {
            Section {
                ForEach(needsProduct) { line in
                    NeedsProductRow(line: line, canChoose: shopping.canEdit) {
                        choose(ProductChoice(excluded: line))
                    }
                }
            } header: {
                Text("Needs a Product")
            } footer: {
                if shopping.canEdit {
                    Text("Choose a Walmart product once and it's used every week.")
                } else {
                    Text("Your role can't choose products.")
                }
            }
        }
        if !proposal.lines.isEmpty {
            Section {
                ForEach(proposal.lines) { line in
                    ReadyLineRow(line: line, packages: packagesBinding(for: line), canEdit: shopping.canEdit) {
                        choose(ProductChoice(line: line))
                    }
                }
            } header: {
                Text("Ready")
            } footer: {
                Text("Counts come from each product's package size. Check flagged lines before opening Walmart.")
            }
        }
        if !notIncluded.isEmpty {
            Section {
                DisclosureGroup(isExpanded: $showsNotIncluded) {
                    ForEach(notIncluded) { line in
                        ExcludedLineRow(line: line)
                    }
                } label: {
                    Text("Not Included (\(notIncluded.count))")
                        .foregroundStyle(Color.primary)
                }
            } footer: {
                Text("Lines at home or checked off on the grocery list stay out of the cart.")
            }
        }
    }

    /// The week the export section's list is built for, and the plan it was built from.
    private struct ShopListKey: Equatable {
        let week: ISOWeek
        let planRevision: Int
    }

    private func packagesBinding(for line: ShoppingHandoffLine) -> Binding<Int> {
        Binding(
            get: { shopping.packages(for: line) },
            set: { shopping.setPackages($0, for: line) })
    }
}

private struct NeedsProductRow: View {
    let line: ShoppingExcludedLine
    let canChoose: Bool
    let choose: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8)) : AnyLayout(HStackLayout(spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 2) {
                Text(line.name)
                if !line.quantityText.isEmpty {
                    Text(line.quantityText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            if !dynamicTypeSize.isAccessibilitySize {
                Spacer(minLength: 8)
            }
            if canChoose {
                Button("Choose Product", action: choose)
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Choose Product for \(line.name)")
            }
        }
    }
}

private struct ReadyLineRow: View {
    let line: ShoppingHandoffLine
    @Binding var packages: Int
    let canEdit: Bool
    let changeProduct: () -> Void

    private var productText: String {
        let size = line.product.packageSize?.text ?? String(localized: "size unknown")
        return "\(line.product.displayName) · \(size)"
    }

    /// The API's coverage for its own count; after a change, what the new count adds.
    private var coverageText: String {
        guard packages != line.packages else { return line.coverageText }
        if let size = line.product.packageSize {
            return String(localized: "\(packages) × \(size.text), changed from \(line.packages)")
        }
        return String(localized: "Changed from \(line.packages)")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(line.name)
                        .font(.headline)
                    Text(productText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    if !coverageText.isEmpty {
                        Text(coverageText)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                Spacer(minLength: 0)
                if canEdit {
                    Menu {
                        Button("Change Product", systemImage: "arrow.triangle.2.circlepath", action: changeProduct)
                    } label: {
                        Image(systemName: "ellipsis.circle")
                            .imageScale(.large)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .accessibilityLabel("Options for \(line.name)")
                }
            }
            if line.checkAmount {
                CheckAmountBadge(text: line.reasonText)
            }
            if canEdit {
                Stepper(value: $packages, in: ShoppingLimits.packages) {
                    Text(ShoppingText.packages(packages))
                        .font(.subheadline)
                }
                .accessibilityLabel("Packages of \(line.name)")
                .accessibilityValue(ShoppingText.packages(packages))
            } else {
                Text(ShoppingText.packages(packages))
                    .font(.subheadline)
            }
        }
        .padding(.vertical, 2)
    }
}

private struct CheckAmountBadge: View {
    let text: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label("Check Amount", systemImage: "exclamationmark.triangle.fill")
                .font(.caption.weight(.semibold))
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .foregroundStyle(Color.orange)
                .background(Color.orange.opacity(0.15), in: .capsule)
            if let text {
                Text(text)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct ExcludedLineRow: View {
    let line: ShoppingExcludedLine

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(line.name.isEmpty ? line.ingredientKey : line.name)
            Text(line.text)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .foregroundStyle(Color.primary)
        .accessibilityElement(children: .combine)
    }
}

private struct OpenHandoffBanner: View {
    let handoff: ShoppingHandoff
    let review: () -> Void

    private var pendingCount: Int {
        handoff.lines.count { $0.confirmation?.status != .confirmed }
    }

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label("Did you order these?", systemImage: "shippingbox")
                    .font(.headline)
                Text(summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Button("Review Order", action: review)
                    .buttonStyle(.borderedProminent)
            }
            .padding(.vertical, 4)
        }
    }

    private var summary: String {
        let sent = handoff.createdAt.formatted(.relative(presentation: .named))
        return pendingCount == 1
            ? String(localized: "1 item went to Walmart \(sent). Add what you ordered to the pantry.")
            : String(localized: "\(pendingCount) items went to Walmart \(sent). Add what you ordered to the pantry.")
    }
}

/// "Open in Walmart", then one button per remaining cart link, then the order question.
private struct ShopHandoffBar: View {
    let proposal: ShoppingProposal

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 8) {
            if let message = errorMessage ?? shopping.linkError {
                FormErrorLabel(message: message)
                    .font(.footnote)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !shopping.canEdit {
                Text("Your role can see this list but can't send it to Walmart.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else if let progress = shopping.weekLinkProgress, progress.openedCount > 0,
                let next = progress.nextIndex
            {
                Button {
                    Task { await shopping.openNextCartLink() }
                } label: {
                    Text("Next Cart Link (\(next + 1) of \(progress.total))")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                Text("Each link adds more items to the same Walmart cart.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else if let progress = shopping.weekLinkProgress, progress.isFinished, shopping.canConfirm,
                shopping.openHandoff?.id == progress.handoff.id
            {
                Button {
                    shopping.reviewOpenHandoff()
                } label: {
                    Text("Did You Order These?")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                openButton(title: "Open in Walmart Again")
                    .buttonStyle(.bordered)
            } else {
                openButton(title: "Open in Walmart")
                    .buttonStyle(.borderedProminent)
                Text(summary)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if proposal.affiliateTracked {
                Text("\(configuration.displayName) may earn a commission on Walmart purchases.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .multilineTextAlignment(.center)
        .padding()
        .frame(maxWidth: .infinity)
        .background(.bar)
    }

    private var summary: String {
        let count = proposal.lines.count
        return count == 1
            ? String(localized: "Adds 1 item to your Walmart cart. You check out in Walmart.")
            : String(localized: "Adds \(count) items to your Walmart cart. You check out in Walmart.")
    }

    private func openButton(title: LocalizedStringKey) -> some View {
        Button(action: open) {
            Group {
                if shopping.isCreatingHandoff {
                    ProgressView()
                } else {
                    Label(title, systemImage: "cart")
                }
            }
            .frame(maxWidth: .infinity)
        }
        .controlSize(.large)
        .disabled(shopping.isCreatingHandoff)
    }

    private func open() {
        errorMessage = nil
        Task {
            do {
                try await shopping.openInWalmart()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// Previous and next week, the shown week's dates, and a jump to this week.
private struct ShopWeekSwitcher: View {
    @Environment(ShoppingStore.self) private var shopping

    var body: some View {
        let week = shopping.week
        HStack(spacing: 12) {
            Button("Previous Week", systemImage: "chevron.left") {
                Task { await shopping.show(week: week.previous) }
            }
            .labelStyle(.iconOnly)
            Spacer(minLength: 0)
            VStack(spacing: 4) {
                Text(week.rangeLabel())
                    .font(.headline)
                if let relative = relativeName(week) {
                    Text(relative)
                        .foregroundStyle(.secondary)
                } else {
                    Button("This Week") {
                        Task { await shopping.showCurrentWeek() }
                    }
                }
            }
            .font(.subheadline)
            .accessibilityElement(children: .contain)
            Spacer(minLength: 0)
            Button("Next Week", systemImage: "chevron.right") {
                Task { await shopping.show(week: week.next) }
            }
            .labelStyle(.iconOnly)
        }
        .font(.title3)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }

    private func relativeName(_ week: ISOWeek) -> LocalizedStringKey? {
        let current = shopping.currentWeek
        switch week {
        case current: return "This Week"
        case current.next: return "Next Week"
        case current.previous: return "Last Week"
        default: return nil
        }
    }
}

#Preview("Matched") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ShopView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(ShopPreviewData.store(session: session))
}

#Preview("First run") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ShopView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(ShopPreviewData.store(session: session, configured: false))
}
