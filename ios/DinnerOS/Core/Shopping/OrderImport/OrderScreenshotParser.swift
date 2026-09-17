import CoreGraphics
import Foundation

/// One item read from order screenshots.
nonisolated struct ParsedOrderItem: Hashable, Sendable, Identifiable {
    enum Status: String, Hashable, Sendable {
        case ordered
        case substituted
        case unavailable
        case refunded
    }

    /// The item's position in the parsed order.
    let id: Int
    let name: String
    /// What the order charged for the item, all of its quantity together.
    let priceCents: Int
    let quantity: Int
    let isWeightAdjusted: Bool
    let status: Status

    /// Unavailable and refunded items weren't paid for, so they aren't matched to lines.
    var wasCharged: Bool { status == .ordered || status == .substituted }
}

/// What order screenshots say: the items with prices, and the order's totals.
nonisolated struct ParsedOrder: Hashable, Sendable {
    var items: [ParsedOrderItem] = []
    var subtotalCents: Int?
    /// The order total with fees, tax, and tip.
    var totalCents: Int?
    var taxCents: Int?
    var tipCents: Int?
    /// Every fee added together.
    var feesCents: Int?
    var savingsCents: Int?
}

/// Reads Walmart order-details text, as on-device text recognition returns it, into items and
/// totals. Deterministic, so it's the fallback for Apple Intelligence and the check on it.
///
/// It expects the app's layout: an item name (sometimes wrapped over two lines), then lines
/// like "Qty 2", "2 x $2.49", "Weight-adjusted", and the price. The cart screen prints the price
/// *above* the name instead, so which side a price is on is decided from where the rows sit
/// (`pricesComeFirst`), not from the order they're read in. Was-prices, per-unit prices, average
/// prices, and item savings are ignored; "Subtotal", fees, "Tax", "Driver tip", "Savings", and
/// "Total" become the order's totals.
nonisolated enum OrderScreenshotParser {
    static func parse(text: String) -> ParsedOrder {
        parse(lines: text.components(separatedBy: .newlines))
    }

    static func parse(lines: [String]) -> ParsedOrder {
        parse(rows: lines.map { RecognizedRow(text: $0) })
    }

    static func parse(rows: [RecognizedRow]) -> ParsedOrder {
        let tokens = rows.map { classify($0.text) }
        var builder = Builder(pricesFirst: pricesComeFirst(tokens: tokens, rows: rows))
        for token in tokens {
            builder.consume(token)
        }
        return builder.finish()
    }

    // MARK: - Tokens

    enum SummaryKind: Equatable, Sendable {
        case subtotal, total, tax, tip, fee, savings
    }

    enum Marker: Equatable, Sendable {
        case substituted, unavailable, refunded, weightAdjusted
    }

    enum Token: Equatable, Sendable {
        case name(String, trailingPrice: Int?)
        case price(Int)
        case quantity(Int, price: Int?)
        case multiplied(quantity: Int, unitCents: Int)
        case marker(Marker)
        /// A heading such as "Unavailable items (2)" that applies to the items below it.
        case section(ParsedOrderItem.Status)
        case summary(SummaryKind, amount: Int?)
        /// A was-price, a per-unit price, or an item's savings.
        case ignoredPrice
        case noise
    }

    static func classify(_ raw: String) -> Token {
        let line = raw.replacingOccurrences(of: "\u{00A0}", with: " ")
            .split(whereSeparator: \.isWhitespace).joined(separator: " ")
        guard !line.isEmpty else { return .noise }
        let lower = line.lowercased()
        let prices = priceMatches(in: line)
        let label = labelText(line)

        // An item's own was-price, average price, or savings, before the order's "Savings" line.
        if lower.range(of: wasPricePattern, options: .regularExpression) != nil {
            return .ignoredPrice
        }
        // The cart's quantity stepper, "- 2 +", is how many of this item are in the cart.
        if let match = line.wholeMatch(of: #/[-−–—]?\s*(\d{1,2})\s*\+/#), let count = Int(match.1) {
            return .quantity(count, price: nil)
        }
        if let kind = summaryKind(label) {
            return .summary(kind, amount: prices.last.map { abs($0.cents) })
        }
        if let multiplied = multiplied(lower) {
            return multiplied
        }
        if let match = lower.firstMatch(of: #/^(?:qty|quantity)[:.]?\s*(\d{1,2})\b/#), let count = Int(match.1) {
            return .quantity(count, price: prices.last.map(\.cents))
        }
        if let heading = section(lower) {
            return heading
        }
        if let marker = marker(lower) {
            return marker
        }
        if let price = prices.first, prices.count == 1 {
            let rest = line.replacingCharacters(in: price.range, with: "").trimmingCharacters(in: .whitespaces)
            if price.cents < 0 {
                return .ignoredPrice
            }
            if rest.isEmpty || rest.lowercased() == "each" || rest.lowercased() == "ea" {
                return .price(price.cents)
            }
            // "$0.31/oz", "$1.12/lb", "$2.48 ea": a price per unit, not what was paid.
            if rest.hasPrefix("/") || rest.lowercased().hasPrefix("per ") {
                return .ignoredPrice
            }
        }
        if isNoise(lower) {
            return .noise
        }
        // A name, maybe with its price on the same row: "Great Value Sour Cream, 16 oz $2.48".
        if let price = prices.last, price.cents >= 0, price.range.upperBound == line.endIndex {
            let name = String(line[..<price.range.lowerBound])
            return OrderTitleCleaner.clean(name).map { .name($0, trailingPrice: price.cents) } ?? .noise
        }
        guard prices.isEmpty, let name = OrderTitleCleaner.clean(line) else { return .noise }
        return .name(name, trailingPrice: nil)
    }

    // MARK: - Assembly

    private struct Draft {
        var name: String
        var price: Int?
        var leadingPrice: Int?
        var quantity: Int?
        var unitCents: Int?
        var isWeightAdjusted = false
        var status: ParsedOrderItem.Status?
        /// Still reading a wrapped name: nothing but name lines since it started.
        var isNameOpen = true
    }

    private struct Builder {
        let pricesFirst: Bool
        var order = ParsedOrder()
        var current: Draft?
        var pendingPrice: Int?
        var pendingSummary: SummaryKind?
        var pendingMarkers: [Marker] = []
        var sectionStatus: ParsedOrderItem.Status?
        var fees: [Int] = []
        var seen: Set<String> = []

        init(pricesFirst: Bool) {
            self.pricesFirst = pricesFirst
        }

        mutating func consume(_ token: Token) {
            if case .price = token {
            } else if case .summary = token {
            } else {
                pendingSummary = nil
            }
            switch token {
            case .summary(let kind, let amount):
                if kind != .savings {
                    finishItem()
                    pendingPrice = nil
                }
                if let amount {
                    record(kind, amount)
                    pendingSummary = nil
                } else {
                    pendingSummary = kind
                }
            case .price(let cents):
                if let kind = pendingSummary {
                    record(kind, cents)
                    pendingSummary = nil
                } else if pricesFirst {
                    finishItem()
                    pendingPrice = cents
                } else if current != nil, current?.price == nil {
                    current?.price = cents
                    current?.isNameOpen = false
                }
            case .name(let text, let trailingPrice):
                if var draft = current, draft.isNameOpen, draft.price == nil {
                    draft.name += " " + text
                    if let trailingPrice {
                        draft.price = trailingPrice
                        draft.isNameOpen = false
                    }
                    current = draft
                } else {
                    finishItem()
                    var draft = Draft(name: text, price: trailingPrice)
                    if pricesFirst {
                        draft.leadingPrice = pendingPrice
                        pendingPrice = nil
                    }
                    if trailingPrice != nil {
                        draft.isNameOpen = false
                    }
                    for marker in pendingMarkers {
                        apply(marker, to: &draft)
                    }
                    pendingMarkers = []
                    current = draft
                }
            case .quantity(let count, let price):
                current?.quantity = count
                current?.isNameOpen = false
                if let price, !pricesFirst, current?.price == nil {
                    current?.price = price
                }
            case .multiplied(let count, let unitCents):
                current?.quantity = count
                current?.unitCents = unitCents
                current?.isNameOpen = false
            case .marker(let marker):
                if var draft = current {
                    apply(marker, to: &draft)
                    draft.isNameOpen = false
                    current = draft
                } else {
                    pendingMarkers.append(marker)
                }
            case .section(let status):
                finishItem()
                pendingPrice = nil
                sectionStatus = status == .ordered ? nil : status
            case .ignoredPrice, .noise:
                current?.isNameOpen = false
            }
        }

        mutating func finish() -> ParsedOrder {
            finishItem()
            if !fees.isEmpty {
                order.feesCents = fees.reduce(0, +)
            }
            return order
        }

        private func apply(_ marker: Marker, to draft: inout Draft) {
            switch marker {
            case .weightAdjusted: draft.isWeightAdjusted = true
            case .substituted: draft.status = .substituted
            case .unavailable: draft.status = .unavailable
            case .refunded: draft.status = .refunded
            }
        }

        private mutating func record(_ kind: SummaryKind, _ cents: Int) {
            switch kind {
            case .subtotal: order.subtotalCents = cents
            case .total: order.totalCents = cents
            case .tax: order.taxCents = (order.taxCents ?? 0) + cents
            case .tip: order.tipCents = cents
            case .fee: fees.append(cents)
            case .savings: order.savingsCents = cents
            }
        }

        private mutating func finishItem() {
            guard let draft = current else { return }
            current = nil
            let quantity = max(draft.quantity ?? 1, 1)
            let price = draft.price ?? draft.unitCents.map { $0 * quantity } ?? draft.leadingPrice
            guard let price else { return }
            let status = draft.status ?? sectionStatus ?? .ordered
            // Screenshots overlap: the same item at the same price is read once.
            let key = "\(OrderPriceMatcher.normalizedName(draft.name))|\(price)|\(quantity)|\(status.rawValue)"
            guard seen.insert(key).inserted else { return }
            order.items.append(
                ParsedOrderItem(
                    id: order.items.count, name: draft.name, priceCents: price, quantity: quantity,
                    isWeightAdjusted: draft.isWeightAdjusted, status: status))
        }
    }

    /// Whether each item's price is printed above its name, as the Walmart cart screen prints it.
    ///
    /// Reading order can't answer this. A cart screen opens with chrome that reads like a name,
    /// and every price sits between the name above it and the name it belongs to, so taking the
    /// price that follows a name gives each item its neighbour's price. Instead every price votes
    /// for the side its nearest name is on, and the majority decides for the whole read.
    static func pricesComeFirst(tokens: [Token], rows: [RecognizedRow]) -> Bool {
        var names: [CGFloat] = []
        var prices: [CGFloat] = []
        for (token, row) in zip(tokens, rows) where row.hasLayout {
            switch token {
            case .name: names.append(row.rect.midY)
            case .price: prices.append(row.rect.midY)
            default: continue
            }
        }
        if names.count >= 2, prices.count >= 2 {
            var above = 0
            var below = 0
            for price in prices {
                let nearestAbove = names.filter { $0 < price }.max()
                let nearestBelow = names.filter { $0 > price }.min()
                switch (nearestAbove, nearestBelow) {
                case (let up?, let down?):
                    if (down - price) < (price - up) { below += 1 } else { above += 1 }
                case (nil, .some): below += 1
                case (.some, nil): above += 1
                case (nil, nil): continue
                }
            }
            if above != below { return below > above }
        }
        return firstItemRowIsAPrice(tokens)
    }

    /// Without a layout to read, whether the first item line is a price rather than a name.
    private static func firstItemRowIsAPrice(_ tokens: [Token]) -> Bool {
        for token in tokens {
            switch token {
            case .name: return false
            case .price: return true
            case .summary: return false
            default: continue
            }
        }
        return false
    }

    // MARK: - Line rules

    struct PriceMatch {
        let cents: Int
        let range: Range<String.Index>
    }

    /// Dollar amounts in a line: "$4.98", "-$3.00", "$1,234.50", "4.98" standing alone, and
    /// "$4 98" — the Walmart app's raised cents, when they reached here as their own word.
    static func priceMatches(in line: String) -> [PriceMatch] {
        let pattern =
            #/(?<sign>[-–−]\s?)?\$\s?(?<whole>\d{1,3}(?:,\d{3})+|\d{1,6})(?:[.,]|\s(?=\d{2}(?!\d)))?(?<fraction>\d{2})?(?!\d)/#
        var matches: [PriceMatch] = line.matches(of: pattern).compactMap { match in
            let whole = Int(match.output.whole.replacingOccurrences(of: ",", with: "")) ?? 0
            let fraction = match.output.fraction.flatMap { Int($0) } ?? 0
            let cents = whole * 100 + fraction
            return PriceMatch(cents: match.output.sign == nil ? cents : -cents, range: match.range)
        }
        if matches.isEmpty, let bare = line.wholeMatch(of: #/(?<sign>-)?(?<whole>\d{1,4})\.(?<fraction>\d{2})/#) {
            let cents = (Int(bare.output.whole) ?? 0) * 100 + (Int(bare.output.fraction) ?? 0)
            matches = [PriceMatch(cents: bare.output.sign == nil ? cents : -cents, range: bare.range)]
        }
        return matches
    }

    /// The line without its amounts, a trailing colon, or a count such as "(24 items)".
    private static func labelText(_ line: String) -> String {
        var text = line
        for match in priceMatches(in: line).reversed() {
            text.removeSubrange(match.range)
        }
        text = text.replacing(#/\(.*?\)/#, with: "")
        return text.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: " :.-–"))
    }

    private static func summaryKind(_ label: String) -> SummaryKind? {
        switch label {
        case "total", "order total", "grand total", "estimated total", "new total", "final total":
            return .total
        case "subtotal", "item subtotal", "items subtotal", "sub total":
            return .subtotal
        case "tax", "taxes", "estimated tax", "estimated taxes", "sales tax":
            return .tax
        case "driver tip", "delivery tip", "tip", "shopper tip":
            return .tip
        case "savings", "total savings", "your savings", "discount", "discounts":
            return .savings
        default:
            let words = label.split(separator: " ")
            if label.hasSuffix("fee") || label.hasSuffix("fees"), (1...4).contains(words.count) {
                return .fee
            }
            return nil
        }
    }

    private static func multiplied(_ lower: String) -> Token? {
        if let match = lower.firstMatch(of: #/^(\d{1,2})\s*[x×@]\s*\$?\s?(\d{1,4})[.,](\d{2})/#),
            let count = Int(match.1), let whole = Int(match.2), let fraction = Int(match.3)
        {
            return .multiplied(quantity: count, unitCents: whole * 100 + fraction)
        }
        if let match = lower.firstMatch(of: #/^\$\s?(\d{1,4})[.,](\d{2})\s*(?:\/\s*ea\s*)?[x×]\s*(\d{1,2})$/#),
            let whole = Int(match.1), let fraction = Int(match.2), let count = Int(match.3)
        {
            return .multiplied(quantity: count, unitCents: whole * 100 + fraction)
        }
        return nil
    }

    private static func section(_ lower: String) -> Token? {
        let heading = lower.replacing(#/\(\d+\)/#, with: "").trimmingCharacters(in: .whitespaces)
        switch heading {
        case "unavailable items", "unavailable", "out of stock items", "out of stock":
            return lower.contains("(") || heading.hasSuffix("items") ? .section(.unavailable) : nil
        case "substituted items", "substitutions", "replaced items":
            return .section(.substituted)
        case "refunded items", "returned items":
            return .section(.refunded)
        case "shopped items", "delivered items", "items", "received items", "picked items", "items ordered":
            return .section(.ordered)
        default:
            return nil
        }
    }

    private static func marker(_ lower: String) -> Token? {
        guard lower.split(separator: " ").count <= 5 else { return nil }
        if lower.contains("weight-adjusted") || lower.contains("weight adjusted") || lower.contains("est. weight")
            || lower.hasPrefix("final price")
        {
            return .marker(.weightAdjusted)
        }
        if lower.hasPrefix("substitut") || lower == "replacement" || lower.hasPrefix("replaced") {
            return .marker(.substituted)
        }
        if lower.hasPrefix("unavailable") || lower.hasPrefix("out of stock") || lower == "not available" {
            return .marker(.unavailable)
        }
        if lower.hasPrefix("refund") || lower == "returned" {
            return .marker(.refunded)
        }
        return nil
    }

    private static let wasPricePattern =
        #"^(was|reg\.?|regular price|list price|orig(inal)?( price)?|you save[d]?|saved|rollback|avg\.?|average)\b"#

    private static let noisePatterns: [String] = [
        #"^order (details|#|number|placed|summary|info)"#,
        #"^#?\s?\d[\d\s-]{5,}$"#,
        #"^(delivered|arrived|arriving|shipped|pickup|picked up|delivery|shipping)\b"#,
        #"^(reorder|buy again|add to cart|write a review|review item|start a return|return|replace|get help|help)\b"#,
        #"^(view|track|see) "#,
        #"^\d+ items?\b"#,
        #"^(payment|paid with|visa|mastercard|amex|discover|debit|credit|gift card|ending in|card)\b"#,
        #"^(sun|mon|tue|tues|wed|thu|thur|thurs|fri|sat|sunday|monday|tuesday|wednesday|thursday|friday|saturday),? "#,
        #"^(jan|feb|mar|apr|jun|jul|aug|sep|sept|oct|nov|dec|january|february|march|april|may|june|july|august|september|october|november|december)\.? \d"#,
        #"^\d{1,2}:\d{2}"#,
        #"^(print|receipt|download|share|done|close|cancel|edit)\b"#,
        #"^(walmart\+?|walmart\.com|store|from store|sold and shipped by)\b"#,
        #"^(temporary hold|authorization|charged|charge)\b"#,
        #"^(address|deliver to|delivery address|instructions)\b"#,
        // The cart screen's own chrome, above and below the items.
        #"^(cart|your cart|continue to checkout|check ?out|reserve a time|select a time)\b"#,
        #"^(items? in cart|saved for later|recommended|you might also|based on your)\b"#,
    ]

    private static func isNoise(_ lower: String) -> Bool {
        noisePatterns.contains { lower.range(of: $0, options: .regularExpression) != nil }
    }
}
