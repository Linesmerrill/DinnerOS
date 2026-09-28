import Foundation

/// The Shop tab's lines arranged meal by meal, from what the API sent: the week's meals in plan
/// order, and each line's shares (how much of it each meal needs, and which component of the
/// meal it's for). Nothing is re-derived on the phone.
///
/// A line several meals use is still one purchase with one package count, so it's shown under
/// each of them but bought in one place: the first of those meals in plan order holds the
/// product and the stepper ("primary"), and the others say which meal it's bought with. That
/// keeps the Walmart hand-off and the package math exactly what the API computed — nothing is
/// counted twice.
nonisolated struct ShopMealLayout: Equatable, Sendable {
    /// What a row shows.
    enum Kind: Equatable, Sendable {
        /// A line with a product, still to send.
        case line(ShoppingHandoffLine)
        /// A line without a saved product yet.
        case needsProduct(ShoppingExcludedLine)
        /// A line whose saved product Walmart no longer lists, or that couldn't be confirmed:
        /// it needs re-choosing or leaving out before anything opens in Walmart.
        case needsDecision(ShoppingExcludedLine)
        /// Left out on purpose: struck through, with a way to put it back.
        case leftOut(ShoppingExcludedLine)
    }

    struct Row: Equatable, Sendable, Identifiable {
        let id: String
        let kind: Kind
        let ingredientKey: String
        let name: String
        /// This meal's part of the line; `nil` in the Other Items group.
        let share: GroceryShare?
        /// The product and the package stepper are shown on this row. False on the other meals
        /// that use the same purchase.
        let isPrimary: Bool
        /// The meal whose group holds the purchase, when this row isn't it.
        let boughtWith: String?
        /// The other meals that use it, for "Also in …".
        let alsoIn: [String]

        var isLeftOut: Bool {
            if case .leftOut = kind { return true }
            return false
        }

        /// The meal a row can be left out of ("just this dish"): its share's recipe, unless the
        /// share was made for several meals together or added as a pairing.
        var recipeID: String? {
            guard let share, !share.combined, !share.extra else { return nil }
            return share.recipeID
        }

        /// This meal's amount, falling back to the whole line's when the share has none.
        var quantityText: String {
            if let share, !share.quantityText.isEmpty { return share.quantityText }
            switch kind {
            case .line(let line): return line.quantityText
            case .needsProduct(let line), .needsDecision(let line), .leftOut(let line): return line.quantityText
            }
        }
    }

    /// A component of a meal — the store ingredients a specialty ingredient such as a crema
    /// became — clumped under its name so it can be left out as a whole.
    struct Component: Equatable, Sendable, Identifiable {
        let id: String
        let component: GroceryComponent
        let rows: [Row]

        var name: String { component.specialtyName }
        /// Everything in it is left out: the meal doesn't make it.
        var isLeftOut: Bool { !rows.isEmpty && rows.allSatisfy(\.isLeftOut) }
    }

    enum GroupKind: Equatable, Sendable {
        case meal(GroceryMeal)
        /// Items added for meals directly, such as accepted pairings.
        case addOns
        /// Lines the API didn't split by meal (an older server, or a line with no recipe).
        case other
    }

    struct Group: Equatable, Sendable, Identifiable {
        let id: String
        let kind: GroupKind
        let rows: [Row]
        let components: [Component]

        var title: String {
            switch kind {
            case .meal(let meal): meal.recipeName
            case .addOns: String(localized: "Add-Ons")
            case .other: String(localized: "Other Items")
            }
        }

        var meal: GroceryMeal? {
            if case .meal(let meal) = kind { return meal }
            return nil
        }
    }

    let groups: [Group]

    var isEmpty: Bool { groups.isEmpty }

    init(groups: [Group]) {
        self.groups = groups
    }

    /// Lays out what the Shop tab can act on per meal: lines still to send, lines that need a
    /// product, and what the household left out. Lines already in the Walmart cart and lines at
    /// home keep their own sections.
    init(proposal: ShoppingProposal) {
        let entries: [(kind: Kind, key: String, name: String, shares: [GroceryShare])] =
            proposal.linesToSend.map { (.line($0), $0.ingredientKey, $0.name, $0.shares) }
            + proposal.needsProduct.map { (.needsProduct($0), $0.ingredientKey, $0.name, $0.shares) }
            + proposal.needsDecision.map { (.needsDecision($0), $0.ingredientKey, $0.name, $0.shares) }
            + proposal.leftOut.map { (.leftOut($0), $0.ingredientKey, $0.name, $0.shares) }

        // Meals in plan order, then any recipe a share names that the plan list didn't.
        var meals = proposal.meals
        for entry in entries {
            for share in entry.shares where !share.extra && !meals.contains(where: { $0.recipeID == share.recipeID }) {
                meals.append(GroceryMeal(recipeID: share.recipeID, recipeName: share.recipeName))
            }
        }
        let mealOrder = Dictionary(
            meals.enumerated().map { ($1.recipeID, $0) }, uniquingKeysWith: { first, _ in first })
        let groupID = { (share: GroceryShare) in share.extra ? "addons" : "meal:\(share.recipeID)" }
        let groupOrder = { (share: GroceryShare) in share.extra ? meals.count : mealOrder[share.recipeID] ?? meals.count
        }
        let mealName = { (share: GroceryShare) in
            share.extra
                ? String(localized: "Add-Ons")
                : meals.first { $0.recipeID == share.recipeID }?.recipeName ?? share.recipeName
        }

        var plain: [String: [Row]] = [:]
        var components: [String: [String: (GroceryComponent, [Row])]] = [:]
        var componentOrder: [String: [String]] = [:]
        var other: [Row] = []

        for entry in entries {
            let kindTag: String
            switch entry.kind {
            case .line: kindTag = "line"
            case .needsProduct: kindTag = "needs"
            case .needsDecision: kindTag = "decide"
            case .leftOut(let line): kindTag = "left:\(line.skipScope?.rawValue ?? "")"
            }
            guard !entry.shares.isEmpty else {
                other.append(
                    Row(
                        id: "other|\(kindTag)|\(entry.key)", kind: entry.kind, ingredientKey: entry.key,
                        name: entry.name, share: nil, isPrimary: true, boughtWith: nil, alsoIn: []))
                continue
            }
            let ordered = entry.shares.enumerated().sorted {
                (groupOrder($0.element), $0.offset) < (groupOrder($1.element), $1.offset)
            }.map(\.element)
            let isPurchase: Bool = {
                if case .leftOut = entry.kind { return false }
                return true
            }()
            let primary = ordered.first
            for share in ordered {
                let group = groupID(share)
                let others = ordered.filter { groupID($0) != group }.map(mealName)
                let isPrimary = !isPurchase || share == primary
                let row = Row(
                    id: "\(group)|\(kindTag)|\(entry.key)|\(share.component?.specialtyID ?? "")",
                    kind: entry.kind, ingredientKey: entry.key, name: entry.name, share: share,
                    isPrimary: isPrimary,
                    boughtWith: isPrimary ? nil : primary.map(mealName),
                    alsoIn: Array(NSOrderedSet(array: others).compactMap { $0 as? String }))
                if let component = share.component {
                    if components[group]?[component.specialtyID] == nil {
                        componentOrder[group, default: []].append(component.specialtyID)
                        components[group, default: [:]][component.specialtyID] = (component, [])
                    }
                    components[group]?[component.specialtyID]?.1.append(row)
                } else {
                    plain[group, default: []].append(row)
                }
            }
        }

        var groups: [Group] = []
        for meal in meals {
            let id = "meal:\(meal.recipeID)"
            if let group = Self.group(
                id: id, kind: .meal(meal), plain: plain, components: components, order: componentOrder)
            {
                groups.append(group)
            }
        }
        if let addOns = Self.group(
            id: "addons", kind: .addOns, plain: plain, components: components, order: componentOrder)
        {
            groups.append(addOns)
        }
        if !other.isEmpty {
            groups.append(Group(id: "other", kind: .other, rows: other, components: []))
        }
        self.groups = groups
    }

    private static func group(
        id: String, kind: GroupKind, plain: [String: [Row]], components: [String: [String: (GroceryComponent, [Row])]],
        order: [String: [String]]
    ) -> Group? {
        let rows = plain[id] ?? []
        let parts = (order[id] ?? []).compactMap { specialtyID -> Component? in
            guard let (component, rows) = components[id]?[specialtyID] else { return nil }
            return Component(id: "\(id)|\(specialtyID)", component: component, rows: rows)
        }
        guard !rows.isEmpty || !parts.isEmpty else { return nil }
        return Group(id: id, kind: kind, rows: rows, components: parts)
    }
}

