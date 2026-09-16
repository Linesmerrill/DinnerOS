import Foundation

/// What a specialty option is (`SpecialtyOption.type` in `api/openapi.yaml`). A value from a
/// newer server keeps its raw value and reads as neither case.
nonisolated struct SpecialtyOptionType: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Regular store ingredients for an amount of the specialty ingredient.
    static let storeAlternative = SpecialtyOptionType(rawValue: "store_alternative")
    /// A small recipe that makes a jar kept in the pantry.
    static let houseMadeBatch = SpecialtyOptionType(rawValue: "house_made_batch")

    var title: String {
        switch self {
        case .storeAlternative: String(localized: "Store Alternative")
        case .houseMadeBatch: String(localized: "House-Made Batch")
        default: rawValue.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }

    var systemImage: String {
        self == .houseMadeBatch ? "house" : "cart"
    }
}

/// The household's choice (`SpecialtyIngredient.choice.type`): an option's type, or `as_is`.
nonisolated struct SpecialtyChoiceType: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Keep the item on grocery lists by its own name and stop asking.
    static let asIs = SpecialtyChoiceType(rawValue: "as_is")
    static let storeAlternative = SpecialtyChoiceType(rawValue: "store_alternative")
    static let houseMadeBatch = SpecialtyChoiceType(rawValue: "house_made_batch")
}

/// Whether an option is curated (global, read-only) or the household's own.
nonisolated struct SpecialtyOptionSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let curated = SpecialtyOptionSource(rawValue: "curated")
    static let household = SpecialtyOptionSource(rawValue: "household")
}

/// The household's standing answer for the specialty ingredients nobody has chosen an option for
/// (`SpecialtySettings.strategy`). A value from a newer server keeps its raw value and reads as
/// neither case.
nonisolated struct SpecialtyStrategy: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Prefer a curated store alternative: quicker, tastes a little different.
    static let similar = SpecialtyStrategy(rawValue: "similar")
    /// Prefer a curated house-made batch: more work, closest to the original.
    static let closest = SpecialtyStrategy(rawValue: "closest")
    /// Apply nothing, and keep asking about every specialty ingredient.
    static let ask = SpecialtyStrategy(rawValue: "ask")

    /// What a household gets before it sets one.
    static let fallback = similar

    var systemImage: String {
        switch self {
        case .similar: "cart"
        case .closest: "house"
        case .ask: "questionmark.circle"
        default: "sparkles"
        }
    }
}

/// Where a specialty ingredient's current plan comes from (`SpecialtyIngredient.choiceSource`).
nonisolated struct SpecialtyChoiceSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// A member chose the option, including `as_is`, so it can be attributed to them.
    static let household = SpecialtyChoiceSource(rawValue: "household")
    /// Nobody chose: the household's strategy picked the option, so nobody may be credited.
    static let strategy = SpecialtyChoiceSource(rawValue: "strategy")
    /// Nothing applies, so the ingredient stays on the list by its own name. Spelled `none` on
    /// the wire; named `unset` here so it never reads as `Optional.none`.
    static let unset = SpecialtyChoiceSource(rawValue: "none")
}

/// One strategy in the words to show. The server writes `label` and `description`, so the app
/// renders the trade-off without hardcoding the copy.
nonisolated struct SpecialtyStrategyOption: Decodable, Hashable, Sendable, Identifiable {
    let value: SpecialtyStrategy
    let label: String
    let description: String

    var id: String { value.rawValue }
}

/// The household's specialty ingredient settings (`GET`/`PUT .../specialty-ingredients/settings`).
nonisolated struct SpecialtySettings: Decodable, Hashable, Sendable {
    let strategy: SpecialtyStrategy
    /// `nil` for a household that never set one: nobody has changed it, so nothing is attributed.
    let updatedBy: String?
    let updatedAt: Date?
    /// Every strategy, in the order to offer them.
    let options: [SpecialtyStrategyOption]

    init(
        strategy: SpecialtyStrategy, updatedBy: String? = nil, updatedAt: Date? = nil,
        options: [SpecialtyStrategyOption] = []
    ) {
        self.strategy = strategy
        self.updatedBy = updatedBy
        self.updatedAt = updatedAt
        self.options = options
    }

    /// The server's words for the current strategy, when it described it.
    var currentOption: SpecialtyStrategyOption? {
        options.first { $0.value == strategy }
    }

    /// Whether anyone has set the strategy. `false` means the household is on the API's default.
    var wasSet: Bool { updatedBy != nil || updatedAt != nil }

    private enum CodingKeys: String, CodingKey {
        case strategy, updatedBy, updatedAt, options
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            strategy: try container.decodeIfPresent(SpecialtyStrategy.self, forKey: .strategy) ?? .fallback,
            updatedBy: try container.decodeIfPresent(String.self, forKey: .updatedBy),
            updatedAt: try container.decodeIfPresent(Date.self, forKey: .updatedAt),
            options: try container.decodeIfPresent([SpecialtyStrategyOption].self, forKey: .options) ?? [])
    }
}

