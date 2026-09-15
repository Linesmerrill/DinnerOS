import Foundation

/// Why an option can't be saved yet.
nonisolated enum SpecialtyOptionProblem: Error, Equatable, Sendable {
    case nameMissing
    case nameTooLong
    case notesTooLong
    case perMissing
    case perAmount(PantryQuantityError)
    case yieldMissing
    case yieldAmount(PantryQuantityError)
    case shelfLife
    case noIngredients
    case tooManyIngredients
    case ingredientNameMissing
    case ingredientNameTooLong(String)
    case ingredientAmountMissing(String)
    case ingredientAmount(String, PantryQuantityError)
    case tooManySteps
    case stepTooLong
}

extension SpecialtyOptionProblem: LocalizedError {
    nonisolated var errorDescription: String? {
        switch self {
        case .nameMissing:
            String(localized: "Give the option a name.")
        case .nameTooLong:
            String(localized: "The name can be at most \(SpecialtyOptionDraft.maxNameLength) characters.")
        case .notesTooLong:
            String(localized: "Notes can be at most \(SpecialtyOptionDraft.maxNotesLength) characters.")
        case .perMissing:
            String(localized: "Enter how much of the specialty ingredient these ingredients replace.")
        case .perAmount(let error), .yieldAmount(let error):
            error.errorDescription
        case .yieldMissing:
            String(localized: "Enter how much one batch makes.")
        case .shelfLife:
            String(
                localized:
                    "Shelf life must be \(SpecialtyOptionDraft.shelfLifeRange.lowerBound) to \(SpecialtyOptionDraft.shelfLifeRange.upperBound) days."
            )
        case .noIngredients:
            String(localized: "Add at least one ingredient.")
        case .tooManyIngredients:
            String(localized: "An option can have at most \(SpecialtyOptionDraft.maxIngredients) ingredients.")
        case .ingredientNameMissing:
            String(localized: "Every ingredient with an amount needs a name.")
        case .ingredientNameTooLong(let name):
            String(localized: "\(name) is too long a name.")
        case .ingredientAmountMissing(let name):
            String(localized: "Enter an amount for \(name). Store alternatives need an amount for every ingredient.")
        case .ingredientAmount(let name, let error):
            "\(name): \(error.errorDescription ?? "")"
        case .tooManySteps:
            String(localized: "A batch can have at most \(SpecialtyOptionDraft.maxSteps) steps.")
        case .stepTooLong:
            String(localized: "Each step can be at most \(SpecialtyOptionDraft.maxStepLength) characters.")
        }
    }
}

