import Foundation

// MARK: - Values

/// A cook-time band (`AutopilotTimeBand`). Unknown values decode as-is.
nonisolated struct AutopilotTimeBand: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let quick = AutopilotTimeBand(rawValue: "quick")
    static let medium = AutopilotTimeBand(rawValue: "medium")
    static let long = AutopilotTimeBand(rawValue: "long")

    static let known: [AutopilotTimeBand] = [.quick, .medium, .long]

    var title: String {
        switch self {
        case .quick: String(localized: "Quick")
        case .medium: String(localized: "Medium")
        case .long: String(localized: "Long")
        default: rawValue.capitalized
        }
    }
}

/// How often a weekday rule applies (`AutopilotRuleFrequency`).
nonisolated struct AutopilotRuleFrequency: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let everyWeek = AutopilotRuleFrequency(rawValue: "every_week")
    static let atMostOnce = AutopilotRuleFrequency(rawValue: "at_most_once")
}

/// Appetite for new meals (`AutopilotNovelty`).
nonisolated struct AutopilotNovelty: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let favorites = AutopilotNovelty(rawValue: "favorites")
    static let balanced = AutopilotNovelty(rawValue: "balanced")
    static let adventurous = AutopilotNovelty(rawValue: "adventurous")
}

/// A proposal's status (`AutopilotProposalStatus`). Only `proposed` can change.
nonisolated struct AutopilotProposalStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let proposed = AutopilotProposalStatus(rawValue: "proposed")
    static let accepted = AutopilotProposalStatus(rawValue: "accepted")
    static let rejected = AutopilotProposalStatus(rawValue: "rejected")
}

/// One of the taste profile's sections (`AutopilotSection`), in the order screens show them.
nonisolated enum AutopilotSection: String, CaseIterable, Codable, Hashable, Sendable, Identifiable {
    case taste, restrictions, schedule, cookTime, equipment, weekdayRules, novelty, pairings

    var id: String { rawValue }

    /// The sections a full `PUT .../profile` sends. `pairings` is left out on purpose: the
    /// API **keeps** a household's pairing rules when a `PUT` omits them, so onboarding
    /// never deletes rules that were made from suggestions.
    static let replaceable: [AutopilotSection] = allCases.filter { $0 != .pairings }

    var title: String {
        switch self {
        case .taste: String(localized: "Taste")
        case .restrictions: String(localized: "Restrictions")
        case .schedule: String(localized: "Schedule")
        case .cookTime: String(localized: "Cook-Time Mix")
        case .equipment: String(localized: "Equipment")
        case .weekdayRules: String(localized: "Weekday Rules")
        case .novelty: String(localized: "Favorites or New")
        case .pairings: String(localized: "Pairings")
        }
    }

    var systemImage: String {
        switch self {
        case .taste: "heart"
        case .restrictions: "exclamationmark.shield"
        case .schedule: "calendar"
        case .cookTime: "timer"
        case .equipment: "frying.pan"
        case .weekdayRules: "calendar.day.timeline.left"
        case .novelty: "sparkles"
        case .pairings: "takeoutbag.and.cup.and.straw"
        }
    }

    /// One line under the preference row saying what the section decides.
    var detail: String {
        switch self {
        case .taste: String(localized: "Cuisines, food types, and proteins you like or don't.")
        case .restrictions: String(localized: "Allergens, diets, and ingredients to never suggest.")
        case .schedule: String(localized: "How many dinners, which nights, and servings.")
        case .cookTime: String(localized: "What counts as quick, and how many long cooks a week.")
        case .equipment: String(localized: "The smoker, grill, or air fryer you cook with.")
        case .weekdayRules: String(localized: "Habits for a day, like Taco Tuesday.")
        case .novelty: String(localized: "Stick to favorites, or mix in something new.")
        case .pairings: String(localized: "Add-ons that come with a meal, like garlic bread.")
        }
    }

    /// Whether first-run setup asks about this section. The rest keep the API's defaults
    /// until someone changes them here (#330).
    var isInSetup: Bool {
        AutopilotOnboardingStep.allCases.contains { $0.section == self }
    }
}

// MARK: - Profile sections

/// Liked or disliked values (`AutopilotChoices`). Cuisines and tags are free text.
nonisolated struct AutopilotChoices: Codable, Hashable, Sendable {
    var cuisines: [String] = []
    var tags: [String] = []
    var proteins: [String] = []
}

nonisolated struct AutopilotTaste: Codable, Hashable, Sendable {
    var likes = AutopilotChoices()
    var dislikes = AutopilotChoices()
}

/// Hard constraints (`AutopilotRestrictions`).
nonisolated struct AutopilotRestrictions: Codable, Hashable, Sendable {
    var diets: [String] = []
    var allergens: [String] = []
    var excludedIngredients: [String] = []
    var excludedCuisines: [String] = []
    var excludedProteins: [String] = []
    var excludedTags: [String] = []
    var noSpicy = false
}

