import Foundation

/// Keeps the product title out of the Walmart app's chrome.
///
/// A cart card prints far more than a name: a unit price ("88¢/lb | Final cost by weight") above
/// it, and "Subscribe", "SNAP EBT eligible", "Free 90-day returns", "Remove", "Save for later",
/// badges, and the quantity stepper below it. Recognition returns those as their own rows, and
/// rows close enough together arrive joined, so without this a line reads "Fresh Zucchini, Each
/// Subscribe - SNAP EBT eligible Free 90-day returns Remove Save for later 2 +" and matches
/// nothing.
///
/// `clean` returns the title, or `nil` when the row is only chrome.
nonisolated enum OrderTitleCleaner {
    static func clean(_ text: String) -> String? {
        var title = text
        if let range = title.range(of: chromePattern, options: [.regularExpression, .caseInsensitive]) {
            title = String(title[..<range.lowerBound])
        }
        title = dropLeadingUnitPrice(title)
        title = title.trimmingCharacters(in: edges)
        return title.count(where: \.isLetter) >= 3 ? title : nil
    }

    /// Drops a unit price and the weight note that can lead a title, however often they repeat:
    /// "88¢/lb | Final cost by weight Fresh Whole Yellow Onion, Each" is about the onion.
    private static func dropLeadingUnitPrice(_ text: String) -> String {
        var title = text
        for _ in 0..<4 {
            var trimmed = title.trimmingCharacters(in: edges)
            for pattern in leadingPatterns {
                if let range = trimmed.range(of: pattern, options: [.regularExpression, .caseInsensitive]),
                    range.lowerBound == trimmed.startIndex
                {
                    trimmed = String(trimmed[range.upperBound...])
                }
            }
            if trimmed == title { return title }
            title = trimmed
        }
        return title
    }

    /// Everything from here on belongs to the app, not the product.
    private static let chromePattern = [
        #"\bsubscribe\b"#,
        #"\bsnap\s?ebt\b"#, #"\bebt eligible\b"#,
        #"\bfree\s?\d*\s?-?\s?day returns\b"#, #"\bfree returns\b"#,
        #"\bgift (eligible|options)\b"#,
        #"\bremove\b"#, #"\bsave for later\b"#, #"\bmove to (cart|list)\b"#, #"\badd to list\b"#,
        #"\bbest seller\b"#, #"\bpopular pick\b"#, #"\brollback\b"#, #"\bclearance\b"#, #"\bsponsored\b"#,
        #"\bmultipack quantity\b"#, #"\bcount per pack\b"#,
        #"\byou save\b"#, #"\bprice when purchased online\b"#,
        #"\bbought (since|in past|\d+ times)\b"#, #"\d+\s?[km]?\+?\s+bought\b"#,
        #"\bin \d+ (cart|people)"#, #"\bshop similar\b"#, #"\bsold (and shipped )?by\b"#,
        #"\bout of stock\b"#, #"\blow in stock\b"#,
    ].joined(separator: "|")

    private static let leadingPatterns = [
        // "88¢/lb", "$1.12/lb", "$0.31 per oz": a unit price printed above the title.
        #"^\d+(\.\d+)?\s?¢\s?(/|per\b)\s?[a-z.]+"#,
        #"^\$\s?\d+(\.\d+)?\s?(/|per\b)\s?[a-z.]+"#,
        #"^final (cost|price) by weight"#,
        #"^[|·•\-–—]+"#,
    ]

    private static let edges = CharacterSet(charactersIn: " \t|·•+-–—").union(.whitespacesAndNewlines)
}
