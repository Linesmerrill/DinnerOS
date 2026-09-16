import Foundation

// MARK: - Values

/// What a pairing adds (`AutopilotPairingKind`).
nonisolated enum PairingKind: String, Codable, Hashable, Sendable {
    case recipe
    case groceryItem = "grocery_item"
}

/// Where a pairing came from (`AutopilotPairingSource`). Unknown values decode as-is, so a
/// source a newer server adds still shows its `reason`.
nonisolated struct PairingSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// A household rule.
    static let rule = PairingSource(rawValue: "rule")
    /// The household's own history.
    static let learned = PairingSource(rawValue: "learned")
}

/// How often a rule applies (`AutopilotPairingFrequency`).
nonisolated struct PairingFrequency: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Included with a proposed meal unless the member takes it out.
    static let always = PairingFrequency(rawValue: "always")
    /// Offered with the meal.
    static let suggest = PairingFrequency(rawValue: "suggest")

    static let known: [PairingFrequency] = [.always, .suggest]

    var title: String {
        switch self {
        case .always: String(localized: "Always")
        case .suggest: String(localized: "Suggest")
        default: rawValue.capitalized
        }
    }
}

/// The kind of dinner pairing rules match on (`AutopilotMealCategory`). The list is closed,
/// but a category from a newer server decodes as-is rather than failing the response.
nonisolated struct MealCategory: RawRepresentable, Codable, Hashable, Sendable, Identifiable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    var id: String { rawValue }

    static let pasta = MealCategory(rawValue: "pasta")
    static let soup = MealCategory(rawValue: "soup")
    static let salad = MealCategory(rawValue: "salad")
    static let tacos = MealCategory(rawValue: "tacos")
    static let curry = MealCategory(rawValue: "curry")
    static let bowl = MealCategory(rawValue: "bowl")
    static let stirFry = MealCategory(rawValue: "stir-fry")
    static let sandwich = MealCategory(rawValue: "sandwich")
    static let pizza = MealCategory(rawValue: "pizza")

    /// The documented order, used until the vocabulary loads.
    static let known: [MealCategory] = [
        .pasta, .soup, .salad, .tacos, .curry, .bowl, .stirFry, .sandwich, .pizza,
    ]

    /// The vocabulary's label, falling back to a readable form of the value.
    var fallbackTitle: String {
        switch self {
        case .pasta: String(localized: "Pasta")
        case .soup: String(localized: "Soup, stew & chili")
        case .salad: String(localized: "Salad")
        case .tacos: String(localized: "Tacos & Mexican")
        case .curry: String(localized: "Curry")
        case .bowl: String(localized: "Rice & grain bowls")
        case .stirFry: String(localized: "Stir-fry & noodles")
        case .sandwich: String(localized: "Burgers & sandwiches")
        case .pizza: String(localized: "Pizza & flatbread")
        default: rawValue.replacingOccurrences(of: "-", with: " ").capitalized
        }
    }
}

// MARK: - Grocery items

/// A grocery line a pairing adds (`AutopilotGroceryItem`).
///
/// `quantity` is a **number** here, unlike the exact-fraction strings the grocery list uses,
/// so it is decoded leniently from a number or a numeric string and always sent as a number.
nonisolated struct PairingGroceryItem: Codable, Hashable, Sendable {
    let name: String
    /// `nil` for no amount. A unit needs a quantity.
    var quantity: Double?
    /// An ingredient unit code (`count`, `package`, `oz`…).
    var unit: String?

    init(name: String, quantity: Double? = nil, unit: String? = nil) {
        self.name = name
        self.quantity = quantity
        self.unit = unit
    }

    /// "1 package", "2", or `nil` when there's no amount.
    var amountText: String? {
        guard let quantity else { return nil }
        let number = quantity.formatted(.number.precision(.fractionLength(0...2)))
        guard let unit, !unit.isEmpty, unit != "count" else { return number }
        return "\(number) \(unit)"
    }

    private enum CodingKeys: String, CodingKey {
        case name, quantity, unit
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        quantity =
            container.decodeLenient(Double.self, forKey: .quantity)
            ?? container.decodeLenient(String.self, forKey: .quantity).flatMap { Double($0) }
        unit = container.decodeLenient(String.self, forKey: .unit).flatMap { $0.isEmpty ? nil : $0 }
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(name, forKey: .name)
        try container.encodeNullable(quantity, forKey: .quantity)
        try container.encodeNullable(unit, forKey: .unit)
    }
}

