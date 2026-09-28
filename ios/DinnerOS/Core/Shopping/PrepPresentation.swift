import Foundation

/// What one prep card shows, apart from the view: the amount to keep out, the freezer bags
/// at the chosen count, the leftover line, the button's name, and what VoiceOver says
/// (docs/shopping-providers.md#the-prep-plan).
///
/// The card is a picture of the counter, not a paragraph: a "this week" amount to weigh out
/// and one frozen bag per future dinner. Every number here comes from the API — the bag size,
/// its thaw time, and the leftover each travel with their count — so changing the count can
/// never show another count's thaw time or leftover.
nonisolated struct PrepCardPresentation: Equatable, Sendable {
    let card: PrepCard
    /// The bag count in effect: the member's choice, or the API's suggestion, always within
    /// `bagRange`.
    let bags: Int
    let locale: Locale

    init(card: PrepCard, chosenBags: Int? = nil, locale: Locale = .autoupdatingCurrent) {
        self.card = card
        self.locale = locale
        let suggested = card.portions?.portions ?? 0
        let range = Self.bagRange(for: card)
        self.bags = min(max(chosenBags ?? suggested, range.lowerBound), range.upperBound)
    }

    // MARK: - Bag count

    /// The most whole dinner-sized bags the surplus holds; zero when it holds none, or when
    /// the card isn't freezable.
    var maxBags: Int { Self.bagRange(for: card).upperBound }

    /// The counts the add/remove controls move through. At least one bag whenever there is
    /// one to freeze: "none" is Skip, and a finished card with no bags would read as the
    /// suggestion to the API.
    var bagRange: ClosedRange<Int> { Self.bagRange(for: card) }

    var canAddBag: Bool { bags < bagRange.upperBound }
    var canRemoveBag: Bool { bags > bagRange.lowerBound }
    /// True when there is a bag row at all.
    var hasBags: Bool { bags > 0 }

    private static func bagRange(for card: PrepCard) -> ClosedRange<Int> {
        guard card.freezable, let plan = card.portions else { return 0...0 }
        let most = max(plan.options.map(\.portions).max() ?? 0, plan.portions)
        return most > 0 ? 1...most : 0...0
    }

    // MARK: - Amounts

    /// The pack line under the name, "24 oz pack".
    var packText: String? {
        guard let bought = card.boughtText, !bought.isEmpty else { return nil }
        return String(localized: "\(bought) pack")
    }

    /// The "this week" readout, "10 oz".
    var keepText: String {
        if let reserved = card.portions?.reservedText, !reserved.isEmpty { return reserved }
        if let needed = card.neededText, !needed.isEmpty { return needed }
        return card.needed + " " + card.unit
    }

    /// Who the kept amount is for: "for Tuesday's Citrus Pork Tacos". Meals already cooked
    /// are left out when any are still ahead; a card with no planned meals says "this week".
    var keepCaption: String {
        String(localized: "for \(mealsPhrase)")
    }

    /// One bag's readout, "10 oz" — every bag is one dinner, so they are all the same size.
    var bagText: String {
        guard let plan = card.portions else { return "" }
        return plan.option(bags)?.sizeText ?? plan.portionSizeText
    }

    /// One bag's thaw time, short enough for a small card: "about 3 hr to thaw".
    var thawText: String {
        guard let thaw else { return "" }
        if thaw.hours > 0, thaw.hours < 24 {
            return String(localized: "about \(thaw.hours) hr to thaw")
        }
        return String(localized: "\(thaw.summary) to thaw")
    }

    /// The small line under the bags when less than a dinner is left:
    /// "4 oz left over — toss it or cook it in". `nil` when nothing is.
    var leftoverLine: String? {
        guard let leftover = leftoverText else { return nil }
        return String(localized: "\(leftover) left over — toss it or cook it in")
    }

    /// Names what the button actually does: whole dinner-sized bags, or just "Done".
    var buttonTitle: String {
        switch bags {
        case 0: String(localized: "Done")
        case 1: String(localized: "Freeze 1 Bag")
        default: String(localized: "Freeze \(bags) Bags")
        }
    }

    /// "1 bag to freeze", for the banner and the count announcement.
    var bagCountText: String {
        bags == 1 ? String(localized: "1 bag to freeze") : String(localized: "\(bags) bags to freeze")
    }

    // MARK: - VoiceOver

    /// "Keep 10 ounces of ground pork out for Tuesday's Citrus Pork Tacos".
    var keepAccessibilityLabel: String {
        String(
            localized: "Keep \(Self.spoken(keepText)) of \(card.name.lowercased(with: locale)) out for \(mealsPhrase)")
    }

    /// "Freezer bag 1 of 2, 10 ounces, about 3 hours to thaw".
    func bagAccessibilityLabel(_ index: Int) -> String {
        let size = Self.spoken(bagText)
        guard let thaw else { return String(localized: "Freezer bag \(index) of \(bags), \(size)") }
        return String(localized: "Freezer bag \(index) of \(bags), \(size), \(thaw.summary) to thaw")
    }

    /// The leftover line as it should be read: ounces, not "oz".
    var leftoverAccessibilityLabel: String? {
        guard let leftover = leftoverText else { return nil }
        return String(localized: "\(Self.spoken(leftover)) left over. Toss it or cook it in.")
    }

    /// Announced after a bag is added or removed.
    var bagCountAnnouncement: String { bagCountText }

    // MARK: - Details

    private var thaw: PantryThaw? {
        guard let plan = card.portions else { return nil }
        return plan.option(bags)?.thaw ?? plan.thaw
    }

    private var leftoverText: String? {
        guard card.freezable, let plan = card.portions else { return nil }
        let text: String?
        if let option = plan.option(bags) {
            text = option.leftoverText
        } else if bags == plan.portions {
            text = plan.leftoverText
        } else {
            text = nil
        }
        guard let text, !text.isEmpty else { return nil }
        return text
    }

    private var mealsPhrase: String {
        let upcoming = card.meals.filter { !$0.past }
        let meals = upcoming.isEmpty ? card.meals : upcoming
        guard !meals.isEmpty else { return String(localized: "this week") }
        let names = meals.map { meal in
            meal.planDay.map { String(localized: "\($0.name(locale: locale))'s \(meal.recipeName)") }
                ?? meal.recipeName
        }
        return names.formatted(.list(type: .and).locale(locale))
    }

    /// "10 oz" as VoiceOver should say it, "10 ounces"; "3 lb 6 oz" as "3 pounds 6 ounces".
    /// Units other than weights are left as the API wrote them.
    static func spoken(_ amount: String) -> String {
        let words = amount.split(separator: " ").map(String.init)
        var out: [String] = []
        for (i, word) in words.enumerated() {
            let one = i > 0 && words[i - 1] == "1"
            switch word {
            case "oz": out.append(one ? String(localized: "ounce") : String(localized: "ounces"))
            case "lb": out.append(one ? String(localized: "pound") : String(localized: "pounds"))
            default: out.append(word)
            }
        }
        return out.joined(separator: " ")
    }
}

