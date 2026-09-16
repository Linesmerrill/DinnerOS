import Foundation

/// A day of an ISO week (`PlanDay` in `api/openapi.yaml`), Monday first.
nonisolated enum PlanDay: String, Codable, CaseIterable, Hashable, Sendable, Identifiable {
    case mon, tue, wed, thu, fri, sat, sun

    var id: String { rawValue }

    /// Days after the week's Monday.
    var offset: Int {
        PlanDay.allCases.firstIndex(of: self) ?? 0
    }

    /// The day's calendar date in `week`, as midnight UTC (the same convention as
    /// `ISOWeek.startDate`).
    func date(in week: ISOWeek) -> Date? {
        week.startDate.map { $0.addingTimeInterval(TimeInterval(offset) * 86_400) }
    }
}

/// A plan's status (`PlanStatus`). A status from a newer server decodes as-is and is
/// treated as locked, because the app doesn't know its rules.
nonisolated struct PlanStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let draft = PlanStatus(rawValue: "draft")
    static let finalized = PlanStatus(rawValue: "finalized")

    var title: String {
        switch self {
        case .draft: String(localized: "Draft")
        case .finalized: String(localized: "Finalized")
        default: rawValue.capitalized
        }
    }
}

/// The recipe snapshot stored on an entry when it was added.
nonisolated struct PlanEntryRecipe: Decodable, Hashable, Sendable {
    let id: String
    let name: String
    let imageURLString: String?
    /// An add-on planned alongside a meal, such as garlic bread with pasta. `false` from a
    /// server that doesn't mark them; `MenuStore.isAddOn` falls back to the menu's own card.
    var isAddon = false

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, name, isAddon
        case imageURLString = "imageUrl"
    }
}

nonisolated extension PlanEntryRecipe {
    /// `isAddon` is additive, so it's read leniently.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            name: try container.decode(String.self, forKey: .name),
            imageURLString: try container.decodeIfPresent(String.self, forKey: .imageURLString),
            isAddon: container.decodeLenient(Bool.self, forKey: .isAddon) ?? false)
    }
}

/// How an entry was added (`PlanEntry.origin`). Unknown values decode as-is.
nonisolated struct PlanEntryOrigin: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let manual = PlanEntryOrigin(rawValue: "manual")
    /// Added by accepting an Autopilot proposal.
    static let autopilot = PlanEntryOrigin(rawValue: "autopilot")
}

/// A planned recipe (`PlanEntry`).
nonisolated struct PlanEntry: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let recipe: PlanEntryRecipe
    /// `nil` when the entry is planned for the week but not a particular day.
    let day: PlanDay?
    /// `YYYY-MM-DD` in the household's time zone; `nil` when unscheduled.
    let date: String?
    let servings: Int
    let note: String
    let addedBy: String
    let addedAt: Date
    /// `nil` from a server older than Autopilot, which only had manual entries.
    var origin: PlanEntryOrigin?
    /// Protein swaps or doubles chosen for this entry; empty when it's the original recipe.
    var customizations: [PlanEntryCustomization] = []

    var isFromAutopilot: Bool { origin == .autopilot }

    private enum CodingKeys: String, CodingKey {
        case id, recipe, day, date, servings, note, addedBy, addedAt, origin, customizations
    }
}

nonisolated extension PlanEntry {
    /// `customizations` is additive, so it's read leniently.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            recipe: try container.decode(PlanEntryRecipe.self, forKey: .recipe),
            day: try container.decodeIfPresent(PlanDay.self, forKey: .day),
            date: try container.decodeIfPresent(String.self, forKey: .date),
            servings: try container.decode(Int.self, forKey: .servings),
            note: try container.decode(String.self, forKey: .note),
            addedBy: try container.decode(String.self, forKey: .addedBy),
            addedAt: try container.decode(Date.self, forKey: .addedAt),
            origin: try container.decodeIfPresent(PlanEntryOrigin.self, forKey: .origin),
            customizations: container.decodeLossyArray(PlanEntryCustomization.self, forKey: .customizations))
    }
}

