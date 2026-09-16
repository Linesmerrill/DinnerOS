import Foundation

/// A list of taste choices: cuisines and tags are free text, proteins a fixed list.
nonisolated enum AutopilotChoiceKind: CaseIterable, Hashable, Sendable {
    case cuisine, tag, protein

    fileprivate var choices: WritableKeyPath<AutopilotChoices, [String]> {
        switch self {
        case .cuisine: \.cuisines
        case .tag: \.tags
        case .protein: \.proteins
        }
    }

    fileprivate var exclusions: WritableKeyPath<AutopilotRestrictions, [String]> {
        switch self {
        case .cuisine: \.excludedCuisines
        case .tag: \.excludedTags
        case .protein: \.excludedProteins
        }
    }

    /// Free-text values the member can type.
    var allowsCustomValues: Bool { self != .protein }
}

/// Whether the household likes a value.
nonisolated enum AutopilotTastePreference: Hashable, Sendable {
    case neutral, liked, disliked

    /// Tapping a chip cycles neutral → liked → disliked → neutral.
    var next: AutopilotTastePreference {
        switch self {
        case .neutral: .liked
        case .liked: .disliked
        case .disliked: .neutral
        }
    }
}

/// Input rules the API validates, applied as the member edits so a save doesn't fail.
nonisolated enum AutopilotInput {
    enum AddResult: Equatable, Sendable {
        case added, duplicate, empty, tooLong, full
    }

    /// Trimmed, lowercased, with runs of whitespace collapsed, the way the API stores free text.
    static func normalized(_ value: String) -> String {
        value.split(whereSeparator: \.isWhitespace).joined(separator: " ").lowercased()
    }

    /// Appends a normalized free-text value unless it's blank, too long, already there, or
    /// the list is full.
    @discardableResult
    static func add(_ raw: String, to list: inout [String], maxCount: Int, maxLength: Int) -> AddResult {
        let value = normalized(raw)
        guard !value.isEmpty else { return .empty }
        guard value.count <= maxLength else { return .tooLong }
        guard !list.contains(value) else { return .duplicate }
        guard list.count < maxCount else { return .full }
        list.append(value)
        return .added
    }

    /// Includes or removes a fixed value, keeping `order` (the vocabulary's); values the
    /// order doesn't know go last.
    static func set(_ value: String, included: Bool, in list: inout [String], order: [String]) {
        list.removeAll { $0 == value }
        guard included else { return }
        list.append(value)
        list.sort { (order.firstIndex(of: $0) ?? .max) < (order.firstIndex(of: $1) ?? .max) }
    }

    /// Days in week order, without duplicates.
    static func sortedDays(_ days: some Sequence<PlanDay>) -> [PlanDay] {
        Array(Set(days)).sorted { $0.offset < $1.offset }
    }
}

