import Foundation
import Observation
import os

/// What one week's plan contributes to its grocery list, and so to the Shop tab's match.
///
/// Deliberately not the whole plan: a note, a move to another day, or an `updatedAt` changes
/// the plan without changing a single quantity, and re-matching for those would put a request
/// behind edits that can't affect the list. What's left is what the list is built from — which
/// recipes, at what serving size, with which ingredient swaps.
nonisolated struct PlanGrocerySignature: Equatable, Sendable {
    /// One planned meal, as the grocery list sees it.
    struct Entry: Equatable, Sendable {
        let id: String
        let recipeID: String
        let servings: Int
        let customizations: [PlanEntryCustomization]
    }

    let week: String
    let entries: [Entry]

    init(_ plan: Plan) {
        week = plan.week
        entries = plan.entries.map {
            Entry(
                id: $0.id, recipeID: $0.recipe.id, servings: $0.servings,
                customizations: $0.customizations)
        }
    }
}

/// The current household's Walmart handoff: store setup, the week's match, cart links, and
/// "Did you order these?" (Phase 8a; docs/shopping-providers.md).
///
/// Main-actor state that lives as long as the app, like `PlanStore`. The match is a preview
/// the API doesn't store, so it's requested again whenever the tab loads, a product is saved,
/// or an order is confirmed. Check-offs live on the device (`GroceryCheckStorage`): the match
/// sends them so checked lines stay out of the cart, and confirming an order checks its lines
/// off so the grocery list doesn't also ask "Add to pantry?" for them.
@Observable
final class ShoppingStore {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    /// A handoff's cart links, opened one after another. Each adds to the same Walmart cart.
    struct LinkProgress: Equatable {
        let handoff: ShoppingHandoff
        fileprivate(set) var openedCount = 0

        var total: Int { handoff.cartLinks.count }
        /// The next link to open, from 0; `nil` once every link has been opened.
        var nextIndex: Int? { openedCount < total ? openedCount : nil }
        var isFinished: Bool { openedCount >= total }
    }

    let provider = ShoppingProviderKey.walmart

    private(set) var householdID: String?
    private(set) var week: ISOWeek

    /// Providers and store settings.
    private(set) var setupPhase: Phase = .idle
    private(set) var providers: [ShoppingProvider] = []
    private(set) var settings: ShoppingSettings?

    /// The shown week's match.
    private(set) var proposalPhase: Phase = .idle
    private(set) var proposal: ShoppingProposal?
    /// Set when a reload failed while content stayed on screen.
    private(set) var refreshError: String?
    /// Package counts the member changed, by `ingredientKey` (line IDs change between matches).
    private(set) var packageOverrides: [String: Int] = [:]
    /// Bumped whenever the shown week's plan changes in a way that changes its grocery list.
    /// The Shop tab's export section rebuilds its grocery list on it, the way the Menu rebuilds
    /// its cards from `planDidChange`.
    private(set) var planRevision = 0

    private(set) var isCreatingHandoff = false
    private(set) var linkProgress: LinkProgress?
    private(set) var linkError: String?
    /// Set when "Open in Walmart" found nothing new to add, so no link was opened.
    private(set) var linkNotice: String?
    /// "Send Again" or "Start Over" is in flight.
    private(set) var isResending = false

    /// The shown week's newest handoff with a line still pending, for the banner.
    private(set) var openHandoff: ShoppingHandoff?
    /// The handoff "Did you order these?" asks about. The app shell presents it.
    private(set) var confirmationPrompt: ShoppingHandoff?

    /// The shown week's order state, derived by the API on every read. `remind` is the only
    /// thing the banner keys off; nothing here is inferred from a handoff.
    private(set) var orderReminder: OrderReminder?
    private(set) var isSettingOrdered = false

    private(set) var preferencesPhase: Phase = .idle
    /// Saved products, by ingredient name.
    private(set) var preferences: [ShoppingPreference] = []
    private(set) var preferencesRefreshError: String?

    /// The store catalog behind "Don't see your store?", and this household's requests.
    private(set) var catalogPhase: Phase = .idle
    private(set) var catalog: [ShoppingCatalogItem] = []
    private(set) var storeRequests: [ShoppingStoreRequest] = []
    /// Cleared by a `404`: this API doesn't have the catalog yet, so the section hides instead
    /// of showing an error for something that isn't built.
    private(set) var isCatalogAvailable = true

    /// The shown week's cost, and every handoff of the week (any status) for adding prices.
    private(set) var costPhase: Phase = .idle
    private(set) var weekCost: WeekCost?
    private(set) var weekHandoffs: [ShoppingHandoff] = []
    /// Cleared by a `404`: this API has no cost endpoints yet, so the card hides.
    private(set) var isCostAvailable = true
    private(set) var isSavingSpend = false
    private(set) var savingsPhase: Phase = .idle
    private(set) var savings: ShoppingSavings?

