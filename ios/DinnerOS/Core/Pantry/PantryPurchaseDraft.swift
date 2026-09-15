import Foundation

/// The editable amount of a purchase: a grocery line confirmed with "Add to pantry?", or a
/// restock from the Pantry tab.
///
/// The draft keeps one `clientPurchaseID` for its lifetime, so trying again after a failure
/// can never record the purchase twice.
nonisolated struct PantryPurchaseDraft: Equatable, Sendable {
    /// Units that count things rather than measure them (`KindDiscrete` in the API). Only
    /// these can have a size.
    static let discreteUnits: Set<String> = ["count", "clove", "can", "package", "slice", "bunch", "pinch", "thumb"]
    /// Volume and weight units a size can be given in, smallest first.
    static let sizeUnits = ["tsp", "tbsp", "floz", "cup", "ml", "l", "oz", "lb", "g", "kg"]

    /// Typed text, parsed with `PantryQuantity.parse`. Empty means no amount.
    var quantityText = ""
    var unit = PantryUnit.defaultCode
    var hasUnitSize = false
    /// How much one `unit` holds, used only while `hasUnitSize` is on and `unit` is discrete.
    var unitSizeText = ""
    var unitSizeUnit = "oz"
    let clientPurchaseID: String

    init(clientPurchaseID: String = UUID().uuidString) {
        self.clientPurchaseID = clientPurchaseID
    }

    /// A restock of `item`, in the unit the item is tracked in, with its known size.
    init(item: PantryItem, clientPurchaseID: String = UUID().uuidString) {
        self.init(clientPurchaseID: clientPurchaseID)
        if let size = item.unitSize {
            unit = size.per
            hasUnitSize = true
            unitSizeText = PantryQuantity.editingText(size.quantity)
            unitSizeUnit = size.unit
        } else {
            unit = item.unit ?? PantryUnit.defaultCode
        }
    }

    /// A checked-off grocery line, prefilled with its first amount (or `amount`, when the
    /// member picked another of the line's amounts).
    init(groceryItem: GroceryItem, amount: GroceryAmount? = nil, clientPurchaseID: String = UUID().uuidString) {
        self.init(clientPurchaseID: clientPurchaseID)
        if let chosen = amount ?? groceryItem.amounts.first {
            use(chosen)
        }
    }

    /// Replaces the amount with one of a grocery line's amounts.
    mutating func use(_ amount: GroceryAmount) {
        quantityText = PantryQuantity.editingText(amount.quantity)
        unit = amount.unit.isEmpty ? PantryUnit.defaultCode : amount.unit
    }

    var isDiscreteUnit: Bool {
        Self.discreteUnits.contains(unit)
    }

    /// The exact quantity to send, or `nil` for no amount.
    func exactQuantity() throws(PantryQuantityError) -> String? {
        try PantryQuantity.parse(quantityText)
    }

    var quantityError: String? {
        do {
            _ = try exactQuantity()
            return nil
        } catch {
            return error.errorDescription
        }
    }

    /// Whether a size is sent: it's on, the unit is discrete, and there's an amount.
    private var sendsUnitSize: Bool {
        hasUnitSize && isDiscreteUnit && !unitSizeText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var unitSizeError: String? {
        guard sendsUnitSize else { return nil }
        if (try? exactQuantity()) == nil {
            return String(localized: "Enter how many you bought to give each one a size.")
        }
        do {
            _ = try PantryQuantity.parse(unitSizeText)
            return nil
        } catch {
            return error.errorDescription
        }
    }

    var isValid: Bool {
        quantityError == nil && unitSizeError == nil
    }

    /// A restock from the Pantry tab.
    func manualPurchase(itemID: String) throws(PantryQuantityError) -> NewPantryPurchase {
        var purchase = try base(source: .manual)
        purchase.itemID = itemID
        return purchase
    }

    /// A checked-off grocery line. A catalogued line is sent by ingredient ID; a line keyed
    /// `name:` is sent by name.
    func groceryPurchase(for item: GroceryItem, week: ISOWeek) throws(PantryQuantityError) -> NewPantryPurchase {
        var purchase = try base(source: .groceryList)
        if item.ingredientKey.hasPrefix("name:") || item.ingredientKey.isEmpty {
            let name = item.name.trimmingCharacters(in: .whitespacesAndNewlines)
            purchase.name = String(name.prefix(PantryItemDraft.maxNameLength))
        } else {
            purchase.ingredientID = item.ingredientKey
        }
        purchase.week = week.description
        return purchase
    }

    private func base(source: PantryPurchaseSource) throws(PantryQuantityError) -> NewPantryPurchase {
        let quantity = try exactQuantity()
        var purchase = NewPantryPurchase(
            source: source, quantity: quantity, unit: quantity == nil ? nil : unit, clientPurchaseID: clientPurchaseID)
        if quantity != nil, sendsUnitSize, let size = try PantryQuantity.parse(unitSizeText) {
            purchase.unitSize = PantryUnitSizeInput(quantity: size, unit: unitSizeUnit)
        }
        return purchase
    }
}
