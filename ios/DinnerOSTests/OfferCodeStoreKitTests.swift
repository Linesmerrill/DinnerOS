import Foundation
import StoreKit
import StoreKitTest
import Testing

@testable import DinnerOS

/// The free month's offer code against StoreKit's own test store, from DinnerOSPlus.storekit
/// (decision 755). This proves StoreKit loads both Plus plans from the file, with the two-week
/// trial, and that the file holds a one-month free offer code on each plan for new, lapsed and
/// current subscribers, the settings App Store Connect needs too. That Apple's redeem sheet
/// opens is checked by ReferralQATests, since another process draws it. A test purchase, with
/// or without the code offer, doesn't finish in this simulator (StoreKit answers notEntitled
/// or never returns), so a redemption's transaction isn't checked here. Nothing here can prove
/// App Store Connect's real codes: those only redeem once the app is live.
@MainActor
@Suite(.serialized)
struct OfferCodeStoreKitTests {
    static let monthly = "com.linesmerrill.dinneros.plus.monthly"
    static let yearly = "com.linesmerrill.dinneros.plus.yearly"

    @Test func bothPlusPlansLoadWithATwoWeekTrial() async throws {
        let session = try SKTestSession(configurationFileNamed: "DinnerOSPlus")
        session.disableDialogs = true
        defer { session.clearTransactions() }
        let products = try await Product.products(for: [Self.monthly, Self.yearly])
        #expect(Set(products.map(\.id)) == [Self.monthly, Self.yearly])
        for product in products {
            let offer = try #require(product.subscription?.introductoryOffer)
            #expect(offer.paymentMode == .freeTrial, "\(product.id): \(offer.paymentMode)")
            #expect(Self.days(offer.period) == 14, "\(product.id): \(offer.period.value) \(offer.period.unit)")
            #expect(product.subscription?.subscriptionGroupID == products.first?.subscription?.subscriptionGroupID)
        }
        let monthly = try #require(products.first { $0.id == Self.monthly })
        #expect(monthly.displayPrice.contains("4.99"))
    }

    @Test func eachPlanHasAOneMonthFreeOfferCodeForEveryone() throws {
        let url = try #require(
            Bundle(for: BundleMarker.self).url(forResource: "DinnerOSPlus", withExtension: "storekit"))
        let file = try JSONDecoder().decode(StoreKitFile.self, from: Data(contentsOf: url))
        let plans = file.subscriptionGroups.flatMap(\.subscriptions)
        #expect(Set(plans.map(\.productID)) == [Self.monthly, Self.yearly])
        for plan in plans {
            let code = try #require(plan.codeOffers.first, "\(plan.productID) has no offer code")
            #expect(code.paymentMode == "free" && code.subscriptionPeriod == "P1M")
            #expect(Set(code.eligibility) == ["new", "expired", "existing"])
        }
    }

    static func days(_ period: Product.SubscriptionPeriod) -> Int {
        switch period.unit {
        case .day: period.value
        case .week: period.value * 7
        case .month: period.value * 30
        case .year: period.value * 365
        @unknown default: period.value
        }
    }

    private final class BundleMarker {}

    /// The parts of a .storekit file these tests read.
    private struct StoreKitFile: Decodable {
        let subscriptionGroups: [Group]

        struct Group: Decodable {
            let subscriptions: [Plan]
        }

        struct Plan: Decodable {
            let productID: String
            let codeOffers: [CodeOffer]
        }

        struct CodeOffer: Decodable {
            let paymentMode: String
            let subscriptionPeriod: String
            let eligibility: [String]
        }
    }
}