/// The body of `PUT .../specialty-ingredients/settings`.
nonisolated struct SpecialtySettingsUpdate: Encodable, Equatable, Sendable {
    let strategy: SpecialtyStrategy
}

/// An exact amount with display text (`SpecialtyAmount`).
nonisolated struct SpecialtyAmount: Decodable, Hashable, Sendable {
    /// Exact, as `"n"` or `"n/d"`.
    let quantity: String
    let quantityValue: Double
    /// A DinnerOS unit code.
    let unit: String
    /// For example "12 tbsp"; a `count` reads as the number alone.
    let text: String
}

/// An estimated packet size, so recipes that count packets convert.
nonisolated struct SpecialtyUnitSize: Decodable, Hashable, Sendable {
    let per: String
    let quantity: String
    let quantityValue: Double
    let unit: String
    let text: String
}

/// One ingredient of an option: per `per` for a store alternative, or for one batch.
nonisolated struct SpecialtyOptionIngredient: Decodable, Hashable, Sendable {
    let name: String
    /// `nil` for "to taste".
    let quantity: String?
    let quantityValue: Double?
    let unit: String?
    /// For example "3 tbsp Chili Powder".
    let text: String
    let category: String?
}

/// A store alternative or house-made batch (`SpecialtyOption`).
nonisolated struct SpecialtyOption: Decodable, Hashable, Sendable, Identifiable {
    /// A slug for curated options (`southwest-spice-blend.batch`), an ObjectID for household ones.
    let id: String
    let specialtyID: String
    let source: SpecialtyOptionSource
    let type: SpecialtyOptionType
    let name: String
    let notes: String
    /// The curated option suggested first.
    let isDefault: Bool
    /// Store alternatives: this much of the specialty ingredient is `ingredients`.
    let per: SpecialtyAmount?
    let ingredients: [SpecialtyOptionIngredient]
    let steps: [String]
    /// What one batch makes; `nil` for a store alternative.
    let batchYield: SpecialtyAmount?
    let shelfLifeDays: Int?
    let basedOnOptionID: String?
    /// English text from the API, for example "Makes about 12 tbsp and keeps 180 days."
    let summary: String
    let createdBy: String?
    let updatedBy: String?
    let createdAt: Date?
    let updatedAt: Date?

    var isHousehold: Bool { source == .household }

    var isBatch: Bool { type == .houseMadeBatch }

    private enum CodingKeys: String, CodingKey {
        case id
        case specialtyID = "specialtyId"
        case source, type, name, notes, isDefault, per, ingredients, steps
        case batchYield = "yield"
        case shelfLifeDays
        case basedOnOptionID = "basedOnOptionId"
        case summary, createdBy, updatedBy, createdAt, updatedAt
    }
}

/// What the household currently does about one specialty ingredient: the option a member chose,
/// or the one the household's standing strategy picked.
nonisolated struct SpecialtyChoice: Decodable, Hashable, Sendable {
    /// The option ID that means "keep as is".
    static let asIsOptionID = "as_is"

    /// `household` when a member chose it, `strategy` when the household's standing answer picked
    /// it. A server that doesn't send it only ever sent a member's own choice.
    let source: SpecialtyChoiceSource
    let optionID: String
    let type: SpecialtyChoiceType
    /// `nil` for `as_is`.
    let optionName: String?
    /// The strategy that picked the option; `nil` when a member chose it.
    let strategy: SpecialtyStrategy?
    /// `nil` when the household's standing strategy picked the option rather than a member:
    /// nobody chose it, so there is no chooser and no time they chose it.
    let chosenBy: String?
    let chosenAt: Date?

    init(
        optionID: String, type: SpecialtyChoiceType, optionName: String?,
        source: SpecialtyChoiceSource = .household, strategy: SpecialtyStrategy? = nil,
        chosenBy: String? = nil, chosenAt: Date? = nil
    ) {
        self.source = source
        self.optionID = optionID
        self.type = type
        self.optionName = optionName
        self.strategy = strategy
        self.chosenBy = chosenBy
        self.chosenAt = chosenAt
    }

    /// Nobody chose this: the household's strategy picked it, so it must not be attributed.
    var isFromStrategy: Bool { source == .strategy }

    private enum CodingKeys: String, CodingKey {
        case source
        case optionID = "optionId"
        case type, optionName, strategy, chosenBy, chosenAt
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            optionID: try container.decode(String.self, forKey: .optionID),
            type: try container.decode(SpecialtyChoiceType.self, forKey: .type),
            optionName: try container.decodeIfPresent(String.self, forKey: .optionName),
            source: try container.decodeIfPresent(SpecialtyChoiceSource.self, forKey: .source) ?? .household,
            strategy: try container.decodeIfPresent(SpecialtyStrategy.self, forKey: .strategy),
            chosenBy: try container.decodeIfPresent(String.self, forKey: .chosenBy),
            chosenAt: try container.decodeIfPresent(Date.self, forKey: .chosenAt))
    }
}

