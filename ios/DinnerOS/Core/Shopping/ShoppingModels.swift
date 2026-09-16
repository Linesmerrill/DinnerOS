import Foundation

/// Provider keys (`ShoppingProvider` path parameter). Phase 8a hands lists to Walmart only.
nonisolated enum ShoppingProviderKey {
    static let walmart = "walmart"
}

/// The API's shopping limits, mirrored for input validation.
nonisolated enum ShoppingLimits {
    /// A line's package count, and a confirmed line's.
    static let packages = 1...99
    /// `checkedOffKeys`, `excludeKeys`, and `lines` each hold at most this many entries.
    static let maxKeys = 300
    static let maxDisplayNameLength = 100
    static let maxStoreNumberLength = 6
}

// MARK: - Providers and store

/// What a provider can do (`ShoppingCapabilities`).
nonisolated struct ShoppingCapabilities: Decodable, Hashable, Sendable {
    /// How a list is handed off, for example `cart_link`.
    let handoff: String
    let pasteProductLink: Bool
    let storeID: Bool

    private enum CodingKeys: String, CodingKey {
        case handoff, pasteProductLink
        case storeID = "storeId"
    }
}

/// An enabled shopping provider (`ShoppingProvider`).
nonisolated struct ShoppingProvider: Decodable, Hashable, Sendable, Identifiable {
    let key: String
    let name: String
    /// Handoff links carry affiliate tracking, so the commission disclosure must be shown.
    let affiliateTracked: Bool
    let capabilities: ShoppingCapabilities

    var id: String { key }

    /// Whether this build can hand a list off to the provider: Walmart cart links in 8a.
    var isSupported: Bool {
        key == ShoppingProviderKey.walmart && capabilities.handoff == "cart_link"
    }
}

nonisolated struct ShoppingProviderList: Decodable, Sendable {
    let items: [ShoppingProvider]
}

/// The household's provider and store (`ShoppingSettings`). Every field is `nil` until set.
nonisolated struct ShoppingSettings: Decodable, Hashable, Sendable {
    let provider: String?
    let storeID: String?
    let updatedBy: String?
    let updatedAt: Date?

    private enum CodingKeys: String, CodingKey {
        case provider
        case storeID = "storeId"
        case updatedBy, updatedAt
    }
}

/// The body of `PUT .../shopping/settings`. A `nil` store is sent as `null`, which clears it.
nonisolated struct UpdateShoppingSettingsRequest: Encodable, Equatable, Sendable {
    var provider: String
    var storeID: String?

    private enum CodingKeys: String, CodingKey {
        case provider
        case storeID = "storeId"
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(provider, forKey: .provider)
        if let storeID {
            try container.encode(storeID, forKey: .storeID)
        } else {
            try container.encodeNil(forKey: .storeID)
        }
    }
}

/// A typed Walmart store number: 1–6 digits. The API drops leading zeros.
nonisolated enum ShoppingStoreNumber {
    /// The trimmed number, or `nil` when the text is empty.
    static func normalized(_ text: String) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    /// Why `text` can't be sent, or `nil` when it's empty or valid.
    static func error(_ text: String) -> String? {
        guard let number = normalized(text) else { return nil }
        let isValid =
            number.count <= ShoppingLimits.maxStoreNumberLength && number.allSatisfy { $0.isASCII && $0.isNumber }
        return isValid ? nil : String(localized: "A store number is 1 to 6 digits.")
    }
}

// MARK: - Saved products

/// An exact amount with display text (`ShoppingAmount`).
nonisolated struct ShoppingAmount: Decodable, Hashable, Sendable {
    /// Exact, as `"n"` or `"n/d"`.
    let quantity: String
    let quantityValue: Double
    /// A DinnerOS unit code.
    let unit: String
    let text: String
}

/// How a saved product names the provider's product: the pasted link, or the item ID.
nonisolated enum ShoppingProductReference: Equatable, Sendable {
    case url(String)
    case itemID(String)
}