/// A choice made for one customizable ingredient of an entry, such as swapping the protein.
nonisolated struct PlanEntryCustomization: Codable, Hashable, Sendable {
    let ingredientKey: String
    let choiceID: String
    /// For example "Ground Beef"; empty when the server didn't send one.
    var label: String = ""

    private enum CodingKeys: String, CodingKey {
        case ingredientKey
        case choiceID = "choiceId"
        case label
    }

    init(ingredientKey: String, choiceID: String, label: String = "") {
        self.ingredientKey = ingredientKey
        self.choiceID = choiceID
        self.label = label
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        ingredientKey = try container.decode(String.self, forKey: .ingredientKey)
        choiceID = try container.decode(String.self, forKey: .choiceID)
        label = container.decodeLenient(String.self, forKey: .label) ?? ""
    }
}

/// Body of `PUT .../plans/{week}/entries/{entryId}/customization`. Empty `selections` resets
/// the entry to the original recipe.
nonisolated struct PlanCustomizationRequest: Encodable, Equatable, Sendable {
    struct Selection: Encodable, Hashable, Sendable {
        let ingredientKey: String
        let choiceID: String

        private enum CodingKeys: String, CodingKey {
            case ingredientKey
            case choiceID = "choiceId"
        }
    }

    let selections: [Selection]
}

/// A household's plan for one ISO week (`Plan`).
nonisolated struct Plan: Decodable, Equatable, Sendable {
    let householdID: String
    let week: String
    let startDate: String
    let endDate: String
    let status: PlanStatus
    /// In the order they were added.
    var entries: [PlanEntry]
    /// `nil` for a week nobody has planned.
    let createdAt: Date?
    let updatedAt: Date?

    /// Entries planned for `day`, or unscheduled entries when `day` is `nil`, in the
    /// order they were added.
    func entries(on day: PlanDay?) -> [PlanEntry] {
        entries.filter { $0.day == day }
    }

    private enum CodingKeys: String, CodingKey {
        case householdID = "householdId"
        case week, startDate, endDate, status, entries, createdAt, updatedAt
    }
}

/// One week in `GET .../plans?from&to` (`PlanSummary`).
nonisolated struct PlanSummary: Decodable, Equatable, Sendable, Identifiable {
    let week: String
    let startDate: String
    let status: PlanStatus
    let entryCount: Int
    let updatedAt: Date?

    var id: String { week }
}

nonisolated struct PlanListResponse: Decodable, Equatable, Sendable {
    let items: [PlanSummary]
}

/// Response to `POST .../plans/{week}/entries`.
nonisolated struct AddPlanEntryResponse: Decodable, Equatable, Sendable {
    let entry: PlanEntry
    let plan: Plan
}

// MARK: - Requests

/// Body of `POST .../plans/{week}/entries`. A `nil` day or empty note is omitted.
nonisolated struct NewPlanEntry: Encodable, Equatable, Sendable {
    var recipeID: String
    var day: PlanDay?
    var servings: Int
    var note: String = ""

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case day, servings, note
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(recipeID, forKey: .recipeID)
        try container.encodeIfPresent(day, forKey: .day)
        try container.encode(servings, forKey: .servings)
        if !note.isEmpty {
            try container.encode(note, forKey: .note)
        }
    }
}

/// Body of `PATCH .../plans/{week}/entries/{entryId}`. Fields left `nil` are omitted
/// and don't change; `day: .some(nil)` sends `"day": null`, which unschedules.
nonisolated struct PlanEntryChanges: Encodable, Equatable, Sendable {
    var day: PlanDay??
    var servings: Int?
    var note: String?

    var isEmpty: Bool { day == nil && servings == nil && note == nil }

    private enum CodingKeys: String, CodingKey {
        case day, servings, note
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        if let day {
            if let day {
                try container.encode(day, forKey: .day)
            } else {
                try container.encodeNil(forKey: .day)
            }
        }
        try container.encodeIfPresent(servings, forKey: .servings)
        try container.encodeIfPresent(note, forKey: .note)
    }
}