/// The pantry item holding a house-made batch.
nonisolated struct SpecialtyBatchStock: Decodable, Hashable, Sendable {
    let pantryItemID: String
    let status: PantryStatus
    /// `nil` when the item has no recorded amount.
    let remaining: SpecialtyAmount?
    let percentRemaining: Int?
    /// `YYYY-MM-DD`.
    let expiresOn: String?

    private enum CodingKeys: String, CodingKey {
        case pantryItemID = "pantryItemId"
        case status, remaining, percentRemaining, expiresOn
    }
}

/// A meal-kit blend, sauce, or concentrate the household's recipes use (`SpecialtyIngredient`).
nonisolated struct SpecialtyIngredient: Decodable, Hashable, Sendable, Identifiable {
    /// A stable slug, such as `southwest-spice-blend`.
    let id: String
    /// The normalized name; also the batch's pantry item key.
    let key: String
    let name: String
    let aliases: [String]
    let category: String
    let ingredientIDs: [String]
    /// The household's recipes that use it.
    let recipeCount: Int
    let unitSizes: [SpecialtyUnitSize]
    let defaultOptionID: String
    let retired: Bool
    /// Where `choice` comes from: a member (`household`), the household's standing strategy
    /// (`strategy`), or nothing at all (`unset`).
    let choiceSource: SpecialtyChoiceSource
    /// The current plan, whoever or whatever produced it; `nil` when nothing applies.
    let choice: SpecialtyChoice?
    /// Curated options first, then the household's, oldest first.
    let options: [SpecialtyOption]
    let batch: SpecialtyBatchStock?

    init(
        id: String, key: String, name: String, aliases: [String], category: String, ingredientIDs: [String],
        recipeCount: Int, unitSizes: [SpecialtyUnitSize], defaultOptionID: String, retired: Bool,
        choice: SpecialtyChoice?, options: [SpecialtyOption], batch: SpecialtyBatchStock?,
        choiceSource: SpecialtyChoiceSource? = nil
    ) {
        self.id = id
        self.key = key
        self.name = name
        self.aliases = aliases
        self.category = category
        self.ingredientIDs = ingredientIDs
        self.recipeCount = recipeCount
        self.unitSizes = unitSizes
        self.defaultOptionID = defaultOptionID
        self.retired = retired
        // A server that doesn't say only ever sent a member's own choice.
        self.choiceSource = choiceSource ?? (choice.map(\.source) ?? .unset)
        self.choice = choice
        self.options = options
        self.batch = batch
    }

    /// The option in force; `nil` without one or for `as_is`.
    var chosenOption: SpecialtyOption? {
        guard let choice else { return nil }
        return options.first { $0.id == choice.optionID }
    }

    var isKeptAsIs: Bool { choice?.type == .asIs }

    var hasBatchChoice: Bool { choice?.type == .houseMadeBatch }

    /// A member's own decision, as opposed to one the strategy picked or none at all. Only this
    /// may be attributed to someone, and only this can be cleared.
    var hasHouseholdChoice: Bool { choiceSource == .household && choice != nil }

    /// The household's standing strategy picked the option. Nobody chose it, so nobody is
    /// credited for it, and a member can still override it.
    var isResolvedByStrategy: Bool { choiceSource == .strategy && choice != nil }

    /// Whether `option` is the current plan, whoever or whatever produced it.
    func isChosen(_ option: SpecialtyOption) -> Bool {
        choice?.optionID == option.id
    }

    /// Whether a member chose `option`. A strategy's pick is not a choice, so it stays offerable.
    func isHouseholdChoice(_ option: SpecialtyOption) -> Bool {
        hasHouseholdChoice && choice?.optionID == option.id
    }

    /// The API allows `SpecialtiesAPI.maxHouseholdOptions` household options per ingredient.
    var canAddHouseholdOption: Bool {
        options.count(where: \.isHousehold) < SpecialtiesAPI.maxHouseholdOptions
    }

    private enum CodingKeys: String, CodingKey {
        case id, key, name, aliases, category
        case ingredientIDs = "ingredientIds"
        case recipeCount, unitSizes
        case defaultOptionID = "defaultOptionId"
        case retired, choiceSource, choice, options, batch
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            key: try container.decode(String.self, forKey: .key),
            name: try container.decode(String.self, forKey: .name),
            aliases: try container.decodeIfPresent([String].self, forKey: .aliases) ?? [],
            category: try container.decodeIfPresent(String.self, forKey: .category) ?? "",
            ingredientIDs: try container.decodeIfPresent([String].self, forKey: .ingredientIDs) ?? [],
            recipeCount: try container.decodeIfPresent(Int.self, forKey: .recipeCount) ?? 0,
            unitSizes: try container.decodeIfPresent([SpecialtyUnitSize].self, forKey: .unitSizes) ?? [],
            defaultOptionID: try container.decodeIfPresent(String.self, forKey: .defaultOptionID) ?? "",
            retired: try container.decodeIfPresent(Bool.self, forKey: .retired) ?? false,
            choice: try container.decodeIfPresent(SpecialtyChoice.self, forKey: .choice),
            options: try container.decodeIfPresent([SpecialtyOption].self, forKey: .options) ?? [],
            batch: try container.decodeIfPresent(SpecialtyBatchStock.self, forKey: .batch),
            choiceSource: try container.decodeIfPresent(SpecialtyChoiceSource.self, forKey: .choiceSource))
    }
}

