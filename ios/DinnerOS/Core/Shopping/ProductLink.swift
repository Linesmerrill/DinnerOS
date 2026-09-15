import Foundation

/// Reads a product out of text a member pastes, and builds the Walmart search link.
///
/// Nothing here fetches a page. The API parses the item ID from the link itself and rejects
/// hosts and page types it doesn't accept; this only picks the link out of shared text
/// ("Check out this item at Walmart https://…") and shows which item was found.
nonisolated enum ProductLink {
    /// The first `http` or `https` link in `text`, or `nil` when there's none. A link written
    /// without a scheme (`www.walmart.com/ip/…`) comes back with `http://`.
    static func firstURL(in text: String) -> String? {
        guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else {
            return nil
        }
        let range = NSRange(text.startIndex..., in: text)
        for match in detector.matches(in: text, range: range) {
            guard let url = match.url, let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https"
            else { continue }
            return url.absoluteString
        }
        return nil
    }

    /// A bare Walmart item ID: 5 to 15 digits and nothing else.
    static func itemID(in text: String) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard (5...15).contains(trimmed.count), trimmed.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        return trimmed
    }

    /// What to send for pasted text: an item ID when the text is only digits, otherwise the
    /// first link in it.
    static func reference(from text: String) -> ShoppingProductReference? {
        if let id = itemID(in: text) {
            return .itemID(id)
        }
        return firstURL(in: text).map { .url($0) }
    }

    /// The item ID in a `walmart.com/ip/<id>` or `walmart.com/ip/<name>/<id>` link, for showing
    /// what was pasted. `nil` for any other link; the API decides what it accepts.
    static func walmartItemID(inURL string: String) -> String? {
        guard
            let components = URLComponents(string: string),
            let host = components.host?.lowercased(), host == "walmart.com" || host == "www.walmart.com"
        else { return nil }
        let parts = components.path.split(separator: "/")
        guard (2...3).contains(parts.count), parts[0] == "ip", let last = parts.last else { return nil }
        return itemID(in: String(last))
    }

    /// Walmart's search page for `query`. It's a plain link the member opens in Safari or the
    /// Walmart app; the app never fetches it.
    static func walmartSearchURL(for query: String) -> URL? {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return URL(string: "https://www.walmart.com/search?q=" + APIClient.encodeQueryComponent(trimmed))
    }
}
