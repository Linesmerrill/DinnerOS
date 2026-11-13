import Foundation

/// A household's DinnerOS Plus (`GET /households/{id}/plus`, decisions 673 and 675).
nonisolated struct PlusStatus: Codable, Hashable, Sendable {
    /// `false` while Plus is switched off on the server: no purchase screen, everything open.
    let enabled: Bool
    let active: Bool
    let trial: Bool
    let expiresAt: Date?
    let willRenew: Bool
    let billingIssue: Bool
    /// When free months from invites end.
    let freeUntil: Date?
    /// The purchase screen's words, while Plus is on.
    let offer: PlusOffer?

    /// Whether a Plus feature is open: always while Plus is off.
    var hasPlus: Bool { !enabled || active }

    /// The plan shown when the server isn't reachable and nothing is saved: Plus off.
    static let off = PlusStatus(
        enabled: false, active: false, trial: false, expiresAt: nil, willRenew: false, billingIssue: false,
        freeUntil: nil, offer: nil)
}

/// The purchase screen's words, from the API so they change without an app update.
nonisolated struct PlusOffer: Codable, Hashable, Sendable {
    let title: String
    let subtitle: String
    let benefits: [String]
    let plans: [PlusPlan]
    let trialDays: Int
    let trialText: String
    let buyText: String
    let terms: String
    let restoreText: String
    let freeText: String
}

/// One way to pay for Plus.
nonisolated struct PlusPlan: Codable, Hashable, Sendable, Identifiable {
    let productID: String
    let name: String
    /// Shown when the App Store can't give the price.
    let priceText: String
    let note: String?

    var id: String { productID }

    private enum CodingKeys: String, CodingKey {
        case productID = "productId"
        case name, priceText, note
    }
}

/// The household's invite code (`GET /households/{id}/plus/referral`).
nonisolated struct PlusReferral: Codable, Hashable, Sendable {
    let code: String
    /// The share sheet's message.
    let shareText: String
    let explainer: String
    let rewarded: Int
    let monthsLeft: Int
}

/// A plan's price as the App Store gives it.
nonisolated struct PlusProduct: Hashable, Sendable {
    let id: String
    /// "$4.99", in the person's own currency.
    let displayPrice: String
    /// Whether the App Store still offers this person the free trial.
    let trialEligible: Bool
}

/// What happened when someone tapped buy.
nonisolated enum PlusPurchaseResult: Hashable, Sendable {
    case purchased
    /// Waiting on a parent's approval or the bank.
    case pending
    case cancelled
}

/// Words the Plus screens show that don't come from the API.
nonisolated enum PlusText {
    /// A status line for someone who has Plus.
    static func status(_ status: PlusStatus, now: Date = .now) -> String {
        if status.billingIssue {
            return String(localized: "Apple couldn't charge your last renewal. Check your payment in Settings.")
        }
        if status.trial, let end = status.expiresAt {
            return String(localized: "Free trial until \(end.formatted(date: .abbreviated, time: .omitted)).")
        }
        if let end = status.expiresAt, end > now {
            let date = end.formatted(date: .abbreviated, time: .omitted)
            return status.willRenew
                ? String(localized: "Renews \(date).") : String(localized: "Ends \(date).")
        }
        if let free = status.freeUntil, free > now {
            return String(localized: "Free until \(free.formatted(date: .abbreviated, time: .omitted)).")
        }
        return String(localized: "You have DinnerOS Plus.")
    }

    /// The plan's price line: the App Store's price when known, the API's words otherwise.
    static func price(_ plan: PlusPlan, product: PlusProduct?) -> String {
        guard let product else { return plan.priceText }
        if plan.productID.hasSuffix(".yearly") {
            return String(localized: "\(product.displayPrice) a year")
        }
        return String(localized: "\(product.displayPrice) a month")
    }
}