/// An add-on recipe offered as a pairing (`AutopilotPairingRecipe`): a recipe summary
/// without ratings.
nonisolated struct PairingRecipe: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let name: String
    var headline: String?
    var imageURLString: String?
    /// `nil` when unknown.
    var cookMinutes: Int?
    var timesOrdered: Int = 0
    var lastOrderedWeek: String?
    var isAddon: Bool = true
    var tags: [String] = []

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    /// The pairing's recipe as a list summary, for opening its screen.
    var summary: RecipeSummary {
        RecipeSummary(
            id: id, name: name, headline: headline, imageURLString: imageURLString, totalMinutes: cookMinutes,
            cookMinutes: cookMinutes, timesOrdered: timesOrdered, lastOrderedWeek: lastOrderedWeek,
            isAddon: isAddon, tags: tags)
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, headline
        case imageURLString = "imageUrl"
        case cookMinutes, timesOrdered, lastOrderedWeek, isAddon, tags
    }

    init(
        id: String, name: String, headline: String? = nil, imageURLString: String? = nil, cookMinutes: Int? = nil,
        timesOrdered: Int = 0, lastOrderedWeek: String? = nil, isAddon: Bool = true, tags: [String] = []
    ) {
        self.id = id
        self.name = name
        self.headline = headline
        self.imageURLString = imageURLString
        self.cookMinutes = cookMinutes
        self.timesOrdered = timesOrdered
        self.lastOrderedWeek = lastOrderedWeek
        self.isAddon = isAddon
        self.tags = tags
    }

    /// Everything but `id` and `name` is read leniently: a pairing is worth showing even
    /// when a display field is missing.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            name: try container.decode(String.self, forKey: .name),
            headline: container.decodeLenient(String.self, forKey: .headline),
            imageURLString: container.decodeLenient(String.self, forKey: .imageURLString),
            cookMinutes: container.decodeLenientInt(forKey: .cookMinutes),
            timesOrdered: container.decodeLenientInt(forKey: .timesOrdered) ?? 0,
            lastOrderedWeek: container.decodeLenient(String.self, forKey: .lastOrderedWeek),
            isAddon: container.decodeLenientBool(forKey: .isAddon) ?? true,
            tags: container.decodeLossyArray(String.self, forKey: .tags))
    }
}

/// The history behind a learned pairing (`AutopilotLearnedPairing`).
nonisolated struct LearnedPairing: Decodable, Hashable, Sendable {
    let weeksTogether: Int
    let mealCategoryWeeks: Int
    let otherWeeks: Int
    let otherWeeksRate: Double

    private enum CodingKeys: String, CodingKey {
        case weeksTogether, mealCategoryWeeks, otherWeeks, otherWeeksRate
    }

    init(weeksTogether: Int, mealCategoryWeeks: Int, otherWeeks: Int, otherWeeksRate: Double) {
        self.weeksTogether = weeksTogether
        self.mealCategoryWeeks = mealCategoryWeeks
        self.otherWeeks = otherWeeks
        self.otherWeeksRate = otherWeeksRate
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            weeksTogether: container.decodeLenientInt(forKey: .weeksTogether) ?? 0,
            mealCategoryWeeks: container.decodeLenientInt(forKey: .mealCategoryWeeks) ?? 0,
            otherWeeks: container.decodeLenientInt(forKey: .otherWeeks) ?? 0,
            otherWeeksRate: container.decodeLenient(Double.self, forKey: .otherWeeksRate) ?? 0)
    }
}

// MARK: - Pairings

