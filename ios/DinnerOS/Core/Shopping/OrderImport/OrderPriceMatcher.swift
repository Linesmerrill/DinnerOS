import Foundation

/// How sure a receipt item's match to a grocery line is.
nonisolated enum MatchConfidence: Int, Comparable, Hashable, Sendable {
    case low
    case medium
    case high

    static func < (lhs: MatchConfidence, rhs: MatchConfidence) -> Bool {
        lhs.rawValue < rhs.rawValue
    }

    var text: String {
        switch self {
        case .high: String(localized: "Good match")
        case .medium: String(localized: "Likely match")
        case .low: String(localized: "Check this match")
        }
    }
}

/// A receipt item paired with a handed-off line.
nonisolated struct OrderPriceMatch: Hashable, Sendable {
    let itemID: ParsedOrderItem.ID
    let lineID: PriceableLine.ID
    let score: Double
    let confidence: MatchConfidence
}

nonisolated struct OrderPriceMatching: Hashable, Sendable {
    /// In receipt order.
    let matches: [OrderPriceMatch]
    /// Items no line matched well enough, in receipt order.
    let unmatchedItemIDs: [ParsedOrderItem.ID]
    /// Lines no item matched, in the order given.
    let unmatchedLineIDs: [PriceableLine.ID]
}

/// Pairs receipt items with the week's handed-off lines by their names.
///
/// Names are compared as sets of words, lowercased, without punctuation, sizes ("16 oz", "3 ct"),
/// store brands ("Great Value"), or plural endings. The score blends Dice similarity with how
/// much of the shorter name the longer one contains, so "Harborline Soy Sauce, 10 fl oz" matches
/// "Soy Sauce". Each line and each item is used once: the best-scoring pairs are taken first.
nonisolated enum OrderPriceMatcher {
    /// Pairs scoring below this aren't matched.
    static let threshold = 0.5
    static let mediumThreshold = 0.65
    static let highThreshold = 0.8

    static func match(items: [ParsedOrderItem], lines: [PriceableLine]) -> OrderPriceMatching {
        let lineTokens = lines.map { (product: tokens($0.line.product.displayName), ingredient: tokens($0.line.name)) }
        var candidates: [(item: Int, line: Int, score: Double)] = []
        for (itemIndex, item) in items.enumerated() where item.wasCharged {
            let itemTokens = tokens(item.name)
            for (lineIndex, names) in lineTokens.enumerated() {
                // The product name is what the receipt prints, so it wins a tie with the ingredient.
                let score = max(Self.score(itemTokens, names.product), Self.score(itemTokens, names.ingredient) * 0.95)
                if score >= threshold {
                    candidates.append((itemIndex, lineIndex, score))
                }
            }
        }
        candidates.sort { a, b in
            if a.score != b.score { return a.score > b.score }
            if a.item != b.item { return a.item < b.item }
            return a.line < b.line
        }
        var usedItems: Set<Int> = []
        var usedLines: Set<Int> = []
        var byItem: [Int: OrderPriceMatch] = [:]
        for candidate in candidates where !usedItems.contains(candidate.item) && !usedLines.contains(candidate.line) {
            usedItems.insert(candidate.item)
            usedLines.insert(candidate.line)
            byItem[candidate.item] = OrderPriceMatch(
                itemID: items[candidate.item].id, lineID: lines[candidate.line].id, score: candidate.score,
                confidence: confidence(candidate.score))
        }
        return OrderPriceMatching(
            matches: items.indices.compactMap { byItem[$0] },
            unmatchedItemIDs: items.indices.filter { byItem[$0] == nil }.map { items[$0].id },
            unmatchedLineIDs: lines.indices.filter { !usedLines.contains($0) }.map { lines[$0].id })
    }

    static func confidence(_ score: Double) -> MatchConfidence {
        if score >= highThreshold { return .high }
        if score >= mediumThreshold { return .medium }
        return .low
    }

    /// 0 when no word is shared, 1 for the same words.
    static func score(_ a: Set<String>, _ b: Set<String>) -> Double {
        guard !a.isEmpty, !b.isEmpty else { return 0 }
        let shared = Double(a.intersection(b).count)
        guard shared > 0 else { return 0 }
        let dice = 2 * shared / Double(a.count + b.count)
        let containment = shared / Double(min(a.count, b.count))
        let bonus = containment == 1 ? 0.1 : 0
        return min(0.5 * dice + 0.5 * containment + bonus, 1)
    }

    /// The comparable words of a name.
    static func tokens(_ name: String) -> Set<String> {
        Set(normalizedName(name).split(separator: " ").map(String.init))
    }

    /// Lowercased words without punctuation, sizes, store brands, filler words, or plural endings.
    static func normalizedName(_ name: String) -> String {
        var text = name.lowercased().folding(options: [.diacriticInsensitive], locale: nil)
        text = text.replacingOccurrences(of: "'", with: "").replacingOccurrences(of: "’", with: "")
        for brand in brands {
            text = text.replacingOccurrences(of: brand, with: " ")
        }
        let words = text.split { !$0.isLetter && !$0.isNumber }.map(String.init)
        return words.filter { word in
            !word.contains(where: \.isNumber) && word.count > 1 && !noiseWords.contains(word)
        }
        .map(singular)
        .joined(separator: " ")
    }

    private static let brands = [
        "great value", "marketside", "freshness guaranteed", "sams choice", "members mark", "equate",
        "parents choice", "mainstays", "bettergoods", "hill country fare",
    ]

    private static let noiseWords: Set<String> = [
        "oz", "fl", "lb", "lbs", "ct", "count", "pk", "pack", "packs", "g", "kg", "ml", "l", "gal", "gallon", "qt",
        "pt", "each", "ea", "bag", "bags", "jar", "can", "cans", "bottle", "box", "tub", "size", "family", "value",
        "and", "with", "the", "of", "in", "for", "to", "or", "per", "new", "item", "shaped",
    ]

    private static func singular(_ word: String) -> String {
        guard word.count > 3 else { return word }
        if word.hasSuffix("ies") { return String(word.dropLast(3)) + "y" }
        if word.hasSuffix("oes") || word.hasSuffix("ches") || word.hasSuffix("shes") || word.hasSuffix("xes") {
            return String(word.dropLast(2))
        }
        if word.hasSuffix("s"), !word.hasSuffix("ss"), !word.hasSuffix("us") { return String(word.dropLast()) }
        return word
    }
}