/// Which days to plan and how many meals (`AutopilotSchedule`). Encodes `null` explicitly
/// so the whole section is always sent.
nonisolated struct AutopilotSchedule: Codable, Hashable, Sendable {
    var planDays: [PlanDay]
    var weeknights: [PlanDay]
    var mealsPerWeek: Int
    /// `nil` uses the household's default servings.
    var defaultServings: Int?
    /// A soft limit on weeknights, or `nil` for none.
    var weeknightMaxMinutes: Int?

    static let defaults = AutopilotSchedule(
        planDays: [.mon, .tue, .wed, .thu, .fri], weeknights: [.mon, .tue, .wed, .thu], mealsPerWeek: 4,
        defaultServings: nil, weeknightMaxMinutes: nil)

    private enum CodingKeys: String, CodingKey {
        case planDays, weeknights, mealsPerWeek, defaultServings, weeknightMaxMinutes
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(planDays, forKey: .planDays)
        try container.encode(weeknights, forKey: .weeknights)
        try container.encode(mealsPerWeek, forKey: .mealsPerWeek)
        try container.encodeNullable(defaultServings, forKey: .defaultServings)
        try container.encodeNullable(weeknightMaxMinutes, forKey: .weeknightMaxMinutes)
    }
}

/// The quick/medium/long mix (`AutopilotCookTime`).
nonisolated struct AutopilotCookTime: Codable, Hashable, Sendable {
    var quickMaxMinutes = 20
    var mediumMaxMinutes = 35
    /// `noLongLimit` means no limit.
    var maxLongPerWeek = 2
    var minQuickPerWeek = 0
    var avoidConsecutiveLong = true

    static let noLongLimit = 7
}

/// A soft preference for one day (`AutopilotWeekdayRule`), such as "Sunday smoker night".
nonisolated struct AutopilotWeekdayRule: Codable, Hashable, Sendable, Identifiable {
    var day: PlanDay
    /// Empty becomes the day's name on the server.
    var label = ""
    var cuisines: [String] = []
    var tags: [String] = []
    var proteins: [String] = []
    /// Each must be in the profile's equipment.
    var methods: [String] = []
    /// `nil` for no preference; `long` means a long cook is OK.
    var timeBand: AutopilotTimeBand?
    var frequency: AutopilotRuleFrequency = .everyWeek

    var id: PlanDay { day }

    /// The API needs at least one cuisine, tag, protein, method, or time band.
    var hasPreference: Bool {
        !cuisines.isEmpty || !tags.isEmpty || !proteins.isEmpty || !methods.isEmpty || timeBand != nil
    }

    private enum CodingKeys: String, CodingKey {
        case day, label, cuisines, tags, proteins, methods, timeBand, frequency
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(day, forKey: .day)
        try container.encode(label, forKey: .label)
        try container.encode(cuisines, forKey: .cuisines)
        try container.encode(tags, forKey: .tags)
        try container.encode(proteins, forKey: .proteins)
        try container.encode(methods, forKey: .methods)
        try container.encodeNullable(timeBand, forKey: .timeBand)
        try container.encode(frequency, forKey: .frequency)
    }
}

/// Every editable section of a taste profile: the body of `PUT .../autopilot/profile`.
/// The defaults match the API's.
nonisolated struct AutopilotSettings: Encodable, Hashable, Sendable {
    var taste = AutopilotTaste()
    var restrictions = AutopilotRestrictions()
    var schedule = AutopilotSchedule.defaults
    var cookTime = AutopilotCookTime()
    var novelty = AutopilotNovelty.balanced
    var equipment: [String] = []
    /// At most one per day, in week order.
    var weekdayRules: [AutopilotWeekdayRule] = []
    /// Add-on pairing rules ("pasta → Garlic Bread"), at most `limits.maxPairingRules`.
    var pairings: [PairingRule] = []

    static let defaults = AutopilotSettings()

    func rule(for day: PlanDay) -> AutopilotWeekdayRule? {
        weekdayRules.first { $0.day == day }
    }

    /// Replaces the day's rule, or removes it when `rule` is `nil`. Keeps week order.
    mutating func setRule(_ rule: AutopilotWeekdayRule?, for day: PlanDay) {
        weekdayRules.removeAll { $0.day == day }
        if var rule {
            rule.day = day
            weekdayRules.append(rule)
            weekdayRules.sort { $0.day.offset < $1.day.offset }
        }
    }

    /// A copy with `section` taken from `other`, for "skip this step".
    func replacing(_ section: AutopilotSection, from other: AutopilotSettings) -> AutopilotSettings {
        var copy = self
        switch section {
        case .taste: copy.taste = other.taste
        case .restrictions: copy.restrictions = other.restrictions
        case .schedule: copy.schedule = other.schedule
        case .cookTime: copy.cookTime = other.cookTime
        case .novelty: copy.novelty = other.novelty
        case .equipment: copy.equipment = other.equipment
        case .weekdayRules: copy.weekdayRules = other.weekdayRules
        case .pairings: copy.pairings = other.pairings
        }
        return copy
    }

    /// Sections whose values differ from `other`.
    func sectionsDiffering(from other: AutopilotSettings) -> Set<AutopilotSection> {
        Set(AutopilotSection.allCases.filter { replacing($0, from: other) != self })
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case taste, restrictions, schedule, cookTime, novelty, equipment, weekdayRules, pairings
    }

    /// The body of a full `PUT .../profile`, which never carries `pairings` (#311).
    func encode(to encoder: any Encoder) throws {
        try AutopilotProfileUpdate(settings: self, sections: Set(AutopilotSection.replaceable)).encode(to: encoder)
    }

    fileprivate func encode(
        _ section: AutopilotSection, to container: inout KeyedEncodingContainer<CodingKeys>
    ) throws {
        switch section {
        case .taste: try container.encode(taste, forKey: .taste)
        case .restrictions: try container.encode(restrictions, forKey: .restrictions)
        case .schedule: try container.encode(schedule, forKey: .schedule)
        case .cookTime: try container.encode(cookTime, forKey: .cookTime)
        case .novelty: try container.encode(novelty, forKey: .novelty)
        case .equipment: try container.encode(equipment, forKey: .equipment)
        case .weekdayRules: try container.encode(weekdayRules, forKey: .weekdayRules)
        case .pairings: try container.encode(pairings, forKey: .pairings)
        }
    }
}

