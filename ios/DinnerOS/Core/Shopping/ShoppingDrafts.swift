import Foundation

/// The Choose Product form: a pasted link, the member's name for the product, and an
/// optional package size.
nonisolated struct SavedProductDraft: Equatable, Sendable {
    static let defaultUnit = "oz"

    var linkText = ""
    var displayName = ""
    var hasPackageSize = false
    var packageQuantityText = ""
    var packageUnit = SavedProductDraft.defaultUnit
    /// One package's price, as typed. Blank keeps whatever is stored.
    var priceText = ""
    /// The saved price the form started with; clearing it sends `null`.
    private(set) var originalPriceCents: Int?

    init() {}

    /// Starts the price field from a saved price. Does nothing once the member typed one.
    mutating func startPrice(_ cents: Int?) {
        guard priceText.isEmpty, originalPriceCents == nil, let cents else { return }
        originalPriceCents = cents
        priceText = MoneyText.editingText(cents)
    }

    var priceError: String? {
        MoneyText.error(priceText)
    }

    /// What Save does to the saved price; `nil` while the text isn't a price.
    var priceChange: FieldChange<Int>? {
        FieldChange.price(text: priceText, original: originalPriceCents)
    }

    /// A new product for an ingredient, named after the ingredient as the recipe lists it.
    /// Almost always that's what the household calls it; the member can still edit it.
    init(ingredientName: String) {
        displayName = ingredientName
    }

    /// Starts from a line's current product, to change it.
    init(product: ShoppingLineProduct) {
        self.init(url: product.productURLString, name: product.displayName, size: product.packageSize)
    }

    init(preference: ShoppingPreference) {
        self.init(url: preference.productURLString, name: preference.displayName, size: preference.packageSize)
        startPrice(preference.priceCents)
    }

    private init(url: String, name: String, size: ShoppingAmount?) {
        linkText = url
        displayName = name
        if let size {
            hasPackageSize = true
            packageQuantityText = PantryQuantity.editingText(size.quantity)
            packageUnit = size.unit
        }
    }

    /// The link or item ID found in `linkText`.
    var product: ShoppingProductReference? {
        ProductLink.reference(from: linkText)
    }

    /// The Walmart item the link names, for a confirmation under the field.
    var itemID: String? {
        switch product {
        case .itemID(let id): id
        case .url(let url): ProductLink.walmartItemID(inURL: url)
        case nil: nil
        }
    }

    var trimmedName: String {
        displayName.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The product name carried by the pasted link's slug, when it has one.
    var derivedName: String? {
        if case .url(let url) = product {
            return ProductLink.walmartProductName(inURL: url)
        }
        return nil
    }

    /// Fills the name in from the link when the member hasn't typed one. The API derives the
    /// same name (and the size) when it saves; doing it here shows the name in time to edit it.
    mutating func fillNameFromLink() {
        guard trimmedName.isEmpty, let derived = derivedName else { return }
        displayName = derived
    }

    var linkError: String? {
        guard !linkText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, product == nil else { return nil }
        return String(localized: "Paste a Walmart product link, such as walmart.com/ip/…, or the item number.")
    }

    var nameError: String? {
        trimmedName.count > ShoppingLimits.maxDisplayNameLength
            ? String(localized: "A product name can be at most \(ShoppingLimits.maxDisplayNameLength) characters.")
            : nil
    }

    var quantityError: String? {
        guard hasPackageSize else { return nil }
        do {
            _ = try PantryQuantity.parse(packageQuantityText)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    /// The package size to send, or `nil` when it's off or not a valid amount.
    private var packageSize: ShoppingPackageSizeInput? {
        guard hasPackageSize, let quantity = try? PantryQuantity.parse(packageQuantityText) else { return nil }
        return ShoppingPackageSizeInput(quantity: quantity, unit: packageUnit)
    }

    var isValid: Bool {
        product != nil && !trimmedName.isEmpty && nameError == nil && (!hasPackageSize || packageSize != nil)
            && priceChange != nil
    }

    /// The request body, or `nil` while the form isn't valid.
    func request(ingredientName: String?) -> ShoppingPreferenceRequest? {
        guard isValid, let product else { return nil }
        let name = String(
            (ingredientName ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                .prefix(ShoppingLimits.maxDisplayNameLength))
        return ShoppingPreferenceRequest(
            product: product, displayName: trimmedName, packageSize: packageSize,
            ingredientName: name.isEmpty ? nil : name, price: priceChange ?? .keep)
    }
}

/// "Did you order these?": which handed-off lines were ordered, and how many packages.
nonisolated struct OrderConfirmationDraft: Equatable, Sendable {
    let handoffID: String
    /// Lines not confirmed yet, in aisle order.
    let lines: [ShoppingHandoffLine]
    private(set) var selected: Set<String>
    private var packageCounts: [String: Int]
    /// Optional prices typed per line, all its packages together.
    private var priceTexts: [String: String] = [:]

    /// Every unconfirmed line starts checked, at the count sent to the cart.
    init(handoff: ShoppingHandoff) {
        handoffID = handoff.id
        lines = handoff.lines.filter { $0.confirmation?.status != .confirmed }
        selected = Set(lines.map(\.id))
        packageCounts = Dictionary(lines.map { ($0.id, $0.packages) }, uniquingKeysWith: { first, _ in first })
    }

    var selectedCount: Int { selected.count }

    func isSelected(_ line: ShoppingHandoffLine) -> Bool {
        selected.contains(line.id)
    }

    mutating func toggle(_ line: ShoppingHandoffLine) {
        if selected.contains(line.id) {
            selected.remove(line.id)
        } else {
            selected.insert(line.id)
        }
    }

    func packages(for line: ShoppingHandoffLine) -> Int {
        packageCounts[line.id] ?? line.packages
    }

    mutating func setPackages(_ count: Int, for line: ShoppingHandoffLine) {
        packageCounts[line.id] = min(max(count, ShoppingLimits.packages.lowerBound), ShoppingLimits.packages.upperBound)
    }

    func priceText(for line: ShoppingHandoffLine) -> String {
        priceTexts[line.id] ?? ""
    }

    mutating func setPriceText(_ text: String, for line: ShoppingHandoffLine) {
        priceTexts[line.id] = text
    }

    func priceError(for line: ShoppingHandoffLine) -> String? {
        MoneyText.error(priceText(for: line))
    }

    /// A checked line's price text isn't a price.
    var hasInvalidPrice: Bool {
        lines.contains { selected.contains($0.id) && priceError(for: $0) != nil }
    }

    private func priceCents(for line: ShoppingHandoffLine) -> Int? {
        let text = priceText(for: line)
        guard MoneyText.error(text) == nil else { return nil }
        return MoneyText.cents(from: text)
    }

    /// "Ordered everything": `{all: true}`, unless a count or price changed or a line was
    /// skipped before, which `all` wouldn't cover; then every line is named.
    var everythingRequest: ConfirmShoppingOrderRequest {
        let isChanged = lines.contains { packages(for: $0) != $0.packages || priceCents(for: $0) != nil }
        let hasSkipped = lines.contains { $0.confirmation?.status == .skipped }
        guard isChanged || hasSkipped else { return .all }
        return .lines(lines.map(orderLine), skipRest: true)
    }

    /// Only the checked lines; the rest are marked not ordered. `nil` when nothing is checked.
    var selectedRequest: ConfirmShoppingOrderRequest? {
        let chosen = lines.filter { selected.contains($0.id) }
        guard !chosen.isEmpty else { return nil }
        return .lines(chosen.map(orderLine), skipRest: true)
    }

    private func orderLine(_ line: ShoppingHandoffLine) -> ConfirmedOrderLine {
        let count = packages(for: line)
        return ConfirmedOrderLine(
            lineID: line.id, packages: count == line.packages ? nil : count, priceCents: priceCents(for: line))
    }

    /// Whether every line is checked.
    var isAllSelected: Bool { !lines.isEmpty && selected.count == lines.count }

    /// The one confirm button: "Ordered All 43", or "Ordered 42 of 43" when some are unchecked.
    var confirmTitle: String {
        isAllSelected
            ? String(localized: "Ordered All \(lines.count)")
            : String(localized: "Ordered \(selectedCount) of \(lines.count)")
    }

    /// What the confirm button sends: everything when all are checked, otherwise the checked
    /// lines with the rest marked not ordered. `nil` when nothing is checked.
    var confirmRequest: ConfirmShoppingOrderRequest? {
        guard !hasInvalidPrice else { return nil }
        return isAllSelected ? everythingRequest : selectedRequest
    }
}

/// "Did you order these?" grouped by meal, so each item reads as "this goes to the tacos".
///
/// Every line appears exactly once: under its meal when it's for one, in `shared` when it's for
/// several, and in `extras` when it's only for add-ons (garlic bread) or no recipe at all.
nonisolated struct OrderConfirmationGroups: Equatable, Sendable {
    nonisolated struct Meal: Equatable, Sendable, Identifiable {
        let recipeID: String
        let name: String
        let imageURL: URL?
        /// `nil` when the plan isn't loaded or the meal has no day.
        let day: PlanDay?
        let lines: [ShoppingHandoffLine]

        var id: String { recipeID }
    }

    nonisolated struct SharedLine: Equatable, Sendable, Identifiable {
        let line: ShoppingHandoffLine
        /// The meals it's for, in plan order.
        let mealNames: [String]

        var id: String { line.id }
    }

    /// In plan order: by day, then the order they were added; meals with nothing to confirm
    /// are left out.
    let meals: [Meal]
    let shared: [SharedLine]
    let extras: [ShoppingHandoffLine]

    /// Groups `lines` by the meals in `entries`, the handoff week's plan. Without a plan
    /// (another week is loaded) the meals come from the lines' own recipes, with no photo or
    /// day, in the order they first appear.
    init(lines: [ShoppingHandoffLine], entries: [PlanEntry]?) {
        struct MealInfo {
            let name: String
            let imageURL: URL?
            let day: PlanDay?
        }
        var order: [String] = []
        var info: [String: MealInfo] = [:]
        var addOns: Set<String> = []
        if let entries {
            let planned = entries.enumerated().sorted { a, b in
                let dayA = a.element.day?.offset ?? PlanDay.allCases.count
                let dayB = b.element.day?.offset ?? PlanDay.allCases.count
                return dayA != dayB ? dayA < dayB : a.offset < b.offset
            }
            for (_, entry) in planned {
                let recipe = entry.recipe
                if recipe.isAddon {
                    addOns.insert(recipe.id)
                    continue
                }
                guard info[recipe.id] == nil else { continue }
                order.append(recipe.id)
                info[recipe.id] = MealInfo(name: recipe.name, imageURL: recipe.imageURL, day: entry.day)
            }
        }
        var byMeal: [String: [ShoppingHandoffLine]] = [:]
        var sharedLines: [(line: ShoppingHandoffLine, mealIDs: Set<String>)] = []
        var extras: [ShoppingHandoffLine] = []
        for line in lines {
            let meals = line.recipes.filter { !addOns.contains($0.id) }
            // Without a plan, and for a recipe no longer planned, the line's own name stands in.
            for recipe in meals where info[recipe.id] == nil {
                order.append(recipe.id)
                info[recipe.id] = MealInfo(name: recipe.name, imageURL: nil, day: nil)
            }
            switch meals.count {
            case 0: extras.append(line)
            case 1: byMeal[meals[0].id, default: []].append(line)
            default: sharedLines.append((line, Set(meals.map(\.id))))
            }
        }
        self.meals = order.compactMap { id in
            guard let lines = byMeal[id], let meal = info[id] else { return nil }
            return Meal(recipeID: id, name: meal.name, imageURL: meal.imageURL, day: meal.day, lines: lines)
        }
        shared = sharedLines.map { shared in
            SharedLine(
                line: shared.line,
                mealNames: order.filter { shared.mealIDs.contains($0) }.compactMap { info[$0]?.name })
        }
        self.extras = extras
    }
}