/// An add-on or grocery item offered with a meal (`AutopilotPairing`).
///
/// `key` identifies the target and is sent back to accept or dismiss. It is the server's own
/// key (`recipe:<id>`, `grocery:<lowercased name>`) and is never synthesized here: a made-up
/// key would be rejected as `404 not_found`.
nonisolated struct Pairing: Decodable, Hashable, Sendable, Identifiable {
    enum Target: Hashable, Sendable {
        case recipe(PairingRecipe)
        case groceryItem(PairingGroceryItem)

        var kind: PairingKind {
            switch self {
            case .recipe: .recipe
            case .groceryItem: .groceryItem
            }
        }
    }

    let key: String
    let target: Target
    /// The add-on's serving size for this meal; `nil` for a grocery item.
    var servings: Int?
    var source: PairingSource
    var frequency: PairingFrequency
    /// The meal category that matched, or `nil`.
    var mealCategory: MealCategory?
    /// The share of the category's weeks that had the add-on; `nil` without history.
    var confidence: Double?
    /// The history behind a learned pairing; `nil` when none backs it.
    var learned: LearnedPairing?
    /// Human-readable text to show. Not stable; never parsed.
    var reason: String?
    /// The household rule this came from, or `nil` for a learned pairing.
    var ruleID: String?
    /// Already planned for this meal's day, or on the week's list.
    var inPlan: Bool
    /// A learned pairing with a meal category, which `pairings/rules` can keep as a rule.
    var canMakeRule: Bool

    var id: String { key }

    var name: String {
        switch target {
        case .recipe(let recipe): recipe.name
        case .groceryItem(let item): item.name
        }
    }

    /// The add-on recipe, when the pairing adds one.
    var recipe: PairingRecipe? {
        guard case .recipe(let recipe) = target else { return nil }
        return recipe
    }

    /// The grocery line, when the pairing adds one.
    var groceryItem: PairingGroceryItem? {
        guard case .groceryItem(let item) = target else { return nil }
        return item
    }

    /// Whether "Always add this" is worth offering: the server says a rule can be made, and
    /// the pairing isn't already a rule.
    var offersRule: Bool {
        canMakeRule && ruleID == nil
    }

    private enum CodingKeys: String, CodingKey {
        case key, target, servings, source, frequency, mealCategory, confidence, learned, reason
        case ruleID = "ruleId"
        case inPlan, canMakeRule
    }

    private enum TargetKeys: String, CodingKey {
        case kind, recipe, groceryItem
    }

    init(
        key: String, target: Target, servings: Int? = nil, source: PairingSource = .rule,
        frequency: PairingFrequency = .suggest, mealCategory: MealCategory? = nil, confidence: Double? = nil,
        learned: LearnedPairing? = nil, reason: String? = nil, ruleID: String? = nil, inPlan: Bool = false,
        canMakeRule: Bool = false
    ) {
        self.key = key
        self.target = target
        self.servings = servings
        self.source = source
        self.frequency = frequency
        self.mealCategory = mealCategory
        self.confidence = confidence
        self.learned = learned
        self.reason = reason
        self.ruleID = ruleID
        self.inPlan = inPlan
        self.canMakeRule = canMakeRule
    }

    /// A target of an unknown kind fails, so a lossy list leaves the pairing out: there is
    /// nothing useful to show for an add the app can't describe. Everything else is lenient,
    /// including an unknown `source` and a missing `learned`.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let targetContainer = try container.nestedContainer(keyedBy: TargetKeys.self, forKey: .target)
        let target: Target
        switch PairingKind(rawValue: try targetContainer.decode(String.self, forKey: .kind)) {
        case .recipe:
            target = .recipe(try targetContainer.decode(PairingRecipe.self, forKey: .recipe))
        case .groceryItem:
            target = .groceryItem(try targetContainer.decode(PairingGroceryItem.self, forKey: .groceryItem))
        case nil:
            throw DecodingError.dataCorruptedError(
                forKey: .kind, in: targetContainer, debugDescription: "Unknown pairing target kind")
        }
        self.init(
            key: try container.decode(String.self, forKey: .key),
            target: target,
            servings: container.decodeLenientInt(forKey: .servings),
            source: container.decodeLenient(PairingSource.self, forKey: .source) ?? .rule,
            frequency: container.decodeLenient(PairingFrequency.self, forKey: .frequency) ?? .suggest,
            mealCategory: container.decodeLenient(MealCategory.self, forKey: .mealCategory),
            confidence: container.decodeLenient(Double.self, forKey: .confidence),
            learned: container.decodeLenient(LearnedPairing.self, forKey: .learned),
            reason: container.decodeLenient(String.self, forKey: .reason).flatMap { $0.isEmpty ? nil : $0 },
            ruleID: container.decodeLenient(String.self, forKey: .ruleID),
            inPlan: container.decodeLenientBool(forKey: .inPlan) ?? false,
            canMakeRule: container.decodeLenientBool(forKey: .canMakeRule) ?? false)
    }
}