    /// `shopping.edit`: store setup, saved products, and handoffs. The API enforces it.
    private(set) var canEdit = false
    /// `pantry.edit`: confirming an order records pantry purchases.
    private(set) var canConfirm = false

    /// This week in the household's time zone, as the Week tab counts it.
    var currentWeek: ISOWeek { .current(in: timeZone, now: now()) }
    var walmart: ShoppingProvider? { providers.first { $0.key == provider } }
    var isConfigured: Bool { settings?.provider == provider }
    var affiliateTracked: Bool { proposal?.affiliateTracked ?? walmart?.affiliateTracked ?? false }
    var readyLines: [ShoppingHandoffLine] { proposal?.lines ?? [] }
    var hasPackageEdits: Bool { !packageOverrides.isEmpty }

    /// Packages the next "Open in Walmart" adds for a line: its count, less what the week's
    /// hand-off already put in the cart.
    func addPackages(for line: ShoppingHandoffLine) -> Int {
        max(packages(for: line) - line.sentPackages, 0)
    }

    /// Packages in the cart beyond the line's count, which a link can't take back.
    func removePackages(for line: ShoppingHandoffLine) -> Int {
        max(line.sentPackages - packages(for: line), 0)
    }

    /// Lines the next "Open in Walmart" adds something for.
    var linesToAdd: [ShoppingHandoffLine] {
        readyLines.filter { addPackages(for: $0) > 0 }
    }

    /// This week's list already went to Walmart and nothing new would be added.
    var isEverythingInCart: Bool {
        proposal?.cart != nil && !readyLines.isEmpty && linesToAdd.isEmpty
    }

    /// Link progress for a handoff of the shown week.
    var weekLinkProgress: LinkProgress? {
        guard let linkProgress, linkProgress.handoff.week == week.description else { return nil }
        return linkProgress
    }

    /// After an order is confirmed, because it changed the pantry. The app refreshes the
    /// pantry and the unread count here.
    @ObservationIgnored var onPantryChanged: (@MainActor () async -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: ShoppingAPI?
    @ObservationIgnored private let checks: any GroceryCheckStorage
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private let openURL: @MainActor (URL) async -> Bool
    @ObservationIgnored private var timeZone: TimeZone = .autoupdatingCurrent
    /// The user the state was loaded for, so another sign-in never sees it.
    @ObservationIgnored private var userID: String?
    /// Handoffs answered "Not yet": not asked about again when the app returns until their
    /// links are opened again or the app relaunches. The banner still offers them.
    @ObservationIgnored private var dismissedHandoffIDs: Set<String> = []
    /// Incremented when the household or user changes, so a slow response can't write into
    /// the new scope.
    @ObservationIgnored private var scope = 0
    /// Incremented by every match, so an older match can't replace a newer one.
    @ObservationIgnored private var proposalGeneration = 0
    /// What each week's plan last contributed to its grocery list, so a plan that merely
    /// arrived again doesn't cost a match. Kept per week because `planDidChange` is handed
    /// every week's plan, not only the shown one.
    @ObservationIgnored private var planSignatures: [String: PlanGrocerySignature] = [:]
    /// The week whose cost was last asked for, so changes refresh it only once it's shown.
    @ObservationIgnored private var costWeek: ISOWeek?

    private static let logger = Logger(subsystem: "DinnerOS", category: "shopping")

    init(
        session: AuthSession, api: ShoppingAPI?, checks: any GroceryCheckStorage,
        now: @escaping () -> Date = Date.init, openURL: @escaping @MainActor (URL) async -> Bool
    ) {
        self.session = session
        self.api = api
        self.checks = checks
        self.now = now
        self.openURL = openURL
        week = .current(in: .autoupdatingCurrent, now: now())
    }

    /// A store frozen in the given state, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, settings: ShoppingSettings?, providers: [ShoppingProvider],
        proposal: ShoppingProposal? = nil, openHandoff: ShoppingHandoff? = nil, preferences: [ShoppingPreference] = [],
        catalog: [ShoppingCatalogItem] = [], storeRequests: [ShoppingStoreRequest] = [],
        orderReminder: OrderReminder? = nil
    ) -> ShoppingStore {
        let store = ShoppingStore(session: session, api: nil, checks: InMemoryGroceryChecks(), openURL: { _ in false })
        store.householdID = "household-preview"
        store.userID = session.currentUser?.id
        store.settings = settings
        store.providers = providers
        store.setupPhase = .loaded
        if let proposal {
            store.week = ISOWeek(proposal.week) ?? store.week
            store.proposal = proposal
            store.proposalPhase = .loaded
        }
        store.openHandoff = openHandoff
        store.orderReminder = orderReminder
        store.preferences = preferences
        store.preferencesPhase = .loaded
        store.catalog = catalog
        store.storeRequests = storeRequests
        store.catalogPhase = .loaded
        store.canEdit = true
        store.canConfirm = true
        return store
    }

    // MARK: - Household

