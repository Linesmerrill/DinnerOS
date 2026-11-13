import Foundation
import RevenueCat
import StoreKit

/// Picks how this build buys Plus: through RevenueCat when the build has its key, otherwise
/// prices only (decision 675).
nonisolated enum PlusPurchasers {
    /// The RevenueCat attribute naming the member who bought (the API's `StartedByAttribute`).
    static let startedByAttribute = "dinneros_user_id"

    static func make(apiKey: String?) -> any PlusPurchasing {
        guard let apiKey, !apiKey.isEmpty else { return StoreKitPlusPurchaser() }
        return RevenueCatPlusPurchaser(apiKey: apiKey)
    }
}

/// Buys Plus through RevenueCat. RevenueCat knows each purchase by the household's ID, so Plus
/// belongs to the household and its webhook tells the API (decision 673). Plans come from the
/// "default" offering's monthly and annual packages, which carry the `plus` entitlement.
nonisolated final class RevenueCatPlusPurchaser: PlusPurchasing, @unchecked Sendable {
    private let apiKey: String
    private let lock = NSLock()

    init(apiKey: String) {
        self.apiKey = apiKey
    }

    var canPurchase: Bool { true }

    /// Sets RevenueCat up on first use, so building the app's dependencies stays cheap.
    private func purchases() -> Purchases {
        lock.lock()
        defer { lock.unlock() }
        if !Purchases.isConfigured {
            Purchases.logLevel = .warn
            Purchases.configure(withAPIKey: apiKey)
        }
        return Purchases.shared
    }

    /// Makes RevenueCat file purchases under this household.
    private func purchases(for householdID: String) async throws -> Purchases {
        let purchases = purchases()
        if purchases.appUserID != householdID {
            _ = try await purchases.logIn(householdID)
        }
        return purchases
    }

    func products(ids: [String]) async -> [PlusProduct] {
        let purchases = purchases()
        var found: [StoreProduct] = []
        if let packages = try? await purchases.offerings().current?.availablePackages {
            found = packages.map(\.storeProduct).filter { ids.contains($0.productIdentifier) }
        }
        if found.isEmpty {
            found = await purchases.products(ids)
        }
        var out: [PlusProduct] = []
        for product in found {
            let eligibility = await purchases.checkTrialOrIntroDiscountEligibility(product: product)
            out.append(
                PlusProduct(
                    id: product.productIdentifier, displayPrice: product.localizedPriceString,
                    trialEligible: eligibility == .eligible))
        }
        return out
    }

    func purchase(productID: String, householdID: String, userID: String?) async throws -> PlusPurchaseResult {
        let purchases = try await purchases(for: householdID)
        // Tells the API, through the webhook, which member started Plus.
        if let userID {
            purchases.attribution.setAttributes([PlusPurchasers.startedByAttribute: userID])
        }
        let result: PurchaseResultData
        do {
            if let package = try? await purchases.offerings().current?.availablePackages.first(where: {
                $0.storeProduct.productIdentifier == productID
            }) {
                result = try await purchases.purchase(package: package)
            } else if let product = await purchases.products([productID]).first {
                result = try await purchases.purchase(product: product)
            } else {
                throw PlusPurchaseError.productMissing
            }
        } catch let error as ErrorCode {
            switch error {
            case .purchaseCancelledError: return .cancelled
            case .paymentPendingError: return .pending
            default: throw error
            }
        }
        return result.userCancelled ? .cancelled : .purchased
    }

    func restore(householdID: String) async throws {
        _ = try await purchases(for: householdID).restorePurchases()
    }
}