/// An editable store alternative or house-made batch: a copy of an option for "Customize", or a
/// household option being edited.
///
/// Amounts are typed text, parsed with `PantryQuantity.parse` into the API's exact form when
/// the request is built. Blank ingredient and step rows are ignored.
nonisolated struct SpecialtyOptionDraft: Equatable, Sendable {
    nonisolated struct Ingredient: Equatable, Sendable, Identifiable {
        let id: UUID
        var name: String
        var quantityText: String
        var unit: String
        /// Kept from the copied option, so the API can file an ingredient the catalog doesn't know.
        var category: String?

        init(
            id: UUID = UUID(), name: String = "", quantityText: String = "", unit: String = PantryUnit.defaultCode,
            category: String? = nil
        ) {
            self.id = id
            self.name = name
            self.quantityText = quantityText
            self.unit = unit
            self.category = category
        }

        var isBlank: Bool {
            name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                && quantityText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    nonisolated struct Step: Equatable, Sendable, Identifiable {
        let id: UUID
        var text: String

        init(id: UUID = UUID(), text: String = "") {
            self.id = id
            self.text = text
        }
    }

    static let maxNameLength = 100
    static let maxNotesLength = 500
    static let maxIngredients = 20
    static let maxSteps = 20
    static let maxStepLength = 500
    static let shelfLifeRange = 1...730

    let type: SpecialtyOptionType
    var name: String
    var notes: String
    /// Store alternatives: how much of the specialty ingredient the ingredients replace.
    var perQuantityText: String
    var perUnit: String
    var ingredients: [Ingredient]
    var steps: [Step]
    /// Batches: how much one batch makes.
    var yieldQuantityText: String
    var yieldUnit: String
    var shelfLifeDays: Int
    let basedOnOptionID: String?

    /// A copy of `option` for "Customize", named so it's told apart from the original.
    init(copying option: SpecialtyOption) {
        self.init(option: option, basedOnOptionID: option.id)
        name = String(String(localized: "\(option.name) (custom)").prefix(Self.maxNameLength))
    }

    /// A household option as it is now.
    init(editing option: SpecialtyOption) {
        self.init(option: option, basedOnOptionID: option.basedOnOptionID)
    }

    private init(option: SpecialtyOption, basedOnOptionID: String?) {
        type = option.type
        name = option.name
        notes = option.notes
        perQuantityText = PantryQuantity.editingText(option.per?.quantity)
        perUnit = option.per?.unit ?? "tbsp"
        ingredients = option.ingredients.map { ingredient in
            Ingredient(
                name: ingredient.name, quantityText: PantryQuantity.editingText(ingredient.quantity),
                unit: ingredient.unit ?? PantryUnit.defaultCode, category: ingredient.category)
        }
        steps = option.steps.map { Step(text: $0) }
        yieldQuantityText = PantryQuantity.editingText(option.batchYield?.quantity)
        yieldUnit = option.batchYield?.unit ?? "tbsp"
        shelfLifeDays = option.shelfLifeDays ?? 30
        self.basedOnOptionID = basedOnOptionID
    }

    var isStoreAlternative: Bool {
        type == .storeAlternative
    }

    var canAddIngredient: Bool {
        ingredients.count < Self.maxIngredients
    }

    var canAddStep: Bool {
        steps.count < Self.maxSteps
    }

    mutating func addIngredient() {
        guard canAddIngredient else { return }
        ingredients.append(Ingredient())
    }

    mutating func addStep() {
        guard canAddStep else { return }
        steps.append(Step())
    }

    /// The first thing to fix, or `nil` when the draft can be saved.
    var problem: SpecialtyOptionProblem? {
        do {
            _ = try request()
            return nil
        } catch {
            return error
        }
    }

    var isValid: Bool {
        problem == nil
    }

    /// The request body, with amounts in the API's exact form.
    func request() throws(SpecialtyOptionProblem) -> SpecialtyOptionRequest {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else { throw .nameMissing }
        guard trimmedName.count <= Self.maxNameLength else { throw .nameTooLong }
        let trimmedNotes = notes.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmedNotes.count <= Self.maxNotesLength else { throw .notesTooLong }

        var request = SpecialtyOptionRequest(
            type: type, name: trimmedName, notes: trimmedNotes.isEmpty ? nil : trimmedNotes, ingredients: [],
            basedOnOptionID: basedOnOptionID)

        if isStoreAlternative {
            guard let per = try Self.parse(perQuantityText, SpecialtyOptionProblem.perAmount) else {
                throw .perMissing
            }
            request.per = SpecialtyAmountInput(quantity: per, unit: perUnit)
        } else {
            guard let amount = try Self.parse(yieldQuantityText, SpecialtyOptionProblem.yieldAmount) else {
                throw .yieldMissing
            }
            request.batchYield = SpecialtyAmountInput(quantity: amount, unit: yieldUnit)
            guard Self.shelfLifeRange.contains(shelfLifeDays) else { throw .shelfLife }
            request.shelfLifeDays = shelfLifeDays
        }

        for ingredient in ingredients where !ingredient.isBlank {
            let ingredientName = ingredient.name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !ingredientName.isEmpty else { throw .ingredientNameMissing }
            guard ingredientName.count <= Self.maxNameLength else { throw .ingredientNameTooLong(ingredientName) }
            let quantity = try Self.parse(ingredient.quantityText) { .ingredientAmount(ingredientName, $0) }
            if isStoreAlternative, quantity == nil {
                throw .ingredientAmountMissing(ingredientName)
            }
            request.ingredients.append(
                SpecialtyIngredientInput(
                    name: ingredientName, quantity: quantity, unit: quantity == nil ? nil : ingredient.unit,
                    category: ingredient.category))
        }
        guard !request.ingredients.isEmpty else { throw .noIngredients }
        guard request.ingredients.count <= Self.maxIngredients else { throw .tooManyIngredients }

        if !isStoreAlternative {
            let stepTexts = steps.map { $0.text.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
            guard stepTexts.count <= Self.maxSteps else { throw .tooManySteps }
            guard stepTexts.allSatisfy({ $0.count <= Self.maxStepLength }) else { throw .stepTooLong }
            request.steps = stepTexts
        }
        return request
    }

    private static func parse(
        _ text: String, _ wrap: (PantryQuantityError) -> SpecialtyOptionProblem
    ) throws(SpecialtyOptionProblem) -> String? {
        do {
            return try PantryQuantity.parse(text)
        } catch {
            throw wrap(error)
        }
    }
}