/// A pairing carried by a proposal slot (`AutopilotProposalPairing`).
nonisolated struct ProposalPairing: Decodable, Hashable, Sendable, Identifiable {
    /// `<slotId>/<key>`; sent in `pairingIds` when accepting.
    let id: String
    /// Accepting adds it unless the member takes it out (the household's `always` rules).
    let included: Bool
    let pairing: Pairing

    var name: String { pairing.name }

    private enum CodingKeys: String, CodingKey {
        case id, included
    }

    init(id: String, included: Bool, pairing: Pairing) {
        self.id = id
        self.included = included
        self.pairing = pairing
    }

    /// The slot's pairings are `AutopilotPairing` with two extra fields, so the pairing is
    /// decoded from the same container.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        pairing = try Pairing(from: decoder)
        id = try container.decode(String.self, forKey: .id)
        included = container.decodeLenientBool(forKey: .included) ?? false
    }
}

/// A paired grocery item on the week's list (`AutopilotPairingGroceryItem`).
nonisolated struct PairingGroceryLine: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let key: String
    let groceryItem: PairingGroceryItem
    /// The meal's plan entry. The item leaves the list when that entry is unplanned.
    let entryID: String
    let recipe: PairingMealRecipe
    var source: PairingSource
    var ruleID: String?
    /// For example "Club Crackers for Chicken Noodle Soup".
    var text: String
    var addedBy: String?
    var addedAt: Date?

    var name: String { groceryItem.name }

    private enum CodingKeys: String, CodingKey {
        case id, key, groceryItem
        case entryID = "entryId"
        case recipe, source
        case ruleID = "ruleId"
        case text, addedBy, addedAt
    }

    init(
        id: String, key: String, groceryItem: PairingGroceryItem, entryID: String, recipe: PairingMealRecipe,
        source: PairingSource = .rule, ruleID: String? = nil, text: String = "", addedBy: String? = nil,
        addedAt: Date? = nil
    ) {
        self.id = id
        self.key = key
        self.groceryItem = groceryItem
        self.entryID = entryID
        self.recipe = recipe
        self.source = source
        self.ruleID = ruleID
        self.text = text
        self.addedBy = addedBy
        self.addedAt = addedAt
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            key: try container.decode(String.self, forKey: .key),
            groceryItem: try container.decode(PairingGroceryItem.self, forKey: .groceryItem),
            entryID: try container.decode(String.self, forKey: .entryID),
            recipe: try container.decode(PairingMealRecipe.self, forKey: .recipe),
            source: container.decodeLenient(PairingSource.self, forKey: .source) ?? .rule,
            ruleID: container.decodeLenient(String.self, forKey: .ruleID),
            text: container.decodeLenient(String.self, forKey: .text) ?? "",
            addedBy: container.decodeLenient(String.self, forKey: .addedBy),
            addedAt: container.decodeLenient(Date.self, forKey: .addedAt))
    }
}

/// The recipe a pairing belongs to, as the pairings endpoints send it.
nonisolated struct PairingMealRecipe: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let name: String
    var imageURLString: String?

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case id, name
        case imageURLString = "imageUrl"
    }

    init(id: String, name: String, imageURLString: String? = nil) {
        self.id = id
        self.name = name
        self.imageURLString = imageURLString
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            name: try container.decode(String.self, forKey: .name),
            imageURLString: container.decodeLenient(String.self, forKey: .imageURLString))
    }
}