nonisolated struct ShoppingPackageSizeInput: Encodable, Equatable, Sendable {
    /// Exact, as produced by `PantryQuantity.parse`.
    var quantity: String
    var unit: String
}

/// The body of `PUT .../preferences/{ingredientKey}`. A `nil` package size is sent as `null`
/// (unknown), and a `nil` ingredient name is omitted (the API's default).
nonisolated struct ShoppingPreferenceRequest: Encodable, Equatable, Sendable {
    var product: ShoppingProductReference
    var displayName: String
    var packageSize: ShoppingPackageSizeInput?
    var ingredientName: String?

    private enum CodingKeys: String, CodingKey {
        case productURL = "productUrl"
        case productID = "productId"
        case displayName, packageSize, ingredientName
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        switch product {
        case .url(let url): try container.encode(url, forKey: .productURL)
        case .itemID(let id): try container.encode(id, forKey: .productID)
        }
        try container.encode(displayName, forKey: .displayName)
        if let packageSize {
            try container.encode(packageSize, forKey: .packageSize)
        } else {
            try container.encodeNil(forKey: .packageSize)
        }
        try container.encodeIfPresent(ingredientName, forKey: .ingredientName)
    }
}

/// The product a household buys for an ingredient (`ShoppingPreference`).
nonisolated struct ShoppingPreference: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let provider: String
    let ingredientKey: String
    /// `nil` for a `name:` key.
    let ingredientID: String?
    let ingredientName: String
    let productID: String
    /// Rebuilt by the API from the item ID, never the pasted text.
    let productURLString: String
    let displayName: String
    /// `nil` when unknown.
    let packageSize: ShoppingAmount?
    let createdBy: String
    let createdAt: Date
    let updatedBy: String
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case id, provider, ingredientKey
        case ingredientID = "ingredientId"
        case ingredientName
        case productID = "productId"
        case productURLString = "productUrl"
        case displayName, packageSize, createdBy, createdAt, updatedBy, updatedAt
    }
}

nonisolated struct ShoppingPreferenceList: Decodable, Sendable {
    let items: [ShoppingPreference]
}

/// Saved Products, split so the products still missing a package size are listed first.
nonisolated struct SavedProductGroups: Equatable, Sendable {
    /// No package size saved, in the given order.
    let needsPackageSize: [ShoppingPreference]
    /// Everything else, in the given order.
    let complete: [ShoppingPreference]

    init(_ preferences: [ShoppingPreference]) {
        needsPackageSize = preferences.filter { $0.packageSize == nil }
        complete = preferences.filter { $0.packageSize != nil }
    }
}

/// What a Check Amount warning's fix does to the product's package size.
nonisolated enum ShoppingPackageSizeFix: Equatable, Sendable {
    /// No size is saved.
    case add
    /// A size is saved, but the recipe's amount can't be measured against it.
    case change

    var title: String {
        switch self {
        case .add: String(localized: "Add Package Size")
        case .change: String(localized: "Change Package Size")
        }
    }
}

// MARK: - Match and handoff

/// One selected line of a match or handoff request.
nonisolated struct ShoppingLineSelection: Encodable, Equatable, Sendable {
    var ingredientKey: String
    /// Overrides the computed package count; omitted to keep it.
    var packages: Int?
}

/// The body of `POST .../match` and `POST .../handoffs`. `nil` fields are omitted.
nonisolated struct ShoppingMatchRequest: Encodable, Equatable, Sendable {
    /// Exactly these lines are candidates, whatever their status.
    var lines: [ShoppingLineSelection]?
    var checkedOffKeys: [String]?
    var excludeKeys: [String]?

    private enum CodingKeys: String, CodingKey {
        case lines, checkedOffKeys, excludeKeys
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(lines, forKey: .lines)
        try container.encodeIfPresent(checkedOffKeys, forKey: .checkedOffKeys)
        try container.encodeIfPresent(excludeKeys, forKey: .excludeKeys)
    }
}