extension ShoppingExcludedLine {
    /// The skips holding a left-out line back, so Put Back resumes exactly those.
    func holdingSkips(in skips: [GrocerySkip]) -> [GrocerySkip] {
        GrocerySkipMatching.skips(
            scope: skipScope, keys: GrocerySkipMatching.keys(ingredientKey: ingredientKey, shares: shares),
            recipeIDs: Set(recipes.map(\.id)).union(shares.map(\.recipeID)), in: skips)
    }
}

/// The words under a line on the meal-by-meal Shop tab.
nonisolated enum ShopMealText {
    /// Under the purchase itself: this meal's own amount when other meals share the line, and
    /// which meals those are. `nil` for a line only this meal uses.
    static func note(for row: ShopMealLayout.Row) -> String? {
        guard !row.alsoIn.isEmpty else { return nil }
        let also = String(localized: "Also in \(row.alsoIn.formatted(.list(type: .and)))")
        guard let amount = row.share?.quantityText, !amount.isEmpty else { return also }
        return String(localized: "\(amount) for this meal · \(also)")
    }

    /// Under a meal that shares a line whose product needs re-choosing under another meal.
    static func rechooseUnder(_ row: ShopMealLayout.Row) -> String {
        let meal = row.boughtWith ?? String(localized: "another meal")
        return String(localized: "Re-choose its product under \(meal)")
    }

    /// Under a meal that shares a purchase listed with another meal: where it's bought, and the
    /// count when it has a product.
    static func boughtWith(_ row: ShopMealLayout.Row, packages: Int?) -> String {
        let meal = row.boughtWith ?? String(localized: "another meal")
        guard let packages else { return String(localized: "Choose its product under \(meal)") }
        return String(localized: "Bought with \(meal) · \(ShoppingText.packages(packages))")
    }
}
