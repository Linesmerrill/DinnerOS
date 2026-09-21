import SwiftUI

/// The Shop tab: the week's grocery list matched to saved Walmart products, handed off as
/// add-to-cart links, then "Did you order these?" (Phase 8a).
struct ShopView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    @State private var choice: ProductChoice?
    @State private var isEditingStore = false
    @State private var isShowingSavedProducts = false
    /// The cost card's sheet is held and presented here, not on the card: the card is a list
    /// section that comes and goes with the week's cost (decision 510).
    @State private var weekCostSheet: WeekCostSheet?
    /// Presented from the screen root for the same reason the cost card's sheets are
    /// (decision 510): the banner that opens it is a list section that comes and goes.
    @State private var isShowingBulkPacks = false

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
            .sheet(item: $weekCostSheet) { sheet in
                WeekCostSheetView(sheet: sheet)
            }
            .sheet(isPresented: $isShowingBulkPacks) {
                if let handoffID = shopping.bulkPacks?.handoffID {
                    BulkPackSheet(packs: shopping.openBulkPacks, week: shopping.week, handoffID: handoffID)
                }
            }
            // A meal kit saved in Household changes every comparison. Watched here rather than
            // on the card, which isn't on screen for every week that has a cost to reload.
            .onChange(of: household?.mealKit) {
                Task { await shopping.loadWeekCost() }
            }
            // The week's newest hand-off is what the packages were counted for, so the bulk
            // packs follow it: a new send, or a confirmation, can change what's left over.
            .onChange(of: shopping.weekHandoffs.first?.id, initial: true) { _, handoffID in
                guard let handoffID else { return }
                Task { await shopping.loadBulkPacks(handoffID: handoffID) }
            }
            .task(id: household?.weekScope) {
                guard let household else { return }
                shopping.activate(
                    householdID: household.id, timeZone: household.planningTimeZone,
                    weekStartsOn: household.weekStartsOn)
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
                ShopWeekList(
                    choose: { choice = $0 }, openStoreSetup: { isEditingStore = true },
                    openWeekCost: { weekCostSheet = $0 }, openBulkPacks: { isShowingBulkPacks = true })
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
    let openWeekCost: (WeekCostSheet) -> Void
    let openBulkPacks: () -> Void

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
            // Above "Did you order these?" on purpose: after a Walmart hand-off both are in
            // view, so marking the week ordered is offered at that moment and never assumed.
            if let reminder = shopping.orderReminder, reminder.remind || reminder.ordered {
                OrderReminderBanner(reminder: reminder)
            }
            if shopping.canEdit, !shopping.openBulkPacks.isEmpty {
                BulkPackBanner(packs: shopping.openBulkPacks, review: openBulkPacks)
            }
            if shopping.canConfirm, let handoff = shopping.openHandoff {
                OpenHandoffBanner(handoff: handoff) {
                    shopping.reviewOpenHandoff()
                }
            }
            WeekCostSection(open: openWeekCost)
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
            await shopping.loadWeekCost()
        }
        // The week's cost and handoffs, kept apart from the match so a slow or missing cost
        // endpoint never holds up the list.
        .task(id: "\(shopping.householdID ?? "")|\(shopping.week)") {
            await shopping.loadWeekCost()
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
                    "You have everything on this week's grocery list. Add recipes to your plan, or uncheck items on the grocery list."
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
        let toSend = proposal.linesToSend
        if !toSend.isEmpty {
            Section {
                ForEach(toSend) { line in
                    ReadyLineRow(
                        line: line, packages: packagesBinding(for: line), canEdit: shopping.canEdit,
                        changeProduct: { choose(ProductChoice(line: line)) },
                        fixPackageSize: { choose(ProductChoice(line: line, packageSizeFix: $0)) })
                }
            } header: {
                Text("Ready")
            } footer: {
                Text(
                    "We work out how many to buy from each product's size. Tap anything marked Check Amount to fix it before opening Walmart."
                )
            }
        }
        if !proposal.linesInCart.isEmpty || !proposal.otherInCart.isEmpty {
            InWalmartCartSection(
                proposal: proposal, packages: packagesBinding(for:),
                changeProduct: { choose(ProductChoice(line: $0)) })
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
                Text(
                    "Items you already have at home, or already checked off on your grocery list, won't be added to your Walmart cart."
                )
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
    /// Opens the product to fix the package size a Check Amount warning is about.
    let fixPackageSize: (ShoppingPackageSizeFix) -> Void

    /// The warning's fix, when this member can make it.
    private var fix: ShoppingPackageSizeFix? {
        canEdit ? line.packageSizeFix : nil
    }

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
                details
                Spacer(minLength: 0)
                if canEdit {
                    Menu {
                        if let fix {
                            Button(fix.title, systemImage: "ruler") { fixPackageSize(fix) }
                        }
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
                if let fix {
                    // The warning is the way to fix it: a warning you can't act on is just noise.
                    Button {
                        fixPackageSize(fix)
                    } label: {
                        CheckAmountBadge(text: line.reasonText, action: fix.title)
                    }
                    .buttonStyle(.plain)
                } else {
                    CheckAmountBadge(text: line.reasonText, action: nil)
                }
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

    /// The name, product and count. With a fixable warning, tapping them fixes it too.
    @ViewBuilder
    private var details: some View {
        if let fix {
            Button {
                fixPackageSize(fix)
            } label: {
                detailsText
                    .foregroundStyle(Color.primary)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint(fix.title)
        } else {
            detailsText
        }
    }

    private var detailsText: some View {
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
            // Sent before at a lower count: only the difference goes to Walmart.
            if line.sentPackages > 0 {
                Text(ShoppingText.cartStatus(sent: line.sentPackages, wanted: packages))
                    .font(.footnote)
                    .foregroundStyle(.tint)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// What this week's hand-off already put in the Walmart cart. Walmart's link only adds, so
/// these lines stay out of the next "Open in Walmart" unless their count goes up; a lower count
/// says what to remove in the Walmart app. "Send Again" and "Start Over" are for a cart the
/// member emptied.
private struct InWalmartCartSection: View {
    let proposal: ShoppingProposal
    let packages: (ShoppingHandoffLine) -> Binding<Int>
    let changeProduct: (ShoppingHandoffLine) -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    @State private var isConfirmingStartOver = false
    @State private var errorMessage: String?

    var body: some View {
        Section {
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
            }
            ForEach(proposal.linesInCart) { line in
                InCartLineRow(
                    line: line, packages: packages(line), canEdit: shopping.canEdit,
                    sendAgain: { run { try await shopping.sendAgain(line) } },
                    changeProduct: { changeProduct(line) })
            }
            ForEach(proposal.otherInCart) { line in
                OtherInCartRow(line: line)
            }
            if shopping.canEdit {
                Button("Start Over and Send Everything", systemImage: "arrow.counterclockwise") {
                    isConfirmingStartOver = true
                }
                .disabled(shopping.isResending || shopping.isCreatingHandoff)
                // Attached to the button: on iOS 26 the dialog is a popover anchored to it.
                .confirmationDialog(
                    "Send Everything Again?", isPresented: $isConfirmingStartOver, titleVisibility: .visible
                ) {
                    Button("Send Everything Again") {
                        run { try await shopping.startOverAndSendEverything() }
                    }
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text(
                        "Only do this if your Walmart cart is empty. Every item is added again, so anything still in the cart would be doubled."
                    )
                }
            }
        } header: {
            Text("In Walmart Cart")
        } footer: {
            Text(
                "Already in your Walmart cart, so opening Walmart again won't add them twice. Once you mark the week ordered, the next list starts fresh."
            )
        }
    }

    private func run(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            errorMessage = nil
            do {
                try await action()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

private struct InCartLineRow: View {
    let line: ShoppingHandoffLine
    @Binding var packages: Int
    let canEdit: Bool
    let sendAgain: () -> Void
    let changeProduct: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(line.name)
                        .font(.headline)
                    Text(line.product.displayName)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Label(ShoppingText.cartStatus(sent: line.sentPackages, wanted: packages), systemImage: "cart.fill")
                        .font(.footnote)
                        .foregroundStyle(
                            packages < line.sentPackages ? AnyShapeStyle(Color.orange) : AnyShapeStyle(.tint))
                }
                .accessibilityElement(children: .combine)
                Spacer(minLength: 0)
                if canEdit {
                    Menu {
                        Button("Send Again", systemImage: "arrow.clockwise", action: sendAgain)
                        Button("Change Product", systemImage: "arrow.triangle.2.circlepath", action: changeProduct)
                    } label: {
                        Image(systemName: "ellipsis.circle")
                            .imageScale(.large)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .accessibilityLabel("Options for \(line.name)")
                    .accessibilityHint(
                        "Send Again adds it to the Walmart cart next time, for when you removed it there.")
                }
            }
            if canEdit {
                Stepper(value: $packages, in: ShoppingLimits.packages) {
                    Text(ShoppingText.packages(packages))
                        .font(.subheadline)
                }
                .accessibilityLabel("Packages of \(line.name)")
                .accessibilityValue(ShoppingText.packages(packages))
            }
        }
        .padding(.vertical, 2)
    }
}

private struct OtherInCartRow: View {
    let line: ShoppingSentLine

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(line.name.isEmpty ? line.product.displayName : line.name)
            Text(line.product.displayName)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Label(line.text, systemImage: line.removePackages > 0 ? "minus.circle" : "cart.fill")
                .font(.footnote)
                .foregroundStyle(line.removePackages > 0 ? AnyShapeStyle(Color.orange) : AnyShapeStyle(.secondary))
        }
        .accessibilityElement(children: .combine)
    }
}

private struct CheckAmountBadge: View {
    let text: String?
    /// What tapping the warning does, shown under it; `nil` when it does nothing.
    let action: String?

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
            if let action {
                Label(action, systemImage: "chevron.right")
                    .labelStyle(TrailingIconLabelStyle())
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tint)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

/// A label with its icon after the title, like a disclosure: "Add Package Size ›".
private struct TrailingIconLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.title
            configuration.icon
                .imageScale(.small)
                .accessibilityHidden(true)
        }
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

/// The week's order reminder, and the one control that silences it.
///
/// It appears from the household's order day until a member marks the week ordered, then
/// stays as a quiet confirmation with an undo — a mis-tap must not leave the household
/// un-remindable for the rest of the week. Nothing else marks a week: handing a list to
/// Walmart is not proof an order was placed.
private struct OrderReminderBanner: View {
    let reminder: OrderReminder

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    /// Optional so previews needn't supply one.
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?

    @State private var errorMessage: String?

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label(title, systemImage: reminder.ordered ? "checkmark.circle.fill" : "calendar.badge.clock")
                    .font(.headline)
                    .foregroundStyle(reminder.ordered ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.primary))
                Text(summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if let errorMessage {
                    FormErrorLabel(message: errorMessage)
                        .font(.footnote)
                }
                if shopping.canEdit {
                    action
                }
                // Every member sees the reminder, but only the one who picked the order
                // day was asked about notifications; this asks the rest, on their tap.
                if !reminder.ordered, let push, push.authorization == .notDetermined {
                    Button("Notify Me on Order Day", systemImage: "bell.badge") {
                        Task { await push.requestAuthorizationIfNeeded() }
                    }
                    .buttonStyle(.borderless)
                    .font(.subheadline)
                    .accessibilityHint("Asks to send a notification when it's time to order.")
                }
            }
            .padding(.vertical, 4)
        }
    }

    @ViewBuilder
    private var action: some View {
        if reminder.ordered {
            Button("Not Ordered Yet") { setOrdered(false) }
                .buttonStyle(.bordered)
                .disabled(shopping.isSettingOrdered)
                .accessibilityHint("Brings this week's reminder back.")
        } else {
            Button("Mark as Ordered") { setOrdered(true) }
                .buttonStyle(.borderedProminent)
                .disabled(shopping.isSettingOrdered)
                .accessibilityHint("Stops reminding you about this week.")
        }
    }

    private var title: String {
        reminder.ordered
            ? String(localized: "Ordered this week")
            : String(localized: "Time to order this week's groceries")
    }

    private var summary: String {
        if reminder.ordered {
            return String(localized: "Marked ordered, so nothing will remind you again until next week.")
        }
        guard let day = reminder.orderDayName else {
            return String(localized: "Send your list to Walmart, then mark the week ordered.")
        }
        return String(
            localized: "\(day) is your order day. Once you've placed the order, mark the week and this stops.")
    }

    private func setOrdered(_ ordered: Bool) {
        Task {
            errorMessage = nil
            do {
                try await shopping.setWeekOrdered(ordered)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// "The smallest pack was bigger than this week needs." Opens the bulk-pack sheet.
private struct BulkPackBanner: View {
    let packs: [ShoppingBulkPack]
    let review: () -> Void

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label("More Than This Week Needs", systemImage: "arrow.up.bin")
                    .font(.headline)
                Text(summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Button("See What to Do", action: review)
                    .buttonStyle(.bordered)
            }
            .padding(.vertical, 4)
        }
    }

    private var summary: String {
        guard let first = packs.first else { return "" }
        if packs.count == 1 {
            return first.surplusText + ". " + String(localized: "Cook it again, or freeze the rest.")
        }
        return String(localized: "\(packs.count) items came in packs bigger than this week needs.")
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
                if shopping.isEverythingInCart {
                    everythingInCart
                } else {
                    openButton(title: "Add New Items in Walmart")
                        .buttonStyle(.bordered)
                    Text(summary)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } else if shopping.isEverythingInCart {
                // Walmart's link would only add these a second time.
                everythingInCart
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

    private var everythingInCart: some View {
        Label(shopping.linkNotice ?? ShoppingText.everythingInCart, systemImage: "checkmark.circle")
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
    }

    /// Counts only what the link adds: lines already in the cart aren't sent again.
    private var summary: String {
        let count = shopping.linesToAdd.count
        if proposal.cart != nil {
            return count == 1
                ? String(localized: "Adds 1 new item to your Walmart cart. What's already there isn't added again.")
                : String(
                    localized: "Adds \(count) new items to your Walmart cart. What's already there isn't added again.")
        }
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
                Text(week.rangeLabel(weekStartsOn: shopping.weekStartsOn))
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
