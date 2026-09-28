import Foundation

/// What a check of a saved product's Walmart page found (docs/shopping-providers.md#checking-saved-products).
nonisolated enum WalmartProductStatus: String, Codable, Hashable, Sendable {
    /// On Walmart and in stock for pickup or delivery.
    case found
    /// On Walmart, but out of stock for pickup and delivery. It still goes in the cart.
    case unavailable
    /// Walmart answered 404: the item doesn't exist. It never goes in the cart.
    case gone
    /// The check couldn't tell: blocked, timed out, or a page it couldn't read. Never treated as
    /// found.
    case unknown
}

/// One product check's result.
nonisolated struct WalmartProductResult: Codable, Hashable, Sendable {
    let status: WalmartProductStatus
    /// Why the status is `unknown`; `nil` otherwise. Logged, never shown.
    var detail: WalmartCheckDetail? = nil
    /// Walmart's name for the product, on found or unavailable.
    var name: String? = nil
    /// Walmart's current price, on found or unavailable, when the page had one it could read.
    var priceCents: Int? = nil
    var checkedAt: Date

    static func unknown(_ detail: WalmartCheckDetail, at date: Date) -> WalmartProductResult {
        WalmartProductResult(status: .unknown, detail: detail, checkedAt: date)
    }
}

/// Why a check is `unknown`. Stable strings, for logs.
nonisolated enum WalmartCheckDetail: String, Codable, Hashable, Sendable {
    case network
    case blocked
    case unexpectedStatus = "unexpected_status"
    case offSite = "off_site"
    case noPageData = "no_page_data"
    case noProduct = "no_product"
    case differentItem = "different_item"
    case noFulfillment = "no_fulfillment"
    /// The check ran out of time before this product.
    case outOfTime = "out_of_time"
}

/// The fields DinnerOS reads from a Walmart product page's `__NEXT_DATA__`
/// (`props.pageProps.initialData.data.product`), and nothing else. The web view fallback returns
/// exactly these, so both paths read a page the same way.
nonisolated struct WalmartPageFields: Codable, Hashable, Sendable {
    struct Fulfillment: Codable, Hashable, Sendable {
        var type: String?
        var availabilityStatus: String?
    }

    var usItemId: String?
    var name: String?
    var availabilityStatus: String?
    var priceString: String?
    /// `nil` when the page had no `fulfillmentOptions` array, which is told apart from an empty one.
    var fulfillmentOptions: [Fulfillment]?
}