/// One planned main meal and the pairings offered with it (`AutopilotMealPairings`).
nonisolated struct MealPairings: Decodable, Hashable, Sendable, Identifiable {
    let entryID: String
    /// `nil` for an unscheduled meal.
    var day: PlanDay?
    var date: String?
    let recipe: PairingMealRecipe
    var servings: Int = 0
    var mealCategories: [MealCategory] = []
    var pairings: [Pairing] = []

    var id: String { entryID }

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case day, date, recipe, servings, mealCategories, pairings
    }

    init(
        entryID: String, day: PlanDay? = nil, date: String? = nil, recipe: PairingMealRecipe, servings: Int = 0,
        mealCategories: [MealCategory] = [], pairings: [Pairing] = []
    ) {
        self.entryID = entryID
        self.day = day
        self.date = date
        self.recipe = recipe
        self.servings = servings
        self.mealCategories = mealCategories
        self.pairings = pairings
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            entryID: try container.decode(String.self, forKey: .entryID),
            day: container.decodeLenient(PlanDay.self, forKey: .day),
            date: container.decodeLenient(String.self, forKey: .date),
            recipe: try container.decode(PairingMealRecipe.self, forKey: .recipe),
            servings: container.decodeLenientInt(forKey: .servings) ?? 0,
            mealCategories: container.decodeLossyArray(MealCategory.self, forKey: .mealCategories),
            pairings: container.decodeLossyArray(Pairing.self, forKey: .pairings))
    }
}

/// Response to `GET .../autopilot/weeks/{week}/pairings` (`AutopilotWeekPairings`).
nonisolated struct WeekPairings: Decodable, Equatable, Sendable {
    var week: String = ""
    var startDate: String = ""
    var endDate: String = ""
    /// One per planned main meal, in plan order. Add-on entries aren't meals.
    var meals: [MealPairings] = []
    /// Accepted grocery items on this week's list.
    var groceryItems: [PairingGroceryLine] = []

    static let empty = WeekPairings()

    /// Every meal's open suggestions, in plan order, paired with the meal they belong to.
    var openSuggestions: [(meal: MealPairings, pairing: Pairing)] {
        meals.flatMap { meal in meal.pairings.map { (meal, $0) } }
    }

    var isEmpty: Bool { openSuggestions.isEmpty }

    func meal(entryID: String) -> MealPairings? {
        meals.first { $0.entryID == entryID }
    }

    private enum CodingKeys: String, CodingKey {
        case week, startDate, endDate, meals, groceryItems
    }

    init(
        week: String = "", startDate: String = "", endDate: String = "", meals: [MealPairings] = [],
        groceryItems: [PairingGroceryLine] = []
    ) {
        self.week = week
        self.startDate = startDate
        self.endDate = endDate
        self.meals = meals
        self.groceryItems = groceryItems
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            week: container.decodeLenient(String.self, forKey: .week) ?? "",
            startDate: container.decodeLenient(String.self, forKey: .startDate) ?? "",
            endDate: container.decodeLenient(String.self, forKey: .endDate) ?? "",
            meals: container.decodeLossyArray(MealPairings.self, forKey: .meals),
            groceryItems: container.decodeLossyArray(PairingGroceryLine.self, forKey: .groceryItems))
    }
}

/// Response to `GET .../recipes/{recipeId}/pairings?week=` (`AutopilotRecipePairings`).
///
/// Unlike the week's pairings, items already in the week are **included** with `inPlan: true`,
/// so the carousel is stable.
nonisolated struct RecipePairings: Decodable, Equatable, Sendable {
    var recipeID: String = ""
    /// `nil` when no week was asked for; nothing is then in the plan.
    var week: String?
    /// The recipe's plan entry in that week, when it is planned.
    var entryID: String?
    var mealCategories: [MealCategory] = []
    /// At most 6. Add-on recipes have none.
    var items: [Pairing] = []

    static let empty = RecipePairings()

    var isEmpty: Bool { items.isEmpty }

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case week
        case entryID = "entryId"
        case mealCategories, items
    }

    init(
        recipeID: String = "", week: String? = nil, entryID: String? = nil, mealCategories: [MealCategory] = [],
        items: [Pairing] = []
    ) {
        self.recipeID = recipeID
        self.week = week
        self.entryID = entryID
        self.mealCategories = mealCategories
        self.items = items
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            recipeID: container.decodeLenient(String.self, forKey: .recipeID) ?? "",
            week: container.decodeLenient(String.self, forKey: .week),
            entryID: container.decodeLenient(String.self, forKey: .entryID),
            mealCategories: container.decodeLossyArray(MealCategory.self, forKey: .mealCategories),
            items: container.decodeLossyArray(Pairing.self, forKey: .items))
    }
}

