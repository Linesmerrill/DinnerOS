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
    /// The member picks the best-by date. Off, the server fills in the recommended one for where
    /// it's kept and when it was put away.
    var hasExpiry = false
    /// Used only while `hasExpiry` is on.
    var expiryDate: Date
    /// Where it's kept, and when it went there.
    var storage: PantryStorage = .pantry
    var storedOn: Date
    /// The member chose the storage, so a suggestion no longer moves it.
    var storageChosen = false
    /// The item had a date of its own when the form opened.
    private(set) var startedWithOwnDate = false
    var note = ""
    /// Other ingredients the item counts as ("Dried Thyme" for a jar of thyme).
    var alsoCountsAs: [String] = []
    /// Off when the item has its own low-stock threshold.
    var usesHouseholdThreshold = true
    /// The item's own threshold, as percent used; sent only while `usesHouseholdThreshold` is off.
    var lowThresholdPercent = PantrySettings.defaultLowThresholdPercent

    init(today: Date = .now, timeZone: TimeZone = .autoupdatingCurrent) {
        expiryDate = PantryDate.calendar(timeZone: timeZone).startOfDay(for: today)
        storedOn = expiryDate
    }

    init(item: PantryItem, today: Date = .now, timeZone: TimeZone = .autoupdatingCurrent) {
        self.init(today: today, timeZone: timeZone)
        status = item.status
        // An item that's out has no amount; an old one showing in a dimmed field read as broken.
        quantityText = item.status == .out ? "" : PantryQuantity.editingText(item.quantity)
        unit = item.unit ?? PantryUnit.defaultCode
        isStaple = item.isStaple
        storage = item.storage
        storageChosen = true
        if let on = item.storedOn ?? item.frozen?.frozenOn, let date = PantryDate.date(from: on, timeZone: timeZone) {
            storedOn = date
        }
        if let expiresOn = item.expiresOn, let date = PantryDate.date(from: expiresOn, timeZone: timeZone) {
            expiryDate = date
            // A date the server recommended (the item says when it was put away) reads as the
            // recommendation, not the member's own.
            hasExpiry = item.storedOn == nil
            startedWithOwnDate = hasExpiry
        }
        note = item.note
        alsoCountsAs = item.alsoCountsAs
        if let percent = item.lowThresholdPercent {
            usesHouseholdThreshold = false
            lowThresholdPercent = percent
        } else if let percent = item.estimate?.lowThresholdPercent {
            lowThresholdPercent = percent
        }
    }

    /// An item that's out has no amount; the API rejects one.
    var allowsAmount: Bool { status != .out }

    /// Sets the typed amount. Typing one into an item that's out means there's some again, so it
    /// becomes in stock rather than leaving a field that ignores what's typed.
    mutating func typeAmount(_ text: String) {
        quantityText = text
        if status == .out, !text.trimmingCharacters(in: .whitespaces).isEmpty {
            status = .inStock
        }
    }

    /// Sets the status. Out clears the amount, since an item that's out has none.
    mutating func setStatus(_ new: PantryStatus) {
        status = new
        if new == .out { quantityText = "" }
    }

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
            note: trimmedNote.isEmpty ? nil : trimmedNote,
            storage: storage,
            storedOn: storedOnString(timeZone: timeZone))
    }

    func storedOnString(timeZone: TimeZone = .autoupdatingCurrent) -> String {
        PantryDate.string(from: storedOn, timeZone: timeZone)
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
        let stored = storedOnString(timeZone: timeZone)
        let moved = storage != item.storage || stored != (item.storedOn ?? item.frozen?.frozenOn ?? stored)
        if hasExpiry {
            let expires = expiresOn(timeZone: timeZone)
            if expires != item.expiresOn {
                changes.expiresOn = expires ?? ""
            }
        } else if startedWithOwnDate {
            // Cleared, so the old date doesn't stay if no recommendation is found.
            changes.expiresOn = ""
        }
        // Moved, put away on another day, or handed back to the recommendation: the server
        // dates it for where it is now.
        if moved || (!hasExpiry && startedWithOwnDate) {
            changes.storage = storage
            changes.storedOn = stored
        }
        if trimmedNote != item.note {
            changes.note = trimmedNote
        }
        if Self.cleaned(alsoCountsAs) != item.alsoCountsAs {
            changes.alsoCountsAs = Self.cleaned(alsoCountsAs)
        }
        let percent = min(max(lowThresholdPercent, PantrySettings.thresholdRange.lowerBound), 100)
        switch (usesHouseholdThreshold, item.lowThresholdPercent) {
        case (true, .some):
            changes.lowThresholdPercent = .household
        case (false, let current) where current != percent:
            changes.lowThresholdPercent = .percent(percent)
        default:
            break
        }
        return changes
    }

    /// The names as the API keeps them: trimmed, no blanks, no repeats ignoring case.
    static func cleaned(_ names: [String]) -> [String] {
        var out: [String] = []
        for name in names {
            let trimmed = name.split(whereSeparator: \.isWhitespace).joined(separator: " ")
            if !trimmed.isEmpty, !out.contains(where: { $0.caseInsensitiveCompare(trimmed) == .orderedSame }) {
                out.append(trimmed)
            }
        }
        return out
    }

    private var trimmedNote: String {
        note.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func expiresOn(timeZone: TimeZone) -> String? {
        hasExpiry ? PantryDate.string(from: expiryDate, timeZone: timeZone) : nil
    }
}
