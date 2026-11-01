import Foundation

// "Your usuals" (decision 651): items a member adds to the week's list by hand, and the
// household's usuals worked out from them on the server. Nothing here touches a store account.

/// An item a member added to a week's list.
nonisolated struct AddedGroceryItem: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let week: String
    let ingredientKey: String
    let name: String
    let count: Int
    let fromUsuals: Bool
}

nonisolated struct AddedGroceryItemList: Decodable, Sendable {
    let items: [AddedGroceryItem]
}

/// One item to add, with how many.
nonisolated struct NewGroceryItem: Codable, Hashable, Sendable {
    var name: String
    var count: Int

    init(name: String, count: Int = 1) {
        self.name = name
        self.count = count
    }

    /// The name as it would be saved: spaces trimmed and runs of spaces joined.
    var cleanName: String {
        name.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// The most of one item the API takes.
    static let maxCount = 20
    /// The longest name the API takes.
    static let maxNameLength = 80
}

nonisolated struct AddGroceryItemsRequest: Codable, Hashable, Sendable {
    let items: [NewGroceryItem]
    let fromUsuals: Bool
}

/// One of the household's usuals.
nonisolated struct UsualItem: Decodable, Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let name: String
    /// The amount to offer.
    let count: Int
    let pinned: Bool
    /// How many of the recent shopping weeks it was added in.
    let weeks: Int
    /// The week's list already has it.
    let onList: Bool

    var id: String { ingredientKey }
}

/// An item marked "not a usual", or a pin.
nonisolated struct UsualPref: Decodable, Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let name: String
    let count: Int
    let pinned: Bool
    let notUsual: Bool

    var id: String { ingredientKey }
}

nonisolated struct UsualPrefEnvelope: Decodable, Sendable {
    let usual: UsualPref
}

/// A week's usuals.
nonisolated struct WeekUsuals: Decodable, Equatable, Sendable {
    let week: String
    let items: [UsualItem]
    /// Ask "Add your usuals?" this week.
    let prompt: Bool
    let shoppingWeeks: Int
    let window: Int
    let minWeeks: Int
    let notUsual: [UsualPref]

    /// What the prompt offers: usuals not on the list yet.
    var offered: [UsualItem] { items.filter { !$0.onList } }

    var shouldPrompt: Bool { prompt && !offered.isEmpty }

    /// How many shopping weeks "of your last …" names: the rule's window, or fewer while the
    /// household has fewer.
    var countedWeeks: Int { shoppingWeeks > 0 ? min(window, shoppingWeeks) : window }

    private enum CodingKeys: String, CodingKey {
        case week, items, prompt, shoppingWeeks, window, minWeeks, notUsual
    }

    init(
        week: String, items: [UsualItem], prompt: Bool, shoppingWeeks: Int = 0, window: Int = 6, minWeeks: Int = 3,
        notUsual: [UsualPref] = []
    ) {
        self.week = week
        self.items = items
        self.prompt = prompt
        self.shoppingWeeks = shoppingWeeks
        self.window = window
        self.minWeeks = minWeeks
        self.notUsual = notUsual
    }

    nonisolated init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        week = try c.decode(String.self, forKey: .week)
        items = c.decodeLossyArray(UsualItem.self, forKey: .items)
        prompt = (try? c.decodeIfPresent(Bool.self, forKey: .prompt)) ?? false
        shoppingWeeks = (try? c.decodeIfPresent(Int.self, forKey: .shoppingWeeks)) ?? 0
        window = (try? c.decodeIfPresent(Int.self, forKey: .window)) ?? 6
        minWeeks = (try? c.decodeIfPresent(Int.self, forKey: .minWeeks)) ?? 3
        notUsual = c.decodeLossyArray(UsualPref.self, forKey: .notUsual)
    }
}

/// Pins an item, marks it "not a usual", or with neither puts it back to being counted.
nonisolated struct SetUsualRequest: Encodable, Equatable, Sendable {
    var ingredientKey: String?
    var name: String
    var count: Int = 0
    var pinned = false
    var notUsual = false
}

/// An add made while the API couldn't be reached, sent once it can. Adds are safe to repeat:
/// adding an item the week has sets its count.
nonisolated struct PendingGroceryAdd: Codable, Hashable, Sendable, Identifiable {
    let id: UUID
    let householdID: String
    let week: String
    let request: AddGroceryItemsRequest
}

/// The plain words for usuals.
enum UsualsText {
    /// "Added 3 of your last 6 shopping weeks"
    static func why(_ item: UsualItem, window: Int) -> String {
        if item.pinned { return String(localized: "Pinned") }
        return String(localized: "On your list \(item.weeks) of the last \(window) weeks")
    }

    /// "Milk", or "Milk × 2"
    static func nameAndCount(_ name: String, count: Int) -> String {
        count > 1 ? "\(name) × \(count)" : name
    }
}