// MARK: - Actions

/// Body of `POST .../pairings/accept` and `.../dismiss` (`AutopilotPairingActionRequest`).
nonisolated struct PairingActionRequest: Encodable, Hashable, Sendable {
    /// The main meal's plan entry.
    let entryID: String
    /// The pairing's `key`, exactly as the server sent it.
    let key: String

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case key
    }
}

/// Whether accepting changed anything.
nonisolated struct PairingAcceptStatus: RawRepresentable, Decodable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let added = PairingAcceptStatus(rawValue: "added")
    /// The week already had it, and nothing changed.
    static let alreadyAdded = PairingAcceptStatus(rawValue: "alreadyAdded")
}

/// Response to `POST .../pairings/accept` (`AutopilotPairingAcceptResult`).
nonisolated struct PairingAcceptResult: Decodable, Equatable, Sendable {
    var status: PairingAcceptStatus = .added
    var pairing: Pairing?
    /// The add-on's plan entry, with origin `autopilot`; `nil` for a grocery item.
    var entry: PlanEntry?
    var groceryItem: PairingGroceryLine?
    let plan: Plan

    private enum CodingKeys: String, CodingKey {
        case status, pairing, entry, groceryItem, plan
    }

    init(
        status: PairingAcceptStatus = .added, pairing: Pairing? = nil, entry: PlanEntry? = nil,
        groceryItem: PairingGroceryLine? = nil, plan: Plan
    ) {
        self.status = status
        self.pairing = pairing
        self.entry = entry
        self.groceryItem = groceryItem
        self.plan = plan
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            status: container.decodeLenient(PairingAcceptStatus.self, forKey: .status) ?? .added,
            pairing: container.decodeLenient(Pairing.self, forKey: .pairing),
            entry: container.decodeLenient(PlanEntry.self, forKey: .entry),
            groceryItem: container.decodeLenient(PairingGroceryLine.self, forKey: .groceryItem),
            plan: try container.decode(Plan.self, forKey: .plan))
    }
}

/// Body of `POST .../pairings/rules` (`AutopilotMakePairingRuleRequest`): exactly one of
/// `entryId` (a planned meal) or `slotId` (a proposal slot).
nonisolated struct MakePairingRuleRequest: Encodable, Hashable, Sendable {
    var entryID: String?
    var slotID: String?
    let key: String
    /// Default `suggest` on the server.
    var frequency: PairingFrequency?

    static func entry(_ entryID: String, key: String, frequency: PairingFrequency? = nil) -> Self {
        MakePairingRuleRequest(entryID: entryID, key: key, frequency: frequency)
    }

    static func slot(_ slotID: String, key: String, frequency: PairingFrequency? = nil) -> Self {
        MakePairingRuleRequest(slotID: slotID, key: key, frequency: frequency)
    }

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case slotID = "slotId"
        case key, frequency
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(entryID, forKey: .entryID)
        try container.encodeIfPresent(slotID, forKey: .slotID)
        try container.encode(key, forKey: .key)
        try container.encodeIfPresent(frequency, forKey: .frequency)
    }
}

/// Response to `POST .../pairings/rules` (`AutopilotMakePairingRuleResult`).
nonisolated struct MakePairingRuleResult: Decodable, Equatable, Sendable {
    /// `created`, `merged` (the category joined the rule that item already had), or
    /// `unchanged` (a rule already covers it).
    var status: String = "created"
    var rule: PairingRule?
    let profile: AutopilotProfile

    private enum CodingKeys: String, CodingKey {
        case status, rule, profile
    }

    init(status: String = "created", rule: PairingRule? = nil, profile: AutopilotProfile) {
        self.status = status
        self.rule = rule
        self.profile = profile
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            status: container.decodeLenient(String.self, forKey: .status) ?? "created",
            rule: container.decodeLenient(PairingRule.self, forKey: .rule),
            profile: try container.decode(AutopilotProfile.self, forKey: .profile))
    }
}

