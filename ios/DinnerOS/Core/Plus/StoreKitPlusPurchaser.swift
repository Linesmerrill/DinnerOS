import Foundation
import StoreKit

/// Reads Plus prices from the App Store with StoreKit. Buying stays closed until RevenueCat's
/// SDK and key are added (decision 675): a purchase RevenueCat never hears about would charge
/// the household without turning Plus on.
nonisolated struct StoreKitPlusPurchaser: PlusPurchasing {
    var canPurchase: Bool { false }

    func products(ids: [String]) async -> [PlusProduct] {
        guard let found = try? await Product.products(for: ids) else { return [] }
        var out: [PlusProduct] = []
        for product in found {
            let eligible = await product.subscription?.isEligibleForIntroOffer ?? false
            out.append(PlusProduct(id: product.id, displayPrice: product.displayPrice, trialEligible: eligible))
        }
        return out
    }

    func purchase(productID: String, householdID: String) async throws -> PlusPurchaseResult {
        throw PlusPurchaseError.notOpen
    }

    func restore(householdID: String) async throws {
        throw PlusPurchaseError.notOpen
    }
}