/// Why a line's package count needs checking. Unknown values decode as-is.
nonisolated struct ShoppingCheckReason: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let unitNotConvertible = ShoppingCheckReason(rawValue: "unit_not_convertible")
    static let noPackageSize = ShoppingCheckReason(rawValue: "no_package_size")
    static let packageCountCapped = ShoppingCheckReason(rawValue: "package_count_capped")
}

nonisolated struct ShoppingLineConfirmationStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let pending = ShoppingLineConfirmationStatus(rawValue: "pending")
    static let confirmed = ShoppingLineConfirmationStatus(rawValue: "confirmed")
    static let skipped = ShoppingLineConfirmationStatus(rawValue: "skipped")
}

/// What a member said about a handed-off line (`ShoppingLineConfirmation`).
nonisolated struct ShoppingLineConfirmation: Decodable, Hashable, Sendable {
    let status: ShoppingLineConfirmationStatus
    let packages: Int?
    let purchaseID: String?
    let confirmedAt: Date?
    let skippedAt: Date?

    private enum CodingKeys: String, CodingKey {
        case status, packages
        case purchaseID = "purchaseId"
        case confirmedAt, skippedAt
    }
}

/// The saved product a line matched.
nonisolated struct ShoppingLineProduct: Decodable, Hashable, Sendable {
    let productID: String
    let displayName: String
    let productURLString: String
    let packageSize: ShoppingAmount?

    private enum CodingKeys: String, CodingKey {
        case productID = "productId"
        case displayName
        case productURLString = "productUrl"
        case packageSize
    }
}

/// A grocery line with a saved product (`ShoppingHandoffLine`).
nonisolated struct ShoppingHandoffLine: Decodable, Hashable, Sendable, Identifiable {
    /// Stable within one match or handoff only, for example `l1`.
    let id: String
    let ingredientKey: String
    let ingredientID: String?
    let name: String
    let category: String
    let amounts: [ShoppingAmount]
    let quantityText: String
    let unquantified: Bool
    let groceryStatus: GroceryItemStatus
    let product: ShoppingLineProduct
    let computedPackages: Int
    /// What the cart link asks for.
    let packages: Int
    let packagesOverridden: Bool
    let checkAmount: Bool
    let reason: ShoppingCheckReason?
    /// For example "Check amount: 4 cloves doesn't convert to a 3 ct package".
    let reasonText: String?
    /// For example "3 × 16 oz covers 36 oz", or "1 × 1 ct covers this week (4 cloves)".
    let coverageText: String
    /// The rule the count was computed under: `per_week` or `per_amount`.
    let coverage: String
    /// True when one package is assumed to cover the week because the need couldn't be
    /// measured against it. `checkAmount` is false in that case.
    let coversWeek: Bool
    /// What to search for to find this product again.
    let searchTerms: ShoppingSearchTerms
    /// `nil` on a match.
    let confirmation: ShoppingLineConfirmation?
    /// On a match, what the week's Walmart hand-off already put in the cart for this line.
    /// `nil` when nothing has been sent this week, and on stored handoffs.
    var cart: ShoppingLineCart? = nil
    /// The planned recipes this line is for; `nil` from a server that doesn't send them.
    var recipeRefs: [GroceryRecipe]? = nil

    /// The planned recipes this line is for, sorted by name. Empty for an extra, and on
    /// handoffs stored before the API recorded them.
    var recipes: [GroceryRecipe] { recipeRefs ?? [] }

    /// Packages already in the Walmart cart.
    var sentPackages: Int { cart?.sentPackages ?? 0 }

    /// Sent before and not wanted in larger numbers, as the API matched it: the Shop tab lists
    /// it under "In Walmart Cart".
    var isInCart: Bool {
        guard let cart else { return false }
        return cart.sentPackages > 0 && cart.addPackages == 0
    }

    /// How a Check Amount warning is fixed from the Shop tab: every reason but a capped count
    /// is about the product's package size, so the warning opens the product focused on it.
    /// `nil` when there's no warning or the size can't fix it.
    var packageSizeFix: ShoppingPackageSizeFix? {
        guard checkAmount else { return nil }
        switch reason {
        case .noPackageSize?: return .add
        case .unitNotConvertible?: return .change
        default: return nil
        }
    }

    private enum CodingKeys: String, CodingKey {
        case id, ingredientKey
        case ingredientID = "ingredientId"
        case name, category, amounts, quantityText, unquantified, groceryStatus, product, computedPackages, packages,
            packagesOverridden, checkAmount, reason, reasonText, coverageText, coverage, coversWeek, searchTerms,
            confirmation, cart
        case recipeRefs = "recipes"
    }
}