/// Body of `PATCH .../autopilot/profile`: only `sections`, each sent whole.
nonisolated struct AutopilotProfileUpdate: Encodable, Hashable, Sendable {
    let settings: AutopilotSettings
    let sections: Set<AutopilotSection>

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: AutopilotSettings.CodingKeys.self)
        for section in AutopilotSection.allCases where sections.contains(section) {
            try settings.encode(section, to: &container)
        }
    }
}

// MARK: - Profile

/// Who last changed a section and when (`AutopilotChange`).
nonisolated struct AutopilotSectionChange: Decodable, Hashable, Sendable {
    let updatedBy: String
    let updatedAt: Date
}

/// Per-section attribution; `nil` for a section never saved.
nonisolated struct AutopilotSectionChanges: Decodable, Hashable, Sendable {
    var taste: AutopilotSectionChange?
    var restrictions: AutopilotSectionChange?
    var schedule: AutopilotSectionChange?
    var cookTime: AutopilotSectionChange?
    var novelty: AutopilotSectionChange?
    var equipment: AutopilotSectionChange?
    var weekdayRules: AutopilotSectionChange?
    var pairings: AutopilotSectionChange?

    subscript(section: AutopilotSection) -> AutopilotSectionChange? {
        switch section {
        case .taste: taste
        case .restrictions: restrictions
        case .schedule: schedule
        case .cookTime: cookTime
        case .novelty: novelty
        case .equipment: equipment
        case .weekdayRules: weekdayRules
        case .pairings: pairings
        }
    }
}

/// A household's taste profile (`AutopilotProfile`).
nonisolated struct AutopilotProfile: Decodable, Equatable, Sendable {
    struct Effective: Decodable, Hashable, Sendable {
        /// The schedule's servings, or the household's default.
        let defaultServings: Int
    }

    let householdID: String
    /// `false` until the household saves a profile; the values are then the defaults.
    let configured: Bool
    let taste: AutopilotTaste
    let restrictions: AutopilotRestrictions
    let schedule: AutopilotSchedule
    let cookTime: AutopilotCookTime
    let novelty: AutopilotNovelty
    let equipment: [String]
    let weekdayRules: [AutopilotWeekdayRule]
    /// Add-on pairing rules. Read leniently: a server without pairings sends none.
    var pairings: [PairingRule] = []
    let sections: AutopilotSectionChanges
    let effective: Effective
    let createdBy: String?
    let createdAt: Date?
    let updatedBy: String?
    let updatedAt: Date?

    var settings: AutopilotSettings {
        AutopilotSettings(
            taste: taste, restrictions: restrictions, schedule: schedule, cookTime: cookTime, novelty: novelty,
            equipment: equipment, weekdayRules: weekdayRules, pairings: pairings)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case householdID = "householdId"
        case configured, taste, restrictions, schedule, cookTime, novelty, equipment, weekdayRules, pairings,
            sections, effective, createdBy, createdAt, updatedBy, updatedAt
    }
}

