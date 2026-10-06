import Foundation
import Testing

@testable import DinnerOS

/// An item that's out takes an amount by coming back in stock: a dimmed field still showing the
/// old 2.62 oz, that ignored typing, read as broken.
struct PantryOutAmountTests {
    private let outOnionPowder = PantryFixtures.item(
        name: "Onion Powder", category: "spices", quantity: "2.62", quantityValue: 2.62, unit: "oz", status: .out)

    @Test func anOutItemOpensWithNoAmount() {
        let draft = PantryItemDraft(item: outOnionPowder)
        #expect(draft.status == .out)
        #expect(draft.quantityText.isEmpty)
    }

    @Test func typingAnAmountPutsItBackInStock() throws {
        var draft = PantryItemDraft(item: outOnionPowder)
        draft.typeAmount("2.5")
        #expect(draft.status == .inStock)
        let changes = try draft.changes(from: outOnionPowder)
        #expect(changes.status == .inStock)
        #expect(changes.quantity == "5/2")
        #expect(changes.unit == "oz")
    }

    @Test func clearingTheFieldDoesNotChangeTheStatus() {
        var draft = PantryItemDraft(item: outOnionPowder)
        draft.typeAmount("   ")
        #expect(draft.status == .out)
    }

    @Test func markingOutClearsTheAmount() {
        var draft = PantryItemDraft(item: PantryFixtures.item(quantity: "8", quantityValue: 8, unit: "oz"))
        draft.setStatus(.out)
        #expect(draft.quantityText.isEmpty)
        #expect(draft.isValid)
        draft.setStatus(.low)
        #expect(draft.status == .low)
    }
}