/// The API's limits on plan requests, mirrored for input validation in the UI.
nonisolated enum PlanLimits {
    static let maxEntriesPerWeek = 50
    static let maxNoteLength = 500
    /// `GET .../plans` covers at most this many weeks.
    static let maxListWeeks = 26
}

// MARK: - Grocery list

/// Response to `GET .../plans/{week}/grocery` (`GroceryList`).
nonisolated struct GroceryList: Decodable, Equatable, Sendable {
    let week: String
    let status: PlanStatus
    /// `false` until the household pantry exists (Phase 7).
    let pantryApplied: Bool
    /// In aisle order; empty categories are left out.
    let categories: [GroceryCategory]
    let skipped: [GrocerySkippedEntry]
    /// The household's specialty ingredient choices were applied. `false` from a server without
    /// specialty ingredients.
    var specialtiesApplied = false
    /// The house-made specialty ingredients the week uses (docs/specialty-ingredients.md).
    var batches: [GroceryBatch] = []

    var isEmpty: Bool { categories.allSatisfy { $0.items.isEmpty } && batches.isEmpty }

    var allItems: [GroceryItem] { categories.flatMap(\.items) }
}

extension GroceryList {
    private enum CodingKeys: String, CodingKey {
        case week, status, pantryApplied, categories, skipped, specialtiesApplied, batches
    }

    /// The specialty fields are additive, so they're read leniently: a response without them, or
    /// with batches this build can't read, still shows the list.
    nonisolated init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            week: try container.decode(String.self, forKey: .week),
            status: try container.decode(PlanStatus.self, forKey: .status),
            pantryApplied: try container.decode(Bool.self, forKey: .pantryApplied),
            categories: try container.decode([GroceryCategory].self, forKey: .categories),
            skipped: try container.decode([GrocerySkippedEntry].self, forKey: .skipped),
            specialtiesApplied: (try? container.decodeIfPresent(Bool.self, forKey: .specialtiesApplied)) ?? false,
            batches: (try? container.decodeIfPresent([GroceryBatch].self, forKey: .batches)) ?? [])
    }
}

nonisolated struct GroceryCategory: Decodable, Equatable, Sendable, Identifiable {
    /// A catalog category code such as `produce` or `meat-seafood`.
    let category: String
    let items: [GroceryItem]

    var id: String { category }

    var title: String { Self.title(for: category) }

    static func title(for code: String) -> String {
        switch code {
        case "produce": String(localized: "Produce")
        case "meat-seafood": String(localized: "Meat & Seafood")
        case "dairy-eggs": String(localized: "Dairy & Eggs")
        case "bakery": String(localized: "Bakery")
        case "deli": String(localized: "Deli")
        case "pantry": String(localized: "Pantry")
        case "spices": String(localized: "Spices")
        case "condiments": String(localized: "Condiments")
        case "frozen": String(localized: "Frozen")
        case "beverages": String(localized: "Beverages")
        case "other": String(localized: "Other")
        default: code.replacingOccurrences(of: "-", with: " ").capitalized
        }
    }
}

/// Whether an item needs buying (`GroceryItemStatus`). Unknown values decode as-is.
nonisolated struct GroceryItemStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let toBuy = GroceryItemStatus(rawValue: "toBuy")
    /// Every contributing recipe marks the item as a pantry staple.
    static let pantryHint = GroceryItemStatus(rawValue: "pantryHint")
    /// The household pantry has it (Phase 7).
    static let inPantry = GroceryItemStatus(rawValue: "inPantry")
}

