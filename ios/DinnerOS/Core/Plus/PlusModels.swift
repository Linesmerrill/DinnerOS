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
    /// The privacy policy and terms of use. Missing from an older API.
    var links: [PlusLink]? = nil

    /// The links to show: the API's, or the website's pages when it sent none. Apple requires
    /// both on the purchase screen.
    var shownLinks: [PlusLink] {
        guard let links, !links.isEmpty else { return PlusLink.defaults }
        return links
    }
}

/// A web page the purchase screen links to.
nonisolated struct PlusLink: Codable, Hashable, Sendable {
    let title: String
    let url: URL

    static let defaults: [PlusLink] = [
        ("Privacy Policy", "https://dinneros.app/privacy/"), ("Terms of Use", "https://dinneros.app/terms/"),
    ].compactMap { title, address in URL(string: address).map { PlusLink(title: title, url: $0) } }
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
    /// Who used the code. Missing from an older API.
    var invites: PlusInvites? = nil
}

/// The invite screen's Your Invites section. Other households are never named.
nonisolated struct PlusInvites: Codable, Hashable, Sendable {
    /// Households that used the code.
    let joined: Int
    let monthsEarned: Int
    let monthsCap: Int
    /// "3 of 12".
    let capText: String
    /// When free months end, while they last.
    let freeUntil: Date?
    /// Shown when no one has used the code.
    let emptyText: String
    /// The most recent, newest first.
    let joins: [PlusInviteJoin]
    /// Set when this household joined with another's code.
    let usedCode: PlusUsedCode?
}

/// One household that used the code.
nonisolated struct PlusInviteJoin: Codable, Hashable, Sendable {
    let at: Date
    /// False once the code had earned every free month.
    let counted: Bool
    /// "Counted" or "Limit reached".
    let status: String
}

/// This household's own use of another household's code.
nonisolated struct PlusUsedCode: Codable, Hashable, Sendable {
    let at: Date
    /// When the free month from it ends.
    let freeUntil: Date
}

/// Referrals across every household (`GET /plus/referrals/summary`), for the people who run
/// DinnerOS only.
nonisolated struct PlusReferralSummary: Codable, Hashable, Sendable {
    let codesUsed: Int
    /// Codes used in the last 7 days.
    let joinedThisWeek: Int
    let joinedAllTime: Int
    let topReferrers: [Referrer]

    nonisolated struct Referrer: Codable, Hashable, Sendable {
        let name: String
        let count: Int
    }
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

    /// "Nov 13, 2026".
    static func day(_ date: Date) -> String {
        date.formatted(date: .abbreviated, time: .omitted)
    }

    /// The line for a household that joined with another's code.
    static func usedCode(_ used: PlusUsedCode, now: Date = .now) -> String {
        used.freeUntil > now
            ? String(localized: "You joined with a code. Free month of Plus until \(day(used.freeUntil)).")
            : String(localized: "You joined with a code. Your free month of Plus ended \(day(used.freeUntil)).")
    }

    /// "W57ZVGUJ" from "W57Z VGUJ": what the copy button puts on the clipboard.
    static func copyableCode(_ code: String) -> String {
        code.filter { !$0.isWhitespace }
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

/// Your Plus (`GET /households/{id}/plus/details`, decision 680). The words come from the API.
nonisolated struct PlusDetails: Codable, Hashable, Sendable {
    let status: PlusStatus
    let plan: Plan?
    /// trial, active, canceled, expired, free, or none.
    let state: String
    let stateText: String
    /// "Renews on", "Ends on", "Ended on" or "Free until", with `date`.
    let dateLabel: String?
    let date: Date?
    let since: Date?
    let sinceText: String
    /// "Founding member".
    let badge: String?
    let startedBy: String?
    /// False for members who see status only.
    let canManage: Bool
    let manageNote: String?
    let history: [HistoryItem]
    let historyNote: String
    let stats: [Stat]
    let statsTitle: String
    let monthsEarned: Int
    let monthsCap: Int

    nonisolated struct Plan: Codable, Hashable, Sendable {
        let productID: String
        let name: String
        let priceText: String

        private enum CodingKeys: String, CodingKey {
            case productID = "productId"
            case name, priceText
        }
    }

    nonisolated struct HistoryItem: Codable, Hashable, Sendable {
        let at: Date
        let title: String
        let amount: Decimal?
        let currency: String?
        /// billing or invite.
        let kind: String

        var isInvite: Bool { kind == "invite" }

        /// "$4.99" in the charge's own currency.
        var amountText: String? {
            guard let amount, let currency, !currency.isEmpty else { return nil }
            return amount.formatted(.currency(code: currency))
        }
    }

    nonisolated struct Stat: Codable, Hashable, Sendable {
        let value: Int?
        let cents: Int64?
        let label: String
        let symbol: String

        /// "12" or "$42".
        var valueText: String {
            if let cents {
                return (Decimal(cents) / 100).formatted(.currency(code: "USD").precision(.fractionLength(0)))
            }
            return (value ?? 0).formatted()
        }
    }
}

/// The thank-you screen after buying Plus. Shown once per purchase, never after a restore.
nonisolated struct PlusCelebration: Identifiable, Hashable, Sendable {
    let id = UUID()
    let productID: String
    /// What Plus unlocks, from the offer.
    let benefits: [String]
}