    /// Shows `householdID`, starting at this week in `timeZone`. Another household or user
    /// clears what was loaded. Loads nothing; the Shop tab loads when it appears.
    func activate(householdID: String, timeZone: TimeZone) {
        self.timeZone = timeZone
        let currentUserID = session.currentUser?.id
        guard householdID != self.householdID || currentUserID != userID else { return }
        clear()
        self.householdID = householdID
        userID = currentUserID
        week = currentWeek
    }

    func setPermissions(canEdit: Bool, canConfirm: Bool) {
        self.canEdit = canEdit
        self.canConfirm = canConfirm
        if !canConfirm {
            confirmationPrompt = nil
        }
    }

    // MARK: - Loading

    /// Loads what the Shop tab needs and isn't loaded: providers and store settings, then the
    /// week's match and open handoff once a store is set.
    func load() async {
        if setupPhase != .loaded {
            await loadSetup(showingProgress: true)
        }
        guard setupPhase == .loaded, isConfigured else { return }
        if proposal == nil || proposalPhase != .loaded {
            await loadProposal(clearing: true)
        }
        await refreshOpenHandoff(presenting: false)
        await refreshOrderReminder()
    }

    /// Pull to refresh: content stays on screen until the responses arrive.
    func reload() async {
        await loadSetup(showingProgress: false)
        guard isConfigured else { return }
        await loadProposal(clearing: false)
        await refreshOpenHandoff(presenting: false)
        await refreshOrderReminder()
    }

    /// Shows `newWeek` and matches it. Changed package counts belong to the previous week.
    func show(week newWeek: ISOWeek) async {
        if newWeek != week {
            proposalGeneration += 1
            week = newWeek
            proposal = nil
            proposalPhase = .idle
            refreshError = nil
            packageOverrides = [:]
            linkError = nil
            linkNotice = nil
            openHandoff = nil
            // Each week has its own order state; the previous week's must not linger.
            orderReminder = nil
            clearWeekCost()
        }
        await load()
    }

    func showCurrentWeek() async {
        await show(week: currentWeek)
    }

    private func loadSetup(showingProgress: Bool) async {
        guard let api, let householdID else { return }
        let started = scope
        if showingProgress || setupPhase != .loaded {
            setupPhase = .loading
        }
        do {
            let loadedProviders = try await session.authorized { token in try await api.providers(accessToken: token) }
            let loadedSettings = try await session.authorized { token in
                try await api.settings(householdID: householdID, accessToken: token)
            }
            guard started == scope else { return }
            providers = loadedProviders
            settings = loadedSettings
            setupPhase = .loaded
        } catch is CancellationError {
            if started == scope, setupPhase == .loading { setupPhase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Shopping setup load failed: \(Self.describe(error), privacy: .public)")
            let message = Self.message(for: error)
            if setupPhase == .loaded {
                refreshError = message
            } else {
                setupPhase = .failed(message)
            }
        }
    }

    private func loadProposal(clearing: Bool) async {
        guard let api, let householdID, isConfigured else { return }
        proposalGeneration += 1
        let started = (proposalGeneration, scope)
        let week = week
        let provider = provider
        let request = ShoppingMatchRequest(checkedOffKeys: checkedOffKeys(for: week))
        if clearing || proposal == nil {
            proposal = nil
            proposalPhase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.match(
                    householdID: householdID, week: week, provider: provider, request: request, accessToken: token)
            }
            guard started == (proposalGeneration, scope) else { return }
            proposal = loaded
            refreshError = nil
            proposalPhase = .loaded
            let readyKeys = Set(loaded.lines.map(\.ingredientKey))
            packageOverrides = packageOverrides.filter { readyKeys.contains($0.key) }
        } catch is CancellationError {
            if started == (proposalGeneration, scope), proposalPhase == .loading { proposalPhase = .idle }
        } catch {
            guard started == (proposalGeneration, scope) else { return }
            Self.logger.notice("Shopping match failed: \(Self.describe(error), privacy: .public)")
            let message = Self.message(for: error)
            if proposalPhase == .loaded {
                refreshError = message
            } else {
                proposalPhase = .failed(message)
            }
        }
    }

    // MARK: - Store settings

    /// Chooses Walmart with an optional store number; empty text clears the store.
    func saveSettings(storeNumber: String) async throws {
        let (api, householdID) = try requireHousehold()
        let request = UpdateShoppingSettingsRequest(
            provider: provider, storeID: ShoppingStoreNumber.normalized(storeNumber))
        let started = scope
        let saved = try await session.authorized { token in
            try await api.updateSettings(householdID: householdID, settings: request, accessToken: token)
        }
        guard started == scope else { return }
        settings = saved
        Self.logger.info("Shopping store saved")
        // The store changes every cart link.
        await loadProposal(clearing: proposal == nil)
        await refreshOpenHandoff(presenting: false)
    }

    // MARK: - Package counts

    func packages(for line: ShoppingHandoffLine) -> Int {
        packageOverrides[line.ingredientKey] ?? line.packages
    }

