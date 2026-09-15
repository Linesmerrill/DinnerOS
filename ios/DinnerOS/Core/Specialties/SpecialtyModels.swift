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

/// The household's choice for one specialty ingredient.
nonisolated struct SpecialtyChoice: Decodable, Hashable, Sendable {
    /// The option ID that means "keep as is".
    static let asIsOptionID = "as_is"

    let optionID: String
    let type: SpecialtyChoiceType
    /// `nil` for `as_is`.
    let optionName: String?
    let chosenBy: String
    let chosenAt: Date

    private enum CodingKeys: String, CodingKey {
        case optionID = "optionId"
        case type, optionName, chosenBy, chosenAt
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
    /// `nil` until the household chooses.
    let choice: SpecialtyChoice?
    /// Curated options first, then the household's, oldest first.
    let options: [SpecialtyOption]
    let batch: SpecialtyBatchStock?

    /// The chosen option; `nil` without a choice or for `as_is`.
    var chosenOption: SpecialtyOption? {
        guard let choice else { return nil }
        return options.first { $0.id == choice.optionID }
    }

    var isKeptAsIs: Bool { choice?.type == .asIs }

    var hasBatchChoice: Bool { choice?.type == .houseMadeBatch }

    func isChosen(_ option: SpecialtyOption) -> Bool {
        choice?.optionID == option.id
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
        case retired, choice, options, batch
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
