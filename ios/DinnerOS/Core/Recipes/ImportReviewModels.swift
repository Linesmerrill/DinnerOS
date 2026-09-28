import Foundation

/// One thing the importer could not map confidently (`ImportReview` in `api/openapi.yaml`).
///
/// These are import bookkeeping, not recipe data: the importer wrote them while reading the
/// source, and only a person closes one (`POST .../import-reviews/resolve`). A `variant` item
/// means HelloFresh redirected a weekly menu clone to a canonical recipe page, so the stored
/// details came from that page while the box may have held a different variant. `value` is
/// the delivered variant's name.
nonisolated struct ImportReview: Decodable, Hashable, Sendable, Identifiable {
    /// The server's id for the item. Resolving sends it.
    let id: String
    /// The household recipe this is about. `nil` when no stored recipe carries the source ID
    /// any more, so there is nothing to open.
    let recipeID: String?
    /// That recipe's photo, when it has one.
    let recipeImageURLString: String?
    /// The stored recipe's name: what the app cooks, shops, and computes allergens from.
    let recipeName: String
    let source: String
    let sourceRecipeID: String
    /// What could not be mapped: `variant`, `steps`, `cookTime`, or `ingredients.<name>.unit`.
    let field: String
    /// What the source said; empty when the source said nothing. For a `variant` item this is
    /// the name of the variant the box actually held.
    let value: String
    let reason: String
    let status: String
    let createdAt: Date

    var recipeImageURL: URL? { recipeImageURLString.flatMap { URL(string: $0) } }

    init(
        id: String? = nil, recipeID: String?, recipeImageURLString: String? = nil, recipeName: String,
        source: String, sourceRecipeID: String, field: String, value: String = "", reason: String,
        status: String = ImportReview.openStatus, createdAt: Date
    ) {
        // One source clone flags one field at most once, so the fallback is unique within a
        // household. Only previews and a server older than the id rely on it.
        self.id = id ?? "\(sourceRecipeID)|\(field)|\(value)"
        self.recipeID = recipeID
        self.recipeImageURLString = recipeImageURLString
        self.recipeName = recipeName
        self.source = source
        self.sourceRecipeID = sourceRecipeID
        self.field = field
        self.value = value
        self.reason = reason
        self.status = status
        self.createdAt = createdAt
    }

    static let openStatus = "open"

    private enum CodingKeys: String, CodingKey {
        case id
        case recipeID = "recipeId"
        case recipeImageURLString = "recipeImageUrl"
        case recipeName, source
        case sourceRecipeID = "sourceRecipeId"
        case field, value, reason, status, createdAt
    }

    /// `recipeId`, `recipeImageUrl` and `value` are `omitempty` on the server, so they are
    /// optional here, and an item whose `field` this build doesn't know still decodes and shows
    /// as an other gap.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decodeIfPresent(String.self, forKey: .id),
            recipeID: try container.decodeIfPresent(String.self, forKey: .recipeID),
            recipeImageURLString: try container.decodeIfPresent(String.self, forKey: .recipeImageURLString),
            recipeName: try container.decode(String.self, forKey: .recipeName),
            source: try container.decode(String.self, forKey: .source),
            sourceRecipeID: try container.decode(String.self, forKey: .sourceRecipeID),
            field: try container.decode(String.self, forKey: .field),
            value: try container.decodeIfPresent(String.self, forKey: .value) ?? "",
            reason: try container.decode(String.self, forKey: .reason),
            status: try container.decode(String.self, forKey: .status),
            createdAt: try container.decode(Date.self, forKey: .createdAt))
    }

    /// What the item is about, for grouping. `field` is a string on the wire, so an unknown
    /// one is carried rather than dropped.
    enum Kind: Hashable, Sendable {
        /// The delivered box may have been a different variant than the stored page.
        case variant
        /// The recipe page had no instructions.
        case steps
        /// Neither a prep nor a total time was on the page.
        case cookTime
        /// An unknown unit the importer refused to guess, for the named ingredient.
        case ingredientUnit(ingredient: String)
        /// Anything a later importer flags that this build doesn't know about.
        case other
    }

    var kind: Kind {
        switch field {
        case "variant" where !value.isEmpty:
            return .variant
        case "steps":
            return .steps
        case "cookTime":
            return .cookTime
        default:
            if field.hasPrefix(Self.ingredientPrefix), field.hasSuffix(Self.unitSuffix) {
                let name = field.dropFirst(Self.ingredientPrefix.count).dropLast(Self.unitSuffix.count)
                return .ingredientUnit(ingredient: String(name))
            }
            return .other
        }
    }

    private static let ingredientPrefix = "ingredients."
    private static let unitSuffix = ".unit"
}

/// Response to `GET /api/v1/households/{householdId}/recipes/import-reviews`.
nonisolated struct ImportReviewListResponse: Decodable, Equatable, Sendable {
    let items: [ImportReview]
}

/// What a member decided about review items (`resolution` in `ResolveImportReviewsRequest`).
nonisolated enum ImportReviewResolution: String, Encodable, Sendable, CaseIterable {
    /// The delivered variant is the stored recipe. The import already counted that delivery
    /// toward it, so nothing else changes.
    case sameRecipe = "same_recipe"
    /// The delivered variant is its own dish. The stored recipe stays as it is; the member
    /// added the delivered one by hand, and it may be linked.
    case differentRecipe = "different_recipe"
    /// Nothing to do.
    case dismissed

    /// Whether the server applies it only to `variant` items. Anything else sent with it stays
    /// open, so the store never removes those from the list either.
    var isVariantOnly: Bool { self != .dismissed }

    func applies(to item: ImportReview) -> Bool {
        !isVariantOnly || item.kind == .variant
    }
}