/// `pairings` is additive, so it's read leniently. The decoder lives in an extension to keep
/// the memberwise initializer.
nonisolated extension AutopilotProfile {
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            householdID: try container.decode(String.self, forKey: .householdID),
            configured: try container.decode(Bool.self, forKey: .configured),
            taste: try container.decode(AutopilotTaste.self, forKey: .taste),
            restrictions: try container.decode(AutopilotRestrictions.self, forKey: .restrictions),
            schedule: try container.decode(AutopilotSchedule.self, forKey: .schedule),
            cookTime: try container.decode(AutopilotCookTime.self, forKey: .cookTime),
            novelty: try container.decode(AutopilotNovelty.self, forKey: .novelty),
            equipment: try container.decode([String].self, forKey: .equipment),
            weekdayRules: try container.decode([AutopilotWeekdayRule].self, forKey: .weekdayRules),
            pairings: container.decodeLossyArray(PairingRule.self, forKey: .pairings),
            sections: try container.decode(AutopilotSectionChanges.self, forKey: .sections),
            effective: try container.decode(Effective.self, forKey: .effective),
            createdBy: try container.decodeIfPresent(String.self, forKey: .createdBy),
            createdAt: try container.decodeIfPresent(Date.self, forKey: .createdAt),
            updatedBy: try container.decodeIfPresent(String.self, forKey: .updatedBy),
            updatedAt: try container.decodeIfPresent(Date.self, forKey: .updatedAt))
    }
}

// MARK: - Vocabulary

/// A choice with its label (`AutopilotOption`).
nonisolated struct AutopilotOption: Decodable, Hashable, Sendable, Identifiable {
    let value: String
    let label: String
    let description: String?
    /// Only on cuisines, tags, and proteins; `0` for starter values.
    let recipeCount: Int?

    var id: String { value }
}

/// The API's validation bounds (`AutopilotLimits`), enforced in the forms.
nonisolated struct AutopilotLimits: Decodable, Hashable, Sendable {
    var maxListValues = 30
    var maxExcludedIngredients = 50
    var maxValueLength = 40
    var maxIngredientLength = 60
    var maxRuleValues = 10
    var maxLabelLength = 40
    var maxNoteLength = 500
    var minCookMinutes = 5
    var maxCookMinutes = 480
    var maxServings = 12
    var maxPairingRules = 20
    var maxGroceryItemNameLength = 60
    var maxGroceryItemQuantity = 99

    /// The documented values, used until the vocabulary loads.
    static let defaults = AutopilotLimits()

    fileprivate enum CodingKeys: String, CodingKey {
        case maxListValues, maxExcludedIngredients, maxValueLength, maxIngredientLength, maxRuleValues,
            maxLabelLength, maxNoteLength, minCookMinutes, maxCookMinutes, maxServings, maxPairingRules,
            maxGroceryItemNameLength, maxGroceryItemQuantity
    }
}

/// Every limit is read leniently, falling back to the documented default.
///
/// Swift's synthesized `Decodable` ignores a property's default value and requires the key, so
/// a limit a newer build knows about but an older server doesn't send would fail the whole
/// vocabulary — and with it every preference screen.
nonisolated extension AutopilotLimits {
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let defaults = AutopilotLimits.defaults
        self.init(
            maxListValues: container.decodeLenientInt(forKey: .maxListValues) ?? defaults.maxListValues,
            maxExcludedIngredients: container.decodeLenientInt(forKey: .maxExcludedIngredients)
                ?? defaults.maxExcludedIngredients,
            maxValueLength: container.decodeLenientInt(forKey: .maxValueLength) ?? defaults.maxValueLength,
            maxIngredientLength: container.decodeLenientInt(forKey: .maxIngredientLength)
                ?? defaults.maxIngredientLength,
            maxRuleValues: container.decodeLenientInt(forKey: .maxRuleValues) ?? defaults.maxRuleValues,
            maxLabelLength: container.decodeLenientInt(forKey: .maxLabelLength) ?? defaults.maxLabelLength,
            maxNoteLength: container.decodeLenientInt(forKey: .maxNoteLength) ?? defaults.maxNoteLength,
            minCookMinutes: container.decodeLenientInt(forKey: .minCookMinutes) ?? defaults.minCookMinutes,
            maxCookMinutes: container.decodeLenientInt(forKey: .maxCookMinutes) ?? defaults.maxCookMinutes,
            maxServings: container.decodeLenientInt(forKey: .maxServings) ?? defaults.maxServings,
            maxPairingRules: container.decodeLenientInt(forKey: .maxPairingRules) ?? defaults.maxPairingRules,
            maxGroceryItemNameLength: container.decodeLenientInt(forKey: .maxGroceryItemNameLength)
                ?? defaults.maxGroceryItemNameLength,
            maxGroceryItemQuantity: container.decodeLenientInt(forKey: .maxGroceryItemQuantity)
                ?? defaults.maxGroceryItemQuantity)
    }
}

