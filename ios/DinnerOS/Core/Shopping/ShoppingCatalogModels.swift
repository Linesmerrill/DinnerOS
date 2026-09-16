import Foundation

/// Limits for a store request, mirrored from the API for input validation.
nonisolated enum ShoppingRequestLimits {
    static let maxNoteLength = 280
    static let maxNameLength = 100
}

/// What kind of place a catalog entry is (`ShoppingCatalogItem.kind`). Unknown values decode
/// as-is, so a kind added by a newer API still lists.
nonisolated struct ShoppingStoreKind: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let grocer = ShoppingStoreKind(rawValue: "grocer")
    static let delivery = ShoppingStoreKind(rawValue: "delivery")
    static let warehouse = ShoppingStoreKind(rawValue: "warehouse")
    static let other = ShoppingStoreKind(rawValue: "other")

    /// The row's small kind icon. An unknown kind reads as `other`.
    var systemImage: String {
        switch self {
        case .grocer: "storefront"
        case .delivery: "bicycle"
        case .warehouse: "shippingbox"
        default: "building.2"
        }
    }

    /// Spoken instead of the icon, which VoiceOver can't read on its own.
    var name: String {
        switch self {
        case .grocer: String(localized: "Grocery store")
        case .delivery: String(localized: "Delivery service")
        case .warehouse: String(localized: "Warehouse club")
        default: String(localized: "Store")
        }
    }
}

/// How far along a store is (`ShoppingCatalogItem.status`). Unknown values decode as-is and
/// show no chip, like `unsupported`.
nonisolated struct ShoppingCatalogStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Usable today, so the row opens store setup instead of asking for it.
    static let available = ShoppingCatalogStatus(rawValue: "available")
    /// Researched, not built: "Looking into it".
    static let researched = ShoppingCatalogStatus(rawValue: "researched")
    /// Nothing known yet. Requests are what decide it.
    static let unsupported = ShoppingCatalogStatus(rawValue: "unsupported")
}

/// A store DinnerOS knows about, supported or not (`GET /api/v1/shopping/catalog`).
///
/// Only `key`, `name`, `kind`, and `status` are required: the counts and the household's own
/// flag are additive, so a response without them still lists.
nonisolated struct ShoppingCatalogItem: Decodable, Hashable, Sendable, Identifiable {
    let key: String
    let name: String
    let kind: ShoppingStoreKind
    let status: ShoppingCatalogStatus
    /// Other names members search by, for example "frys" for Fry's.
    var aliases: [String] = []
    /// The API's own sentence about the store, when it has one.
    var note: String?
    /// This household already asked for it.
    var requestedByHousehold = false
    /// How many households asked, including this one.
    var requests = 0

    var id: String { key }

    /// Whether `query` matches the name or an alias. Case- and diacritic-insensitive, so
    /// "traders" finds "Trader Joe's".
    func matches(_ query: String) -> Bool {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return true }
        return ([name] + aliases).contains {
            $0.range(of: trimmed, options: [.caseInsensitive, .diacriticInsensitive]) != nil
        }
    }

    /// Quiet social proof, shown only when more than one household asked.
    var socialProof: String? {
        guard requests > 1 else { return nil }
        return String(localized: "\(requests) households asked")
    }

    private enum CodingKeys: String, CodingKey {
        case key, name, kind, status, aliases, note, requestedByHousehold, requests
    }

    nonisolated init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            key: try container.decode(String.self, forKey: .key),
            name: try container.decode(String.self, forKey: .name),
            kind: try container.decode(ShoppingStoreKind.self, forKey: .kind),
            status: try container.decode(ShoppingCatalogStatus.self, forKey: .status),
            aliases: (try? container.decodeIfPresent([String].self, forKey: .aliases)) ?? [],
            note: try? container.decodeIfPresent(String.self, forKey: .note),
            requestedByHousehold: (try? container.decodeIfPresent(Bool.self, forKey: .requestedByHousehold)) ?? false,
            requests: (try? container.decodeIfPresent(Int.self, forKey: .requests)) ?? 0)
    }

    /// For previews and the optimistic row after a request is sent.
    init(
        key: String, name: String, kind: ShoppingStoreKind, status: ShoppingCatalogStatus, aliases: [String] = [],
        note: String? = nil, requestedByHousehold: Bool = false, requests: Int = 0
    ) {
        self.key = key
        self.name = name
        self.kind = kind
        self.status = status
        self.aliases = aliases
        self.note = note
        self.requestedByHousehold = requestedByHousehold
        self.requests = requests
    }
}

nonisolated struct ShoppingCatalogList: Decodable, Sendable {
    let items: [ShoppingCatalogItem]
}

/// A store this household asked for (`.../shopping/requests`).
nonisolated struct ShoppingStoreRequest: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// `nil` when the member typed a name the catalog doesn't have.
    let key: String?
    let name: String
    let status: ShoppingCatalogStatus
    let note: String?
    let requestedBy: String
    let requestedAt: Date

    private enum CodingKeys: String, CodingKey {
        case id, key, name, status, note, requestedBy, requestedAt
    }
}

nonisolated struct ShoppingStoreRequestList: Decodable, Sendable {
    let items: [ShoppingStoreRequest]
}

/// The `{"request": …}` envelope `POST .../shopping/requests` answers with.
nonisolated struct ShoppingStoreRequestEnvelope: Decodable, Sendable {
    let request: ShoppingStoreRequest
}

/// The body of `POST .../shopping/requests`: a catalog `key`, or a `name` the member typed.
nonisolated struct CreateShoppingStoreRequest: Encodable, Equatable, Sendable {
    var key: String?
    var name: String?
    var note: String?

    /// Asks for a catalog entry.
    static func key(_ key: String, note: String? = nil) -> CreateShoppingStoreRequest {
        CreateShoppingStoreRequest(key: key, name: nil, note: Self.trimmedNote(note))
    }

    /// Asks for a store the catalog doesn't list.
    static func name(_ name: String, note: String? = nil) -> CreateShoppingStoreRequest {
        CreateShoppingStoreRequest(
            key: nil,
            name: String(
                name.trimmingCharacters(in: .whitespacesAndNewlines).prefix(
                    ShoppingRequestLimits.maxNameLength)),
            note: Self.trimmedNote(note))
    }

    /// The note within the API's limit, or `nil` when it's blank.
    private static func trimmedNote(_ note: String?) -> String? {
        let trimmed = note?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? nil : String(trimmed.prefix(ShoppingRequestLimits.maxNoteLength))
    }

    private enum CodingKeys: String, CodingKey {
        case key, name, note
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(key, forKey: .key)
        try container.encodeIfPresent(name, forKey: .name)
        try container.encodeIfPresent(note, forKey: .note)
    }
}
