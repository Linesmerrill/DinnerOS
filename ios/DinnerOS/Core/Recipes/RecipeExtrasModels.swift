import Foundation

// MARK: - Customizations

/// Response to `GET .../recipes/{recipeId}/customizations`: ingredients a household can swap
/// or double, usually the protein.
nonisolated struct RecipeCustomizations: Decodable, Equatable, Sendable {
    let groups: [CustomizationGroup]

    private enum CodingKeys: String, CodingKey {
        case groups
    }

    init(groups: [CustomizationGroup]) {
        self.groups = groups
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        groups = container.decodeLossyArray(CustomizationGroup.self, forKey: .groups).filter { !$0.choices.isEmpty }
    }
}

/// How a choice differs from the recipe as written. Unknown values decode as-is.
nonisolated struct CustomizationKind: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let original = CustomizationKind(rawValue: "original")
    static let double = CustomizationKind(rawValue: "double")
    static let swap = CustomizationKind(rawValue: "swap")
    static let swapDouble = CustomizationKind(rawValue: "swap_double")
}

/// One customizable ingredient and its choices.
nonisolated struct CustomizationGroup: Decodable, Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let ingredientName: String
    let amountText: String
    var imageURLString: String?
    let choices: [CustomizationChoice]

    var id: String { ingredientKey }
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    /// The recipe as written: the `original` choice, else the first.
    var originalChoice: CustomizationChoice? {
        choices.first { $0.kind == .original } ?? choices.first
    }

    private enum CodingKeys: String, CodingKey {
        case ingredientKey, ingredientName, amountText
        case imageURLString = "imageUrl"
        case choices
    }

    init(
        ingredientKey: String, ingredientName: String, amountText: String, imageURLString: String? = nil,
        choices: [CustomizationChoice]
    ) {
        self.ingredientKey = ingredientKey
        self.ingredientName = ingredientName
        self.amountText = amountText
        self.imageURLString = imageURLString
        self.choices = choices
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        ingredientKey = try container.decode(String.self, forKey: .ingredientKey)
        ingredientName = container.decodeLenient(String.self, forKey: .ingredientName) ?? ""
        amountText = container.decodeLenient(String.self, forKey: .amountText) ?? ""
        imageURLString = container.decodeLenient(String.self, forKey: .imageURLString)
        choices = container.decodeLossyArray(CustomizationChoice.self, forKey: .choices)
    }
}

nonisolated struct CustomizationChoice: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// For example "2x Ground Pork".
    let label: String
    let ingredientName: String
    /// For example "20 ounce".
    let amountText: String
    var imageURLString: String?
    let kind: CustomizationKind
    /// For example "Double portion"; `nil` for none.
    var badge: String?

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case id, label, ingredientName, amountText
        case imageURLString = "imageUrl"
        case kind, badge
    }

    init(
        id: String, label: String, ingredientName: String, amountText: String, imageURLString: String? = nil,
        kind: CustomizationKind, badge: String? = nil
    ) {
        self.id = id
        self.label = label
        self.ingredientName = ingredientName
        self.amountText = amountText
        self.imageURLString = imageURLString
        self.kind = kind
        self.badge = badge
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        label = try container.decode(String.self, forKey: .label)
        ingredientName = container.decodeLenient(String.self, forKey: .ingredientName) ?? label
        amountText = container.decodeLenient(String.self, forKey: .amountText) ?? ""
        imageURLString = container.decodeLenient(String.self, forKey: .imageURLString)
        kind = container.decodeLenient(CustomizationKind.self, forKey: .kind) ?? .swap
        badge = container.decodeLenient(String.self, forKey: .badge).flatMap { $0.isEmpty ? nil : $0 }
    }
}

// MARK: - Pairings

/// Response to `GET .../recipes/{recipeId}/pairings?week=`: add-ons that go with a recipe.
nonisolated struct RecipePairings: Decodable, Equatable, Sendable {
    let items: [RecipePairing]

    private enum CodingKeys: String, CodingKey {
        case items
    }

    init(items: [RecipePairing]) {
        self.items = items
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = container.decodeLossyArray(RecipePairing.self, forKey: .items)
    }
}

/// A grocery item suggested as a pairing, such as crackers.
nonisolated struct PairingGroceryItem: Decodable, Hashable, Sendable {
    let name: String
    var quantity: String?
    var unit: String?

    private enum CodingKeys: String, CodingKey {
        case name, quantity, unit
    }

    init(name: String, quantity: String? = nil, unit: String? = nil) {
        self.name = name
        self.quantity = quantity
        self.unit = unit
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        quantity =
            container.decodeLenient(String.self, forKey: .quantity)
            ?? container.decodeLenient(Double.self, forKey: .quantity).map {
                $0.formatted(.number.precision(.fractionLength(0...2)))
            }
        unit = container.decodeLenient(String.self, forKey: .unit).flatMap { $0.isEmpty ? nil : $0 }
    }
}

nonisolated struct RecipePairing: Decodable, Hashable, Sendable, Identifiable {
    enum Target: Hashable, Sendable {
        case recipe(RecipeSummary)
        case groceryItem(PairingGroceryItem)
    }

    let target: Target
    /// `rule` or `learned`.
    let source: String
    let confidence: Double?
    let reason: String?
    /// Already in the week, so the checkbox starts checked.
    var inPlan: Bool
    let ruleID: String?

    var id: String {
        switch target {
        case .recipe(let recipe): "recipe:\(recipe.id)"
        case .groceryItem(let item): "item:\(item.name)"
        }
    }

    var name: String {
        switch target {
        case .recipe(let recipe): recipe.name
        case .groceryItem(let item): item.name
        }
    }

    private enum CodingKeys: String, CodingKey {
        case target, source, confidence, reason, inPlan
        case ruleID = "ruleId"
    }

    private enum TargetKeys: String, CodingKey {
        case kind, recipe, groceryItem
    }

    init(target: Target, source: String = "rule", confidence: Double? = nil, reason: String? = nil, inPlan: Bool) {
        self.target = target
        self.source = source
        self.confidence = confidence
        self.reason = reason
        self.inPlan = inPlan
        ruleID = nil
    }

    /// A target of an unknown kind fails, so the list leaves the pairing out.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let targetContainer = try container.nestedContainer(keyedBy: TargetKeys.self, forKey: .target)
        switch try targetContainer.decode(String.self, forKey: .kind) {
        case "recipe":
            target = .recipe(try targetContainer.decode(RecipeSummary.self, forKey: .recipe))
        case "grocery_item":
            target = .groceryItem(try targetContainer.decode(PairingGroceryItem.self, forKey: .groceryItem))
        default:
            throw DecodingError.dataCorruptedError(
                forKey: .kind, in: targetContainer, debugDescription: "Unknown pairing target")
        }
        source = container.decodeLenient(String.self, forKey: .source) ?? ""
        confidence = container.decodeLenient(Double.self, forKey: .confidence)
        reason = container.decodeLenient(String.self, forKey: .reason).flatMap { $0.isEmpty ? nil : $0 }
        inPlan = container.decodeLenientBool(forKey: .inPlan) ?? false
        ruleID = container.decodeLenient(String.self, forKey: .ruleID)
    }
}