/// Choices for the onboarding and preference screens (`AutopilotVocabulary`).
nonisolated struct AutopilotVocabulary: Decodable, Equatable, Sendable {
    let cuisines: [AutopilotOption]
    let tags: [AutopilotOption]
    let proteins: [AutopilotOption]
    let diets: [AutopilotOption]
    let allergens: [AutopilotOption]
    let equipment: [AutopilotOption]
    let novelty: [AutopilotOption]
    let timeBands: [AutopilotOption]
    let frequencies: [AutopilotOption]
    let days: [AutopilotOption]
    /// The meal categories pairing rules match on, with how many main meals are in each.
    /// Read leniently: a server without pairings sends none, and the closed list stands in.
    var mealCategories: [AutopilotOption] = []
    var pairingFrequencies: [AutopilotOption] = []
    let catalogRecipeCount: Int
    let limits: AutopilotLimits

    /// The option's label, or the value itself when the list doesn't have it (a custom
    /// cuisine, or a value from a newer server).
    static func label(for value: String, in options: [AutopilotOption]) -> String {
        options.first { $0.value == value }?.label ?? value.capitalized
    }

    /// The meal categories to offer, from the vocabulary when it has them, else the closed
    /// list this build knows.
    var mealCategoryOptions: [AutopilotOption] {
        guard mealCategories.isEmpty else { return mealCategories }
        return MealCategory.known.map {
            AutopilotOption(value: $0.rawValue, label: $0.fallbackTitle, description: nil, recipeCount: nil)
        }
    }

    var pairingFrequencyOptions: [AutopilotOption] {
        guard pairingFrequencies.isEmpty else { return pairingFrequencies }
        return PairingFrequency.known.map {
            AutopilotOption(value: $0.rawValue, label: $0.title, description: nil, recipeCount: nil)
        }
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case cuisines, tags, proteins, diets, allergens, equipment, novelty, timeBands, frequencies, days,
            mealCategories, pairingFrequencies, catalogRecipeCount, limits
    }
}

/// The pairing lists are additive, so they're read leniently. The decoder lives in an
/// extension to keep the memberwise initializer.
nonisolated extension AutopilotVocabulary {
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            cuisines: try container.decode([AutopilotOption].self, forKey: .cuisines),
            tags: try container.decode([AutopilotOption].self, forKey: .tags),
            proteins: try container.decode([AutopilotOption].self, forKey: .proteins),
            diets: try container.decode([AutopilotOption].self, forKey: .diets),
            allergens: try container.decode([AutopilotOption].self, forKey: .allergens),
            equipment: try container.decode([AutopilotOption].self, forKey: .equipment),
            novelty: try container.decode([AutopilotOption].self, forKey: .novelty),
            timeBands: try container.decode([AutopilotOption].self, forKey: .timeBands),
            frequencies: try container.decode([AutopilotOption].self, forKey: .frequencies),
            days: try container.decode([AutopilotOption].self, forKey: .days),
            mealCategories: container.decodeLossyArray(AutopilotOption.self, forKey: .mealCategories),
            pairingFrequencies: container.decodeLossyArray(AutopilotOption.self, forKey: .pairingFrequencies),
            catalogRecipeCount: try container.decode(Int.self, forKey: .catalogRecipeCount),
            limits: try container.decode(AutopilotLimits.self, forKey: .limits))
    }
}

// MARK: - Week context

/// One day's overrides (`AutopilotDayOverride`).
nonisolated struct AutopilotDayOverride: Codable, Hashable, Sendable, Identifiable {
    var day: PlanDay
    var skip = false
    /// A hard cap for the day.
    var maxMinutes: Int?
    /// Guests: servings for the day.
    var servings: Int?

    var id: PlanDay { day }

    /// Nothing set; the API drops such days.
    var isEmpty: Bool { !skip && maxMinutes == nil && servings == nil }

    private enum CodingKeys: String, CodingKey {
        case day, skip, maxMinutes, servings
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(day, forKey: .day)
        try container.encode(skip, forKey: .skip)
        try container.encodeNullable(maxMinutes, forKey: .maxMinutes)
        try container.encodeNullable(servings, forKey: .servings)
    }
}

/// What's special about one ISO week (`AutopilotWeekContext`).
nonisolated struct AutopilotWeekContext: Decodable, Equatable, Sendable {
    let householdID: String
    let week: String
    let startDate: String
    let endDate: String
    /// `false` when the week has no saved context.
    let configured: Bool
    let skip: Bool
    let busy: Bool
    let mealsPerWeek: Int?
    let maxMinutes: Int?
    let servings: Int?
    let days: [AutopilotDayOverride]
    let note: String
    let updatedBy: String?
    let updatedAt: Date?

    var draft: AutopilotWeekContextDraft {
        AutopilotWeekContextDraft(
            skip: skip, busy: busy, mealsPerWeek: mealsPerWeek, maxMinutes: maxMinutes, servings: servings,
            days: days, note: note)
    }

    private enum CodingKeys: String, CodingKey {
        case householdID = "householdId"
        case week, startDate, endDate, configured, skip, busy, mealsPerWeek, maxMinutes, servings, days, note,
            updatedBy, updatedAt
    }
}