    /// Changes a ready line's count, within 1–99. Setting it back to the match's count
    /// forgets the change.
    func setPackages(_ count: Int, for line: ShoppingHandoffLine) {
        let clamped = min(max(count, ShoppingLimits.packages.lowerBound), ShoppingLimits.packages.upperBound)
        packageOverrides[line.ingredientKey] = clamped == line.packages ? nil : clamped
    }

    /// The handoff body. Without changes it's the match's defaults. Once a count changed it
    /// names every ready line, because `lines` replaces the default candidates, with
    /// `packages` only where the member changed it.
    var handoffRequest: ShoppingMatchRequest {
        let keys = checkedOffKeys(for: week)
        guard hasPackageEdits else { return ShoppingMatchRequest(checkedOffKeys: keys) }
        let lines = readyLines.prefix(ShoppingLimits.maxKeys).map {
            ShoppingLineSelection(ingredientKey: $0.ingredientKey, packages: packageOverrides[$0.ingredientKey])
        }
        return ShoppingMatchRequest(lines: Array(lines), checkedOffKeys: keys)
    }

    // MARK: - Handoff

    /// Sends the shown week's list to its Walmart hand-off and opens the first cart link.
    ///
    /// Walmart's link adds to whatever the cart holds and DinnerOS can't read that cart, so the
    /// API keeps what the week already sent and links only what's new. When nothing is, no
    /// link opens and `linkNotice` says everything is already in the cart. The week is matched
    /// again afterwards so the sent lines move under "In Walmart Cart" on every phone.
    func openInWalmart() async throws {
        let (api, householdID) = try requireHousehold()
        guard !isCreatingHandoff else { return }
        let week = week
        let provider = provider
        let request = handoffRequest
        let started = scope
        isCreatingHandoff = true
        linkError = nil
        linkNotice = nil
        let handoff: ShoppingHandoff
        do {
            handoff = try await session.authorized { token in
                try await api.createHandoff(
                    householdID: householdID, week: week, provider: provider, request: request, accessToken: token)
            }
        } catch {
            if started == scope { isCreatingHandoff = false }
            throw error
        }
        guard started == scope else { return }
        isCreatingHandoff = false
        Self.logger.info("Shopping handoff sent with \(handoff.cartLinks.count, privacy: .public) links")
        if handoff.status == .open, handoff.week == self.week.description {
            openHandoff = handoff
        }
        guard !handoff.cartLinks.isEmpty else {
            linkProgress = nil
            linkNotice = ShoppingText.everythingInCart
            await loadProposal(clearing: false)
            return
        }
        linkProgress = LinkProgress(handoff: handoff)
        dismissedHandoffIDs.remove(handoff.id)
        await openNextCartLink()
        if handoff.week == self.week.description {
            await loadProposal(clearing: false)
        }
    }

