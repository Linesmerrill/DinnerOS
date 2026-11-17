import Foundation

/// A recipe in the global catalog, as a browse or search result
/// (`CatalogSummary` in `api/openapi.yaml`).
///
/// The catalog belongs to no household, so a summary carries no rating, no
/// order history, and no notes — only the recipe itself, plus whether this
/// household already has it.
nonisolated struct CatalogSummary: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// The catalog's identity for this recipe (`source:sourceRecipeId`, or a
    /// fingerprint). Two households' copies of one meal-kit recipe share it.
    let catalogKey: String
    /// Where the recipe came from, for example `hellofresh`.
    let source: String
    let name: String
    let headline: String?
    let imageURLString: String?
    let isAddon: Bool
    let totalMinutes: Int?
    let cookMinutes: Int?
    let timeBand: AutopilotTimeBand?
    let calories: Int?
    let proteinGrams: Int?
    let cuisines: [String]
    let tags: [String]
    /// True when the household already has this recipe.
    let inLibrary: Bool
    /// The household's own copy, when it has one.
    let libraryRecipeID: String?
    /// Why discovery ranked it here ("You like Thai"). Empty for search.
    let reasons: [String]
    /// A partner's paid placement; `nil` for every recipe a household shared.
    var promotion: CatalogPromotion? = nil

    /// `nil` when missing or not a valid URL; catalog data isn't trusted to be well formed.
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    /// The time to show, falling back to the source's total for an older server.
    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: nil, total: totalMinutes)
    }

    /// This recipe, marked as the household's own.
    func marked(libraryRecipeID: String) -> CatalogSummary {
        CatalogSummary(
            id: id, catalogKey: catalogKey, source: source, name: name, headline: headline,
            imageURLString: imageURLString, isAddon: isAddon, totalMinutes: totalMinutes, cookMinutes: cookMinutes,
            timeBand: timeBand, calories: calories, proteinGrams: proteinGrams, cuisines: cuisines, tags: tags,
            inLibrary: true, libraryRecipeID: libraryRecipeID, reasons: reasons, promotion: promotion)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, catalogKey, source, name, headline
        case imageURLString = "imageUrl"
        case isAddon, totalMinutes, cookMinutes, timeBand, calories, proteinGrams, cuisines, tags, inLibrary
        case libraryRecipeID = "libraryRecipeId"
        case imageCredit
        case reasons, promotion
    }
}

nonisolated extension CatalogSummary {
    /// Fields added after this client shipped decode leniently, as recipe summaries do.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            catalogKey: try container.decodeIfPresent(String.self, forKey: .catalogKey) ?? "",
            source: try container.decodeIfPresent(String.self, forKey: .source) ?? "",
            name: try container.decode(String.self, forKey: .name),
            headline: try container.decodeIfPresent(String.self, forKey: .headline),
            imageURLString: try container.decodeIfPresent(String.self, forKey: .imageURLString),
            isAddon: try container.decodeIfPresent(Bool.self, forKey: .isAddon) ?? false,
            totalMinutes: container.decodeLenientInt(forKey: .totalMinutes),
            cookMinutes: container.decodeLenientInt(forKey: .cookMinutes),
            timeBand: container.decodeLenient(AutopilotTimeBand.self, forKey: .timeBand),
            calories: container.decodeLenientInt(forKey: .calories),
            proteinGrams: container.decodeLenientInt(forKey: .proteinGrams),
            cuisines: try container.decodeIfPresent([String].self, forKey: .cuisines) ?? [],
            tags: try container.decodeIfPresent([String].self, forKey: .tags) ?? [],
            inLibrary: try container.decodeIfPresent(Bool.self, forKey: .inLibrary) ?? false,
            libraryRecipeID: try container.decodeIfPresent(String.self, forKey: .libraryRecipeID),
            reasons: try container.decodeIfPresent([String].self, forKey: .reasons) ?? [],
            promotion: container.decodeLenient(CatalogPromotion.self, forKey: .promotion))
    }
}

/// A partner's paid placement in Shared Recipes. The server writes the label.
nonisolated struct CatalogPromotion: Decodable, Hashable, Sendable {
    let label: String
    let partner: String?
}

/// One page of catalog results.
nonisolated struct CatalogListPage: Decodable, Equatable, Sendable {
    let items: [CatalogSummary]
    /// Send back as `cursor` for the next page; `nil` on the last page.
    let nextCursor: String?
    /// How many entries matched, capped by the server's paging limit.
    var total: Int = 0
    /// What to show when discovery has nothing new; `nil` from an older server.
    var emptyTitle: String? = nil
    var emptyText: String? = nil

    fileprivate enum CodingKeys: String, CodingKey {
        case items, nextCursor, total, emptyTitle, emptyText
    }
}