/// An editable week context: the body of `PUT .../weeks/{week}/context`. Every field is
/// sent, with `null` for unset numbers; days with nothing set are left out.
nonisolated struct AutopilotWeekContextDraft: Encodable, Hashable, Sendable {
    var skip = false
    var busy = false
    var mealsPerWeek: Int?
    var maxMinutes: Int?
    var servings: Int?
    var days: [AutopilotDayOverride] = []
    var note = ""

    /// Nothing set, so saving it means the same as clearing.
    var isEmpty: Bool {
        !skip && !busy && mealsPerWeek == nil && maxMinutes == nil && servings == nil
            && days.allSatisfy(\.isEmpty) && trimmedNote.isEmpty
    }

    var trimmedNote: String { note.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// The day's overrides; an empty override when it has none.
    subscript(day: PlanDay) -> AutopilotDayOverride {
        get { days.first { $0.day == day } ?? AutopilotDayOverride(day: day) }
        set {
            days.removeAll { $0.day == day }
            var override = newValue
            override.day = day
            days.append(override)
            days.sort { $0.day.offset < $1.day.offset }
        }
    }

    private enum CodingKeys: String, CodingKey {
        case skip, busy, mealsPerWeek, maxMinutes, servings, days, note
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(skip, forKey: .skip)
        try container.encode(busy, forKey: .busy)
        try container.encodeNullable(mealsPerWeek, forKey: .mealsPerWeek)
        try container.encodeNullable(maxMinutes, forKey: .maxMinutes)
        try container.encodeNullable(servings, forKey: .servings)
        try container.encode(
            days.filter { !$0.isEmpty }.sorted { $0.day.offset < $1.day.offset }, forKey: .days)
        try container.encode(trimmedNote, forKey: .note)
    }
}

// MARK: - Proposals

/// A code with display text (`AutopilotText`). Show `text`; branch only on `code`.
nonisolated struct AutopilotText: Decodable, Hashable, Sendable {
    let code: String
    let text: String
}

nonisolated struct AutopilotSlotRecipe: Decodable, Hashable, Sendable {
    let id: String
    let name: String
    let imageURLString: String?

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case id, name
        case imageURLString = "imageUrl"
    }
}

/// A proposed meal for one day (`AutopilotSlot`). Its `id` is its day code.
nonisolated struct AutopilotSlot: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let day: PlanDay
    /// `YYYY-MM-DD`.
    let date: String
    let recipe: AutopilotSlotRecipe
    let servings: Int
    /// `nil` when unknown.
    let cookMinutes: Int?
    let timeBand: AutopilotTimeBand
    let score: Double
    let signals: [String: Double]
    /// At most three, most important first.
    let reasons: [AutopilotText]
    let swapCount: Int
    /// Add-ons and grocery items to offer with this meal. Ones with `included` are added when
    /// the proposal is accepted unless the member takes them out.
    var pairings: [ProposalPairing] = []

    /// The reasons joined for display, for example "Sunday smoker night · Pork · Long cook OK".
    var reasonText: String {
        reasons.map(\.text).filter { !$0.isEmpty }.joined(separator: " · ")
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, day, date, recipe, servings, cookMinutes, timeBand, score, signals, reasons, swapCount, pairings
    }
}

/// `pairings` is additive and read leniently, so a pairing this build can't read costs one
/// toggle rather than the whole proposal.
nonisolated extension AutopilotSlot {
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            day: try container.decode(PlanDay.self, forKey: .day),
            date: try container.decode(String.self, forKey: .date),
            recipe: try container.decode(AutopilotSlotRecipe.self, forKey: .recipe),
            servings: try container.decode(Int.self, forKey: .servings),
            cookMinutes: try container.decodeIfPresent(Int.self, forKey: .cookMinutes),
            timeBand: try container.decode(AutopilotTimeBand.self, forKey: .timeBand),
            score: try container.decode(Double.self, forKey: .score),
            signals: try container.decode([String: Double].self, forKey: .signals),
            reasons: try container.decode([AutopilotText].self, forKey: .reasons),
            swapCount: try container.decode(Int.self, forKey: .swapCount),
            pairings: container.decodeLossyArray(ProposalPairing.self, forKey: .pairings))
    }
}

/// A day the proposal wanted to plan but couldn't (`AutopilotUnfilled`).
nonisolated struct AutopilotUnfilled: Decodable, Hashable, Sendable, Identifiable {
    let day: PlanDay
    let date: String
    let code: String
    let text: String

    var id: PlanDay { day }
}