    /// "Send Again": the member removed a line from the Walmart cart, so the next "Open in
    /// Walmart" adds it in full. The week is matched again, which moves it back to Ready.
    func sendAgain(_ line: ShoppingHandoffLine) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let week = week
        let provider = provider
        isResending = true
        defer {
            if started == scope { isResending = false }
        }
        try await session.authorized { token in
            try await api.sendLineAgain(
                householdID: householdID, week: week, provider: provider, ingredientKey: line.ingredientKey,
                accessToken: token)
        }
        guard started == scope, week == self.week else { return }
        Self.logger.info("Shopping line will be sent again")
        linkNotice = nil
        await loadProposal(clearing: false)
    }

    /// "Start Over": forgets what this week sent (the member emptied the Walmart cart) and
    /// sends every line again.
    func startOverAndSendEverything() async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let week = week
        let provider = provider
        isResending = true
        do {
            try await session.authorized { token in
                try await api.startOverHandoff(
                    householdID: householdID, week: week, provider: provider, accessToken: token)
            }
        } catch {
            if started == scope { isResending = false }
            throw error
        }
        guard started == scope, week == self.week else { return }
        isResending = false
        Self.logger.info("Shopping handoff started over")
        linkProgress = nil
        linkNotice = nil
        openHandoff = nil
        confirmationPrompt = nil
        await loadProposal(clearing: false)
        try await openInWalmart()
    }

    /// Opens the handoff's next cart link in Walmart (the app when installed, otherwise Safari).
    func openNextCartLink() async {
        guard var progress = linkProgress, let index = progress.nextIndex else { return }
        guard let url = progress.handoff.cartLinks[index].url else {
            linkError = String(localized: "This cart link is invalid. Try opening Walmart again.")
            return
        }
        let opened = await openURL(url)
        guard linkProgress?.handoff.id == progress.handoff.id else { return }
        if opened {
            progress.openedCount += 1
            dismissedHandoffIDs.remove(progress.handoff.id)
            linkProgress = progress
            linkError = nil
        } else {
            linkError = String(localized: "Couldn't open Walmart. Try again.")
        }
    }

    // MARK: - Did you order these?

    /// The app came back to the foreground, often from Walmart: asks about the week's open
    /// handoff, unless its links aren't all opened yet or the member said "Not yet".
    func appDidBecomeActive() async {
        await refreshOpenHandoff(presenting: true)
    }

    /// Reads the shown week's newest open handoff for the banner and, when `presenting`, asks
    /// about it. Best effort: a failure keeps the last state and is only logged.
    func refreshOpenHandoff(presenting: Bool) async {
        guard let api, let householdID else { return }
        let started = scope
        let week = week
        do {
            let items = try await session.authorized { token in
                try await api.handoffs(householdID: householdID, week: week, status: .open, accessToken: token)
            }
            guard started == scope, week == self.week else { return }
            let latest = items.first { $0.status == .open }
            openHandoff = latest
            if let prompt = confirmationPrompt, prompt.id != latest?.id {
                confirmationPrompt = nil
            }
            guard presenting, let latest, canConfirm, confirmationPrompt == nil,
                !dismissedHandoffIDs.contains(latest.id)
            else { return }
            if let progress = linkProgress, progress.handoff.id == latest.id, !progress.isFinished {
                return
            }
            confirmationPrompt = latest
        } catch is CancellationError {
            return
        } catch {
            Self.logger.notice("Open handoff check failed: \(Self.describe(error), privacy: .public)")
        }
    }

    /// Asks about the open handoff now, from the banner.
    func reviewOpenHandoff() {
        guard canConfirm, let openHandoff else { return }
        confirmationPrompt = openHandoff
    }

    /// "Not yet": closes the question without recording anything.
    func dismissConfirmation() {
        guard let prompt = confirmationPrompt else { return }
        dismissedHandoffIDs.insert(prompt.id)
        confirmationPrompt = nil
    }

    /// Records what was ordered. A `409` (another confirmation in progress) is retried once;
    /// confirming is idempotent per line, so a retry never records twice. Afterwards the
    /// confirmed lines are checked off on this device, the pantry refreshes, and the week is
    /// matched again.
    @discardableResult
    func confirm(_ request: ConfirmShoppingOrderRequest, for handoff: ShoppingHandoff) async throws
        -> ConfirmShoppingOrderResponse
    {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let response: ConfirmShoppingOrderResponse
        do {
            response = try await sendConfirmation(request, handoffID: handoff.id, api: api, householdID: householdID)
        } catch let error as APIError where error.status == 409 {
            Self.logger.notice("Order confirmation conflicted; retrying once")
            response = try await sendConfirmation(request, handoffID: handoff.id, api: api, householdID: householdID)
        }
        Self.logger.info("Order confirmed: \(response.purchases.count, privacy: .public) lines")
        guard started == scope else { return response }

        markCheckedOff(response.purchases.map(\.ingredientKey), week: response.handoff.week, householdID: householdID)
        let updated = response.handoff
        if let index = weekHandoffs.firstIndex(where: { $0.id == updated.id }) {
            weekHandoffs[index] = updated
        }
        if openHandoff?.id == updated.id {
            openHandoff = updated.status == .open ? updated : nil
        }
        if confirmationPrompt?.id == updated.id {
            confirmationPrompt = nil
        }
        if linkProgress?.handoff.id == updated.id {
            linkProgress = nil
        }
        await onPantryChanged?()
        if updated.week == week.description {
            await loadProposal(clearing: false)
            if costWeek == week {
                await loadWeekCost()
            }
        }
        return response
    }

    private func sendConfirmation(
        _ request: ConfirmShoppingOrderRequest, handoffID: String, api: ShoppingAPI, householdID: String
    ) async throws -> ConfirmShoppingOrderResponse {
        try await session.authorized { token in
            try await api.confirm(householdID: householdID, handoffID: handoffID, request: request, accessToken: token)
        }
    }

    // MARK: - Order reminder

    /// Reads the shown week's order state. Best effort: a failure keeps what's on screen and
    /// is only logged, because a missing banner isn't worth an error over the week's list.
    func refreshOrderReminder() async {
        guard let api, let householdID else { return }
        let started = scope
        let week = week
        do {
            let reminder = try await session.authorized { token in
                try await api.orderReminder(householdID: householdID, week: week, accessToken: token)
            }
            guard started == scope, week == self.week else { return }
            orderReminder = reminder
        } catch is CancellationError {
            return
        } catch {
            Self.logger.notice("Order reminder failed: \(Self.describe(error), privacy: .public)")
        }
    }

    /// Marks the shown week's groceries ordered, or takes that back. This is the only thing
    /// that silences the reminder: opening a Walmart cart link is not proof an order was
    /// placed, so nothing infers it.
    func setWeekOrdered(_ ordered: Bool) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let week = week
        isSettingOrdered = true
        defer {
            if started == scope { isSettingOrdered = false }
        }
        let reminder = try await session.authorized { token in
            try await api.setWeekOrdered(householdID: householdID, week: week, ordered: ordered, accessToken: token)
        }
        guard started == scope, week == self.week else { return }
        orderReminder = reminder
        // Marking also reads the bell's copy of the reminder server-side, so the badge is
        // refreshed here alongside the pantry.
        await onPantryChanged?()
        // Ordering checks out the Walmart cart, so the API closes the week's hand-off and the
        // next send starts fresh; taking the mark back reopens it. The match shows either.
        linkProgress = nil
        linkNotice = nil
        if isConfigured, proposal != nil {
            await loadProposal(clearing: false)
        }
    }

    // MARK: - Prices and week cost

    /// The shown week's handed-off lines that could carry a price: every line not marked
    /// "not ordered", in handoff then aisle order.
    var priceableLines: [PriceableLine] {
        weekHandoffs.reversed().flatMap { handoff in
            handoff.lines.filter { $0.confirmation?.status != .skipped }.map {
                PriceableLine(handoffID: handoff.id, line: $0)
            }
        }
    }

    /// Loads the shown week's handoffs and cost. Best effort: a failure keeps what's shown,
    /// and an API without the cost endpoints (`404`) hides the card instead of failing.
    func loadWeekCost() async {
        guard let api, let householdID else { return }
        let started = scope
        let week = week
        costWeek = week
        if weekCost == nil {
            costPhase = .loading
        }
        do {
            let handoffs = try await session.authorized { token in
                try await api.handoffs(householdID: householdID, week: week, accessToken: token)
            }
            guard started == scope, week == self.week else { return }
            weekHandoffs = handoffs
            let cost = try await session.authorized { token in
                try await api.weekCost(householdID: householdID, week: week, accessToken: token)
            }
            guard started == scope, week == self.week else { return }
            weekCost = cost
            isCostAvailable = true
            costPhase = .loaded
        } catch let error as APIError where error.status == 404 {
            guard started == scope, week == self.week else { return }
            Self.logger.info("Week cost isn't available on this API yet")
            weekCost = nil
            isCostAvailable = false
            costPhase = .loaded
        } catch is CancellationError {
            if started == scope, costPhase == .loading { costPhase = .idle }
        } catch {
            guard started == scope, week == self.week else { return }
            Self.logger.notice("Week cost load failed: \(Self.describe(error), privacy: .public)")
            costPhase = weekCost == nil ? .failed(Self.message(for: error)) : .loaded
        }
    }

    /// Sets the shown week's order total with fees, tax, and tip, or clears it with `nil`.
    func setOrderTotal(_ cents: Int?) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let week = week
        isSavingSpend = true
        defer {
            if started == scope { isSavingSpend = false }
        }
        let cost = try await session.authorized { token in
            try await api.setWeekSpend(householdID: householdID, week: week, orderTotalCents: cents, accessToken: token)
        }
        guard started == scope, week == self.week else { return }
        Self.logger.info("Week order total saved")
        weekCost = cost
        costWeek = week
        costPhase = .loaded
    }

    /// Saves line prices, one request per handoff, then reads the week's cost again. Saved
    /// products pick up the new per-package prices on the API, so they're reloaded when shown.
    func savePrices(_ prices: [PriceableLine.ID: Int?]) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let byHandoff = Dictionary(grouping: prices, by: \.key.handoffID)
        for (handoffID, entries) in byHandoff.sorted(by: { $0.key < $1.key }) {
            let lines = entries.sorted { $0.key.lineID < $1.key.lineID }.map {
                ShoppingLinePrice(lineID: $0.key.lineID, priceCents: $0.value)
            }
            for chunk in stride(from: 0, to: lines.count, by: ShoppingLimits.maxKeys) {
                let request = ShoppingLinePricesRequest(
                    lines: Array(lines[chunk..<min(chunk + ShoppingLimits.maxKeys, lines.count)]))
                let updated = try await session.authorized { token in
                    try await api.setLinePrices(
                        householdID: householdID, handoffID: handoffID, request: request, accessToken: token)
                }
                guard started == scope else { return }
                replaceHandoff(updated)
            }
        }
        Self.logger.info("Saved \(prices.count, privacy: .public) line prices")
        guard started == scope else { return }
        await loadWeekCost()
        if preferencesPhase == .loaded {
            await loadPreferences()
        }
    }

    /// Recent weeks' savings for the Savings screen.
    func loadSavings(limit: Int = 12) async {
        guard let api, let householdID else { return }
        let started = scope
        if savings == nil {
            savingsPhase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.savings(householdID: householdID, limit: limit, accessToken: token)
            }
            guard started == scope else { return }
            savings = loaded
            savingsPhase = .loaded
        } catch is CancellationError {
            if started == scope, savingsPhase == .loading { savingsPhase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Savings load failed: \(Self.describe(error), privacy: .public)")
            savingsPhase = savings == nil ? .failed(Self.message(for: error)) : .loaded
        }
    }

    private func replaceHandoff(_ updated: ShoppingHandoff) {
        if let index = weekHandoffs.firstIndex(where: { $0.id == updated.id }) {
            weekHandoffs[index] = updated
        }
        if openHandoff?.id == updated.id {
            openHandoff = updated.status == .open ? updated : nil
        }
    }

    private func clearWeekCost() {
        costWeek = nil
        costPhase = .idle
        weekCost = nil
        weekHandoffs = []
        isSavingSpend = false
    }

    // MARK: - Saved products

    func loadPreferences() async {
        guard let api, let householdID else { return }
        let started = scope
        let provider = provider
        if preferencesPhase != .loaded {
            preferencesPhase = .loading
        }
        do {
            let items = try await session.authorized { token in
                try await api.preferences(householdID: householdID, provider: provider, accessToken: token)
            }
            guard started == scope else { return }
            preferences = items
            preferencesRefreshError = nil
            preferencesPhase = .loaded
        } catch is CancellationError {
            if started == scope, preferencesPhase == .loading { preferencesPhase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Saved products load failed: \(Self.describe(error), privacy: .public)")
            let message = Self.message(for: error)
            if preferencesPhase == .loaded {
                preferencesRefreshError = message
            } else {
                preferencesPhase = .failed(message)
            }
        }
    }

    /// Saves the product for a grocery line, then matches the week again.
    @discardableResult
    func savePreference(ingredientKey: String, request: ShoppingPreferenceRequest) async throws -> ShoppingPreference {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let provider = provider
        let saved = try await session.authorized { token in
            try await api.savePreference(
                householdID: householdID, provider: provider, ingredientKey: ingredientKey, preference: request,
                accessToken: token)
        }
        guard started == scope else { return saved }
        Self.logger.info("Saved product saved")
        if preferencesPhase == .loaded {
            preferences = (preferences.filter { $0.ingredientKey != saved.ingredientKey } + [saved]).sorted {
                $0.ingredientName.localizedCaseInsensitiveCompare($1.ingredientName) == .orderedAscending
            }
        }
        await loadProposal(clearing: false)
        return saved
    }

    /// Removes a saved product. One someone else already removed (`404`) counts as removed.
    func deletePreference(ingredientKey: String) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let provider = provider
        do {
            try await session.authorized { token in
                try await api.deletePreference(
                    householdID: householdID, provider: provider, ingredientKey: ingredientKey, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            Self.logger.notice("Saved product was already removed")
        }
        guard started == scope else { return }
        preferences.removeAll { $0.ingredientKey == ingredientKey }
        await loadProposal(clearing: false)
    }

    // MARK: - Store requests

    /// Asking for a store needs only membership: it changes nothing the household owns, and
    /// the API counts demand per household.
    var canRequestStore: Bool { householdID != nil }

    /// The catalog with this household's requests folded in, so a row reads "Requested" even
    /// when the response was cached before the request.
    var catalogWithRequests: [ShoppingCatalogItem] {
        let requestedKeys = Set(storeRequests.compactMap(\.key))
        return catalog.map { item in
            guard requestedKeys.contains(item.key), !item.requestedByHousehold else { return item }
            return ShoppingCatalogItem(
                key: item.key, name: item.name, kind: item.kind, status: item.status, aliases: item.aliases,
                note: item.note, requestedByHousehold: true, requests: max(item.requests, 1))
        }
    }

    /// This household's request for a catalog entry, which Undo takes back.
    func storeRequest(forKey key: String) -> ShoppingStoreRequest? {
        storeRequests.first { $0.key == key }
    }

    /// Loads the catalog and this household's requests. A `404` means the API doesn't have
    /// the endpoints yet, which hides the section rather than failing.
    func loadCatalog() async {
        guard let api, let householdID else { return }
        let started = scope
        if catalogPhase != .loaded {
            catalogPhase = .loading
        }
        do {
            let items = try await session.authorized { token in try await api.catalog(accessToken: token) }
            let requests = try await session.authorized { token in
                try await api.storeRequests(householdID: householdID, accessToken: token)
            }
            guard started == scope else { return }
            catalog = items
            storeRequests = requests
            isCatalogAvailable = true
            catalogPhase = .loaded
        } catch let error as APIError where error.status == 404 {
            guard started == scope else { return }
            Self.logger.info("Store catalog isn't available on this API yet")
            catalog = []
            storeRequests = []
            isCatalogAvailable = false
            catalogPhase = .loaded
        } catch is CancellationError {
            if started == scope, catalogPhase == .loading { catalogPhase = .idle }
        } catch {
            guard started == scope else { return }
            Self.logger.notice("Store catalog load failed: \(Self.describe(error), privacy: .public)")
            catalogPhase = .failed(Self.message(for: error))
        }
    }

    /// Asks for a store. The row shows "Requested" as soon as this returns.
    @discardableResult
    func requestStore(_ request: CreateShoppingStoreRequest) async throws -> ShoppingStoreRequest {
        let (api, householdID) = try requireHousehold()
        let started = scope
        let created = try await session.authorized { token in
            try await api.createStoreRequest(householdID: householdID, request: request, accessToken: token)
        }
        guard started == scope else { return created }
        Self.logger.info("Store requested")
        storeRequests.removeAll { $0.id == created.id }
        storeRequests.append(created)
        applyRequestedCount(forKey: created.key, delta: 1, requested: true)
        return created
    }

    /// Undo: takes a request back. One someone else already removed (`404`) counts as removed.
    func undoStoreRequest(_ request: ShoppingStoreRequest) async throws {
        let (api, householdID) = try requireHousehold()
        let started = scope
        do {
            try await session.authorized { token in
                try await api.deleteStoreRequest(
                    householdID: householdID, requestID: request.id, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            Self.logger.notice("Store request was already taken back")
        }
        guard started == scope else { return }
        storeRequests.removeAll { $0.id == request.id }
        applyRequestedCount(forKey: request.key, delta: -1, requested: false)
    }

    /// Keeps the shown count and flag in step with a request the member just made or undid,
    /// so the row doesn't need the catalog loaded again.
    private func applyRequestedCount(forKey key: String?, delta: Int, requested: Bool) {
        guard let key, let index = catalog.firstIndex(where: { $0.key == key }) else { return }
        let item = catalog[index]
        catalog[index] = ShoppingCatalogItem(
            key: item.key, name: item.name, kind: item.kind, status: item.status, aliases: item.aliases,
            note: item.note, requestedByHousehold: requested, requests: max(item.requests + delta, 0))
    }

    // MARK: - Plan changes

    /// Takes a plan `PlanStore` received, the way `MenuStore.applyPlan` does.
    ///
    /// The match is computed from the week's grocery list, so a serving size changed on the
    /// Menu changes every quantity here — and nothing else would ever ask again: the tab's
    /// `.task` is keyed on the household, and `load()` keeps a proposal it already has. So the
    /// shown week re-matches, and `planRevision` tells the export section to rebuild its list.
    ///
    /// Only a real change costs a request. `planDidChange` fires on every load and every edit
    /// of any week, including plans that are byte-for-byte what we already matched, so the
    /// signature is compared first: what's planned, at what size, cooked which way.
    func planDidChange(_ plan: Plan) async {
        guard plan.householdID == householdID else { return }
        let signature = PlanGrocerySignature(plan)
        let previous = planSignatures[plan.week]
        guard signature != previous else { return }
        planSignatures[plan.week] = signature
        guard plan.week == week.description else { return }
        planRevision += 1
        // The first plan for this week is the one the tab's own load matches anyway.
        guard previous != nil, proposalPhase == .loaded, isConfigured else { return }
        await loadProposal(clearing: false)
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        userID = nil
        dismissedHandoffIDs = []
        canEdit = false
        canConfirm = false
    }

    private func clear() {
        scope += 1
        proposalGeneration += 1
        setupPhase = .idle
        providers = []
        settings = nil
        proposalPhase = .idle
        proposal = nil
        refreshError = nil
        packageOverrides = [:]
        isCreatingHandoff = false
        linkProgress = nil
        linkError = nil
        linkNotice = nil
        isResending = false
        openHandoff = nil
        confirmationPrompt = nil
        orderReminder = nil
        isSettingOrdered = false
        preferencesPhase = .idle
        preferences = []
        preferencesRefreshError = nil
        catalogPhase = .idle
        catalog = []
        storeRequests = []
        isCatalogAvailable = true
        planSignatures = [:]
        clearWeekCost()
        isCostAvailable = true
        savingsPhase = .idle
        savings = nil
    }

    // MARK: - Helpers

    /// Lines checked off on this device's grocery list for `week`, which the match leaves out.
    private func checkedOffKeys(for week: ISOWeek) -> [String]? {
        guard let householdID else { return nil }
        let keys = checks.checkedItems(householdID: householdID, week: week).sorted()
        return keys.isEmpty ? nil : Array(keys.prefix(ShoppingLimits.maxKeys))
    }

    private func markCheckedOff(_ keys: [String], week: String, householdID: String) {
        guard !keys.isEmpty, let week = ISOWeek(week) else { return }
        let checked = checks.checkedItems(householdID: householdID, week: week).union(keys)
        checks.setCheckedItems(checked, householdID: householdID, week: week)
    }

    private func requireHousehold() throws -> (ShoppingAPI, String) {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let householdID else { throw AuthSessionError.signedOut }
        return (api, householdID)
    }

    /// Like `HouseholdStore.message(for:)`, with shopping's meaning of `provider_unavailable`.
    static func message(for error: any Error) -> String {
        if (error as? APIError)?.code == "provider_unavailable" {
            return String(localized: "Walmart isn't available right now. Try again later.")
        }
        return HouseholdStore.message(for: error)
    }

    /// Status and error code only; never tokens, links, or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}