/// One aggregated ingredient.
nonisolated struct GroceryItem: Decodable, Equatable, Sendable, Identifiable {
    let ingredientKey: String
    let name: String
    let amounts: [GroceryAmount]
    /// The amounts joined for display ("1 ½ + 8 oz"); empty when there are none.
    let quantityText: String
    /// At least one recipe gave no amount ("salt to taste").
    let unquantified: Bool
    let status: GroceryItemStatus
    let recipes: [GroceryRecipe]
    /// A specialty ingredient by its own name: not chosen yet, kept as is, or covered by a
    /// house-made batch in the pantry.
    var specialty = false
    var specialtyDetail: GrocerySpecialty?
    /// The specialty ingredients this item stands in for ("for Tex-Mex Paste in Smoky Pork
    /// Tacos"). Empty for ordinary items.
    var via: [GroceryVia] = []
    /// What put the item on the list besides a recipe, such as an accepted pairing ("Club
    /// Crackers for Chicken Noodle Soup"). Empty for ordinary items.
    var extras: [GroceryExtra] = []

    var id: String { ingredientKey }

    /// The pairing extras on this item, which can be taken off the week's list.
    var pairingExtras: [GroceryExtra] {
        extras.filter { $0.origin == .pairing }
    }

    /// A specialty ingredient the household hasn't chosen an option for.
    var needsSpecialtyChoice: Bool {
        specialty && specialtyDetail != nil && specialtyDetail?.choiceType == nil
    }

    /// Covered by a house-made batch in the pantry ("In pantry (house-made)").
    var isHouseMade: Bool {
        specialtyDetail?.houseMade == true
    }
}

extension GroceryItem {
    private enum CodingKeys: String, CodingKey {
        case ingredientKey, name, amounts, quantityText, unquantified, status, recipes, specialty,
            specialtyDetail, via, extras
    }

    /// The specialty fields are additive, so they're read leniently, like `GroceryList`'s.
    nonisolated init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            ingredientKey: try container.decode(String.self, forKey: .ingredientKey),
            name: try container.decode(String.self, forKey: .name),
            amounts: try container.decode([GroceryAmount].self, forKey: .amounts),
            quantityText: try container.decode(String.self, forKey: .quantityText),
            unquantified: try container.decode(Bool.self, forKey: .unquantified),
            status: try container.decode(GroceryItemStatus.self, forKey: .status),
            recipes: try container.decode([GroceryRecipe].self, forKey: .recipes),
            specialty: (try? container.decodeIfPresent(Bool.self, forKey: .specialty)) ?? false,
            specialtyDetail: (try? container.decodeIfPresent(GrocerySpecialty.self, forKey: .specialtyDetail)) ?? nil,
            via: (try? container.decodeIfPresent([GroceryVia].self, forKey: .via)) ?? [],
            extras: container.decodeLossyArray(GroceryExtra.self, forKey: .extras))
    }
}

/// What put a grocery item on the list besides a recipe (`GroceryItem.extras`), separate
/// from `via`. Rendered the same way: one quiet line under the item.
nonisolated struct GroceryExtra: Decodable, Equatable, Sendable, Identifiable {
    /// Why it's on the list. Unknown origins decode as-is and are shown but not removable.
    nonisolated struct Origin: RawRepresentable, Decodable, Hashable, Sendable {
        let rawValue: String

        init(rawValue: String) {
            self.rawValue = rawValue
        }

        /// An accepted Autopilot pairing (docs/autopilot.md#add-on-pairings).
        static let pairing = Origin(rawValue: "pairing")
    }

    /// Identifies the extra where it is stored, so it can be removed.
    let id: String
    let origin: Origin
    /// For example "Club Crackers for Chicken Noodle Soup".
    let text: String
}

nonisolated struct GroceryAmount: Decodable, Equatable, Sendable {
    let quantity: String
    let quantityValue: Double
    let unit: String
    let text: String
}

nonisolated struct GroceryRecipe: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let name: String
}

/// Why an entry couldn't contribute to the grocery list (`SkipReason`).
nonisolated struct GrocerySkipReason: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let recipeUnavailable = GrocerySkipReason(rawValue: "recipeUnavailable")
    static let servingsUnavailable = GrocerySkipReason(rawValue: "servingsUnavailable")

    var explanation: String {
        switch self {
        case .recipeUnavailable: String(localized: "The recipe is no longer in this household.")
        case .servingsUnavailable: String(localized: "The recipe no longer offers that serving size.")
        default: String(localized: "It couldn't be included.")
        }
    }
}

nonisolated struct GrocerySkippedEntry: Decodable, Equatable, Sendable, Identifiable {
    let entryID: String
    let recipeID: String
    let recipeName: String
    let reason: GrocerySkipReason

    var id: String { entryID }

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case recipeID = "recipeId"
        case recipeName, reason
    }
}