/// A catalog recipe in full (`GET .../catalog/recipes/{id}`).
nonisolated struct CatalogRecipe: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let catalogKey: String
    let source: String
    let name: String
    let headline: String?
    let description: String?
    let imageURLString: String?
    let sourceURLString: String?
    let isAddon: Bool
    let servings: [Int]
    let prepMinutes: Int?
    let totalMinutes: Int?
    let cookMinutes: Int?
    let cuisines: [String]
    let tags: [String]
    let allergens: [String]
    let ingredients: [CatalogRecipeIngredient]
    let steps: [RecipeStep]
    let inLibrary: Bool
    let libraryRecipeID: String?
    /// Who took a photo picked from photo search.
    var imageCredit: PhotoCredit? = nil

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }
    var sourceURL: URL? { sourceURLString.flatMap { URL(string: $0) } }

    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: prepMinutes, total: totalMinutes)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, catalogKey, source, name, headline, description
        case imageURLString = "imageUrl"
        case sourceURLString = "sourceUrl"
        case isAddon, servings, prepMinutes, totalMinutes, cookMinutes, cuisines, tags, allergens
        case ingredients, steps, inLibrary
        case libraryRecipeID = "libraryRecipeId"
    }
}

/// An ingredient line on a catalog recipe. It carries no grocery category: the
/// category is read from the ingredient catalog once the recipe is in a library.
nonisolated struct CatalogRecipeIngredient: Decodable, Equatable, Sendable, Identifiable {
    let ingredientID: String
    let name: String
    let pantryStaple: Bool
    let amounts: [RecipeAmount]

    var id: String { ingredientID + "/" + name }

    fileprivate enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, pantryStaple, amounts
    }
}

/// Result of adding a catalog recipe to the household's library.
nonisolated struct AddToLibraryResult: Decodable, Equatable, Sendable {
    /// The household's own copy.
    let recipeID: String
    /// False when the household already had it, which is not an error.
    let created: Bool

    fileprivate enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case created
    }
}

/// What the catalog browse and search screens ask for.
nonisolated struct CatalogQuery: Equatable, Sendable {
    var search: String = ""
    var cuisine: String?
    var tag: String?
    /// Total time at most this many minutes.
    var maxMinutes: Int?
    /// Serves at least this many.
    var minServings: Int?

    static let maxSearchLength = 100

    /// The search text as the server sees it: trimmed and length-capped.
    var normalizedSearch: String {
        String(search.trimmingCharacters(in: .whitespacesAndNewlines).prefix(Self.maxSearchLength))
    }

    /// Whether two queries would produce the same request.
    func isSameRequest(as other: CatalogQuery) -> Bool {
        normalizedSearch == other.normalizedSearch && cuisine == other.cuisine && tag == other.tag
            && maxMinutes == other.maxMinutes && minServings == other.minServings
    }

    var isSearching: Bool { !normalizedSearch.isEmpty }

    /// Whether any filter chip is set.
    var hasFilters: Bool { cuisine != nil || maxMinutes != nil || minServings != nil }

    /// The value set for a filter chip's query parameter, as the server named it.
    func value(forParam param: String) -> String? {
        switch param {
        case "cuisine": cuisine
        case "maxMinutes": maxMinutes.map(String.init)
        case "minServings": minServings.map(String.init)
        default: nil
        }
    }

    /// Sets (or with `nil`, clears) a filter chip's query parameter.
    mutating func setValue(_ value: String?, forParam param: String) {
        switch param {
        case "cuisine": cuisine = value
        case "maxMinutes": maxMinutes = value.flatMap { Int($0) }
        case "minServings": minServings = value.flatMap { Int($0) }
        default: break
        }
    }
}

