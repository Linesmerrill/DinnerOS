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
    /// Set while DinnerOS gives the household Plus. Missing from an older API.
    var comp: PlusComp? = nil
    /// This household's progress toward the free month from an invite code it entered
    /// (decision 698). Missing from an older API, and when no code was entered.
    var referralProgress: ReferralProgress? = nil

    /// Whether a Plus feature is open: always while Plus is off.
    var hasPlus: Bool { !enabled || active }

    /// The plan shown when the server isn't reachable and nothing is saved: Plus off.
    static let off = PlusStatus(
        enabled: false, active: false, trial: false, expiresAt: nil, willRenew: false, billingIssue: false,
        freeUntil: nil, offer: nil)
}

/// A household that entered an invite code, on its way to the free month: what's left to do.
/// The words come from the API, from the same settings that decide when the invite counts.
nonisolated struct ReferralProgress: Codable, Hashable, Sendable {
    /// "pending", "qualified" or "expired".
    let state: String
    /// "Add a recipe and plan a dinner to unlock your free month".
    let message: String
    let steps: [Step]
    /// When a pending invite stops counting. Gone once setup is done.
    var expiresAt: Date? = nil
    /// "Free month: 1 of 3 done", for the Menu card. Missing from an older API.
    var summary: String? = nil
    /// "14 days left", while `expiresAt` is set.
    var timeLeft: String? = nil
    /// "What is DinnerOS Plus?" and a few lines under it, at the top of the checklist
    /// (decision 708). Missing from an older API.
    var plusTitle: String? = nil
    var plusLines: [String]? = nil

    var isPending: Bool { state == "pending" }

    /// The explainer's title, or the bundled one for an API that doesn't send it.
    var explainerTitle: String {
        if let plusTitle, !plusTitle.isEmpty { return plusTitle }
        return String(localized: "What is DinnerOS Plus?")
    }

    /// The explainer's lines, or the bundled ones for an API that doesn't send them.
    var explainerLines: [String] {
        if let plusLines, !plusLines.isEmpty { return plusLines }
        return Self.bundledPlusLines
    }

    static let bundledPlusLines = [
        String(localized: "Plus plans your week for you."),
        String(localized: "Autopilot picks dinners your household will like and fits them to your busy nights."),
        String(localized: "You also see what each week costs and what you saved."),
    ]

    /// The Menu card's line, worked out here for an API that doesn't send one.
    var summaryText: String {
        summary ?? "Free month: \(steps.filter(\.done).count) of \(steps.count) done"
    }

    nonisolated struct Step: Codable, Hashable, Sendable, Identifiable {
        let id: String
        let title: String
        let done: Bool
        var count: Int? = nil
        var target: Int? = nil
    }
}

/// Plus given by DinnerOS (decision 682), and the thank-you shown once for it.
nonisolated struct PlusComp: Codable, Hashable, Sendable {
    let reason: String
    let since: Date
    let thanksTitle: String
    let thanksText: String
    /// What Plus includes, the purchase screen's list. Missing from an older API.
    var benefitsTitle: String? = nil
    var benefits: [String]? = nil
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
    /// The message without its link, and the invite page, so the share sheet shows the link's
    /// preview. Missing from an older API, which sends the whole message only.
    var shareMessage: String? = nil
    var shareURL: URL? = nil
    /// Who used the code. Missing from an older API.
    var invites: PlusInvites? = nil
    /// "How it works", above the code. Missing from an older API.
    var howItWorks: HowItWorks? = nil
    /// This household's own progress, when it entered someone else's code.
    var referralProgress: ReferralProgress? = nil
    /// Code changes left in the next 30 days, and the limit. Missing from an older API, which
    /// can't change codes.
    var codeChangesLeft: Int? = nil
    var codeChangesLimit: Int? = nil
    /// When the code can change again, once no changes are left.
    var codeChangesNext: Date? = nil
    /// "You can change your code 4 more times in the next 30 days."
    var codeChangesText: String? = nil
    /// "Use 3 to 20 letters, numbers or hyphens."
    var codeHelp: String? = nil

    /// Whether this API lets the household change its code.
    var canChangeCode: Bool { codeChangesLeft != nil }

    nonisolated struct HowItWorks: Codable, Hashable, Sendable {
        let title: String
        let steps: [String]
    }

    private enum CodingKeys: String, CodingKey {
        case code, shareText, explainer, rewarded, monthsLeft, shareMessage, invites, howItWorks, referralProgress
        case codeChangesLeft, codeChangesLimit, codeChangesNext, codeChangesText, codeHelp
        case shareURL = "shareUrl"
    }
}

