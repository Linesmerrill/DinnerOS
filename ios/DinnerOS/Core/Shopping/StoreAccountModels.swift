import Foundation

// A store the household signs in to and searches (Kroger's Fry's; decision 649 in
// docs/architecture.md): the account connection, the store finder, and product search.

/// The household's signed-in store account. Never its tokens.
nonisolated struct ShoppingConnection: Decodable, Hashable, Sendable {
    let provider: String
    let connected: Bool
    let connectedBy: String?
    let connectedAt: Date?
}

/// Where to send the member to sign in, and the URL scheme the sign-in ends on.
nonisolated struct ShoppingConnectStart: Decodable, Sendable {
    let authorizeURLString: String
    let callbackScheme: String

    var authorizeURL: URL? { URL(string: authorizeURLString) }

    private enum CodingKeys: String, CodingKey {
        case authorizeURLString = "authorizeUrl"
        case callbackScheme
    }
}

/// How a sign-in ended, read from the URL it came back on
/// (`dinneros://shopping/connected?provider=kroger&result=connected`).
nonisolated enum ShoppingConnectResult: String, Sendable {
    case connected
    case cancelled
    case failed

    init(callback url: URL) {
        let value = URLComponents(url: url, resolvingAgainstBaseURL: false)?
            .queryItems?.first { $0.name == "result" }?.value
        self = value.flatMap(Self.init(rawValue:)) ?? .failed
    }
}

/// A store from the store finder.
nonisolated struct ShoppingStoreLocation: Decodable, Hashable, Sendable, Identifiable {
    let storeID: String
    let name: String
    let address: String

    var id: String { storeID }

    private enum CodingKeys: String, CodingKey {
        case storeID = "storeId"
        case name, address
    }
}

nonisolated struct ShoppingStoreLocationList: Decodable, Sendable {
    let items: [ShoppingStoreLocation]
}

/// A product from the store's search, priced and stocked at the household's store.
nonisolated struct ShoppingSearchProduct: Decodable, Hashable, Sendable, Identifiable {
    let productID: String
    let name: String
    let brand: String?
    let size: String?
    let packageSize: ShoppingAmount?
    let imageURLString: String?
    let urlString: String
    let priceCents: Int?
    /// "high", "low", "out", or `nil` when the store didn't say.
    let stock: String?

    var id: String { productID }
    var imageURL: URL? { imageURLString.flatMap(URL.init(string:)) }
    var isOutOfStock: Bool { stock == "out" }

    private enum CodingKeys: String, CodingKey {
        case productID = "productId"
        case name, brand, size, packageSize
        case imageURLString = "imageUrl"
        case urlString = "url"
        case priceCents, stock
    }

    /// What saving this product for a line sends: its UPC, name, size, and price.
    func preferenceRequest(ingredientName: String) -> ShoppingPreferenceRequest {
        var request = ShoppingPreferenceRequest(
            product: .itemID(productID),
            displayName: String(name.prefix(ShoppingLimits.maxDisplayNameLength)),
            packageSize: packageSize.map { ShoppingPackageSizeInput(quantity: $0.quantity, unit: $0.unit) },
            ingredientName: ingredientName)
        if let priceCents {
            request.price = .set(priceCents)
        }
        return request
    }
}

nonisolated struct ShoppingSearchProductList: Decodable, Sendable {
    let items: [ShoppingSearchProduct]
}

/// A product pre-picked at one store for an ingredient the household saved a product for at
/// another (`GET .../{provider}/suggestions`).
nonisolated struct ShoppingMatchSuggestion: Decodable, Hashable, Sendable, Identifiable {
    struct Source: Decodable, Hashable, Sendable {
        let provider: String
        let productID: String
        let displayName: String
        let packageSize: ShoppingAmount?

        private enum CodingKeys: String, CodingKey {
            case provider, displayName, packageSize
            case productID = "productId"
        }
    }

    let ingredientKey: String
    let ingredientName: String
    let from: Source
    /// `nil` when the store found nothing that's clearly the same thing.
    let product: ShoppingSearchProduct?
    var alternatives: [ShoppingSearchProduct] = []

    var id: String { ingredientKey }

    private enum CodingKeys: String, CodingKey {
        case ingredientKey, ingredientName, from, product, alternatives
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        ingredientKey = try container.decode(String.self, forKey: .ingredientKey)
        ingredientName = try container.decode(String.self, forKey: .ingredientName)
        from = try container.decode(Source.self, forKey: .from)
        product = try container.decodeIfPresent(ShoppingSearchProduct.self, forKey: .product)
        alternatives = (try? container.decodeIfPresent([ShoppingSearchProduct].self, forKey: .alternatives)) ?? []
    }

    init(
        ingredientKey: String, ingredientName: String, from: Source, product: ShoppingSearchProduct?,
        alternatives: [ShoppingSearchProduct] = []
    ) {
        self.ingredientKey = ingredientKey
        self.ingredientName = ingredientName
        self.from = from
        self.product = product
        self.alternatives = alternatives
    }
}

/// One page of suggestions. `next` is the ingredient key to continue after, `nil` at the end.
nonisolated struct ShoppingMatchSuggestionPage: Decodable, Sendable {
    let items: [ShoppingMatchSuggestion]
    let next: String?
    let total: Int
}
