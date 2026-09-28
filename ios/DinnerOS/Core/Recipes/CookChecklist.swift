import Foundation

/// One step's share of an ingredient the recipe uses in more than one step: "2 tbsp" of the
/// butter, for step 1.
nonisolated struct CookPart: Equatable, Sendable, Identifiable {
    let id: String
    let amountText: String
    let stepIndex: Int
}

/// An ingredient on the cooking checklist: the whole amount, and its per-step shares when the
/// recipe splits it across steps (butter melted in step 1 and stirred in at step 3).
nonisolated struct CookIngredient: Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    let amountText: String?
    let imageURL: URL?
    let isLeftOut: Bool
    /// Empty unless two or more steps each say how much of it they use.
    let parts: [CookPart]
}

/// Builds the cooking checklist from the recipe and its rendered steps.
nonisolated enum CookChecklist {
    static func make(recipe: Recipe, servings: Int, instructions: RecipeInstructions?) -> [CookIngredient] {
        let lines = recipe.ingredientLines(servings: servings)
        let states = RecipeIngredientStates.make(lines: lines, instructions: instructions)
        return zip(recipe.ingredients, states).map { ingredient, state in
            var parts: [CookPart] = []
            for step in instructions?.steps ?? [] {
                for segment in step.segments where segment.isIngredient && segment.part && !segment.leftOut {
                    guard let amount = segment.amount, matches(segment, ingredient) else { continue }
                    let id = "\(state.id)#\(step.index)-\(parts.count)"
                    parts.append(CookPart(id: id, amountText: amount.text, stepIndex: step.index))
                }
            }
            return CookIngredient(
                id: state.id, name: state.line.name, amountText: state.line.amount, imageURL: state.line.imageURL,
                isLeftOut: state.isLeftOut, parts: parts.count >= 2 ? parts : [])
        }
    }

    static func matches(_ segment: InstructionSegment, _ ingredient: RecipeIngredient) -> Bool {
        if let id = segment.ingredientID, !id.isEmpty, !ingredient.ingredientID.isEmpty {
            return id == ingredient.ingredientID
        }
        return segment.name?.caseInsensitiveCompare(ingredient.name) == .orderedSame
    }
}

/// What the cook has ticked off and which step they're on, per recipe, for as long as the
/// cooking screen is open. Switching dishes keeps each dish's progress.
@MainActor @Observable
final class CookSession {
    private(set) var checked: [String: Set<String>] = [:]
    private(set) var currentStep: [String: Int] = [:]

    func isChecked(_ id: String, recipe: String) -> Bool { checked[recipe, default: []].contains(id) }

    /// Ticks an ingredient, and with it every share of it; unticks the same way.
    func toggle(_ ingredient: CookIngredient, recipe: String) {
        var set = checked[recipe, default: []]
        let ids = [ingredient.id] + ingredient.parts.map(\.id)
        if set.contains(ingredient.id) {
            set.subtract(ids)
        } else {
            set.formUnion(ids)
        }
        checked[recipe] = set
    }

    /// Ticks one share; the ingredient is ticked once every share is.
    func toggle(_ part: CookPart, of ingredient: CookIngredient, recipe: String) {
        var set = checked[recipe, default: []]
        if set.contains(part.id) {
            set.remove(part.id)
        } else {
            set.insert(part.id)
        }
        if ingredient.parts.allSatisfy({ set.contains($0.id) }) {
            set.insert(ingredient.id)
        } else {
            set.remove(ingredient.id)
        }
        checked[recipe] = set
    }

    /// Ticks a by-step row and its parts together.
    func toggle(_ item: CookStepItem, recipe: String) {
        var set = checked[recipe, default: []]
        let ids = [item.id] + item.parts.indices.map(item.partID)
        if set.contains(item.id) { set.subtract(ids) } else { set.formUnion(ids) }
        checked[recipe] = set
    }

    /// Ticks one part; the row is ticked once every part is.
    func togglePart(_ index: Int, of item: CookStepItem, recipe: String) {
        var set = checked[recipe, default: []]
        let id = item.partID(index)
        if set.contains(id) { set.remove(id) } else { set.insert(id) }
        if item.parts.indices.allSatisfy({ set.contains(item.partID($0)) }) {
            set.insert(item.id)
        } else {
            set.remove(item.id)
        }
        checked[recipe] = set
    }

    func current(recipe: String) -> Int? { currentStep[recipe] }

    /// Tapping the step you're on again clears it.
    func tapStep(_ index: Int, recipe: String) {
        currentStep[recipe] = currentStep[recipe] == index ? nil : index
    }
}

// MARK: - By step