/// The Shop and Pantry banner's short version of the session: the first card still to do.
nonisolated struct PrepBannerPresentation: Equatable, Sendable {
    let session: PrepSession
    /// The card the banner shows — the first nobody has answered.
    let card: PrepCard?

    init(session: PrepSession) {
        self.session = session
        self.card = session.cards.first { !$0.isAnswered }
    }

    /// "10 oz this week".
    var amountText: String? {
        guard let card else { return nil }
        return String(localized: "\(PrepCardPresentation(card: card).keepText) this week")
    }

    /// "1 bag to freeze", or "3 items to put away" when there is more than one card.
    var detailText: String? {
        if session.pending > 1 {
            return String(localized: "\(session.pending) items to put away")
        }
        guard let card else { return nil }
        let presentation = PrepCardPresentation(card: card)
        return presentation.hasBags ? presentation.bagCountText : nil
    }

    /// True when the detail line is about the freezer, so it takes the freezer's ice blue.
    var detailIsFreezer: Bool {
        guard session.pending <= 1, let card else { return false }
        return PrepCardPresentation(card: card).hasBags
    }

    /// The row read as one sentence.
    var accessibilityLabel: String {
        guard let card else { return session.headline }
        let presentation = PrepCardPresentation(card: card)
        var parts = [
            String(localized: "Put the groceries away"), card.name,
            String(localized: "\(PrepCardPresentation.spoken(presentation.keepText)) this week"),
        ]
        if let detailText { parts.append(detailText) }
        return parts.joined(separator: ", ")
    }
}