/// Reads one Walmart product page into a result. Pure: fetching lives in `WalmartProductCheck`.
///
/// The rule it's built around: a page it can't fully understand is `unknown`, never `found`.
/// Walmart can change its page at any time, and reading a changed page as "fine" is exactly how a
/// dead item would slip into a cart. Measured on walmart.com: a real item answers 200 after a
/// redirect to `/ip/<slug>/<id>`; a missing one answers 404 at `/ip/seort/<id>`; a bot challenge
/// redirects to `/blocked?...` and answers 200.
nonisolated enum WalmartProductPage {
    /// Walmart's hosts. A final URL anywhere else is `unknown`.
    static let hosts: Set<String> = ["www.walmart.com", "walmart.com"]

    /// Channel availability values that mean "can be bought". Anything else isn't.
    static let inStock: Set<String> = ["IN_STOCK", "LIMITED_STOCK", "AVAILABLE"]

    /// The page a check requests for an item.
    static func url(itemID: String) -> URL? {
        URL(string: "https://www.walmart.com/ip/\(itemID)")
    }

    /// Walmart's bot challenge: a redirect to `/blocked`.
    static func isBlocked(_ url: URL) -> Bool {
        isWalmart(url) && url.path.hasPrefix("/blocked")
    }

    static func isWalmart(_ url: URL) -> Bool {
        guard let host = url.host?.lowercased() else { return false }
        return hosts.contains(host)
    }

    /// What an HTTP answer says before its body is read: `gone`, a reason it's `unknown`, or `nil`
    /// when the body should be read.
    private static func verdict(statusCode: Int, finalURL: URL) -> (
        status: WalmartProductStatus, detail: WalmartCheckDetail?
    )? {
        guard isWalmart(finalURL) else { return (.unknown, .offSite) }
        if isBlocked(finalURL) { return (.unknown, .blocked) }
        guard finalURL.path.hasPrefix("/ip/") else { return (.unknown, .offSite) }
        if statusCode == 404 { return (.gone, nil) }
        guard statusCode == 200 else { return (.unknown, .unexpectedStatus) }
        return nil
    }

    /// Reads a fetched page: its status code, the URL after redirects, and its HTML.
    static func parse(itemID: String, statusCode: Int, finalURL: URL, html: String, at date: Date)
        -> WalmartProductResult
    {
        if let verdict = verdict(statusCode: statusCode, finalURL: finalURL) {
            return WalmartProductResult(status: verdict.status, detail: verdict.detail, checkedAt: date)
        }
        guard let json = nextData(in: html), let data = json.data(using: .utf8) else {
            return .unknown(.noPageData, at: date)
        }
        guard let fields = fields(fromNextData: data) else {
            return .unknown(.noPageData, at: date)
        }
        return result(itemID: itemID, fields: fields, at: date)
    }

    /// Reads what the web view returned for a page it loaded: the status code of the main
    /// document, the URL after redirects, and the fields its script read (`nil` when it found no
    /// `__NEXT_DATA__`).
    static func parse(itemID: String, statusCode: Int, finalURL: URL, fields: WalmartPageFields?, at date: Date)
        -> WalmartProductResult
    {
        if let verdict = verdict(statusCode: statusCode, finalURL: finalURL) {
            return WalmartProductResult(status: verdict.status, detail: verdict.detail, checkedAt: date)
        }
        guard let fields else { return .unknown(.noPageData, at: date) }
        return result(itemID: itemID, fields: fields, at: date)
    }

    /// The text of the page's `<script id="__NEXT_DATA__">`, or `nil`.
    static func nextData(in html: String) -> String? {
        guard let marker = html.range(of: "id=\"__NEXT_DATA__\"") else { return nil }
        guard let open = html[marker.upperBound...].range(of: ">"),
            let close = html[open.upperBound...].range(of: "</script>")
        else { return nil }
        return String(html[open.upperBound..<close.lowerBound])
    }

    /// The product fields in `__NEXT_DATA__` JSON; `nil` when it isn't JSON. A page without a
    /// product object gives empty fields.
    static func fields(fromNextData data: Data) -> WalmartPageFields? {
        guard let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        let props = root["props"] as? [String: Any]
        let pageProps = props?["pageProps"] as? [String: Any]
        let initialData = pageProps?["initialData"] as? [String: Any]
        let payload = initialData?["data"] as? [String: Any]
        guard let product = payload?["product"] as? [String: Any] else { return WalmartPageFields() }
        let priceInfo = product["priceInfo"] as? [String: Any]
        let currentPrice = priceInfo?["currentPrice"] as? [String: Any]
        var fields = WalmartPageFields(
            usItemId: string(product["usItemId"]), name: product["name"] as? String,
            availabilityStatus: product["availabilityStatus"] as? String,
            priceString: currentPrice?["priceString"] as? String)
        if let options = product["fulfillmentOptions"] as? [[String: Any]] {
            fields.fulfillmentOptions = options.map {
                WalmartPageFields.Fulfillment(
                    type: $0["type"] as? String, availabilityStatus: $0["availabilityStatus"] as? String)
            }
        }
        return fields
    }

    /// What the fields say about `itemID`. `found` needs the item asked for, a name, and a
    /// `fulfillmentOptions` array; anything missing is `unknown`.
    static func result(itemID: String, fields: WalmartPageFields, at date: Date) -> WalmartProductResult {
        let name = (fields.name ?? "").split(whereSeparator: \.isWhitespace).joined(separator: " ")
        guard let id = fields.usItemId, !id.isEmpty, !name.isEmpty else { return .unknown(.noProduct, at: date) }
        guard id == itemID else { return .unknown(.differentItem, at: date) }
        guard let options = fields.fulfillmentOptions else { return .unknown(.noFulfillment, at: date) }
        let channels = options.filter { $0.type == "PICKUP" || $0.type == "DELIVERY" }
        let inStockNow: Bool =
            channels.isEmpty
            ? inStock.contains(fields.availabilityStatus ?? "")
            : channels.contains { inStock.contains($0.availabilityStatus ?? "") }
        return WalmartProductResult(
            status: inStockNow ? .found : .unavailable, name: name, priceCents: price(fields.priceString),
            checkedAt: date)
    }

    /// Reads a price exactly as Walmart prints one ("$0.85", "$12", "$1,024.50") into cents.
    /// Anything else ("$1.24/lb", "From $3") is `nil`.
    static func price(_ text: String?) -> Int? {
        guard var text = text?.trimmingCharacters(in: .whitespaces), text.hasPrefix("$") else { return nil }
        text.removeFirst()
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count <= 2, let whole = parts.first, !whole.isEmpty else { return nil }
        let groups = whole.split(separator: ",", omittingEmptySubsequences: false)
        if groups.count > 1 {
            guard (1...3).contains(groups[0].count), groups.dropFirst().allSatisfy({ $0.count == 3 }) else {
                return nil
            }
        }
        let digits = groups.joined()
        guard !digits.isEmpty, digits.allSatisfy(\.isASCIIDigit), let dollars = Int(digits) else { return nil }
        var cents = dollars * 100
        if parts.count == 2 {
            let fraction = parts[1]
            guard fraction.count == 2, fraction.allSatisfy(\.isASCIIDigit), let value = Int(fraction) else {
                return nil
            }
            cents += value
        }
        return cents
    }

    private static func string(_ value: Any?) -> String? {
        switch value {
        case let text as String: text
        case let number as NSNumber: number.stringValue
        default: nil
        }
    }
}

extension Character {
    fileprivate nonisolated var isASCIIDigit: Bool { isASCII && isNumber }
}
