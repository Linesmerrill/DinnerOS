import Foundation

/// The editable fields of a pantry item, shared by the add and edit forms.
nonisolated struct PantryItemDraft: Equatable, Sendable {
    static let maxNameLength = 100
    static let maxNoteLength = 500

    var status: PantryStatus = .inStock
    /// Typed text, parsed with `PantryQuantity.parse`. Empty means no amount.
    var quantityText = ""
    var unit = PantryUnit.defaultCode
    var isStaple = false
    var hasExpiry = false
    /// Used only while `hasExpiry` is on.
    var expiryDate: Date
    var note = ""

    init(today: Date = .now, timeZone: TimeZone = .autoupdatingCurrent) {
        expiryDate = PantryDate.calendar(timeZone: timeZone).startOfDay(for: today)
    }

    init(item: PantryItem, today: Date = .now, timeZone: TimeZone = .autoupdatingCurrent) {
        self.init(today: today, timeZone: timeZone)
        status = item.status
        quantityText = PantryQuantity.editingText(item.quantity)
        unit = item.unit ?? PantryUnit.defaultCode
        isStaple = item.isStaple
        if let expiresOn = item.expiresOn, let date = PantryDate.date(from: expiresOn, timeZone: timeZone) {
            hasExpiry = true
            expiryDate = date
        }
        note = item.note
    }

    /// An item that's out has no amount; the API rejects one.
    var allowsAmount: Bool { status != .out }

    /// The exact quantity to send, or `nil` for no amount.
    func exactQuantity() throws(PantryQuantityError) -> String? {
        guard allowsAmount else { return nil }
        return try PantryQuantity.parse(quantityText)
    }

    var quantityError: String? {
        do {
            _ = try exactQuantity()
            return nil
        } catch {
            return error.errorDescription
        }
    }

    var noteError: String? {
        trimmedNote.count > Self.maxNoteLength
            ? String(localized: "A note can be at most \(Self.maxNoteLength) characters.") : nil
    }

    var isValid: Bool { quantityError == nil && noteError == nil }

    /// The add request. A catalog ingredient is sent by ID and keeps its catalog name. A
    /// staple flag is only sent when on, so adding an existing staple doesn't clear it.
    func newItem(
        name: String, ingredientID: String?, timeZone: TimeZone = .autoupdatingCurrent
    ) throws(PantryQuantityError) -> NewPantryItem {
        let quantity = try exactQuantity()
        return NewPantryItem(
            ingredientID: ingredientID,
            name: ingredientID == nil ? name.trimmingCharacters(in: .whitespacesAndNewlines) : nil,
            quantity: quantity,
            unit: quantity == nil ? nil : unit,
            status: status,
            isStaple: isStaple ? true : nil,
            expiresOn: expiresOn(timeZone: timeZone),
            note: trimmedNote.isEmpty ? nil : trimmedNote)
    }

    /// Only the fields that differ from `item`. The API clears the amount when an item
    /// becomes out, so the amount isn't sent then.
    func changes(
        from item: PantryItem, timeZone: TimeZone = .autoupdatingCurrent
    ) throws(PantryQuantityError) -> PantryItemChanges {
        var changes = PantryItemChanges()
        if status != item.status {
            changes.status = status
        }
        if allowsAmount {
            if let quantity = try exactQuantity() {
                if quantity != item.quantity || unit != item.unit {
                    changes.quantity = quantity
                    changes.unit = unit
                }
            } else if item.quantity != nil {
                changes.quantity = ""
            }
        }
        if isStaple != item.isStaple {
            changes.isStaple = isStaple
        }
        let expires = expiresOn(timeZone: timeZone)
        if expires != item.expiresOn {
            changes.expiresOn = expires ?? ""
        }
        if trimmedNote != item.note {
            changes.note = trimmedNote
        }
        return changes
    }

    private var trimmedNote: String {
        note.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func expiresOn(timeZone: TimeZone) -> String? {
        hasExpiry ? PantryDate.string(from: expiryDate, timeZone: timeZone) : nil
    }
}
