import Foundation
import Observation
import os

/// Buys and restores Plus. The App Store sells it; RevenueCat tells the API, which is the only
/// place Plus turns on (decision 673), so the app never grants Plus itself.
protocol PlusPurchasing: Sendable {
    /// Whether buying works in this build: `false` until RevenueCat is set up.
    var canPurchase: Bool { get }
    /// The App Store's prices for `ids`; empty when the App Store can't be reached.
    func products(ids: [String]) async -> [PlusProduct]
    /// `userID` is the member buying; the API shows them as who started Plus.
    func purchase(productID: String, householdID: String, userID: String?) async throws -> PlusPurchaseResult
    func restore(householdID: String) async throws
}

/// Why buying didn't start.
nonisolated enum PlusPurchaseError: Error, Equatable, LocalizedError {
    /// The build has no RevenueCat key yet.
    case notOpen
    case productMissing

    var errorDescription: String? {
        switch self {
        case .notOpen: String(localized: "DinnerOS Plus isn't for sale yet.")
        case .productMissing: String(localized: "The App Store didn't answer. Try again in a minute.")
        }
    }
}

/// DinnerOS Plus for the current household: whether it has Plus, the purchase screen's words,
/// prices, and the invite code (decision 675). While Plus is off on the server, everything is
/// open and nothing here shows.
@Observable
final class PlusStore {
    private(set) var status: PlusStatus?
    private(set) var referral: PlusReferral?
    /// Your Plus.
    private(set) var details: PlusDetails?
    /// The thank-you screen, showing. Set from `pendingCelebration` once the purchase screen
    /// closes, so it shows once per purchase and never after a restore.
    var celebration: PlusCelebration?
    /// Waiting for the purchase screen to close.
    var pendingCelebration: PlusCelebration?
    /// App Store prices by product ID.
    private(set) var products: [String: PlusProduct] = [:]
    private(set) var isPurchasing = false
    /// Shows the purchase screen. Set when a tapped feature is part of Plus.
    var showsPurchase = false

    /// Whether Plus is switched on, so its screens show.
    var isEnabled: Bool { status?.enabled == true }
    /// Whether Plus features are open: yes while Plus is off or not yet known.
    var hasPlus: Bool { status?.hasPlus ?? true }
    var canPurchase: Bool { purchaser.canPurchase }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PlusAPI?
    @ObservationIgnored private let purchaser: any PlusPurchasing
    @ObservationIgnored private var householdID: String?
    /// How long to wait between status checks after a purchase, while RevenueCat tells the API.
    @ObservationIgnored var confirmDelay: Duration = .seconds(2)

    private static let logger = Logger(subsystem: "DinnerOS", category: "plus")

    init(session: AuthSession, api: PlusAPI?, purchaser: any PlusPurchasing) {
        self.session = session
        self.api = api
        self.purchaser = purchaser
    }

    /// Loads the household's Plus. Offline, the saved answer stands in; with none, Plus is
    /// treated as off so nothing is locked.
    func activate(householdID: String) async {
        if householdID != self.householdID {
            self.householdID = householdID
            status = nil
            referral = nil
            details = nil
        }
        await reload()
        #if DEBUG
            // Simulator check of the thank-you screen without an App Store purchase.
            if ProcessInfo.processInfo.arguments.contains("-PlusThanksCheck"), status?.active == true {
                celebration = PlusCelebration(
                    productID: "com.linesmerrill.dinneros.plus.monthly", benefits: status?.offer?.benefits ?? [])
            }
        #endif
    }

    func reload() async {
        guard let api, let householdID else { return }
        do {
            let loaded = try await session.authorized { token in
                try await api.status(householdID: householdID, accessToken: token)
            }
            guard householdID == self.householdID else { return }
            status = loaded
            if loaded.enabled, products.isEmpty, let offer = loaded.offer {
                await loadProducts(offer)
            }
        } catch let error as APIError where error.status == 404 {
            // An API from before Plus.
            status = .off
        } catch {
            Self.logger.notice("Plus status failed: \(String(describing: error), privacy: .public)")
        }
    }

    /// Opens the purchase screen when `error` says the feature is part of Plus. Returns whether
    /// it did, so the caller shows nothing else.
    @discardableResult
    func handle(_ error: Error) -> Bool {
        guard Self.isPlusRequired(error) else { return false }
        showsPurchase = true
        return true
    }

    static func isPlusRequired(_ error: Error) -> Bool {
        (error as? APIError)?.code == "plus_required"
    }

    // MARK: Buying

    /// Buys a plan, then waits for the API to hear about it. Returns whether Plus is on.
    func purchase(productID: String) async throws -> Bool {
        guard let householdID else { return false }
        isPurchasing = true
        defer { isPurchasing = false }
        switch try await purchaser.purchase(
            productID: productID, householdID: householdID, userID: session.currentUser?.id)
        {
        case .cancelled, .pending:
            return false
        case .purchased:
            Self.logger.info("Plus purchased")
            let on = await waitForPlus()
            if on {
                pendingCelebration = PlusCelebration(productID: productID, benefits: status?.offer?.benefits ?? [])
            }
            return on
        }
    }

    func restore() async throws -> Bool {
        guard let householdID else { return false }
        isPurchasing = true
        defer { isPurchasing = false }
        try await purchaser.restore(householdID: householdID)
        return await waitForPlus()
    }

    /// RevenueCat tells the API within seconds; check a few times before giving up.
    private func waitForPlus() async -> Bool {
        for attempt in 0..<5 {
            if attempt > 0 { try? await Task.sleep(for: confirmDelay) }
            await reload()
            if status?.active == true { return true }
        }
        return false
    }

    private func loadProducts(_ offer: PlusOffer) async {
        let loaded = await purchaser.products(ids: offer.plans.map(\.productID))
        products = Dictionary(loaded.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
    }

    /// Shows the thank-you screen for a purchase that just finished.
    func showPendingCelebration() {
        guard let pending = pendingCelebration else { return }
        pendingCelebration = nil
        celebration = pending
    }

    // MARK: Your Plus

    func loadDetails() async throws {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        let loaded = try await session.authorized { token in
            try await api.details(householdID: householdID, accessToken: token)
        }
        guard householdID == self.householdID else { return }
        details = loaded
        status = loaded.status
    }

    // MARK: Invites

    func loadReferral() async throws {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        let loaded = try await session.authorized { token in
            try await api.referral(householdID: householdID, accessToken: token)
        }
        guard householdID == self.householdID else { return }
        referral = loaded
    }

    /// Uses another household's code. Both households get a free month.
    func redeem(code: String) async throws {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        let updated = try await session.authorized { token in
            try await api.redeem(householdID: householdID, code: code, accessToken: token)
        }
        guard householdID == self.householdID else { return }
        status = updated
        Self.logger.info("Invite code used")
        // Your Invites now shows the code this household used.
        try? await loadReferral()
    }

    /// Referrals across every household, or nil for anyone who doesn't run DinnerOS or an API
    /// without the route.
    func referralSummary() async throws -> PlusReferralSummary? {
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            return try await session.authorized { token in try await api.referralSummary(accessToken: token) }
        } catch let error as APIError where error.status == 403 || error.status == 404 {
            return nil
        }
    }
}

#if DEBUG
    extension PlusStore {
        /// Sets state directly, for previews and tests.
        func preview(status: PlusStatus?, referral: PlusReferral? = nil, products: [PlusProduct] = []) {
            self.status = status
            self.referral = referral
            self.products = Dictionary(products.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        }
    }
#endif
