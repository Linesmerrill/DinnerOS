import Foundation
import StoreKit

/// Reads Plus prices from the App Store with StoreKit, for a build without RevenueCat's key.
/// Buying stays closed (decision 675): a purchase RevenueCat never hears about would charge the
/// household without turning Plus on.
nonisolated struct StoreKitPlusPurchaser: PlusPurchasing {
    var canPurchase: Bool { false }

    func products(ids: [String]) async -> [PlusProduct] {
        guard let found = try? await Product.products(for: ids) else { return [] }
        var out: [PlusProduct] = []
        for product in found {
            let subscription = product.subscription
            let eligible = await subscription?.isEligibleForIntroOffer ?? false
            let trial = subscription?.introductoryOffer.flatMap { $0.paymentMode == .freeTrial ? $0.period : nil }
            out.append(
                PlusProduct(
                    id: product.id, displayPrice: product.displayPrice, trialEligible: eligible,
                    trialDays: trial.map(Self.days),
                    billedYearly: subscription.map { $0.subscriptionPeriod.unit == .year }))
        }
        return out
    }

    /// Days in a period, for the trial: a week is 7, a month 30, a year 365.
    static func days(_ period: Product.SubscriptionPeriod) -> Int {
        switch period.unit {
        case .day: period.value
        case .week: period.value * 7
        case .month: period.value * 30
        case .year: period.value * 365
        @unknown default: period.value
        }
    }

    func purchase(productID: String, householdID: String, userID: String?) async throws -> PlusPurchaseResult {
        throw PlusPurchaseError.notOpen
    }

    func restore(householdID: String) async throws {
        throw PlusPurchaseError.notOpen
    }
}