/// The top of Shared Recipes (`GET .../discover/home`): rows the server picks,
/// the filter chips, and the words around Browse All.
nonisolated struct DiscoverHome: Decodable, Equatable, Sendable {
    var sections: [DiscoverSection]
    var filters: [CatalogFilterGroup]
    var browseTitle: String?
    var searchPrompt: String?
    var seeAllTitle: String?
    var emptyTitle: String?
    var emptyText: String?
    var noMatchTitle: String?
    var noMatchText: String?

    fileprivate enum CodingKeys: String, CodingKey {
        case sections, filters, browseTitle, searchPrompt, seeAllTitle, emptyTitle, emptyText, noMatchTitle,
            noMatchText
    }

    init(
        sections: [DiscoverSection], filters: [CatalogFilterGroup], browseTitle: String? = nil,
        searchPrompt: String? = nil, seeAllTitle: String? = nil, emptyTitle: String? = nil, emptyText: String? = nil,
        noMatchTitle: String? = nil, noMatchText: String? = nil
    ) {
        self.sections = sections
        self.filters = filters
        self.browseTitle = browseTitle
        self.searchPrompt = searchPrompt
        self.seeAllTitle = seeAllTitle
        self.emptyTitle = emptyTitle
        self.emptyText = emptyText
        self.noMatchTitle = noMatchTitle
        self.noMatchText = noMatchText
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            // A row with nothing in it is never shown, a paid one least of all; the server
            // leaves them out too.
            sections: (try c.decodeIfPresent([DiscoverSection].self, forKey: .sections) ?? [])
                .filter { !$0.items.isEmpty },
            filters: try c.decodeIfPresent([CatalogFilterGroup].self, forKey: .filters) ?? [],
            browseTitle: try c.decodeIfPresent(String.self, forKey: .browseTitle),
            searchPrompt: try c.decodeIfPresent(String.self, forKey: .searchPrompt),
            seeAllTitle: try c.decodeIfPresent(String.self, forKey: .seeAllTitle),
            emptyTitle: try c.decodeIfPresent(String.self, forKey: .emptyTitle),
            emptyText: try c.decodeIfPresent(String.self, forKey: .emptyText),
            noMatchTitle: try c.decodeIfPresent(String.self, forKey: .noMatchTitle),
            noMatchText: try c.decodeIfPresent(String.self, forKey: .noMatchText))
    }

    /// The chips to show when the server sent none (an older server): time and servings.
    static let fallbackFilters: [CatalogFilterGroup] = [
        CatalogFilterGroup(
            id: "time", title: String(localized: "Time"), param: "maxMinutes",
            options: [
                CatalogFilterOption(value: "30", label: String(localized: "30 min or less")),
                CatalogFilterOption(value: "45", label: String(localized: "45 min or less")),
                CatalogFilterOption(value: "60", label: String(localized: "1 hour or less")),
            ]),
        CatalogFilterGroup(
            id: "servings", title: String(localized: "Servings"), param: "minServings",
            options: [
                CatalogFilterOption(value: "2", label: "2+"), CatalogFilterOption(value: "4", label: "4+"),
                CatalogFilterOption(value: "6", label: "6+"),
            ]),
    ]
}

/// One row at the top of Shared Recipes, chosen by the server.
nonisolated struct DiscoverSection: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let title: String
    var subtitle: String?
    /// Only a paid row has one, and it is always shown ("Sponsored by X").
    var sponsorLabel: String?
    var items: [CatalogSummary]
    /// More recipes than `items` behind See All.
    var hasMore: Bool

    fileprivate enum CodingKeys: String, CodingKey {
        case id, title, subtitle, sponsorLabel, items, hasMore
    }

    init(
        id: String, title: String, subtitle: String? = nil, sponsorLabel: String? = nil, items: [CatalogSummary],
        hasMore: Bool = false
    ) {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.sponsorLabel = sponsorLabel
        self.items = items
        self.hasMore = hasMore
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try c.decode(String.self, forKey: .id), title: try c.decode(String.self, forKey: .title),
            subtitle: try c.decodeIfPresent(String.self, forKey: .subtitle),
            sponsorLabel: try c.decodeIfPresent(String.self, forKey: .sponsorLabel),
            items: try c.decodeIfPresent([CatalogSummary].self, forKey: .items) ?? [],
            hasMore: try c.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false)
    }
}

/// A row in full, a page at a time (`GET .../discover/sections/{id}`), for See All.
nonisolated struct DiscoverSectionPage: Decodable, Equatable, Sendable {
    let section: DiscoverSection
    let nextCursor: String?

    fileprivate enum CodingKeys: String, CodingKey { case nextCursor }

    init(from decoder: any Decoder) throws {
        section = try DiscoverSection(from: decoder)
        nextCursor = try decoder.container(keyedBy: CodingKeys.self).decodeIfPresent(String.self, forKey: .nextCursor)
    }
}

/// A filter chip (Cuisine, Time, Servings) and its choices, named by the server.
nonisolated struct CatalogFilterGroup: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let title: String
    /// The query parameter a choice's value is sent as.
    let param: String
    let options: [CatalogFilterOption]
}

nonisolated struct CatalogFilterOption: Decodable, Hashable, Sendable, Identifiable {
    let value: String
    let label: String
    var id: String { value }
}

/// Why someone reports a shared recipe (decision 686).
nonisolated enum CatalogReportReason: String, CaseIterable, Identifiable, Sendable {
    case inappropriate, copyright, spam, wrong, other

    var id: String { rawValue }

    var title: String {
        switch self {
        case .inappropriate: String(localized: "It's offensive or inappropriate")
        case .copyright: String(localized: "It's copied from somewhere else")
        case .spam: String(localized: "It's spam or an ad")
        case .wrong: String(localized: "It's not a real recipe")
        case .other: String(localized: "Something else")
        }
    }
}

/// The answer to a report.
nonisolated struct CatalogReportResult: Decodable, Equatable, Sendable {
    let hidden: Bool
    let message: String
}
