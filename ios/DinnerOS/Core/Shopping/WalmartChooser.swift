import Foundation

/// Choosing products in Walmart without leaving it: the week's lines that need a product, one at
/// a time, over an in-app Walmart page. Browsing to a product page is the choice; there's no link
/// to copy. Pure, so the stepping and the save request are tested without a web view.
nonisolated struct WalmartChooser: Equatable, Sendable {
    /// One line to choose for.
    struct Item: Equatable, Sendable, Identifiable {
        let ingredientKey: String
        let name: String
        /// "10 oz", or empty.
        let amountText: String
        let searchQuery: String
        var id: String { ingredientKey }

        init(line: ShoppingExcludedLine) {
            ingredientKey = line.ingredientKey
            name = line.name
            amountText = line.quantityText
            searchQuery = line.searchTerms.query
        }

        init(ingredientKey: String, name: String, amountText: String, searchQuery: String) {
            self.ingredientKey = ingredientKey
            self.name = name
            self.amountText = amountText
            self.searchQuery = searchQuery
        }
    }

    let items: [Item]
    private(set) var index = 0
    /// Lines a product was saved for, so "N left" and the finish count only the rest.
    private(set) var chosen: Set<String> = []

    init(items: [Item]) {
        self.items = items
    }

    var current: Item? { items.indices.contains(index) ? items[index] : nil }
    var remaining: Int { items.count - chosen.count }
    var canGoBack: Bool { index > 0 }
    var isFinished: Bool { index >= items.count }

    mutating func back() {
        if index > 0 { index -= 1 }
    }

    /// Past this line without choosing.
    mutating func skip() {
        if index < items.count { index += 1 }
    }

    /// A product was saved for the current line: on to the next.
    mutating func didChoose() {
        guard let current else { return }
        chosen.insert(current.ingredientKey)
        index += 1
    }

    /// The Walmart search for the current line.
    var searchURL: URL? { current.flatMap { ProductLink.walmartSearchURL(for: $0.searchQuery) } }

    /// The item ID when `url` is a Walmart product page (`/ip/<slug>/<id>`), else `nil`: a search
    /// or category page isn't a choice.
    static func productItemID(at url: URL?) -> String? {
        url.flatMap { ProductLink.walmartItemID(inURL: $0.absoluteString) }
    }

    /// The product page's address without the search's tracking (`?athbdg=…&from=/search`).
    static func cleanProductURL(_ url: URL) -> URL {
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return url }
        components.query = nil
        components.fragment = nil
        return components.url ?? url
    }

    /// What Save sends for a product page: its link, the name the page shows (or the one in its
    /// address), and its price when the page read one. The API derives the package size.
    static func request(
        for item: Item, url: URL, pageName: String?, priceText: String?
    ) -> ShoppingPreferenceRequest? {
        guard productItemID(at: url) != nil else { return nil }
        var draft = SavedProductDraft(ingredientName: item.name)
        draft.linkText = cleanProductURL(url).absoluteString
        if let name = pageName?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty {
            draft.displayName = String(name.prefix(ShoppingLimits.maxDisplayNameLength))
        } else if let derived = ProductLink.walmartProductName(inURL: url.absoluteString) {
            draft.displayName = derived
        }
        if let priceText, MoneyText.cents(from: priceText) != nil {
            draft.priceText = priceText.replacingOccurrences(of: "$", with: "")
        }
        return draft.request(ingredientName: item.name)
    }
}
