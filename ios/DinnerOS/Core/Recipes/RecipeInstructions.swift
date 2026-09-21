import Foundation

/// Response to `GET /api/v1/households/{householdId}/recipes/{recipeId}/instructions`: the
/// recipe's steps rendered for one serving size, with the household's specialty ingredient
/// choices already applied.
///
/// The API decides *what* a step says; the app decides only how it looks. Joining every
/// segment's `text` gives the step's `text`, so nothing here needs character offsets.
nonisolated struct RecipeInstructions: Decodable, Equatable, Sendable {
    let recipeID: String
    let recipeName: String
    /// The serving size every amount is for.
    let servings: Int
    let servingOptions: [Int]
    /// `false` when the server couldn't read the household's choices, so the steps read as
    /// the recipe was written.
    let specialtiesApplied: Bool
    let steps: [InstructionStep]
    /// The specialty ingredients that read differently because of the household's choices.
    let substitutions: [InstructionSubstitution]
    /// Specialty ingredients the household hasn't decided about. They read as the card wrote
    /// them, so the screen can offer the choice.
    let unchosenSpecialties: [InstructionSpecialtyRef]

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case recipeName, servings, servingOptions, specialtiesApplied, steps, substitutions
        case unchosenSpecialties
    }

    static let empty = RecipeInstructions(
        recipeID: "", recipeName: "", servings: 0, servingOptions: [], specialtiesApplied: false,
        steps: [], substitutions: [], unchosenSpecialties: [])
}

nonisolated extension RecipeInstructions {
    /// Additive fields are read leniently: an older server that sends only steps still works.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            recipeID: try container.decode(String.self, forKey: .recipeID),
            recipeName: (try? container.decode(String.self, forKey: .recipeName)) ?? "",
            servings: (try? container.decode(Int.self, forKey: .servings)) ?? 0,
            servingOptions: container.decodeLossyArray(Int.self, forKey: .servingOptions),
            specialtiesApplied: (try? container.decode(Bool.self, forKey: .specialtiesApplied)) ?? false,
            steps: container.decodeLossyArray(InstructionStep.self, forKey: .steps),
            substitutions: container.decodeLossyArray(InstructionSubstitution.self, forKey: .substitutions),
            unchosenSpecialties: container.decodeLossyArray(InstructionSpecialtyRef.self, forKey: .unchosenSpecialties))
    }
}

nonisolated struct InstructionStep: Decodable, Equatable, Sendable, Identifiable {
    /// 1-based.
    let index: Int
    /// The whole step, the same string the segments join to.
    let text: String
    /// The recipe's own wording, `nil` unless a substitution changed the step.
    let originalText: String?
    let imageURLString: String?
    let segments: [InstructionSegment]
    let notes: [InstructionNote]

    var id: Int { index }
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case index, text, originalText, segments, notes
        case imageURLString = "imageUrl"
    }

    init(
        index: Int, text: String, originalText: String? = nil, imageURLString: String? = nil,
        segments: [InstructionSegment] = [], notes: [InstructionNote] = []
    ) {
        self.index = index
        self.text = text
        self.originalText = originalText
        self.imageURLString = imageURLString
        self.segments = segments
        self.notes = notes
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            index: try container.decode(Int.self, forKey: .index),
            text: try container.decode(String.self, forKey: .text),
            originalText: container.decodeLenient(String.self, forKey: .originalText),
            imageURLString: container.decodeLenient(String.self, forKey: .imageURLString),
            segments: container.decodeLossyArray(InstructionSegment.self, forKey: .segments),
            notes: container.decodeLossyArray(InstructionNote.self, forKey: .notes))
    }
}

nonisolated extension InstructionStep {
    /// What VoiceOver says: the step's sentence with spicy ingredients announced as spicy, so
    /// the heat doesn't depend on seeing red.
    var spokenText: String {
        guard !segments.isEmpty else { return text }
        return segments.reduce(into: "") { spoken, segment in
            spoken += segment.isIngredient && segment.spicy ? "spicy \(segment.text)" : segment.text
        }
    }

    /// The ingredients the step names, in the order it names them.
    var ingredientSegments: [InstructionSegment] { segments.filter(\.isIngredient) }
}