/// What a match line already has in the Walmart cart (`ShoppingLineCart`). A cart link can only
/// add, so what went down is removed by the member in the Walmart app.
nonisolated struct ShoppingLineCart: Decodable, Hashable, Sendable {
    let sentPackages: Int
    let addPackages: Int
    let removePackages: Int
}

/// Why a product in the cart isn't one of the match's lines. Unknown values decode as-is.
nonisolated struct ShoppingSentReason: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let notOnList = ShoppingSentReason(rawValue: "not_on_list")
    static let productChanged = ShoppingSentReason(rawValue: "product_changed")
    static let notIncluded = ShoppingSentReason(rawValue: "not_included")
}

/// A product the week's hand-off put in the cart that isn't a match line (`ShoppingSentLine`).
nonisolated struct ShoppingSentLine: Decodable, Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let name: String
    let product: ShoppingLineProduct
    let sentPackages: Int
    let removePackages: Int
    let reason: ShoppingSentReason
    /// The API's display text, for example "No longer on the list; remove 2 in the Walmart app".
    let text: String

    var id: String { "\(ingredientKey)|\(product.productID)" }
}

/// What the week's current hand-off put in the cart (`ShoppingCartState`).
nonisolated struct ShoppingCartState: Decodable, Hashable, Sendable {
    let handoffID: String
    let sentAt: Date
    let other: [ShoppingSentLine]

    private enum CodingKeys: String, CodingKey {
        case handoffID = "handoffId"
        case sentAt, other
    }
}

/// The search the API suggests for a grocery line (`ShoppingSearchTerms`).
///
/// These are words to search Walmart with, not results: DinnerOS has no product-search API, so
/// it can't filter `avoid` out itself and shows it to the member instead.
nonisolated struct ShoppingSearchTerms: Decodable, Hashable, Sendable {
    /// What to search for, for example "fresh whole Garlic".
    let query: String
    /// The words added ahead of the ingredient name, for example ["fresh", "whole"]. Empty when
    /// the category has no bias, or the name already states a form ("Garlic Powder").
    let qualifiers: [String]
    /// Wrong-form products to skip, for example ["powder", "minced", "dried"].
    let avoid: [String]
    /// One sentence explaining the bias, or empty when none was applied.
    let why: String

    /// For a line the API didn't send terms for, such as a saved product opened from the
    /// Saved Products screen.
    static func plain(_ name: String) -> ShoppingSearchTerms {
        ShoppingSearchTerms(query: name, qualifiers: [], avoid: [], why: "")
    }
}

/// Why a grocery line isn't in the cart (`ShoppingExcludedLine.reason`). Unknown values decode as-is.
nonisolated struct ShoppingExclusionReason: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let inPantry = ShoppingExclusionReason(rawValue: "in_pantry")
    static let pantryHint = ShoppingExclusionReason(rawValue: "pantry_hint")
    static let houseMade = ShoppingExclusionReason(rawValue: "house_made")
    static let checkedOff = ShoppingExclusionReason(rawValue: "checked_off")
    static let excluded = ShoppingExclusionReason(rawValue: "excluded")
    static let notSelected = ShoppingExclusionReason(rawValue: "not_selected")
    /// No saved product yet: the line needs one chosen.
    static let noProduct = ShoppingExclusionReason(rawValue: "no_product")
    static let notOnList = ShoppingExclusionReason(rawValue: "not_on_list")
}