/// Whether a custom code can be used, checked as it's typed.
nonisolated struct ReferralCodeAvailability: Codable, Hashable, Sendable {
    let available: Bool
    /// "That code is taken."
    let message: String
}

/// A shared invite link, `https://dinneros.app/invite/<code>`: opens the invite screen with
/// the code filled in.
nonisolated enum ReferralLink {
    static let host = "dinneros.app"
    /// `dinneros://referral/CODE`: the same as the web link, for UI tests and links
    /// opened from inside the app.
    static let schemeHost = "referral"

    /// The code in `url`, or nil when it isn't an invite page link.
    static func code(in url: URL) -> String? {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        if components.scheme?.lowercased() == InviteLink.defaultScheme, components.host?.lowercased() == schemeHost {
            let parts = components.path.split(separator: "/")
            guard parts.count == 1 else { return nil }
            let code = InviteCode.normalize(String(parts[0]))
            return code.isEmpty ? nil : code
        }
        guard
            components.scheme?.lowercased() == "https",
            let host = components.host?.lowercased(), host == Self.host || host == "www." + Self.host
        else { return nil }
        let parts = components.path.split(separator: "/")
        guard parts.count == 2, parts[0] == "invite" else { return nil }
        let code = InviteCode.normalize(String(parts[1]))
        return code.isEmpty ? nil : code
    }
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
    /// Invites by state (decision 698). Missing from an older API.
    var pending: Int? = nil
    var qualified: Int? = nil
    var expired: Int? = nil
    /// Each household that used the code, newest first, with where it stands.
    var items: [PlusInviteItem]? = nil
}

/// One household that used this household's code: "Pending, needs 2 more recipes",
/// "Free month earned", or "Expired".
nonisolated struct PlusInviteItem: Codable, Hashable, Sendable, Identifiable {
    let id: String
    let householdName: String
    /// "pending", "qualified" or "expired".
    let state: String
    /// What's left, or the result, in the API's words. Empty for a plain "Pending".
    let detail: String
    let at: Date

    /// The state as a person says it.
    var stateTitle: String {
        switch state {
        case "qualified": String(localized: "Free month earned")
        case "expired": String(localized: "Expired")
        default: String(localized: "Pending")
        }
    }

    /// The line under the name: the detail, unless it only repeats the state.
    var detailLine: String? {
        let trimmed = detail.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, trimmed != stateTitle else { return nil }
        return trimmed
    }
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
    /// Codes used by state, and the footer that explains them (decision 698). Missing from an
    /// older API.
    var pending: Int? = nil
    var qualified: Int? = nil
    var expired: Int? = nil
    var footerText: String? = nil

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
        if status.comp != nil {
            return String(localized: "Plus is on the house. Thank you for testing.")
        }
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

    /// What the selected plan charges: "Free for 14 days, then $39.99 a year." with a trial.
    static func chargeLine(_ plan: PlusPlan, product: PlusProduct?, trialDays: Int) -> String {
        let price = Self.price(plan, product: product)
        guard trialDays > 0 else { return String(localized: "\(price). Renews until you cancel.") }
        return String(localized: "Free for \(trialDays) days, then \(price).")
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

extension PlusComp: Identifiable {
    var id: Date { since }
}

/// Who sent an invite link, shown before signing in (`GET /api/v1/referral-codes/{code}`).
nonisolated struct InvitePreview: Codable, Hashable, Sendable {
    let code: String
    var householdName: String? = nil
    /// "Lines invited you to DinnerOS".
    let title: String
    /// "You both get a free month of Plus once you're set up."
    let message: String
    /// The setup steps, in order.
    let steps: [String]
    /// Shown instead to a household that's already set up.
    let forExisting: Notice
    /// Shown instead to the household the code belongs to.
    let forOwn: Notice

    nonisolated struct Notice: Codable, Hashable, Sendable {
        let title: String
        let message: String
    }
}

/// An invite link that just opened, for the invite screen.
nonisolated struct IncomingInvite: Identifiable, Hashable, Sendable {
    let id = UUID()
    let code: String
}

/// An invite code kept on this device until a new household uses it: from a link, or typed
/// during setup. `accepted` is set once the person says yes to it.
nonisolated struct SavedInvite: Codable, Hashable, Sendable {
    let code: String
    var accepted: Bool
    /// When it was accepted. Only a household made after this uses it, so an existing
    /// household that signs in never picks it up by accident.
    var acceptedAt: Date?
    /// "Lines invited you to DinnerOS", for setup.
    var title: String?
}