/// One ingredient as a step uses it: "2 scallions, sliced", with the parts the step splits it
/// into ("Scallion whites", "Scallion greens").
nonisolated struct CookStepItem: Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    let amountText: String?
    /// "sliced", "peeled, cored, and diced"; `nil` when the step says nothing about it.
    let prep: String?
    let parts: [String]
    let isLeftOut: Bool
    func partID(_ index: Int) -> String { "\(id)#\(index)" }
}

/// Everything one step needs. Index 0 holds what no step names (salt, oil).
nonisolated struct CookStepGroup: Equatable, Sendable, Identifiable {
    let index: Int
    let items: [CookStepItem]
    var id: Int { index }
}

nonisolated extension CookChecklist {
    static func byStep(recipe: Recipe, servings: Int, instructions: RecipeInstructions?) -> [CookStepGroup] {
        let all = make(recipe: recipe, servings: servings, instructions: instructions)
        guard let steps = instructions?.steps, !steps.isEmpty else {
            return [CookStepGroup(index: 0, items: all.map(item(for:)))]
        }
        var used = Set<String>()
        var groups: [CookStepGroup] = []
        for step in steps {
            var items: [CookStepItem] = []
            var clause = ""
            for (i, segment) in step.segments.enumerated() {
                guard segment.isIngredient else {
                    clause = lastClause(clause + segment.text)
                    continue
                }
                guard let index = recipe.ingredients.firstIndex(where: { matches(segment, $0) }), index < all.count
                else { continue }
                let ingredient = all[index]
                used.insert(ingredient.id)
                let after =
                    i + 1 < step.segments.count && !step.segments[i + 1].isIngredient ? step.segments[i + 1].text : ""
                let partWord = leadingPart(after)
                let name = partWord.map { "\(singular(ingredient.name)) \($0)" } ?? ingredient.name
                // "Crushed Tomatoes, crushed" says nothing.
                let prep = prepWords(in: clause).flatMap { words in
                    ingredient.name.lowercased().contains(words) ? nil : words
                }
                let parts = splitParts(after, name: ingredient.name)
                if let existing = items.firstIndex(where: { $0.name == name }) {
                    // Named again in the same step ("zest the lemon … halve lemon"): add what's done.
                    let old = items[existing]
                    // Only what's new: "grate 1 zucchini … place grated zucchini" is grated once.
                    let adds = prep.flatMap { new in old.prep?.contains(new) == true ? nil : new }
                    let merged = [old.prep, adds].compactMap { $0 }.joined(separator: ", ")
                    items[existing] = CookStepItem(
                        id: old.id, name: old.name, amountText: old.amountText, prep: merged.isEmpty ? nil : merged,
                        parts: old.parts.isEmpty ? parts : old.parts, isLeftOut: old.isLeftOut)
                } else {
                    items.append(
                        CookStepItem(
                            id: "\(ingredient.id)@\(step.index)-\(items.count)", name: name,
                            amountText: partWord == nil ? segment.amount?.text : segment.amount?.text,
                            prep: prep, parts: parts, isLeftOut: ingredient.isLeftOut || segment.leftOut))
                }
                clause = ""
            }
            if !items.isEmpty { groups.append(CookStepGroup(index: step.index, items: items)) }
        }
        let rest = all.filter { !used.contains($0.id) }.map(item(for:))
        if !rest.isEmpty { groups.insert(CookStepGroup(index: 0, items: rest), at: 0) }
        return groups
    }

    private static func item(for ingredient: CookIngredient) -> CookStepItem {
        CookStepItem(
            id: "\(ingredient.id)@0", name: ingredient.name, amountText: ingredient.amountText, prep: nil, parts: [],
            isLeftOut: ingredient.isLeftOut)
    }

    /// The text since the last sentence or clause break.
    private static func lastClause(_ text: String) -> String {
        let breaks: [Character] = [".", ";", "\n", ":"]
        guard let last = text.lastIndex(where: { breaks.contains($0) }) else { return text }
        return String(text[text.index(after: last)...])
    }

    /// Prep verbs, as done to the ingredient: "Trim and slice" → "trimmed and sliced".
    private static let verbs: [String: String] = [
        "slice": "sliced", "sliced": "sliced", "dice": "diced", "diced": "diced", "mince": "minced",
        "minced": "minced", "chop": "chopped", "chopped": "chopped", "quarter": "quartered",
        "quartered": "quartered", "halve": "halved", "halved": "halved", "zest": "zested", "zested": "zested",
        "grate": "grated", "grated": "grated", "peel": "peeled", "peeled": "peeled", "core": "cored",
        "cored": "cored", "trim": "trimmed", "trimmed": "trimmed", "juice": "juiced", "shred": "shredded",
        "shredded": "shredded", "cube": "cubed", "cubed": "cubed", "crush": "crushed", "crushed": "crushed",
        "pit": "pitted", "pitted": "pitted", "drain": "drained", "drained": "drained", "rinse": "rinsed",
        "rinsed": "rinsed", "melt": "melted", "melted": "melted", "soften": "softened", "tear": "torn",
        "pat": "patted dry", "julienne": "julienned", "smash": "smashed",
    ]
    private static let adverbs: Set<String> = ["thinly", "finely", "roughly", "coarsely", "thickly"]

    /// Words allowed between prep verbs and the ingredient they apply to: "Halve, peel, and
    /// finely dice 1 shallot", "juice from half 1 lime". Anything else ends the run, so a word
    /// that belongs to something earlier in the sentence ("Stir drained rigatoni, half the
    /// Parmesan, and 1 tbsp butter") never lands on the wrong ingredient.
    private static let fillers: Set<String> = [
        "and", "or", "the", "a", "an", "then", "from", "half", "of", "your", "remaining",
        "tsp", "tbsp", "cup", "cups", "oz", "clove", "cloves", "lb", "g", "can", "cans",
    ]

    /// What may sit between a participle and its ingredient: "diced 1 tbsp butter", "the chopped".
    private static let closeFillers: Set<String> = [
        "the", "a", "an", "half", "of", "your", "remaining", "tsp", "tbsp", "cup", "cups", "oz", "clove",
        "cloves", "lb", "g",
    ]
    private static let citrus: Set<String> = ["lime", "lemon", "orange", "pineapple", "apple", "grapefruit"]

    static func prepWords(in clause: String) -> String? {
        // Words from the end of the clause back to the first one that isn't a prep verb, an
        // adverb for one, a filler, or a number.
        let words = clause.lowercased().split { !$0.isLetter && !$0.isNumber && !"½¼¾⅓⅔/⁄".contains($0) }
            .map(String.init)
        var run: [String] = []
        var wordBeforeRun: String?
        for word in words.reversed() {
            let isNumber = word.allSatisfy { $0.isNumber || "½¼¾⅓⅔/⁄".contains($0) }
            guard verbs[word] != nil || adverbs.contains(word) || fillers.contains(word) || isNumber else {
                wordBeforeRun = word
                break
            }
            run.insert(word, at: 0)
        }
        // "lime zest", "pineapple juice": a noun, not something done to the next ingredient.
        if let first = run.first(where: { verbs[$0] != nil }), ["zest", "juice"].contains(first),
            let noun = wordBeforeRun, citrus.contains(noun)
        {
            return nil
        }
        // A participle only counts right before the ingredient ("add diced butter"); a verb
        // counts anywhere in the run ("peel, core, and dice").
        var out: [String] = []
        var joinedByOr: Set<Int> = []
        for (i, word) in run.enumerated() {
            guard let done = verbs[word], word != "trim", word != "trimmed" else { continue }
            let isParticiple = done == word
            // Only amounts and articles may sit between it and the ingredient: in "butter has
            // melted and Worcestershire", melted isn't about the Worcestershire.
            if isParticiple,
                run[(i + 1)...].contains(where: { !closeFillers.contains($0) && !$0.allSatisfy(\.isNumber) })
            {
                continue
            }
            guard !out.contains(where: { $0.hasSuffix(done) }) else { continue }
            if i > 0, run[i - 1] == "or" { joinedByOr.insert(out.count) }
            out.append(i > 0 && adverbs.contains(run[i - 1]) ? "\(run[i - 1]) \(done)" : done)
        }
        guard !out.isEmpty else { return nil }
        // "minced or grated", "peeled, cored, and diced".
        var text = out[0]
        for index in out.indices.dropFirst() {
            let isLast = index == out.count - 1
            let joiner = joinedByOr.contains(index) ? "or" : "and"
            if isLast {
                text += out.count > 2 ? ", \(joiner) \(out[index])" : " \(joiner) \(out[index])"
            } else {
                text += ", \(out[index])"
            }
        }
        return text
    }

    /// "greens" in "2 scallion greens".
    private static func leadingPart(_ text: String) -> String? {
        // Only a word right after the name ("scallion greens"), never one after a comma.
        let lower = text.lowercased()
        for part in ["whites", "greens"] where lower.hasPrefix(" \(part)") { return part }
        return nil
    }

    /// "separating whites from greens" → Scallion whites, Scallion greens.
    private static func splitParts(_ text: String, name: String) -> [String] {
        let clause = String(text.lowercased().prefix { $0 != "." && $0 != ";" })
        if clause.contains("white"), clause.contains("green"), clause.contains("separat") || clause.contains(" and ") {
            return ["\(singular(name)) whites", "\(singular(name)) greens"]
        }
        return []
    }

    private static func singular(_ name: String) -> String {
        name.hasSuffix("s") && !name.hasSuffix("ss") ? String(name.dropLast()) : name
    }
}