/// A grocery line left out of the cart (`ShoppingExcludedLine`).
nonisolated struct ShoppingExcludedLine: Decodable, Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let ingredientID: String?
    /// Empty for `not_on_list`.
    let name: String
    let category: String
    let amounts: [ShoppingAmount]
    let quantityText: String
    let unquantified: Bool
    let groceryStatus: GroceryItemStatus?
    let reason: ShoppingExclusionReason
    /// The API's display text, for example "Choose a Walmart product".
    let text: String
    /// What to search for when choosing a product for this line.
    let searchTerms: ShoppingSearchTerms

    var id: String { "\(reason.rawValue)|\(ingredientKey)" }

    private enum CodingKeys: String, CodingKey {
        case ingredientKey
        case ingredientID = "ingredientId"
        case name, category, amounts, quantityText, unquantified, groceryStatus, reason, text, searchTerms
    }
}

/// One add-to-cart link (`ShoppingCartLink`). Open them in order; each fills the same cart.
nonisolated struct ShoppingCartLink: Decodable, Hashable, Sendable {
    let urlString: String
    let lineIDs: [String]
    /// Distinct products in the link.
    let itemCount: Int

    var url: URL? { URL(string: urlString) }

    private enum CodingKeys: String, CodingKey {
        case urlString = "url"
        case lineIDs = "lineIds"
        case itemCount
    }
}

/// Response to `POST .../match` (`ShoppingProposal`): nothing is stored.
nonisolated struct ShoppingProposal: Decodable, Hashable, Sendable {
    let provider: String
    let week: String
    let storeID: String?
    /// Lines with a saved product, in aisle order.
    let lines: [ShoppingHandoffLine]
    let excluded: [ShoppingExcludedLine]
    let cartLinks: [ShoppingCartLink]
    let affiliateTracked: Bool
    /// Set on a match once this week's list went to Walmart; `cartLinks` then add only what
    /// isn't in the cart yet.
    var cart: ShoppingCartState? = nil

    /// Lines still to send: never sent, or wanted in larger numbers than the cart holds.
    var linesToSend: [ShoppingHandoffLine] {
        lines.filter { !$0.isInCart }
    }

    /// Lines already in the Walmart cart at the matched count or more.
    var linesInCart: [ShoppingHandoffLine] {
        lines.filter(\.isInCart)
    }

    /// Products in the cart that aren't lines: off the list, replaced, or left out.
    var otherInCart: [ShoppingSentLine] {
        cart?.other ?? []
    }

    /// Lines without a saved product. The API lists them in `excluded`, not `lines`.
    var needsProduct: [ShoppingExcludedLine] {
        excluded.filter { $0.reason == .noProduct }
    }

    /// Lines left out for any other reason: at home, checked off, and so on.
    var notIncluded: [ShoppingExcludedLine] {
        excluded.filter { $0.reason != .noProduct }
    }

    private enum CodingKeys: String, CodingKey {
        case provider, week
        case storeID = "storeId"
        case lines, excluded, cartLinks, affiliateTracked, cart
    }
}

nonisolated struct ShoppingHandoffStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Some line is pending.
    static let open = ShoppingHandoffStatus(rawValue: "open")
    static let done = ShoppingHandoffStatus(rawValue: "done")
}

/// A stored handoff (`ShoppingHandoff`): a proposal snapshot with an ID and confirmations.
nonisolated struct ShoppingHandoff: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let status: ShoppingHandoffStatus
    let createdBy: String
    let createdAt: Date
    let updatedAt: Date
    let proposal: ShoppingProposal

    var week: String { proposal.week }
    var lines: [ShoppingHandoffLine] { proposal.lines }
    var cartLinks: [ShoppingCartLink] { proposal.cartLinks }

    init(
        id: String, status: ShoppingHandoffStatus, createdBy: String, createdAt: Date, updatedAt: Date,
        proposal: ShoppingProposal
    ) {
        self.id = id
        self.status = status
        self.createdBy = createdBy
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.proposal = proposal
    }

    private enum CodingKeys: String, CodingKey {
        case id, status, createdBy, createdAt, updatedAt
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            status: try container.decode(ShoppingHandoffStatus.self, forKey: .status),
            createdBy: try container.decode(String.self, forKey: .createdBy),
            createdAt: try container.decode(Date.self, forKey: .createdAt),
            updatedAt: try container.decode(Date.self, forKey: .updatedAt),
            proposal: try ShoppingProposal(from: decoder))
    }
}