/// A pairing an accepted proposal added (`AutopilotAddedPairing`).
nonisolated struct AddedPairing: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let slotID: String
    var pairing: Pairing?
    var entry: PlanEntry?
    var groceryItem: PairingGroceryLine?

    /// What to call it in the summary.
    var name: String {
        pairing?.name ?? entry?.recipe.name ?? groceryItem?.name ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case slotID = "slotId"
        case pairing, entry, groceryItem
    }

    init(
        id: String, slotID: String, pairing: Pairing? = nil, entry: PlanEntry? = nil,
        groceryItem: PairingGroceryLine? = nil
    ) {
        self.id = id
        self.slotID = slotID
        self.pairing = pairing
        self.entry = entry
        self.groceryItem = groceryItem
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            slotID: container.decodeLenient(String.self, forKey: .slotID) ?? "",
            pairing: container.decodeLenient(Pairing.self, forKey: .pairing),
            entry: container.decodeLenient(PlanEntry.self, forKey: .entry),
            groceryItem: container.decodeLenient(PairingGroceryLine.self, forKey: .groceryItem))
    }
}

/// A chosen pairing that wasn't added (`AutopilotSkippedPairing`).
nonisolated struct SkippedPairing: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let slotID: String
    let key: String
    /// `alreadyPlanned` or `unavailable`.
    var reason: String = ""

    /// What the pairing was, from its key, when nothing else names it.
    var explanation: String {
        switch reason {
        case "alreadyPlanned": String(localized: "It's already planned that day.")
        case "unavailable": String(localized: "It can no longer be planned.")
        default: String(localized: "It couldn't be added.")
        }
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case slotID = "slotId"
        case key, reason
    }

    init(id: String, slotID: String, key: String, reason: String = "") {
        self.id = id
        self.slotID = slotID
        self.key = key
        self.reason = reason
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            slotID: container.decodeLenient(String.self, forKey: .slotID) ?? "",
            key: container.decodeLenient(String.self, forKey: .key) ?? "",
            reason: container.decodeLenient(String.self, forKey: .reason) ?? "")
    }
}

// MARK: - Rules

/// What a pairing rule matches on (`AutopilotPairingRule.when`). A meal matches when it
/// matches **every** group the rule sets; within a group any value matches.
nonisolated struct PairingRuleConditions: Codable, Hashable, Sendable {
    var mealCategories: [MealCategory] = []
    var cuisines: [String] = []
    var tags: [String] = []
    var proteins: [String] = []

    /// A rule needs at least one condition.
    var isEmpty: Bool {
        mealCategories.isEmpty && cuisines.isEmpty && tags.isEmpty && proteins.isEmpty
    }

    private enum CodingKeys: String, CodingKey {
        case mealCategories, cuisines, tags, proteins
    }

    init(
        mealCategories: [MealCategory] = [], cuisines: [String] = [], tags: [String] = [], proteins: [String] = []
    ) {
        self.mealCategories = mealCategories
        self.cuisines = cuisines
        self.tags = tags
        self.proteins = proteins
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            mealCategories: container.decodeLossyArray(MealCategory.self, forKey: .mealCategories),
            cuisines: container.decodeLossyArray(String.self, forKey: .cuisines),
            tags: container.decodeLossyArray(String.self, forKey: .tags),
            proteins: container.decodeLossyArray(String.self, forKey: .proteins))
    }
}