/// Response to `GET .../specialty-ingredients`.
nonisolated struct SpecialtyIngredientList: Decodable, Equatable, Sendable {
    let items: [SpecialtyIngredient]
}

/// Response to `POST .../specialty-ingredients/choices/defaults`.
nonisolated struct SpecialtyDefaultsResponse: Decodable, Equatable, Sendable {
    /// The specialty ingredients that got their default.
    let items: [SpecialtyIngredient]
    /// How many already had a choice.
    let skipped: Int
}

// MARK: - Requests

/// The body of `PUT .../{specialtyId}/choice`.
nonisolated struct SpecialtyChoiceRequest: Encodable, Equatable, Sendable {
    /// A curated option ID, a household option ID, or `SpecialtyChoice.asIsOptionID`.
    let optionID: String

    private enum CodingKeys: String, CodingKey {
        case optionID = "optionId"
    }
}

/// An exact amount to send.
nonisolated struct SpecialtyAmountInput: Encodable, Equatable, Sendable {
    var quantity: String
    var unit: String
}

/// One ingredient to send. `nil` fields are omitted.
nonisolated struct SpecialtyIngredientInput: Encodable, Equatable, Sendable {
    var name: String
    /// Exact; required for store alternatives, omitted for "to taste" in a batch.
    var quantity: String?
    var unit: String?
    var category: String?
}

/// The body of `POST .../{specialtyId}/options` and `PUT .../options/{optionId}`. `nil` fields
/// are omitted.
nonisolated struct SpecialtyOptionRequest: Encodable, Equatable, Sendable {
    var type: SpecialtyOptionType
    var name: String
    var notes: String?
    /// Required for a store alternative; not allowed for a batch.
    var per: SpecialtyAmountInput?
    var ingredients: [SpecialtyIngredientInput]
    /// Batches only.
    var steps: [String]?
    /// Required for a batch.
    var batchYield: SpecialtyAmountInput?
    /// Required for a batch, 1–730.
    var shelfLifeDays: Int?
    var basedOnOptionID: String?

    private enum CodingKeys: String, CodingKey {
        case type, name, notes, per, ingredients, steps
        case batchYield = "yield"
        case shelfLifeDays
        case basedOnOptionID = "basedOnOptionId"
    }
}

/// The body of `POST .../{specialtyId}/batches`. `nil` fields are omitted.
nonisolated struct RecordSpecialtyBatchRequest: Encodable, Equatable, Sendable {
    /// A batch option; the household's choice when `nil`.
    var optionID: String?
    /// 1–10; the API's default of 1 when `nil`.
    var batches: Int?
    /// The same ID on every attempt, so a retry after a lost response records one batch.
    var clientPurchaseID: String

    private enum CodingKeys: String, CodingKey {
        case optionID = "optionId"
        case batches
        case clientPurchaseID = "clientPurchaseId"
    }
}

/// Response to `POST .../{specialtyId}/batches`.
nonisolated struct RecordSpecialtyBatchResponse: Decodable, Hashable, Sendable {
    /// `source: house_made`.
    let purchase: PantryPurchase
    /// The batch's pantry item after the purchase.
    let item: PantryItem
    let option: SpecialtyOption
}