/// A proposed week (`AutopilotProposal`), kept separate from the plan until accepted.
nonisolated struct AutopilotProposal: Decodable, Equatable, Sendable, Identifiable {
    /// A slot or an unfilled day, for showing the week in day order.
    enum Row: Hashable, Sendable, Identifiable {
        case slot(AutopilotSlot)
        case unfilled(AutopilotUnfilled)

        var day: PlanDay {
            switch self {
            case .slot(let slot): slot.day
            case .unfilled(let unfilled): unfilled.day
            }
        }

        var id: String { day.rawValue }
    }

    let id: String
    let householdID: String
    let week: String
    let startDate: String
    let endDate: String
    let status: AutopilotProposalStatus
    /// Send with every swap, accept, and reject.
    let version: Int
    let attempt: Int
    let modelVersion: String
    let requestedMeals: Int
    let plannedMeals: Int
    let candidateCount: Int
    let coldStart: Bool
    let slots: [AutopilotSlot]
    let unfilled: [AutopilotUnfilled]
    let messages: [AutopilotText]
    let swapCount: Int
    let excludedSlotIDs: [String]
    let generatedBy: String
    let generatedAt: Date
    let updatedAt: Date
    let decidedBy: String?
    let decidedAt: Date?

    var isPending: Bool { status == .proposed }

    var rows: [Row] {
        (slots.map(Row.slot) + unfilled.map(Row.unfilled)).sorted { $0.day.offset < $1.day.offset }
    }

    func slot(id: String) -> AutopilotSlot? {
        slots.first { $0.id == id }
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case week, startDate, endDate, status, version, attempt, modelVersion, requestedMeals, plannedMeals,
            candidateCount, coldStart, slots, unfilled, messages, swapCount
        case excludedSlotIDs = "excludedSlotIds"
        case generatedBy, generatedAt, updatedAt, decidedBy, decidedAt
    }
}

/// A meal accepting didn't add (`AutopilotSkippedSlot`).
nonisolated struct AutopilotSkippedSlot: Decodable, Hashable, Sendable, Identifiable {
    let slotID: String
    let day: PlanDay
    /// `dayTaken` or `alreadyPlanned`.
    let reason: String

    var id: String { slotID }

    var explanation: String {
        switch reason {
        case "dayTaken": String(localized: "\(day.name()) already has a meal.")
        case "alreadyPlanned": String(localized: "\(day.name())'s recipe is already planned this week.")
        default: String(localized: "\(day.name())'s meal couldn't be added.")
        }
    }

    private enum CodingKeys: String, CodingKey {
        case slotID = "slotId"
        case day, reason
    }
}

/// Response to `POST .../proposal/accept` (`AutopilotAcceptResult`).
nonisolated struct AutopilotAcceptResult: Decodable, Equatable, Sendable {
    let proposal: AutopilotProposal
    let plan: Plan
    /// The new entries, with origin `autopilot`.
    let added: [PlanEntry]
    let skipped: [AutopilotSkippedSlot]
    /// The pairings added with the meals: add-on plan entries and grocery items.
    var pairingsAdded: [AddedPairing] = []
    /// Chosen pairings that weren't added.
    var pairingsSkipped: [SkippedPairing] = []

    fileprivate enum CodingKeys: String, CodingKey {
        case proposal, plan, added, skipped, pairingsAdded, pairingsSkipped
    }
}

/// The pairing lists are additive, so they're read leniently.
nonisolated extension AutopilotAcceptResult {
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            proposal: try container.decode(AutopilotProposal.self, forKey: .proposal),
            plan: try container.decode(Plan.self, forKey: .plan),
            added: try container.decode([PlanEntry].self, forKey: .added),
            skipped: try container.decode([AutopilotSkippedSlot].self, forKey: .skipped),
            pairingsAdded: container.decodeLossyArray(AddedPairing.self, forKey: .pairingsAdded),
            pairingsSkipped: container.decodeLossyArray(SkippedPairing.self, forKey: .pairingsSkipped))
    }
}

nonisolated struct AutopilotGenerateRequest: Encodable, Hashable, Sendable {
    /// `nil` leaves the API's default (avoid the replaced proposal's meals).
    var avoidPrevious: Bool?
}

nonisolated struct AutopilotVersionRequest: Encodable, Hashable, Sendable {
    let version: Int
}

nonisolated struct AutopilotAcceptRequest: Encodable, Hashable, Sendable {
    let version: Int
    let excludeSlotIDs: [String]
    /// The slots' pairings to add, by `ProposalPairing.id`. `nil` leaves the field out, which
    /// means the ones the proposal includes; an empty array adds none.
    var pairingIDs: [String]?

    private enum CodingKeys: String, CodingKey {
        case version
        case excludeSlotIDs = "excludeSlotIds"
        case pairingIDs = "pairingIds"
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(version, forKey: .version)
        try container.encode(excludeSlotIDs, forKey: .excludeSlotIDs)
        try container.encodeIfPresent(pairingIDs, forKey: .pairingIDs)
    }
}

// MARK: - Recipe attributes

/// Whether a recipe suits one cooking method (`AutopilotMethod`).
nonisolated struct AutopilotMethod: Decodable, Hashable, Sendable, Identifiable {
    let method: String
    let label: String
    /// What Autopilot uses, after the household's override.
    let suits: Bool
    /// `heuristic` or `override`.
    let source: String
    let heuristicSuits: Bool
    /// What the heuristic matched; `nil` when nothing did.
    let evidence: String?

    var id: String { method }

    var setting: AutopilotMethodSetting {
        guard source == "override" else { return .automatic }
        return suits ? .yes : .no
    }
}