/// One run of a step: plain text, or an ingredient the recipe lists.
nonisolated struct InstructionSegment: Decodable, Equatable, Sendable {
    enum Kind: String, Decodable, Sendable {
        case text
        case ingredient
    }

    let kind: Kind
    let text: String
    let ingredientID: String?
    /// What the segment stands for, after any substitution.
    let name: String?
    /// The amount for the rendered serving size, `nil` when there is none to show.
    let amount: InstructionAmount?
    /// The ingredient brings heat. Never the only signal the app shows for it.
    let spicy: Bool
    /// The household's specialty choice changed this mention.
    let substituted: Bool
    let specialtyID: String?
    let specialtyName: String?

    var isIngredient: Bool { kind == .ingredient }

    private enum CodingKeys: String, CodingKey {
        case kind, text, name, amount, spicy, substituted
        case ingredientID = "ingredientId"
        case specialtyID = "specialtyId"
        case specialtyName
    }

    init(
        kind: Kind, text: String, ingredientID: String? = nil, name: String? = nil,
        amount: InstructionAmount? = nil, spicy: Bool = false, substituted: Bool = false,
        specialtyID: String? = nil, specialtyName: String? = nil
    ) {
        self.kind = kind
        self.text = text
        self.ingredientID = ingredientID
        self.name = name
        self.amount = amount
        self.spicy = spicy
        self.substituted = substituted
        self.specialtyID = specialtyID
        self.specialtyName = specialtyName
    }

    /// A kind this build doesn't know reads as plain text, so a newer server never blanks a step.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            kind: (try? container.decode(Kind.self, forKey: .kind)) ?? .text,
            text: try container.decode(String.self, forKey: .text),
            ingredientID: container.decodeLenient(String.self, forKey: .ingredientID),
            name: container.decodeLenient(String.self, forKey: .name),
            amount: container.decodeLenient(InstructionAmount.self, forKey: .amount),
            spicy: (try? container.decode(Bool.self, forKey: .spicy)) ?? false,
            substituted: (try? container.decode(Bool.self, forKey: .substituted)) ?? false,
            specialtyID: container.decodeLenient(String.self, forKey: .specialtyID),
            specialtyName: container.decodeLenient(String.self, forKey: .specialtyName))
    }
}

nonisolated struct InstructionAmount: Decodable, Equatable, Sendable {
    let quantity: String
    let quantityValue: Double
    let unit: String
    /// The amount spelled for people ("1 ½ cups"); show this.
    let text: String
}

/// A sentence under a step: what to use instead, when a swapped word can't say it.
nonisolated struct InstructionNote: Decodable, Equatable, Sendable, Identifiable {
    let kind: String
    let specialtyID: String?
    let text: String

    var id: String { (specialtyID ?? "") + "\u{0}" + text }

    private enum CodingKeys: String, CodingKey {
        case kind, text
        case specialtyID = "specialtyId"
    }
}

nonisolated struct InstructionSubstitution: Decodable, Equatable, Sendable, Identifiable {
    let specialtyID: String
    let specialtyKey: String
    let specialtyName: String
    let optionID: String
    let optionName: String
    /// `store_alternative` or `house_made_batch`.
    let type: String
    /// `household` when a member chose it, `strategy` when the household's default did.
    let source: String
    let text: String

    var id: String { specialtyID }
    var isDefaultChoice: Bool { source == "strategy" }

    private enum CodingKeys: String, CodingKey {
        case specialtyID = "specialtyId"
        case specialtyKey, specialtyName
        case optionID = "optionId"
        case optionName, type, source, text
    }
}

nonisolated struct InstructionSpecialtyRef: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let key: String
    let name: String
}