nonisolated extension AutopilotSettings {
    // MARK: Taste and exclusions

    func preference(for value: String, kind: AutopilotChoiceKind) -> AutopilotTastePreference {
        if taste.likes[keyPath: kind.choices].contains(value) { return .liked }
        if taste.dislikes[keyPath: kind.choices].contains(value) { return .disliked }
        return .neutral
    }

    /// Likes or dislikes a value. A value can't be both, and a liked value can't also be
    /// excluded, so the other lists lose it. Returns `false`, changing nothing, when the
    /// target list is at `limits.maxListValues`.
    @discardableResult
    mutating func setPreference(
        _ preference: AutopilotTastePreference, for value: String, kind: AutopilotChoiceKind, limits: AutopilotLimits
    ) -> Bool {
        let path = kind.choices
        switch preference {
        case .liked where !taste.likes[keyPath: path].contains(value):
            guard taste.likes[keyPath: path].count < limits.maxListValues else { return false }
        case .disliked where !taste.dislikes[keyPath: path].contains(value):
            guard taste.dislikes[keyPath: path].count < limits.maxListValues else { return false }
        default:
            break
        }
        taste.likes[keyPath: path].removeAll { $0 == value }
        taste.dislikes[keyPath: path].removeAll { $0 == value }
        switch preference {
        case .liked:
            taste.likes[keyPath: path].append(value)
            restrictions[keyPath: kind.exclusions].removeAll { $0 == value }
        case .disliked:
            taste.dislikes[keyPath: path].append(value)
        case .neutral:
            break
        }
        return true
    }

    func isExcluded(_ value: String, kind: AutopilotChoiceKind) -> Bool {
        restrictions[keyPath: kind.exclusions].contains(value)
    }

    /// Never suggest a value. Excluding removes it from likes. Returns `false` when the
    /// exclusion list is full.
    @discardableResult
    mutating func setExcluded(
        _ excluded: Bool, value: String, kind: AutopilotChoiceKind, limits: AutopilotLimits
    ) -> Bool {
        let path = kind.exclusions
        guard excluded else {
            restrictions[keyPath: path].removeAll { $0 == value }
            return true
        }
        guard !restrictions[keyPath: path].contains(value) else { return true }
        guard restrictions[keyPath: path].count < limits.maxListValues else { return false }
        restrictions[keyPath: path].append(value)
        taste.likes[keyPath: kind.choices].removeAll { $0 == value }
        return true
    }

    // MARK: Schedule

    /// Plans or stops planning a day. At least one day stays, and meals per week never
    /// exceed the plan days.
    mutating func setPlanDay(_ day: PlanDay, included: Bool) {
        var days = Set(schedule.planDays)
        if included {
            days.insert(day)
        } else if days.count > 1 {
            days.remove(day)
        }
        schedule.planDays = AutopilotInput.sortedDays(days)
        schedule.mealsPerWeek = min(max(schedule.mealsPerWeek, 1), schedule.planDays.count)
    }

    /// Plans or stops planning a night during setup, where the nights you cook *are* the
    /// dinners you want: meals per week follows the count rather than asking twice (#337).
    /// Preferences can separate the two again — plan six nights, ask for four dinners.
    mutating func setPlanNight(_ day: PlanDay, included: Bool) {
        setPlanDay(day, included: included)
        schedule.mealsPerWeek = schedule.planDays.count
    }

    mutating func setWeeknight(_ day: PlanDay, included: Bool) {
        var days = Set(schedule.weeknights)
        if included {
            days.insert(day)
        } else {
            days.remove(day)
        }
        schedule.weeknights = AutopilotInput.sortedDays(days)
    }

    // MARK: Equipment

    /// Adds or removes equipment. Weekday rules can only use equipment the household has,
    /// so removing it also removes the method from rules, and drops a rule left with no
    /// preference.
    mutating func setEquipment(_ method: String, owned: Bool, order: [String]) {
        AutopilotInput.set(method, included: owned, in: &equipment, order: order)
        guard !owned else { return }
        weekdayRules = weekdayRules.compactMap { rule in
            var rule = rule
            rule.methods.removeAll { $0 == method }
            return rule.hasPreference ? rule : nil
        }
    }

    // MARK: Validation

    /// Why the API would reject these settings, or `nil` when they're valid. The forms
    /// already prevent most of these.
    var validationMessage: String? {
        if schedule.planDays.isEmpty {
            return String(localized: "Choose at least one day to plan.")
        }
        if !(1...schedule.planDays.count).contains(schedule.mealsPerWeek) {
            return String(localized: "Meals per week can't be more than the days you plan.")
        }
        if cookTime.mediumMaxMinutes <= cookTime.quickMaxMinutes {
            return String(localized: "The medium limit must be longer than the quick limit.")
        }
        if let rule = weekdayRules.first(where: { !$0.hasPreference }) {
            return String(localized: "\(rule.day.name())'s rule needs at least one preference.")
        }
        if let rule = weekdayRules.first(where: { !Set($0.methods).isSubset(of: equipment) }) {
            return String(localized: "\(rule.day.name())'s rule uses equipment the household doesn't have.")
        }
        return nil
    }
}

nonisolated extension AutopilotWeekdayRule {
    /// "Smoker night: chicken or pork, long cook OK, every week". Needs a smoker.
    static func smokerNight(on day: PlanDay) -> AutopilotWeekdayRule {
        AutopilotWeekdayRule(
            day: day, label: String(localized: "Smoker night"), proteins: ["chicken", "pork"], methods: ["smoker"],
            timeBand: .long, frequency: .everyWeek)
    }

    /// "Taco night: Mexican, at most once a week".
    static func tacoNight(on day: PlanDay) -> AutopilotWeekdayRule {
        AutopilotWeekdayRule(
            day: day, label: String(localized: "Taco night"), cuisines: ["mexican"], frequency: .atMostOnce)
    }

    /// "Quick night: a quick meal, every week".
    static func quickNight(on day: PlanDay) -> AutopilotWeekdayRule {
        AutopilotWeekdayRule(day: day, label: String(localized: "Quick night"), timeBand: .quick, frequency: .everyWeek)
    }
}
