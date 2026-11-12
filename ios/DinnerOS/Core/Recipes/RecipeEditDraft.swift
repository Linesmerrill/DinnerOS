import Foundation

/// A recipe's ingredients and steps while a member edits them: swap the ramen for lo mein, fix
/// an amount, rewrite a step. Amounts are edited for one serving size at a time; the others
/// follow in proportion, so a recipe made for 2 and 4 stays right for both. Pure, so what gets
/// saved is tested without the screen.
nonisolated struct RecipeEditDraft: Equatable, Sendable {
    /// One ingredient line.
    struct Line: Equatable, Sendable, Identifiable {
        let id: UUID
        /// The catalog ingredient the line had, kept while its name is unchanged.
        let originalID: String?
        /// The name the line was loaded or last swapped with, for renaming the steps.
        var swappedFrom: String
        var name: String
        var pantryStaple: Bool
        /// Quantity text by serving size ("1/2", "6"); empty for none.
        var quantities: [Int: String]
        var unit: String
        /// The units as loaded, by serving size: kept for any size until the unit is changed.
        let loadedUnits: [Int: String]
        let loadedUnit: String

        init(
            originalID: String? = nil, name: String = "", pantryStaple: Bool = false,
            quantities: [Int: String] = [:], unit: String = "count", units: [Int: String] = [:]
        ) {
            self.id = UUID()
            self.originalID = originalID
            self.swappedFrom = name
            self.name = name
            self.pantryStaple = pantryStaple
            self.quantities = quantities
            self.unit = unit
            self.loadedUnit = unit
            self.loadedUnits = units
        }
    }

    let servingSizes: [Int]
    /// The size whose amounts are shown and typed.
    var servings: Int
    var lines: [Line]
    var steps: [String]
    private let original: [Line]
    private let originalSteps: [String]

    init(recipe: Recipe, servings preferred: Int? = nil) {
        let sizes = recipe.servings.isEmpty ? [2] : recipe.servings
        servingSizes = sizes
        servings = preferred.flatMap { sizes.contains($0) ? $0 : nil } ?? sizes[0]
        lines = recipe.ingredients.map { ingredient in
            var quantities: [Int: String] = [:]
            var units: [Int: String] = [:]
            for amount in ingredient.amounts {
                quantities[amount.servings] = amount.quantity.map(Self.display) ?? ""
                units[amount.servings] = amount.unit
            }
            let unit = ingredient.amounts.first { !$0.unit.isEmpty }?.unit ?? "count"
            return Line(
                originalID: ingredient.ingredientID, name: ingredient.name, pantryStaple: ingredient.pantryStaple,
                quantities: quantities, unit: unit, units: units)
        }
        steps = recipe.steps.sorted { $0.index < $1.index }.map(\.text)
        original = lines
        originalSteps = steps
    }

    var hasChanges: Bool {
        lines.map(Snapshot.init) != original.map(Snapshot.init) || steps != originalSteps
    }

    /// What can't be saved yet, in plain words; `nil` when it can.
    var problem: String? {
        if lines.isEmpty { return String(localized: "Add at least one ingredient.") }
        if lines.contains(where: { $0.name.trimmingCharacters(in: .whitespaces).isEmpty }) {
            return String(localized: "Every ingredient needs a name.")
        }
        if steps.allSatisfy({ $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }) {
            return String(localized: "Add at least one step.")
        }
        for line in lines {
            for text in line.quantities.values where !text.isEmpty && Self.number(text) == nil {
                return String(localized: "The amount for \(line.name) isn't a number.")
            }
        }
        return nil
    }

    // MARK: Amounts

    func quantity(of line: Line) -> String { line.quantities[servings] ?? "" }

    /// Sets the shown size's amount, and every other size's in proportion: from its own old
    /// amount when it had one, else by servings.
    mutating func setQuantity(_ text: String, for id: Line.ID) {
        guard let i = lines.firstIndex(where: { $0.id == id }) else { return }
        let old = lines[i].quantities
        lines[i].quantities[servings] = text
        guard let new = Self.number(text) else {
            if text.trimmingCharacters(in: .whitespaces).isEmpty {
                for size in servingSizes { lines[i].quantities[size] = "" }
            }
            return
        }
        let base = old[servings].flatMap(Self.number)
        for size in servingSizes where size != servings {
            let scaled: Double
            if let base, base > 0, let other = old[size].flatMap(Self.number) {
                scaled = new * other / base
            } else {
                scaled = new * Double(size) / Double(servings)
            }
            lines[i].quantities[size] = Self.format(scaled)
        }
    }

    // MARK: Lines and steps

    mutating func addLine() {
        lines.append(Line())
    }

    mutating func removeLines(at offsets: IndexSet) {
        lines.remove(atOffsets: offsets)
    }

    /// Marks a swap as handled, so the next rename of the line starts from the new name.
    mutating func finishSwap(for id: Line.ID) {
        guard let i = lines.firstIndex(where: { $0.id == id }) else { return }
        lines[i].swappedFrom = lines[i].name
    }

    /// A line renamed since it was loaded or last swapped: the old and new names to swap in
    /// the steps. A new line has nothing to rename.
    func pendingSwap(for id: Line.ID) -> (old: String, new: String)? {
        guard let line = lines.first(where: { $0.id == id }) else { return nil }
        let old = line.swappedFrom.trimmingCharacters(in: .whitespaces)
        let new = line.name.trimmingCharacters(in: .whitespaces)
        guard !old.isEmpty, !new.isEmpty, old.caseInsensitiveCompare(new) != .orderedSame else { return nil }
        return (old, new)
    }

    // MARK: Saving

    var request: RecipeEditRequest {
        RecipeEditRequest(
            ingredients: lines.map { line in
                let name = line.name.trimmingCharacters(in: .whitespaces)
                let kept = original.first { $0.originalID == line.originalID && $0.originalID != nil }
                // A renamed line is a different ingredient: the server finds it by name.
                let id =
                    kept.map { $0.name.caseInsensitiveCompare(name) == .orderedSame } == true ? line.originalID : nil
                return RecipeEditRequest.Ingredient(
                    ingredientID: id, name: name, pantryStaple: line.pantryStaple,
                    amounts: servingSizes.map { size in
                        let quantity = line.quantities[size] ?? ""
                        let unit =
                            line.unit == line.loadedUnit
                            ? line.loadedUnits[size] ?? (line.unit == "count" && quantity.isEmpty ? "" : line.unit)
                            : line.unit
                        return RecipeEditRequest.Amount(
                            servings: size, quantity: Self.number(quantity) == nil ? "" : quantity, unit: unit)
                    })
            },
            steps: steps.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
    }

    // MARK: Numbers

    /// "1/2", "1 1/2", "0.5", "6" as a number.
    static func number(_ text: String) -> Double? {
        let parts = text.trimmingCharacters(in: .whitespaces).split(separator: " ")
        guard !parts.isEmpty, parts.count <= 2 else { return nil }
        var total = 0.0
        for part in parts {
            if let slash = part.firstIndex(of: "/") {
                guard let n = Double(part[..<slash]), let d = Double(part[part.index(after: slash)...]), d > 0 else {
                    return nil
                }
                total += n / d
            } else if let value = Double(part) {
                total += value
            } else {
                return nil
            }
        }
        return total > 0 ? total : nil
    }

    /// A server quantity ("3/2") as a person types it ("1 1/2").
    static func display(_ quantity: String) -> String {
        guard let slash = quantity.firstIndex(of: "/"), !quantity.contains(" "),
            let n = Int(quantity[..<slash]), let d = Int(quantity[quantity.index(after: slash)...]), d > 0, n > d
        else { return quantity }
        return n % d == 0 ? "\(n / d)" : "\(n / d) \(n % d)/\(d)"
    }

    /// A scaled amount: whole numbers plain, others to two decimal places.
    static func format(_ value: Double) -> String {
        let rounded = (value * 100).rounded() / 100
        if rounded == rounded.rounded() { return String(Int(rounded)) }
        return String(format: "%.2f", rounded).replacingOccurrences(of: #"0+$"#, with: "", options: .regularExpression)
    }

    /// The parts of a line that are saved, for telling whether anything changed.
    private struct Snapshot: Equatable {
        let name: String
        let quantities: [Int: String]
        let unit: String
        let pantryStaple: Bool
        init(_ line: Line) {
            name = line.name
            quantities = line.quantities
            unit = line.unit
            pantryStaple = line.pantryStaple
        }
    }
}

/// Body of `PUT /households/{id}/recipes/{recipeId}/edits`.
nonisolated struct RecipeEditRequest: Encodable, Equatable, Sendable {
    struct Ingredient: Encodable, Equatable, Sendable {
        let ingredientID: String?
        let name: String
        let pantryStaple: Bool
        let amounts: [Amount]

        private enum CodingKeys: String, CodingKey {
            case ingredientID = "ingredientId"
            case name, pantryStaple, amounts
        }
    }

    struct Amount: Encodable, Equatable, Sendable {
        let servings: Int
        let quantity: String
        let unit: String
    }

    let ingredients: [Ingredient]
    let steps: [String]
}

/// Body of `POST /households/{id}/recipes/swap-preview`.
nonisolated struct RecipeSwapRequest: Encodable, Equatable, Sendable {
    let oldName: String
    let newName: String
    let steps: [String]
    var minutes: Int = 0
}

/// The steps with a swapped ingredient, and its cook time.
nonisolated struct RecipeSwapPreview: Decodable, Equatable, Sendable {
    struct Step: Decodable, Equatable, Sendable {
        let text: String
        let changed: Bool
    }

    struct CookTime: Decodable, Equatable, Sendable {
        let lowMinutes: Int
        let highMinutes: Int
        let suggestedMinutes: Int
        /// "Lo mein noodles usually cook 4 to 6 minutes."
        let text: String
    }

    struct Time: Decodable, Equatable, Sendable {
        /// From 0.
        let step: Int
        /// As the step writes it: "2-3 minutes".
        let text: String
        let minutes: Int
    }

    let steps: [Step]
    let cookTime: CookTime?
    let times: [Time]

    var changedCount: Int { steps.filter(\.changed).count }
}
