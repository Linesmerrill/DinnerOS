import Foundation

/// Display text for Autopilot values.
nonisolated enum AutopilotFormat {
    // MARK: People and dates

    /// "You", the member's name, or a placeholder for someone no longer in the household.
    static func memberName(_ userID: String, members: [HouseholdMember]?, currentUserID: String?) -> String {
        if userID == currentUserID {
            return String(localized: "You")
        }
        guard let member = members?.first(where: { $0.userID == userID }) else {
            return String(localized: "A former member")
        }
        return member.name
    }

    /// For example "Changed by Ada Lovelace · Sep 14, 2026". `nil` for a section never saved.
    static func attribution(
        _ change: AutopilotSectionChange?, members: [HouseholdMember]?, currentUserID: String?,
        locale: Locale = .autoupdatingCurrent, timeZone: TimeZone = .autoupdatingCurrent
    ) -> String? {
        guard let change else { return nil }
        let name = memberName(change.updatedBy, members: members, currentUserID: currentUserID)
        let date = change.updatedAt.formatted(
            Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone))
        return String(localized: "Changed by \(name) · \(date)")
    }

    /// `YYYY-MM-DD` as "Sunday, Sep 20".
    static func dayTitle(date: String, day: PlanDay, locale: Locale = .autoupdatingCurrent) -> String {
        guard let parsed = try? Date(date, strategy: Date.ISO8601FormatStyle(timeZone: .gmt).year().month().day())
        else { return day.name(locale: locale) }
        return parsed.formatted(
            Date.FormatStyle(locale: locale, timeZone: .gmt).weekday(.wide).month(.abbreviated).day())
    }

    // MARK: Minutes

    /// "18 min", or "Time unknown".
    static func cookTime(_ minutes: Int?) -> String {
        guard let minutes, minutes > 0 else { return String(localized: "Time unknown") }
        return RecipeFormat.minutes(minutes)
    }

    /// "Up to 20 min", or `none` when there's no limit.
    static func limit(_ minutes: Int?, none: String) -> String {
        guard let minutes else { return none }
        return String(localized: "Up to \(RecipeFormat.minutes(minutes))")
    }

    // MARK: Week context

    /// A one-line summary such as "Busy week · Up to 20 min · 2 day changes"; `nil` when
    /// the week has nothing special.
    static func contextSummary(_ context: AutopilotWeekContext?) -> String? {
        guard let context, context.configured else { return nil }
        if context.skip {
            return String(localized: "Skipping this week")
        }
        var parts: [String] = []
        if context.busy {
            parts.append(String(localized: "Busy weeknights"))
        }
        if let maxMinutes = context.maxMinutes {
            parts.append(limit(maxMinutes, none: ""))
        }
        if let meals = context.mealsPerWeek {
            parts.append(meals == 1 ? String(localized: "1 meal") : String(localized: "\(meals) meals"))
        }
        if let servings = context.servings {
            parts.append(String(localized: "Serves \(servings)"))
        }
        let dayCount = context.days.filter { !$0.isEmpty }.count
        if dayCount == 1 {
            parts.append(String(localized: "1 day change"))
        } else if dayCount > 1 {
            parts.append(String(localized: "\(dayCount) day changes"))
        }
        if parts.isEmpty, !context.note.isEmpty {
            parts.append(context.note)
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    // MARK: Profile

    /// What a section is set to, for the preferences list.
    static func sectionSummary(
        _ section: AutopilotSection, settings: AutopilotSettings, vocabulary: AutopilotVocabulary?
    ) -> String {
        switch section {
        case .taste:
            let liked =
                settings.taste.likes.cuisines.count + settings.taste.likes.tags.count
                + settings.taste.likes.proteins.count
            let disliked =
                settings.taste.dislikes.cuisines.count + settings.taste.dislikes.tags.count
                + settings.taste.dislikes.proteins.count
            if liked == 0 && disliked == 0 {
                return String(localized: "No favorites yet")
            }
            return String(localized: "\(liked) liked · \(disliked) disliked")
        case .restrictions:
            let restrictions = settings.restrictions
            var parts = (restrictions.diets + restrictions.allergens).map {
                label($0, vocabulary?.diets ?? [], vocabulary?.allergens ?? [])
            }
            let excluded =
                restrictions.excludedIngredients.count + restrictions.excludedCuisines.count
                + restrictions.excludedProteins.count + restrictions.excludedTags.count
            if excluded > 0 {
                parts.append(String(localized: "\(excluded) excluded"))
            }
            if restrictions.noSpicy {
                parts.append(String(localized: "No spicy"))
            }
            return parts.isEmpty ? String(localized: "None") : parts.joined(separator: " · ")
        case .schedule:
            let schedule = settings.schedule
            let days = schedule.planDays.map { $0.name().prefix(3) }.joined(separator: ", ")
            return String(localized: "\(schedule.mealsPerWeek) meals · \(days)")
        case .cookTime:
            let cookTime = settings.cookTime
            let long =
                cookTime.maxLongPerWeek >= AutopilotCookTime.noLongLimit
                ? String(localized: "any long meals")
                : String(localized: "up to \(cookTime.maxLongPerWeek) long")
            return String(
                localized:
                    "Quick ≤ \(cookTime.quickMaxMinutes) min · Medium ≤ \(cookTime.mediumMaxMinutes) min · \(long)")
        case .equipment:
            guard !settings.equipment.isEmpty else { return String(localized: "None") }
            return settings.equipment.map { label($0, vocabulary?.equipment ?? []) }.joined(separator: ", ")
        case .weekdayRules:
            guard !settings.weekdayRules.isEmpty else { return String(localized: "No rules") }
            return settings.weekdayRules.map { ruleTitle($0) }.joined(separator: ", ")
        case .novelty:
            return label(settings.novelty.rawValue, vocabulary?.novelty ?? [])
        }
    }

    /// "Sunday: Smoker night", or the day alone when the rule has no label.
    static func ruleTitle(_ rule: AutopilotWeekdayRule, locale: Locale = .autoupdatingCurrent) -> String {
        let day = rule.day.name(locale: locale)
        let label = rule.label.trimmingCharacters(in: .whitespacesAndNewlines)
        return label.isEmpty || label == day ? day : "\(day): \(label)"
    }

    /// For example "Chicken or pork · Smoker · Long cook OK · Every week".
    static func ruleSummary(_ rule: AutopilotWeekdayRule, vocabulary: AutopilotVocabulary?) -> String {
        var parts: [String] = []
        let or = { (values: [String], options: [AutopilotOption]) -> String in
            values.map { label($0, options) }.formatted(.list(type: .or))
        }
        if !rule.proteins.isEmpty {
            parts.append(or(rule.proteins, vocabulary?.proteins ?? []))
        }
        if !rule.cuisines.isEmpty {
            parts.append(or(rule.cuisines, vocabulary?.cuisines ?? []))
        }
        if !rule.tags.isEmpty {
            parts.append(or(rule.tags, vocabulary?.tags ?? []))
        }
        if !rule.methods.isEmpty {
            parts.append(or(rule.methods, vocabulary?.equipment ?? []))
        }
        if let band = rule.timeBand {
            parts.append(timeBandLabel(band, vocabulary: vocabulary))
        }
        parts.append(frequencyLabel(rule.frequency, vocabulary: vocabulary))
        return parts.joined(separator: " · ")
    }

    static func timeBandLabel(_ band: AutopilotTimeBand, vocabulary: AutopilotVocabulary?) -> String {
        if let option = vocabulary?.timeBands.first(where: { $0.value == band.rawValue }) {
            return option.label
        }
        return band == .long ? String(localized: "Long cook OK") : band.title
    }

    static func frequencyLabel(_ frequency: AutopilotRuleFrequency, vocabulary: AutopilotVocabulary?) -> String {
        if let option = vocabulary?.frequencies.first(where: { $0.value == frequency.rawValue }) {
            return option.label
        }
        switch frequency {
        case .everyWeek: return String(localized: "Every week")
        case .atMostOnce: return String(localized: "At most once a week")
        default: return frequency.rawValue
        }
    }

    /// The first list's label for `value`, then the next list's, else the value capitalized.
    static func label(_ value: String, _ lists: [AutopilotOption]...) -> String {
        for options in lists {
            if let option = options.first(where: { $0.value == value }) {
                return option.label
            }
        }
        return value.capitalized
    }

    // MARK: Recipe methods

    /// "Automatic (Yes)" or "Automatic (No)", naming the heuristic's answer.
    static func methodSettingTitle(_ setting: AutopilotMethodSetting, heuristicSuits: Bool) -> String {
        switch setting {
        case .automatic:
            heuristicSuits ? String(localized: "Automatic (Yes)") : String(localized: "Automatic (No)")
        case .yes: String(localized: "Yes")
        case .no: String(localized: "No")
        }
    }

    // MARK: History

    /// A title for a history item, such as "Cook-Time Mix", "Week of Sep 14, 2026", or
    /// "Good for Smoker".
    static func historyTitle(
        _ item: AutopilotHistoryItem, vocabulary: AutopilotVocabulary?, recipeName: String?,
        locale: Locale = .autoupdatingCurrent
    ) -> String {
        switch item.type {
        case AutopilotHistoryItem.preferencesUpdated:
            let titles = (item.sections ?? []).map { AutopilotSection(rawValue: $0)?.title ?? $0 }
            return titles.isEmpty ? String(localized: "Preferences") : titles.formatted(.list(type: .and))
        case AutopilotHistoryItem.weekContextUpdated:
            guard let week = item.week.flatMap({ ISOWeek($0) }) else { return String(localized: "This Week's Plans") }
            return week.weekOf(locale: locale)
        case AutopilotHistoryItem.recipeOverrideUpdated:
            let method = label(item.method ?? "", vocabulary?.equipment ?? [])
            let title = String(localized: "Good for \(method)")
            return recipeName.map { "\(title): \($0)" } ?? title
        default:
            return String(localized: "Autopilot")
        }
    }

    /// One line per change, such as "Max long meals: 2 → 1" or "Liked cuisines: added Thai".
    static func historyLines(_ item: AutopilotHistoryItem, vocabulary: AutopilotVocabulary?) -> [String] {
        if item.cleared == true {
            return [String(localized: "Cleared")]
        }
        if item.type == AutopilotHistoryItem.recipeOverrideUpdated {
            let value = overrideValue(item.value)
            guard let previous = item.previous else { return [value] }
            return [String(localized: "\(overrideValue(previous)) → \(value)")]
        }
        return (item.changes ?? []).map { changeLine($0, vocabulary: vocabulary) }
    }

    static func changeLine(_ change: AutopilotFieldChange, vocabulary: AutopilotVocabulary?) -> String {
        let name = fieldName(change.field)
        var parts: [String] = []
        if let added = change.added, !added.isEmpty {
            let values = added.map { valueLabel($0, vocabulary: vocabulary) }.formatted(.list(type: .and))
            parts.append(String(localized: "added \(values)"))
        }
        if let removed = change.removed, !removed.isEmpty {
            let values = removed.map { valueLabel($0, vocabulary: vocabulary) }.formatted(.list(type: .and))
            parts.append(String(localized: "removed \(values)"))
        }
        if parts.isEmpty {
            let from = change.from.map { valueLabel($0, vocabulary: vocabulary) } ?? String(localized: "not set")
            let to = change.to.map { valueLabel($0, vocabulary: vocabulary) } ?? String(localized: "not set")
            parts.append("\(from) → \(to)")
        }
        return "\(name): " + parts.joined(separator: "; ")
    }

    private static func overrideValue(_ value: String?) -> String {
        switch value {
        case "yes": String(localized: "Yes")
        case "no": String(localized: "No")
        default: String(localized: "Automatic")
        }
    }

    private static func valueLabel(_ value: String, vocabulary: AutopilotVocabulary?) -> String {
        switch value {
        case "true": return String(localized: "On")
        case "false": return String(localized: "Off")
        default:
            break
        }
        if let day = PlanDay(rawValue: value) {
            return day.name()
        }
        guard let vocabulary else { return value }
        let lists = [
            vocabulary.cuisines, vocabulary.tags, vocabulary.proteins, vocabulary.diets, vocabulary.allergens,
            vocabulary.equipment, vocabulary.novelty, vocabulary.timeBands, vocabulary.frequencies,
        ]
        for options in lists {
            if let option = options.first(where: { $0.value == value }) {
                return option.label
            }
        }
        return value
    }

    /// A readable name for a dotted field path; unknown paths show as written.
    static func fieldName(_ field: String) -> String {
        if field.hasPrefix("weekdayRules."), let day = PlanDay(rawValue: String(field.dropFirst(13))) {
            return String(localized: "\(day.name()) rule")
        }
        if field.hasPrefix("days."), let day = PlanDay(rawValue: String(field.dropFirst(5))) {
            return day.name()
        }
        return fieldNames[field] ?? field
    }

    private static let fieldNames: [String: String] = [
        "taste.likes.cuisines": String(localized: "Liked cuisines"),
        "taste.likes.tags": String(localized: "Liked food types"),
        "taste.likes.proteins": String(localized: "Liked proteins"),
        "taste.dislikes.cuisines": String(localized: "Disliked cuisines"),
        "taste.dislikes.tags": String(localized: "Disliked food types"),
        "taste.dislikes.proteins": String(localized: "Disliked proteins"),
        "restrictions.diets": String(localized: "Diets"),
        "restrictions.allergens": String(localized: "Allergens"),
        "restrictions.excludedIngredients": String(localized: "Excluded ingredients"),
        "restrictions.excludedCuisines": String(localized: "Excluded cuisines"),
        "restrictions.excludedProteins": String(localized: "Excluded proteins"),
        "restrictions.excludedTags": String(localized: "Excluded food types"),
        "restrictions.noSpicy": String(localized: "No spicy"),
        "schedule.planDays": String(localized: "Days to plan"),
        "schedule.weeknights": String(localized: "Weeknights"),
        "schedule.mealsPerWeek": String(localized: "Meals per week"),
        "schedule.defaultServings": String(localized: "Servings"),
        "schedule.weeknightMaxMinutes": String(localized: "Weeknight time limit"),
        "cookTime.quickMaxMinutes": String(localized: "Quick limit"),
        "cookTime.mediumMaxMinutes": String(localized: "Medium limit"),
        "cookTime.maxLongPerWeek": String(localized: "Max long meals"),
        "cookTime.minQuickPerWeek": String(localized: "Min quick meals"),
        "cookTime.avoidConsecutiveLong": String(localized: "Avoid back-to-back long meals"),
        "novelty": String(localized: "Favorites or new"),
        "equipment": String(localized: "Equipment"),
        "skip": String(localized: "Skip the week"),
        "busy": String(localized: "Busy week"),
        "maxMinutes": String(localized: "Time limit"),
        "servings": String(localized: "Servings"),
        "mealsPerWeek": String(localized: "Meals this week"),
        "note": String(localized: "Note"),
    ]
}
