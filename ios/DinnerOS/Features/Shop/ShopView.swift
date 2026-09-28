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
    @State private var isShowingPrep = false

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
            .sheet(isPresented: $isShowingPrep) {
                PrepSessionSheet()
            }
            // A meal kit saved in Household changes every comparison. Watched here rather than
            // on the card, which isn't on screen for every week that has a cost to reload.
            .onChange(of: household?.mealKit) {
                Task { await shopping.loadWeekCost() }
            }
            // The week's newest hand-off is what the packages were counted for, so the prep
            // checklist follows it: a new send, or a confirmation, can change what's left over.
            .onChange(of: shopping.weekHandoffs.first?.id, initial: true) { _, handoffID in
                guard handoffID != nil else { return }
                Task { await shopping.loadPrepSession() }
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
                    openWeekCost: { weekCostSheet = $0 }, openPrep: { isShowingPrep = true })
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
    let openPrep: () -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(PlanStore.self) private var plans
    @Environment(PantryStore.self) private var pantry
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(GrocerySkipStore.self) private var grocerySkips

    @State private var showsNotIncluded = false
    @State private var isRequestingStore = false
    @State private var showsLeftOut = false
    /// Lines with a leave-out or put-back in flight, by row ID.
    @State private var working: Set<String> = []
    @State private var leaveOutError: String?
    /// The skip revision this list already re-matched for, so its own changes don't match twice.
    @State private var handledSkipRevision: Int?

    /// Leaving something out changes what the week buys, so it needs `plan.edit`, as on the
    /// grocery list. Everyone else sees what's left out, read-only.
    private var canLeaveOut: Bool {
        households.access?.can(.planEdit) == true && !grocerySkips.isForbidden
    }
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
            if shopping.canConfirm, let prep = shopping.prepSession, prep.hasWork {
                PrepSessionBanner(session: prep, open: openPrep)
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
        .sheet(isPresented: $showsLeftOut) {
            SkippedIngredientsSheet(week: shopping.week)
        }
        .alert("Couldn't Change That", isPresented: Binding(presenting: $leaveOutError)) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(leaveOutError ?? "")
        }
        .task(id: shopping.householdID) {
            // Put Back needs the household's skips, and the review sheet reads them.
            guard let householdID = shopping.householdID else { return }
            await grocerySkips.activate(householdID: householdID)
            handledSkipRevision = grocerySkips.revision
        }
        // Left out or put back on the grocery list, a recipe, or the review sheet.
        .onChange(of: grocerySkips.revision) { _, revision in
            guard revision != handledSkipRevision else { return }
            handledSkipRevision = revision
            Task { await shopping.groceryDidChange() }
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
        let layout = ShopMealLayout(proposal: proposal)
        ForEach(Array(layout.groups.enumerated()), id: \.element.id) { index, group in
            Section {
                ForEach(group.rows) { row in
                    mealRow(row, in: group)
                }
                ForEach(group.components) { component in
                    ShopComponentHeader(
                        component: component, canEdit: canLeaveOut && group.meal != nil,
                        isWorking: working.contains(component.id),
                        leaveOut: { scope in leaveOut(component: component, in: group, scope: scope) },
                        putBack: { putBack(component: component) })
                    ForEach(component.rows) { row in
                        mealRow(row, in: group, inComponent: true)
                    }
                }
            } header: {
                ShopMealHeader(group: group)
            } footer: {
                if index == layout.groups.count - 1 {
                    mealsFooter(needsProduct: !needsProduct.isEmpty)
                }
            }
        }
        if !proposal.leftOut.isEmpty || !grocerySkips.items.isEmpty {
            Section {
                Button("Everything You Leave Out", systemImage: "cart.badge.minus") {
                    showsLeftOut = true
                }
                .foregroundStyle(.secondary)
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

    @ViewBuilder
    private func mealsFooter(needsProduct: Bool) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(
                "An item several meals use is bought once, with the meal it's listed under first. We work out how many to buy from each product's size."
            )
            if needsProduct {
                Text(
                    shopping.canEdit
                        ? "Choose a Walmart product once and it's used every week."
                        : "Your role can't choose products.")
            }
        }
    }

    /// One line under a meal: the purchase itself where it's bought, a pointer to it under the
    /// other meals that use it, or a struck-through line the household left out.
    @ViewBuilder
    private func mealRow(_ row: ShopMealLayout.Row, in group: ShopMealLayout.Group, inComponent: Bool = false)
        -> some View
    {
        let options = inComponent ? nil : removeOptions(for: row, in: group)
        Group {
            switch row.kind {
            case .line(let line) where row.isPrimary:
                ReadyLineRow(
                    line: line, packages: packagesBinding(for: line), canEdit: shopping.canEdit,
                    changeProduct: { choose(ProductChoice(line: line)) },
                    fixPackageSize: { choose(ProductChoice(line: line, packageSizeFix: $0)) },
                    mealNote: ShopMealText.note(for: row), remove: options)
            case .line(let line):
                SharedLineRow(
                    row: row, detail: ShopMealText.boughtWith(row, packages: shopping.packages(for: line)),
                    isWorking: working.contains(row.id), remove: options)
            case .needsProduct(let line) where row.isPrimary:
                NeedsProductRow(
                    line: line, canChoose: shopping.canEdit, mealNote: ShopMealText.note(for: row), remove: options
                ) {
                    choose(ProductChoice(excluded: line))
                }
            case .needsProduct:
                SharedLineRow(
                    row: row, detail: ShopMealText.boughtWith(row, packages: nil),
                    isWorking: working.contains(row.id), remove: options)
            case .leftOut(let line):
                LeftOutShopRow(
                    row: row, text: line.text, canPutBack: canLeaveOut && !inComponent,
                    isWorking: working.contains(row.id)
                ) {
                    putBack(line, rowID: row.id)
                }
            }
        }
        .padding(.leading, inComponent ? 12 : 0)
    }

    /// "Remove from the list": out of this meal only, for this week, or always. `nil` when this
    /// member can't, or for a pairing, which is removed from the grocery list instead.
    private func removeOptions(for row: ShopMealLayout.Row, in group: ShopMealLayout.Group) -> ShopRemoveOptions? {
        guard canLeaveOut, !row.isLeftOut, row.share?.extra != true else { return nil }
        let name = row.name
        let key = row.ingredientKey
        var options = ShopRemoveOptions(name: name, mealName: nil, justThisMeal: nil, thisWeek: {}, always: {})
        if let recipeID = row.recipeID, let meal = group.meal {
            options.mealName = meal.recipeName
            options.justThisMeal = {
                leaveOut(.leaveOut(ingredientKey: key, name: name, recipeID: recipeID), key: key, rowID: row.id)
            }
        }
        options.thisWeek = {
            leaveOut(
                GrocerySkipRequest(ingredientKey: key, name: name, scope: .week, week: shopping.week.description),
                key: key, rowID: row.id)
        }
        options.always = {
            leaveOut(.always(ingredientKey: key, name: name), key: key, rowID: row.id)
        }
        return options
    }

    private func leaveOut(
        component: ShopMealLayout.Component, in group: ShopMealLayout.Group, scope: GrocerySkipScope
    ) {
        let key = GroceryComponentKey.skipKey(for: component.component)
        let request: GrocerySkipRequest
        switch scope {
        case .recipe:
            guard let meal = group.meal else { return }
            request = .leaveOut(ingredientKey: key, name: component.name, recipeID: meal.recipeID)
        default:
            request = .always(ingredientKey: key, name: component.name)
        }
        leaveOut(request, keys: Set(component.rows.map(\.ingredientKey)), rowID: component.id)
    }

    private func putBack(component: ShopMealLayout.Component) {
        var skips: [GrocerySkip] = []
        for row in component.rows {
            if case .leftOut(let line) = row.kind {
                skips += line.holdingSkips(in: grocerySkips.items).filter { skip in !skips.contains(skip) }
            }
        }
        resume(skips, keys: Set(component.rows.map(\.ingredientKey)), rowID: component.id)
    }

    private func leaveOut(_ request: GrocerySkipRequest, key: String, rowID: String) {
        leaveOut(request, keys: [key], rowID: rowID)
    }

    private func leaveOut(_ request: GrocerySkipRequest, keys: Set<String>, rowID: String) {
        change(rowID: rowID, keys: keys) {
            try await grocerySkips.skip(request, householdID: shopping.householdID)
        }
    }

    private func putBack(_ line: ShoppingExcludedLine, rowID: String) {
        resume(line.holdingSkips(in: grocerySkips.items), keys: [line.ingredientKey], rowID: rowID)
    }

    private func resume(_ skips: [GrocerySkip], keys: Set<String>, rowID: String) {
        guard !skips.isEmpty else { return }
        change(rowID: rowID, keys: keys) {
            for skip in skips {
                try await grocerySkips.resume(skipID: skip.id, householdID: shopping.householdID)
            }
        }
    }

    /// Runs a change, then re-matches at once so the totals and package counts show it.
    private func change(rowID: String, keys: Set<String>, _ body: @escaping () async throws -> Void) {
        guard !working.contains(rowID) else { return }
        working.insert(rowID)
        Task {
            defer { working.remove(rowID) }
            do {
                try await body()
                handledSkipRevision = grocerySkips.revision
                await shopping.groceryDidChange(forgettingCountsFor: keys)
            } catch is CancellationError {
                return
            } catch {
                leaveOutError = ShopErrors.message(for: error, households: households)
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
    var mealNote: String? = nil
    var remove: ShopRemoveOptions? = nil
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
                if let mealNote {
                    Text(mealNote)
                        .font(.footnote)
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
            if let remove {
                ShopRemoveMenu(options: remove)
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
    /// This meal's amount and the other meals that use it, under a meal.
    var mealNote: String? = nil
    /// Ways to take it off the list; `nil` when this member can't.
    var remove: ShopRemoveOptions? = nil

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
                if canEdit || remove != nil {
                    Menu {
                        if canEdit {
                            if let fix {
                                Button(fix.title, systemImage: "ruler") { fixPackageSize(fix) }
                            }
                            Button("Change Product", systemImage: "arrow.triangle.2.circlepath", action: changeProduct)
                        }
                        if let remove {
                            ShopRemoveMenuItems(options: remove)
                        }
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
            if let mealNote {
                Text(mealNote)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
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

/// A line bought with another meal, shown under this one with this meal's own amount.
private struct SharedLineRow: View {
    let row: ShopMealLayout.Row
    let detail: String
    let isWorking: Bool
    let remove: ShopRemoveOptions?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.name)
                if !row.quantityText.isEmpty {
                    Text(row.quantityText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Label(detail, systemImage: "arrow.turn.down.right")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            if isWorking {
                ProgressView()
            } else if let remove {
                ShopRemoveMenu(options: remove)
            }
        }
    }
}

/// A line the household left out, struck through under the meal it was left out of.
private struct LeftOutShopRow: View {
    let row: ShopMealLayout.Row
    let text: String
    let canPutBack: Bool
    let isWorking: Bool
    let putBack: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.name)
                    .strikethrough()
                    .foregroundStyle(.secondary)
                if !row.quantityText.isEmpty {
                    Text(row.quantityText)
                        .font(.subheadline)
                        .strikethrough()
                        .foregroundStyle(.secondary)
                }
                Label(text, systemImage: "minus.circle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(row.name), left out. \(text)")
            Spacer(minLength: 0)
            if isWorking {
                ProgressView()
            } else if canPutBack {
                Button("Put Back", action: putBack)
                    .buttonStyle(.bordered)
                    .font(.subheadline)
                    .accessibilityLabel("Put \(row.name) Back")
            }
        }
    }
}

/// A component of a meal (a crema made from store ingredients): its name, and "Don't Make
/// This" for the whole of it, or Put Back when the meal doesn't make it.
private struct ShopComponentHeader: View {
    let component: ShopMealLayout.Component
    let canEdit: Bool
    let isWorking: Bool
    let leaveOut: (GrocerySkipScope) -> Void
    let putBack: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Label(component.name, systemImage: "square.stack.3d.up")
                    .font(.subheadline.weight(.semibold))
                    .strikethrough(component.isLeftOut)
                    .foregroundStyle(component.isLeftOut ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.primary))
                Text(component.isLeftOut ? "Not making this" : "Made from the items below")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            if isWorking {
                ProgressView()
            } else if canEdit, component.isLeftOut {
                Button("Put Back", action: putBack)
                    .buttonStyle(.bordered)
                    .font(.subheadline)
                    .accessibilityLabel("Make \(component.name) Again")
            } else if canEdit {
                Menu {
                    Section("Don't Make \(component.name)") {
                        // A taste preference about this dish, so that's the first choice.
                        Button("Not for This Dish", systemImage: "fork.knife") { leaveOut(.recipe) }
                        Button("Never Make It", systemImage: "nosign") { leaveOut(.always) }
                    }
                } label: {
                    Image(systemName: "minus.circle")
                        .imageScale(.large)
                        .frame(minWidth: 44, minHeight: 44)
                }
                .accessibilityLabel("Don't Make \(component.name)")
            }
        }
    }
}

/// A meal's section header: its photo, when it has one, and its name.
private struct ShopMealHeader: View {
    let group: ShopMealLayout.Group

    var body: some View {
        HStack(spacing: 10) {
            if let image = group.meal?.imageURL.flatMap(URL.init(string:)) {
                RecipePhoto(url: image, aspectRatio: 1, pointWidth: 32, cornerRadius: 6)
                    .frame(width: 32, height: 32)
                    .accessibilityHidden(true)
            }
            Text(group.title)
        }
    }
}

/// What "Remove" offers for a line under a meal.
struct ShopRemoveOptions {
    let name: String
    var mealName: String?
    /// Leaves it out of this meal only; `nil` when it can't be (a pairing, a shared batch).
    var justThisMeal: (() -> Void)?
    var thisWeek: () -> Void
    var always: () -> Void
}

private struct ShopRemoveMenuItems: View {
    let options: ShopRemoveOptions

    var body: some View {
        Section("Remove \(options.name)") {
            if let justThisMeal = options.justThisMeal, let meal = options.mealName {
                Button("Just for \(meal)", systemImage: "fork.knife", action: justThisMeal)
            }
            Button("Just This Week", systemImage: "calendar", action: options.thisWeek)
            Button("Always", systemImage: "nosign", action: options.always)
        }
    }
}

private struct ShopRemoveMenu: View {
    let options: ShopRemoveOptions

    var body: some View {
        Menu {
            ShopRemoveMenuItems(options: options)
        } label: {
            Image(systemName: "minus.circle")
                .imageScale(.large)
                .foregroundStyle(.secondary)
                .frame(minWidth: 44, minHeight: 44)
        }
        .accessibilityLabel("Remove \(options.name)")
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
