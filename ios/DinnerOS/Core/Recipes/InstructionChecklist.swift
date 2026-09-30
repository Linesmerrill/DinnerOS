import Foundation

/// The cooking screen's ingredient checklist as the server builds it (`checklist` on the
/// instructions response): every ingredient all together, and grouped by the step that uses it
/// with its prep words. The app shows it as sent, so the wording improves with an API deploy.
nonisolated struct InstructionChecklist: Decodable, Equatable, Sendable {
    let all: [Ingredient]
    let byStep: [Group]

    nonisolated struct Ingredient: Decodable, Equatable, Sendable {
        let id: String
        let index: Int
        let name: String
        var amountText: String? = nil
        var leftOut = false
        var ingredientKey: String? = nil
        var parts: [Part] = []
        var components: [String] = []

        private enum CodingKeys: String, CodingKey {
            case id, index, name, amountText, leftOut, ingredientKey, parts, components
        }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            index = try c.decode(Int.self, forKey: .index)
            name = try c.decode(String.self, forKey: .name)
            amountText = c.decodeLenient(String.self, forKey: .amountText)
            leftOut = c.decodeLenientBool(forKey: .leftOut) ?? false
            ingredientKey = c.decodeLenient(String.self, forKey: .ingredientKey)
            parts = c.decodeLossyArray(Part.self, forKey: .parts)
            components = c.decodeLossyArray(String.self, forKey: .components)
        }
    }

    nonisolated struct Part: Decodable, Equatable, Sendable {
        let id: String
        let amountText: String
        let stepIndex: Int
    }

    nonisolated struct Group: Decodable, Equatable, Sendable {
        let index: Int
        let items: [Item]

        private enum CodingKeys: String, CodingKey { case index, items }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            index = try c.decode(Int.self, forKey: .index)
            items = c.decodeLossyArray(Item.self, forKey: .items)
        }
    }

    nonisolated struct Item: Decodable, Equatable, Sendable {
        let id: String
        let ingredientIndex: Int
        let name: String
        var amountText: String? = nil
        var prep: String? = nil
        var parts: [String] = []
        var leftOut = false
        var ingredientKey: String? = nil

        private enum CodingKeys: String, CodingKey {
            case id, ingredientIndex, name, amountText, prep, parts, leftOut, ingredientKey
        }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            ingredientIndex = try c.decode(Int.self, forKey: .ingredientIndex)
            name = try c.decode(String.self, forKey: .name)
            amountText = c.decodeLenient(String.self, forKey: .amountText)
            prep = c.decodeLenient(String.self, forKey: .prep)
            parts = c.decodeLossyArray(String.self, forKey: .parts)
            leftOut = c.decodeLenientBool(forKey: .leftOut) ?? false
            ingredientKey = c.decodeLenient(String.self, forKey: .ingredientKey)
        }
    }
}

/// A cooking time in a step, named by the server: "2-3 minutes", a Sauce timer starting at 3:00.
nonisolated struct InstructionTimer: Decodable, Equatable, Sendable {
    let text: String
    let lowSeconds: Int
    let highSeconds: Int
    let startSeconds: Int
    var subject: String? = nil

    private enum CodingKeys: String, CodingKey { case text, lowSeconds, highSeconds, startSeconds, subject }

    init(text: String, lowSeconds: Int, highSeconds: Int, startSeconds: Int, subject: String? = nil) {
        self.text = text
        self.lowSeconds = lowSeconds
        self.highSeconds = highSeconds
        self.startSeconds = startSeconds
        self.subject = subject
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        text = try c.decode(String.self, forKey: .text)
        lowSeconds = try c.decode(Int.self, forKey: .lowSeconds)
        highSeconds = try c.decode(Int.self, forKey: .highSeconds)
        startSeconds = (try? c.decode(Int.self, forKey: .startSeconds)) ?? highSeconds
        subject = c.decodeLenient(String.self, forKey: .subject).flatMap { $0.isEmpty ? nil : $0 }
    }
}

nonisolated extension InstructionChecklist {
    /// The all-together list in the cooking screen's shape, with each row's photo from the recipe.
    func cookIngredients(recipe: Recipe) -> [CookIngredient] {
        all.map { ingredient in
            CookIngredient(
                id: ingredient.id, name: ingredient.name, amountText: ingredient.amountText,
                imageURL: recipe.ingredients.indices.contains(ingredient.index)
                    ? recipe.ingredients[ingredient.index].imageURL : nil,
                isLeftOut: ingredient.leftOut, ingredientKey: ingredient.ingredientKey,
                parts: ingredient.parts.map { CookPart(id: $0.id, amountText: $0.amountText, stepIndex: $0.stepIndex) },
                components: ingredient.components)
        }
    }

    /// The by-step list in the cooking screen's shape.
    var cookStepGroups: [CookStepGroup] {
        byStep.map { group in
            CookStepGroup(
                index: group.index,
                items: group.items.map { item in
                    CookStepItem(
                        id: item.id, name: item.name, amountText: item.amountText, prep: item.prep, parts: item.parts,
                        isLeftOut: item.leftOut, ingredientKey: item.ingredientKey)
                })
        }
    }
}