/// A household's answer for a method: automatic (the heuristic), yes, or no.
nonisolated enum AutopilotMethodSetting: String, CaseIterable, Hashable, Sendable, Identifiable {
    case automatic, yes, no

    var id: String { rawValue }

    /// The override to send; `nil` returns the method to automatic.
    var overrideValue: Bool? {
        switch self {
        case .automatic: nil
        case .yes: true
        case .no: false
        }
    }
}

nonisolated struct AutopilotRecipeOverride: Decodable, Hashable, Sendable {
    let recipeID: String
    let methods: [String: Bool]
    let updatedBy: String
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case methods, updatedBy, updatedAt
    }
}

/// What Autopilot derives from a recipe (`AutopilotRecipeAttributes`).
nonisolated struct AutopilotRecipeAttributes: Decodable, Equatable, Sendable {
    let recipeID: String
    let cookMinutes: Int?
    let timeBand: AutopilotTimeBand
    /// Canonical and lowercase, such as `north american`.
    let cuisines: [String]
    /// Broader regions of `cuisines`, nearest first. Likes, exclusions, and rules match them too.
    /// `nil` from a server older than canonical cuisines.
    let cuisineRegions: [String]?
    let tags: [String]
    let proteins: [String]
    let allergens: [String]
    let diets: [String]
    let spicy: Bool
    let spicyEvidence: String?
    /// Every equipment value, whether or not the household has it.
    let methods: [AutopilotMethod]
    let override: AutopilotRecipeOverride?

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case cookMinutes, timeBand, cuisines, cuisineRegions, tags, proteins, allergens, diets, spicy, spicyEvidence,
            methods, override
    }
}

/// Body of `PUT .../recipes/{recipeId}/override`. `nil` clears a method's override.
nonisolated struct AutopilotOverrideRequest: Encodable, Hashable, Sendable {
    let methods: [String: Bool?]

    private enum CodingKeys: String, CodingKey {
        case methods
    }

    private struct MethodKey: CodingKey {
        let stringValue: String
        var intValue: Int? { nil }

        init(stringValue: String) {
            self.stringValue = stringValue
        }

        init?(intValue: Int) {
            nil
        }
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        var nested = container.nestedContainer(keyedBy: MethodKey.self, forKey: .methods)
        for (method, value) in methods.sorted(by: { $0.key < $1.key }) {
            try nested.encodeNullable(value, forKey: MethodKey(stringValue: method))
        }
    }
}

// MARK: - History

/// How one field changed (`AutopilotFieldChange`).
nonisolated struct AutopilotFieldChange: Decodable, Hashable, Sendable {
    /// A dotted path such as `schedule.mealsPerWeek`.
    let field: String
    let added: [String]?
    let removed: [String]?
    let from: String?
    let to: String?
}

/// One preference change (`AutopilotHistoryItem`).
nonisolated struct AutopilotHistoryItem: Decodable, Hashable, Sendable {
    static let preferencesUpdated = "autopilot.preferences_updated"
    static let weekContextUpdated = "autopilot.week_context_updated"
    static let recipeOverrideUpdated = "autopilot.recipe_override_updated"

    let type: String
    let userID: String
    let occurredAt: Date
    let week: String?
    let recipeID: String?
    let sections: [String]?
    let changes: [AutopilotFieldChange]?
    let cleared: Bool?
    let method: String?
    /// `auto`, `yes`, or `no`.
    let value: String?
    let previous: String?

    private enum CodingKeys: String, CodingKey {
        case type
        case userID = "userId"
        case occurredAt, week
        case recipeID = "recipeId"
        case sections, changes, cleared, method, value, previous
    }
}

nonisolated struct AutopilotHistory: Decodable, Equatable, Sendable {
    let items: [AutopilotHistoryItem]
}

// MARK: - Errors

/// The Autopilot `409` codes the app reacts to.
nonisolated enum AutopilotConflict: String, Sendable {
    case proposalChanged = "proposal_changed"
    case proposalNotPending = "proposal_not_pending"
    case planFinalized = "plan_finalized"
    case planFull = "plan_full"
    case noAlternative = "no_alternative"
    case nothingToAccept = "nothing_to_accept"
    case proposalStale = "proposal_stale"
    case conflict

    init?(_ error: any Error) {
        guard let apiError = error as? APIError, apiError.status == 409, let code = apiError.code else { return nil }
        self.init(rawValue: code)
    }
}

// MARK: - Encoding

nonisolated extension KeyedEncodingContainer {
    /// Encodes `value`, or an explicit `null` when it's `nil`.
    mutating func encodeNullable<Value: Encodable>(_ value: Value?, forKey key: Key) throws {
        if let value {
            try encode(value, forKey: key)
        } else {
            try encodeNil(forKey: key)
        }
    }
}