/// Body of `POST /api/v1/households/{householdId}/recipes/import-reviews/resolve`.
nonisolated struct ResolveImportReviewsRequest: Encodable, Equatable, Sendable {
    let ids: [String]
    let resolution: String
    let recipeId: String?
}

/// Response to `POST .../import-reviews/resolve`.
nonisolated struct ResolveImportReviewsResponse: Decodable, Equatable, Sendable {
    let resolved: Int
}

/// Every `variant` item recorded against one stored recipe: the deliveries whose box may not
/// have matched the page the details came from.
///
/// A recipe is a merge across many deliveries, so this is never "the recipe is wrong". It is
/// "on at least one delivered week the box differed".
nonisolated struct ImportVariantGroup: Hashable, Sendable, Identifiable {
    /// The recipe to open; `nil` when no stored recipe carries the source ID any more.
    let recipeID: String?
    /// What the app stores for this dish.
    let storedName: String
    /// The stored recipe's photo, when it has one.
    let imageURL: URL?
    /// The distinct delivered variant names, oldest flagged first.
    let deliveredNames: [String]
    /// The review items behind this group. Resolving the group resolves all of them.
    let items: [ImportReview]

    /// How many delivered clones were flagged. Not how many weeks: see `ImportReviewDigest`.
    var flaggedDeliveries: Int { items.count }

    var id: String { recipeID ?? storedName }

    /// Whether every delivered name is the stored name written differently, by
    /// `ImportVariantMatch.isSpellingOnly`. A heuristic, and deliberately the conservative
    /// direction: anything it isn't sure about counts as a real difference.
    var isSpellingOnly: Bool {
        deliveredNames.allSatisfy { ImportVariantMatch.isSpellingOnly(stored: storedName, delivered: $0) }
    }
}

/// The client-side guess at which variant items are worth a second look.
///
/// The API deliberately records no severity (docs/architecture.md #463): deciding this is a
/// name-matching heuristic, and the one place that comparison belongs is `sameVariant` in the
/// importer. This is the screen's own conservative version of it, and the UI says so.
nonisolated enum ImportVariantMatch {
    /// `true` when the two names differ only in case, spacing, punctuation, or accents, like
    /// "honey butter cornbread" against "Honey Butter Corn Bread".
    ///
    /// Everything else is treated as a real difference, including a name this can't read, so a
    /// genuine protein swap is never hidden by a clever match.
    static func isSpellingOnly(stored: String, delivered: String) -> Bool {
        let stored = normalized(stored)
        guard !stored.isEmpty else { return false }
        return stored == normalized(delivered)
    }

    /// Lowercased, accents folded, and everything that isn't a letter or digit removed, so
    /// spacing and punctuation can't make two spellings look like two dishes.
    static func normalized(_ name: String) -> String {
        let folded = name.folding(options: [.diacriticInsensitive, .caseInsensitive, .widthInsensitive], locale: nil)
        return String(folded.unicodeScalars.filter(CharacterSet.alphanumerics.contains).map(Character.init))
    }
}

/// The backlog sorted into what the owner should actually look at.
///
/// Groups keep the API's order (oldest flagged first), and a recipe appears in exactly one of
/// `differences` and `spellingOnly`.
nonisolated struct ImportReviewDigest: Equatable, Sendable {
    /// Recipes where a delivered box carried a name this build reads as a different dish.
    let differences: [ImportVariantGroup]
    /// Recipes where every delivered name is the stored name written differently.
    let spellingOnly: [ImportVariantGroup]
    /// Missing steps, unknown units, and anything else the importer flagged.
    let otherItems: [ImportReview]

    var isEmpty: Bool { differences.isEmpty && spellingOnly.isEmpty && otherItems.isEmpty }

    /// Every variant item, however it was sorted.
    var variantRecipeCount: Int { differences.count + spellingOnly.count }

    /// The rows the screen lists: one per recipe for box differences and spellings, one per
    /// item for the rest. It is the sum of the three tiles at the top of the screen, and the
    /// only number any Import Review badge shows, so a badge and the screen always agree.
    var openCount: Int { differences.count + spellingOnly.count + otherItems.count }

    init(items: [ImportReview]) {
        var order: [String] = []
        var grouped: [String: [ImportReview]] = [:]
        var others: [ImportReview] = []

        for item in items {
            switch item.kind {
            case .variant:
                // Clones of one dish merge under the newest source ID, so the stored recipe is
                // the identity here; a name is the fallback when no recipe carries the ID.
                let key = item.recipeID ?? "name:\(ImportVariantMatch.normalized(item.recipeName))"
                if grouped[key] == nil { order.append(key) }
                grouped[key, default: []].append(item)
            case .steps, .cookTime, .ingredientUnit, .other:
                others.append(item)
            }
        }

        var differences: [ImportVariantGroup] = []
        var spellingOnly: [ImportVariantGroup] = []
        for key in order {
            guard let members = grouped[key], let first = members.first else { continue }
            var delivered: [String] = []
            for member in members where !delivered.contains(member.value) {
                delivered.append(member.value)
            }
            let group = ImportVariantGroup(
                recipeID: first.recipeID,
                storedName: first.recipeName,
                imageURL: members.lazy.compactMap(\.recipeImageURL).first,
                deliveredNames: delivered,
                items: members)
            if group.isSpellingOnly {
                spellingOnly.append(group)
            } else {
                differences.append(group)
            }
        }

        self.differences = differences
        self.spellingOnly = spellingOnly
        otherItems = others
    }
}
