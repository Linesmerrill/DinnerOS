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

// Pairings live in `Core/Autopilot/AutopilotPairingModels.swift`: the recipe carousel, the
// week's suggestions, and the proposal review all read the same `Pairing`.
