import Foundation

/// The week's prep plan: the guided "you just got the groceries" checklist that turns a bulk
/// pack into portions (docs/shopping-providers.md#the-prep-plan).
///
/// Only each card's answer is stored by the API; the cards themselves are derived on read, so
/// the session is resumable and a household that never opens it loses nothing.
nonisolated struct PrepSession: Decodable, Equatable, Sendable {
    let week: String
    let headline: String
    let pending: Int
    let done: Int
    let skipped: Int
    let updatedAt: Date?
    let cards: [PrepCard]

    /// `nothing_to_prep` when the week bought nothing oversized, which is an ordinary
    /// outcome and not an empty screen.
    enum State: String, Sendable {
        case nothingToPrep = "nothing_to_prep"
        case ready
        case finished
    }

    var state: State { State(rawValue: stateRaw) ?? .nothingToPrep }
    /// True when there is a checklist worth opening.
    var hasWork: Bool { pending > 0 }
    var isEmpty: Bool { cards.isEmpty }

    private let stateRaw: String

    private enum CodingKeys: String, CodingKey {
        case week, headline, pending, done, skipped, updatedAt, cards
        case stateRaw = "state"
    }
}

/// One card's answer.
nonisolated enum PrepCardStatus: String, Sendable {
    case pending
    case done
    /// "Not this one." The card stays in the list and can still be finished later.
    case skipped
}

/// Which reminder the app may promise once a card is finished. There is no case for
/// "we'll remember": the only reminder the system has is the thaw sweep.
nonisolated enum PrepReminder: String, Sendable {
    /// Nothing is being frozen, so nothing is promised.
    case none
    /// A planned meal that needs it is still ahead, so the hourly sweep will remind the
    /// household that morning.
    case thaw
    /// Nothing is planned for it yet; the freezer keeps it on the list until something is.
    case list
}

/// One thing to prep — today always a bulk pack.
nonisolated struct PrepCard: Decodable, Equatable, Sendable, Identifiable {
    /// `"<handoffId>:<lineId>"`, the same reference the freezer records.
    let id: String
    let handoffID: String
    let lineID: String
    let name: String
    let category: String
    let productName: String
    /// The package size's unit; every amount on the card is in it.
    let unit: String
    let bought: String
    let boughtValue: Double
    let needed: String
    let neededValue: Double
    let surplus: String
    let surplusValue: Double
    let surplusPercent: Int
    /// Ready to show: "This week uses 10 oz of 64 oz".
    let surplusText: String
    let freezable: Bool
    /// The remainder is already in the freezer.
    let frozen: Bool
    /// The card's one line of guidance, ready to show.
    let instruction: String
    let reminderText: String
    /// The planned meals the reserved amount is for, in day order.
    let meals: [PrepMeal]
    /// Null for a card with nothing to freeze.
    let portions: PrepPortionPlan?
    let frozenItemID: String?
    /// The portion count actually recorded, or 0.
    let frozenPortions: Int
    let answeredAt: Date?
    /// Second meals to plan this week that use the surplus, best first.
    let suggestions: [PrepSuggestion]

    var status: PrepCardStatus { PrepCardStatus(rawValue: statusRaw) ?? .pending }
    var reminder: PrepReminder { PrepReminder(rawValue: reminderRaw) ?? .none }
    var isAnswered: Bool { status != .pending }

    private let statusRaw: String
    private let reminderRaw: String

    private enum CodingKeys: String, CodingKey {
        case id, name, category, unit, bought, boughtValue, needed, neededValue, surplus,
            surplusValue, surplusPercent, surplusText, freezable, frozen, instruction,
            reminderText, meals, portions, frozenPortions, answeredAt, suggestions
        case handoffID = "handoffId"
        case lineID = "lineId"
        case productName
        case frozenItemID = "frozenItemId"
        case statusRaw = "status"
        case reminderRaw = "reminder"
    }
}

/// One planned meal a card's ingredient was bought for.
nonisolated struct PrepMeal: Decodable, Equatable, Sendable, Identifiable {
    let recipeID: String
    let recipeName: String
    /// Null for a meal planned this week with no day yet.
    let day: String?
    let date: String?
    /// The meal's date has already gone by in the household's time zone.
    let past: Bool

    var id: String { recipeID + (day ?? "") + (date ?? "") }
    var planDay: PlanDay? { day.flatMap(PlanDay.init(rawValue:)) }

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case recipeName, day, date, past
    }
}

/// The card's portioning advice. One portion is one meal's worth: the week's need divided by
/// the meals that need it, with the surplus measured in those. The thaw estimate always
/// follows the chosen count, which is what the old "Freeze the Rest" button got wrong.
nonisolated struct PrepPortionPlan: Decodable, Equatable, Sendable {
    let unit: String
    /// What this week's meals need, and what therefore stays out of the freezer.
    let reserved: String
    let reservedValue: Double
    let reservedText: String
    let surplus: String
    let surplusValue: Double
    /// How many planned meals the reserve is for.
    let meals: Int
    let typicalMeal: String
    let typicalMealValue: Double
    let typicalMealText: String
    let portions: Int
    let portionSize: String
    let portionSizeValue: Double
    let portionSizeText: String
    let thaw: PantryThaw
    /// The counts the app offers, each with its own size and thaw estimate, so a stepper can
    /// never show a thaw time that belongs to another count.
    let options: [PrepPortionOption]

    /// `meal`: the week's own meals said how much a dinner takes. `week`: the line records no
    /// recipes, so the week's whole need stands in for one meal.
    enum Basis: String, Sendable {
        case meal
        case week
    }

    var basis: Basis { Basis(rawValue: basisRaw) ?? .week }

    /// The option for `count`, or the plan's own numbers when the API didn't offer it.
    func option(_ count: Int) -> PrepPortionOption? {
        options.first { $0.portions == count }
    }

    private let basisRaw: String

    private enum CodingKeys: String, CodingKey {
        case unit, reserved, reservedValue, reservedText, surplus, surplusValue, meals,
            typicalMeal, typicalMealValue, typicalMealText, portions, portionSize,
            portionSizeValue, portionSizeText, thaw, options
        case basisRaw = "basis"
    }
}

/// One choice of portion count, with the size and thaw time that follow from it.
nonisolated struct PrepPortionOption: Decodable, Equatable, Sendable, Identifiable {
    let portions: Int
    let size: String
    let sizeValue: Double
    let sizeText: String
    let thaw: PantryThaw

    var id: Int { portions }
}

/// A recipe Autopilot suggests planning later the same week so the surplus gets cooked.
nonisolated struct PrepSuggestion: Decodable, Equatable, Sendable, Identifiable {
    let recipeID: String
    let recipeName: String
    let imageURL: String?
    /// The open day it was ranked for, `"thu"`.
    let day: String
    let servings: Int
    let cookMinutes: Int?
    /// Autopilot's own reasons for the pick, most important first.
    let reasons: [String]

    var id: String { recipeID }

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case recipeName
        case imageURL = "imageUrl"
        case day, servings, cookMinutes, reasons
    }
}

/// Response to finishing or skipping a card: the card as it now stands, and the session
/// around it, so the app never has to reload to know what is left.
nonisolated struct PrepCardResult: Decodable, Equatable, Sendable {
    let card: PrepCard
    let session: PrepSession
}

/// Body of `POST .../prep/weeks/{week}/cards/{cardId}/done`.
nonisolated struct CompletePrepCardRequest: Encodable, Sendable {
    /// Overrides the suggested count; omit it to take the suggestion.
    let portions: Int?
}
