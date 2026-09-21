import Foundation

/// Where a pantry item is kept (`PantryItem.storage`). Unknown values decode as-is, so a
/// build that predates a new place still shows the item rather than dropping it.
nonisolated struct PantryStorage: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// On a shelf. Items written before the freezer existed read as this.
    static let pantry = PantryStorage(rawValue: "pantry")
    /// Portioned, sealed and frozen, usually a bulk pack's remainder.
    static let freezer = PantryStorage(rawValue: "freezer")
}

/// A freezer item's extra facts: when it went in, how it was split, and the thaw estimate.
nonisolated struct PantryFrozen: Decodable, Hashable, Sendable {
    /// A calendar date, `YYYY-MM-DD`; `nil` for an item frozen before the date was recorded.
    let frozenOn: String?
    /// How many sealed portions the amount was split into; at least 1.
    let portions: Int
    let thaw: PantryThaw
}

/// How long one portion takes to thaw in the fridge.
nonisolated struct PantryThaw: Decodable, Hashable, Sendable {
    let hours: Int
    /// `false` when the amount isn't a weight and `hours` is the category default rather
    /// than a calculation. The app says "about" either way, but only promises a number
    /// it measured.
    let measured: Bool
    /// The weight the estimate used, in ounces; `nil` when unmeasured.
    let portionOunces: Double?
    /// Ready to show: "about 4 hours".
    let summary: String
}

/// The body of `POST .../pantry/freezer`: what was actually sealed, which is the remainder,
/// not the package size.
nonisolated struct FreezePantryItemRequest: Encodable, Equatable, Sendable {
    var ingredientID: String?
    var name: String?
    /// Exact, as produced by `PantryQuantity.parse`.
    var quantity: String
    var unit: String
    /// How many portions it was split into; 0 means one. It decides the thaw estimate.
    var portions: Int = 0
    var note: String?
    /// The handoff line the remainder came from, which makes freezing idempotent.
    var source: FreezeSource?

    private enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, quantity, unit, portions, note, source
    }

    /// The handoff line a bulk pack's remainder came from.
    nonisolated struct FreezeSource: Encodable, Equatable, Sendable {
        var provider: String
        var handoffID: String
        var lineID: String

        private enum CodingKeys: String, CodingKey {
            case provider
            case handoffID = "handoffId"
            case lineID = "lineId"
        }
    }
}

/// Response to `POST .../pantry/freezer`.
nonisolated struct FreezePantryItemResponse: Decodable, Equatable, Sendable {
    let item: PantryItem
    /// The same handoff line had already been sealed, and nothing changed.
    let alreadyFrozen: Bool
    let thaw: PantryThaw
}

// MARK: - Thaw reminders

/// Response to `GET /api/v1/households/{householdId}/thaw`: what today's meals need out of
/// the freezer.
nonisolated struct ThawDue: Decodable, Equatable, Sendable {
    /// Today, in the household's time zone.
    let date: String
    /// The local hour the household's thaw reminders go out.
    let reminderHour: Int
    let items: [ThawItem]

    var isEmpty: Bool { items.isEmpty }
}

/// One frozen item today's meals need.
nonisolated struct ThawItem: Decodable, Equatable, Sendable, Identifiable {
    let itemID: String
    let name: String
    /// The meals that need it, by name.
    let recipes: [String]
    /// The fridge thaw estimate for one portion.
    let hours: Int
    /// `false` when `hours` is the category default rather than a weight calculation.
    let measured: Bool
    /// The local time to put it in the fridge, `HH:mm`.
    let moveBy: String
    /// `moveBy` has already passed today, so the honest advice is "move it now".
    let overnight: Bool
    let summary: String

    var id: String { itemID }

    private enum CodingKeys: String, CodingKey {
        case itemID = "itemId"
        case name, recipes, hours, measured, moveBy, overnight, summary
    }
}

extension ThawItem {
    /// `moveBy` written the way a person reads a clock: "1 PM", "6:30 AM".
    var moveByText: String {
        let parser = DateFormatter()
        parser.locale = Locale(identifier: "en_US_POSIX")
        parser.dateFormat = "HH:mm"
        guard let time = parser.date(from: moveBy) else { return moveBy }
        let display = DateFormatter()
        display.timeStyle = .short
        display.dateStyle = .none
        return display.string(from: time)
    }
}
