import Foundation

/// An item Apple Intelligence read from the order text, before it's checked.
nonisolated struct ModelOrderItem: Hashable, Sendable {
    var name: String
    /// As the model wrote it, for example "$4.98".
    var price: String
    var quantity: Int
}

/// Checks Apple Intelligence's reading of order text against the text itself.
///
/// The model is better at names that wrap or sit beside their prices, but it can invent or
/// misread numbers, so a model item is kept only when its price appears in the recognized text.
/// The model's items replace the parser's only when there are at least as many of them.
nonisolated enum OrderImportMerge {
    static func merge(
        parsed: ParsedOrder, modelItems: [ModelOrderItem], modelTotal: String?, lines: [String]
    ) -> ParsedOrder {
        let seenPrices = Set(lines.flatMap { OrderScreenshotParser.priceMatches(in: $0).map { abs($0.cents) } })
        let notCharged = Set(parsed.items.filter { !$0.wasCharged }.map { OrderPriceMatcher.normalizedName($0.name) })
        let parsedByName = Dictionary(
            parsed.items.map { (OrderPriceMatcher.normalizedName($0.name), $0) },
            uniquingKeysWith: { first, _ in first })
        var accepted: [ParsedOrderItem] = []
        var keys: Set<String> = []
        for item in modelItems {
            let name = item.name.trimmingCharacters(in: .whitespacesAndNewlines)
            let normalized = OrderPriceMatcher.normalizedName(name)
            guard name.count(where: \.isLetter) >= 3, !normalized.isEmpty, !notCharged.contains(normalized),
                let cents = MoneyText.cents(from: item.price), cents > 0, seenPrices.contains(cents)
            else { continue }
            let quantity = min(max(item.quantity, 1), ShoppingLimits.packages.upperBound)
            guard keys.insert("\(normalized)|\(cents)|\(quantity)").inserted else { continue }
            let parsedItem = parsedByName[normalized]
            accepted.append(
                ParsedOrderItem(
                    id: accepted.count, name: name, priceCents: cents, quantity: quantity,
                    isWeightAdjusted: parsedItem?.isWeightAdjusted ?? false, status: parsedItem?.status ?? .ordered))
        }
        var result = parsed
        let chargedCount = parsed.items.count(where: \.wasCharged)
        if !accepted.isEmpty, accepted.count >= chargedCount {
            let uncharged = parsed.items.filter { !$0.wasCharged }
            result.items =
                accepted
                + uncharged.enumerated().map { offset, item in
                    ParsedOrderItem(
                        id: accepted.count + offset, name: item.name, priceCents: item.priceCents,
                        quantity: item.quantity, isWeightAdjusted: item.isWeightAdjusted, status: item.status)
                }
        }
        if result.totalCents == nil, let modelTotal, let cents = MoneyText.cents(from: modelTotal),
            seenPrices.contains(cents)
        {
            result.totalCents = cents
        }
        return result
    }
}

/// The import review: which line each receipt item prices, at what price, and whether to save
/// the order total. Nothing is saved until the member taps Save.
nonisolated struct OrderImportDraft: Equatable, Sendable {
    nonisolated struct Row: Equatable, Sendable, Identifiable {
        let item: ParsedOrderItem
        var lineID: PriceableLine.ID?
        var priceText: String
        /// The matcher's confidence; `nil` once the member picks the line themselves.
        var confidence: MatchConfidence?

        var id: ParsedOrderItem.ID { item.id }
        var priceCents: Int? { MoneyText.cents(from: priceText) }
        var priceError: String? {
            priceText.trimmingCharacters(in: .whitespaces).isEmpty
                ? String(localized: "Enter a price.") : MoneyText.error(priceText)
        }
    }

    let lines: [PriceableLine]
    private(set) var rows: [Row]
    let detectedTotalCents: Int?
    var savesTotal: Bool

    init(order: ParsedOrder, lines: [PriceableLine]) {
        self.lines = lines
        let matching = OrderPriceMatcher.match(items: order.items, lines: lines)
        let byItem = Dictionary(matching.matches.map { ($0.itemID, $0) }, uniquingKeysWith: { first, _ in first })
        rows = order.items.map { item in
            let match = byItem[item.id]
            return Row(
                item: item, lineID: match?.lineID, priceText: MoneyText.editingText(item.priceCents),
                confidence: match?.confidence)
        }
        detectedTotalCents = order.totalCents
        savesTotal = order.totalCents != nil
    }

    var matchedRows: [Row] { rows.filter { $0.lineID != nil } }
    var unmatchedRows: [Row] { rows.filter { $0.lineID == nil } }

    /// Lines no item prices that don't have a price yet.
    var linesWithoutPrice: [PriceableLine] {
        let assigned = Set(rows.compactMap(\.lineID))
        return lines.filter { !assigned.contains($0.id) && $0.line.priceCents == nil }
    }

    func line(_ id: PriceableLine.ID?) -> PriceableLine? {
        guard let id else { return nil }
        return lines.first { $0.id == id }
    }

    /// Points an item at a line, or at nothing ("Don't use"). A line prices one item, so any
    /// other item on that line lets go of it.
    mutating func assign(_ itemID: ParsedOrderItem.ID, to lineID: PriceableLine.ID?) {
        guard let index = rows.firstIndex(where: { $0.id == itemID }), rows[index].lineID != lineID else { return }
        if let lineID {
            for other in rows.indices where other != index && rows[other].lineID == lineID {
                rows[other].lineID = nil
                rows[other].confidence = nil
            }
        }
        rows[index].lineID = lineID
        rows[index].confidence = nil
    }

    mutating func setPriceText(_ text: String, for itemID: ParsedOrderItem.ID) {
        guard let index = rows.firstIndex(where: { $0.id == itemID }) else { return }
        rows[index].priceText = text
    }

    var hasInvalidPrice: Bool {
        matchedRows.contains { $0.priceError != nil }
    }

    /// The prices Save sends, by line.
    var prices: [PriceableLine.ID: Int?] {
        var result: [PriceableLine.ID: Int?] = [:]
        for row in matchedRows {
            guard let lineID = row.lineID, let cents = row.priceCents else { continue }
            result[lineID] = .some(cents)
        }
        return result
    }

    /// The order total Save sends, when there is one and it's switched on.
    var totalToSave: Int? {
        savesTotal ? detectedTotalCents : nil
    }

    var canSave: Bool {
        !hasInvalidPrice && (!prices.isEmpty || totalToSave != nil)
    }
}