nonisolated struct ShoppingHandoffList: Decodable, Sendable {
    let items: [ShoppingHandoff]
}

// MARK: - Confirm

/// One line a member says was ordered.
nonisolated struct ConfirmedOrderLine: Encodable, Equatable, Sendable {
    var lineID: String
    /// What was ordered; omitted to use the line's count.
    var packages: Int?

    private enum CodingKeys: String, CodingKey {
        case lineID = "lineId"
        case packages
    }
}

/// The body of `POST .../handoffs/{handoffId}/confirm`.
nonisolated enum ConfirmShoppingOrderRequest: Encodable, Equatable, Sendable {
    /// Every line that isn't skipped, at its package count.
    case all
    /// These lines; `skipRest` marks the other pending lines as not ordered.
    case lines([ConfirmedOrderLine], skipRest: Bool)

    private enum CodingKeys: String, CodingKey {
        case all, lines, skipRest
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .all:
            try container.encode(true, forKey: .all)
        case .lines(let lines, let skipRest):
            try container.encode(lines, forKey: .lines)
            if skipRest {
                try container.encode(true, forKey: .skipRest)
            }
        }
    }
}

/// A pantry purchase recorded for a confirmed line.
nonisolated struct ShoppingConfirmedPurchase: Decodable, Hashable, Sendable {
    let lineID: String
    let ingredientKey: String
    /// `false` when the line was already confirmed; the first purchase is returned.
    let created: Bool
    /// Read leniently: the order is recorded even when this build can't read the purchase.
    let purchase: PantryPurchase?
    let item: PantryItem?

    private enum CodingKeys: String, CodingKey {
        case lineID = "lineId"
        case ingredientKey, created, purchase, item
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        lineID = try container.decode(String.self, forKey: .lineID)
        ingredientKey = try container.decode(String.self, forKey: .ingredientKey)
        created = try container.decode(Bool.self, forKey: .created)
        purchase = try? container.decodeIfPresent(PantryPurchase.self, forKey: .purchase)
        item = try? container.decodeIfPresent(PantryItem.self, forKey: .item)
    }
}

/// Response to `POST .../confirm`.
nonisolated struct ConfirmShoppingOrderResponse: Decodable, Hashable, Sendable {
    let handoff: ShoppingHandoff
    let purchases: [ShoppingConfirmedPurchase]
}

// MARK: - Formatting

nonisolated enum ShoppingText {
    /// "1 package" or "3 packages".
    static func packages(_ count: Int) -> String {
        count == 1 ? String(localized: "1 package") : String(localized: "\(count) packages")
    }

    /// "In Walmart cart · 2".
    static func inCart(_ count: Int) -> String {
        String(localized: "In Walmart cart · \(count)")
    }

    /// A link can't take packages out of the cart, so the member does: "Remove 1 in the Walmart app".
    static func removeInWalmart(_ count: Int) -> String {
        String(localized: "Remove \(count) in the Walmart app")
    }

    /// What the next "Open in Walmart" adds to a line already in the cart: "Adds 1 more".
    static func addsMore(_ count: Int) -> String {
        String(localized: "Adds \(count) more")
    }

    /// A sent line against the count wanted now: "In Walmart cart · 2", with "Adds 1 more" or
    /// "Remove 1 in the Walmart app" when they differ.
    static func cartStatus(sent: Int, wanted: Int) -> String {
        if wanted > sent {
            return "\(inCart(sent)) · \(addsMore(wanted - sent))"
        }
        if wanted < sent {
            return "\(inCart(sent)) · \(removeInWalmart(sent - wanted))"
        }
        return inCart(sent)
    }

    static let everythingInCart = String(localized: "Everything is already in your Walmart cart")
}