/// What a rule adds (`AutopilotPairingRuleTarget`): exactly one of an add-on `recipeId` or a
/// `groceryItem`. `kind` and `recipeName` are read-only.
nonisolated struct PairingRuleTarget: Codable, Hashable, Sendable {
    var kind: PairingKind?
    /// An add-on recipe of this household that has serving sizes.
    var recipeID: String?
    /// Read-only: the recipe's name when the rule was saved.
    var recipeName: String?
    var groceryItem: PairingGroceryItem?

    static func recipe(id: String, name: String? = nil) -> Self {
        PairingRuleTarget(kind: .recipe, recipeID: id, recipeName: name)
    }

    static func groceryItem(_ item: PairingGroceryItem) -> Self {
        PairingRuleTarget(kind: .groceryItem, groceryItem: item)
    }

    /// What to show for the rule's add.
    var name: String {
        recipeName ?? groceryItem?.name ?? recipeID ?? ""
    }

    /// Exactly one of the two must be set.
    var isValid: Bool {
        (recipeID != nil) != (groceryItem != nil)
    }

    private enum CodingKeys: String, CodingKey {
        case kind
        case recipeID = "recipeId"
        case recipeName, groceryItem
    }

    init(
        kind: PairingKind? = nil, recipeID: String? = nil, recipeName: String? = nil,
        groceryItem: PairingGroceryItem? = nil
    ) {
        self.kind = kind
        self.recipeID = recipeID
        self.recipeName = recipeName
        self.groceryItem = groceryItem
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            kind: container.decodeLenient(PairingKind.self, forKey: .kind),
            recipeID: container.decodeLenient(String.self, forKey: .recipeID),
            recipeName: container.decodeLenient(String.self, forKey: .recipeName),
            groceryItem: container.decodeLenient(PairingGroceryItem.self, forKey: .groceryItem))
    }

    /// Only the writable half is sent: `kind` and `recipeName` are the server's to set.
    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeNullable(recipeID, forKey: .recipeID)
        try container.encodeNullable(groceryItem, forKey: .groceryItem)
    }
}

/// A household pairing rule, the profile's `pairings` section (`AutopilotPairingRule`).
nonisolated struct PairingRule: Codable, Hashable, Sendable, Identifiable {
    /// `nil` for a new rule; a saved rule's id is sent back to keep it.
    var id: String?
    var label: String = ""
    var when = PairingRuleConditions()
    var add = PairingRuleTarget()
    var frequency: PairingFrequency = .suggest

    /// New rules have no id yet, so the editor keys them by their own identity.
    var identity: String { id ?? clientID }

    /// A stable key for a rule that hasn't been saved. Local only: it takes no part in
    /// equality, so a rule read back from the server still compares equal to the one on
    /// screen and "Save" stays disabled until something really changed.
    private(set) var clientID: String = UUID().uuidString

    static func == (lhs: PairingRule, rhs: PairingRule) -> Bool {
        lhs.id == rhs.id && lhs.label == rhs.label && lhs.when == rhs.when && lhs.add == rhs.add
            && lhs.frequency == rhs.frequency
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
        hasher.combine(label)
        hasher.combine(when)
        hasher.combine(add)
        hasher.combine(frequency)
    }

    private enum CodingKeys: String, CodingKey {
        case id, label, when, add, frequency
    }

    init(
        id: String? = nil, label: String = "", when: PairingRuleConditions = PairingRuleConditions(),
        add: PairingRuleTarget = PairingRuleTarget(), frequency: PairingFrequency = .suggest
    ) {
        self.id = id
        self.label = label
        self.when = when
        self.add = add
        self.frequency = frequency
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: container.decodeLenient(String.self, forKey: .id),
            label: container.decodeLenient(String.self, forKey: .label) ?? "",
            when: container.decodeLenient(PairingRuleConditions.self, forKey: .when) ?? PairingRuleConditions(),
            add: container.decodeLenient(PairingRuleTarget.self, forKey: .add) ?? PairingRuleTarget(),
            frequency: container.decodeLenient(PairingFrequency.self, forKey: .frequency) ?? .suggest)
    }

    /// A rule without an `id` is created; one with an id is kept. `clientID` is local only.
    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(id, forKey: .id)
        try container.encode(label, forKey: .label)
        try container.encode(when, forKey: .when)
        try container.encode(add, forKey: .add)
        try container.encode(frequency, forKey: .frequency)
    }
}
