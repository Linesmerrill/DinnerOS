import SwiftUI

/// One profile section's controls, as `Form` sections. Onboarding steps and the preference
/// editors both show these.
struct AutopilotSectionForm: View {
    let section: AutopilotSection
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary
    /// The household's default servings, shown for "Household default".
    var householdServings: Int?

    var body: some View {
        switch section {
        case .taste:
            TasteSectionForm(settings: $settings, vocabulary: vocabulary)
        case .restrictions:
            RestrictionsSectionForm(settings: $settings, vocabulary: vocabulary)
        case .schedule:
            ScheduleSectionForm(settings: $settings, limits: vocabulary.limits, householdServings: householdServings)
        case .cookTime:
            CookTimeSectionForm(settings: $settings, limits: vocabulary.limits)
        case .equipment:
            EquipmentSectionForm(settings: $settings, vocabulary: vocabulary)
        case .weekdayRules:
            WeekdayRulesSectionForm(settings: $settings, vocabulary: vocabulary)
        case .novelty:
            NoveltySectionForm(settings: $settings, vocabulary: vocabulary)
        }
    }
}

// MARK: - Taste

private struct TasteSectionForm: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    var body: some View {
        Section {
            TasteChipGroup(options: vocabulary.cuisines, kind: .cuisine, settings: $settings, limits: vocabulary.limits)
        } header: {
            Text("Cuisines")
        } footer: {
            Text("Tap once to like, twice for “not for us,” and again to clear. Numbers are your recipes.")
        }
        Section("Food Types") {
            TasteChipGroup(options: vocabulary.tags, kind: .tag, settings: $settings, limits: vocabulary.limits)
        }
        Section("Proteins") {
            TasteChipGroup(options: vocabulary.proteins, kind: .protein, settings: $settings, limits: vocabulary.limits)
        }
    }
}

/// Chips that cycle neutral → liked → disliked, with custom values for free-text lists.
private struct TasteChipGroup: View {
    let options: [AutopilotOption]
    let kind: AutopilotChoiceKind
    @Binding var settings: AutopilotSettings
    let limits: AutopilotLimits

    @State private var showsAll = false
    @State private var customValue = ""
    @State private var message: String?

    private static let initialCount = 18

    private var values: [AutopilotOption] {
        let chosen = settings.taste.likes[kind] + settings.taste.dislikes[kind]
        let custom = chosen.filter { value in !options.contains { $0.value == value } }
            .map { AutopilotOption(value: $0, label: $0.capitalized, description: nil, recipeCount: nil) }
        return custom + options
    }

    var body: some View {
        let all = values
        let shown = showsAll ? all : Array(all.prefix(Self.initialCount))
        VStack(alignment: .leading, spacing: 10) {
            ChipFlowLayout {
                ForEach(shown) { option in
                    let preference = settings.preference(for: option.value, kind: kind)
                    ChoiceChip(
                        title: option.label, count: option.recipeCount, state: chipState(preference),
                        accessibilityValue: accessibilityValue(preference)
                    ) {
                        cycle(option.value, from: preference)
                    }
                    .accessibilityHint("Changes between like, not for us, and no preference")
                }
            }
            if all.count > shown.count {
                Button("Show All \(all.count)") { showsAll = true }
                    .buttonStyle(.borderless)
                    .font(.subheadline)
            }
            if kind.allowsCustomValues {
                HStack {
                    TextField(kind == .cuisine ? "Add a cuisine" : "Add a food type", text: $customValue)
                        .textInputAutocapitalization(.never)
                        .submitLabel(.done)
                        .onSubmit(addCustom)
                    Button("Like", action: addCustom)
                        .buttonStyle(.borderless)
                        .disabled(AutopilotInput.normalized(customValue).isEmpty)
                }
            }
            if let message {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
        }
        .padding(.vertical, 4)
    }

    private func chipState(_ preference: AutopilotTastePreference) -> ChoiceChip.State {
        switch preference {
        case .neutral: .off
        case .liked: .on
        case .disliked: .negative
        }
    }

    private func accessibilityValue(_ preference: AutopilotTastePreference) -> String {
        switch preference {
        case .neutral: String(localized: "No preference")
        case .liked: String(localized: "Liked")
        case .disliked: String(localized: "Not for us")
        }
    }

    private func cycle(_ value: String, from preference: AutopilotTastePreference) {
        message = nil
        if !settings.setPreference(preference.next, for: value, kind: kind, limits: limits) {
            // The liked list is full: skip straight to disliked, then say why.
            if preference == .neutral, settings.setPreference(.disliked, for: value, kind: kind, limits: limits) {
                message = String(localized: "You can like up to \(limits.maxListValues).")
            } else {
                message = String(localized: "You can choose up to \(limits.maxListValues).")
            }
        }
    }

    private func addCustom() {
        let value = AutopilotInput.normalized(customValue)
        guard !value.isEmpty else { return }
        guard value.count <= limits.maxValueLength else {
            message = String(localized: "Keep it to \(limits.maxValueLength) characters.")
            return
        }
        if settings.setPreference(.liked, for: value, kind: kind, limits: limits) {
            customValue = ""
            message = nil
        } else {
            message = String(localized: "You can like up to \(limits.maxListValues).")
        }
    }
}

extension AutopilotChoices {
    fileprivate subscript(kind: AutopilotChoiceKind) -> [String] {
        switch kind {
        case .cuisine: cuisines
        case .tag: tags
        case .protein: proteins
        }
    }
}

// MARK: - Restrictions

private struct RestrictionsSectionForm: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    private var limits: AutopilotLimits { vocabulary.limits }

    var body: some View {
        Section {
            ValueChipGroup(
                options: vocabulary.diets, selection: $settings.restrictions.diets, maxCount: vocabulary.diets.count,
                order: vocabulary.diets.map(\.value))
        } header: {
            Text("Diets")
        } footer: {
            Text("Autopilot never suggests a recipe that doesn't fit every diet you choose.")
        }
        Section {
            ValueChipGroup(
                options: vocabulary.allergens, selection: $settings.restrictions.allergens,
                maxCount: vocabulary.allergens.count, order: vocabulary.allergens.map(\.value))
            Toggle("No Spicy Food", isOn: $settings.restrictions.noSpicy)
        } header: {
            Text("Allergens")
        } footer: {
            Text("Recipes with these are never suggested.")
        }
        Section {
            ValueChipGroup(
                options: [], selection: $settings.restrictions.excludedIngredients,
                maxCount: limits.maxExcludedIngredients, maxLength: limits.maxIngredientLength,
                addPrompt: "Add an ingredient, like cilantro")
        } header: {
            Text("Never Include")
        } footer: {
            Text("Matches ingredient names, so “mushroom” also rules out cremini mushrooms.")
        }
        Section {
            ValueChipGroup(
                options: vocabulary.proteins, selection: exclusions(.protein), maxCount: limits.maxListValues,
                order: vocabulary.proteins.map(\.value))
        } header: {
            Text("Never Suggest These Proteins")
        }
        Section {
            ValueChipGroup(
                options: vocabulary.cuisines, selection: exclusions(.cuisine), maxCount: limits.maxListValues,
                maxLength: limits.maxValueLength, addPrompt: "Add a cuisine", initialCount: 10)
        } header: {
            Text("Never Suggest These Cuisines")
        } footer: {
            Text("Excluding something you liked removes it from your likes.")
        }
    }

    /// Excluding a value also removes it from likes.
    private func exclusions(_ kind: AutopilotChoiceKind) -> Binding<[String]> {
        Binding(
            get: {
                kind == .protein ? settings.restrictions.excludedProteins : settings.restrictions.excludedCuisines
            },
            set: { newValue in
                let old =
                    kind == .protein ? settings.restrictions.excludedProteins : settings.restrictions.excludedCuisines
                for value in old where !newValue.contains(value) {
                    settings.setExcluded(false, value: value, kind: kind, limits: limits)
                }
                for value in newValue where !old.contains(value) {
                    settings.setExcluded(true, value: value, kind: kind, limits: limits)
                }
            })
    }
}

// MARK: - Schedule

private struct ScheduleSectionForm: View {
    @Binding var settings: AutopilotSettings
    let limits: AutopilotLimits
    let householdServings: Int?

    var body: some View {
        Section {
            DayChipRow(selection: settings.schedule.planDays) { day in
                settings.setPlanDay(day, included: !settings.schedule.planDays.contains(day))
            }
            Stepper(value: $settings.schedule.mealsPerWeek, in: 1...max(settings.schedule.planDays.count, 1)) {
                LabeledContent("Meals per Week", value: settings.schedule.mealsPerWeek.formatted())
            }
        } header: {
            Text("Days to Plan")
        } footer: {
            Text("Autopilot fills up to this many of the chosen days. Days you've already planned count.")
        }
        Section {
            Picker("Servings", selection: $settings.schedule.defaultServings) {
                Text(householdServings.map { "Household Default (\($0))" } ?? "Household Default")
                    .tag(Int?.none)
                ForEach(1...limits.maxServings, id: \.self) { count in
                    Text("\(count)").tag(Int?.some(count))
                }
            }
        }
        Section {
            OptionalNumberRow(
                title: String(localized: "Weeknight Time Limit"), value: $settings.schedule.weeknightMaxMinutes,
                range: limits.minCookMinutes...limits.maxCookMinutes, step: 5, defaultValue: 30,
                format: { String(localized: "Up to \(RecipeFormat.minutes($0))") })
            if settings.schedule.weeknightMaxMinutes != nil {
                DayChipRow(selection: settings.schedule.weeknights) { day in
                    settings.setWeeknight(day, included: !settings.schedule.weeknights.contains(day))
                }
            }
        } header: {
            Text("Weeknights")
        } footer: {
            Text("A soft limit: Autopilot favors meals that fit on these nights.")
        }
    }
}

// MARK: - Cook time

private struct CookTimeSectionForm: View {
    @Binding var settings: AutopilotSettings
    let limits: AutopilotLimits

    var body: some View {
        let cookTime = settings.cookTime
        Section {
            Text("Mix quick and longer meals so you're not cooking 40-minute recipes every night.")
                .foregroundStyle(.secondary)
        }
        Section {
            Stepper(
                value: $settings.cookTime.quickMaxMinutes,
                in: limits.minCookMinutes...max(cookTime.mediumMaxMinutes - 1, limits.minCookMinutes), step: 5
            ) {
                LabeledContent(
                    "Quick", value: String(localized: "Up to \(RecipeFormat.minutes(cookTime.quickMaxMinutes))"))
            }
            Stepper(
                value: $settings.cookTime.mediumMaxMinutes,
                in: min(cookTime.quickMaxMinutes + 1, limits.maxCookMinutes)...limits.maxCookMinutes, step: 5
            ) {
                LabeledContent(
                    "Medium", value: String(localized: "Up to \(RecipeFormat.minutes(cookTime.mediumMaxMinutes))"))
            }
        } header: {
            Text("Time Bands")
        } footer: {
            Text("Anything longer than medium is a long cook.")
        }
        Section {
            Stepper(value: $settings.cookTime.maxLongPerWeek, in: 0...AutopilotCookTime.noLongLimit) {
                LabeledContent("Long Meals per Week", value: longLimitText(cookTime.maxLongPerWeek))
            }
            Stepper(value: $settings.cookTime.minQuickPerWeek, in: 0...7) {
                LabeledContent(
                    "Quick Meals per Week",
                    value: cookTime.minQuickPerWeek == 0
                        ? String(localized: "Any") : String(localized: "At least \(cookTime.minQuickPerWeek)"))
            }
            Toggle("Avoid Back-to-Back Long Cooks", isOn: $settings.cookTime.avoidConsecutiveLong)
        } header: {
            Text("Each Week")
        }
    }

    private func longLimitText(_ count: Int) -> String {
        switch count {
        case AutopilotCookTime.noLongLimit...: String(localized: "No limit")
        case 0: String(localized: "None")
        default: String(localized: "Up to \(count)")
        }
    }
}

// MARK: - Equipment

private struct EquipmentSectionForm: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    var body: some View {
        Section {
            ForEach(vocabulary.equipment) { option in
                Toggle(
                    isOn: Binding(
                        get: { settings.equipment.contains(option.value) },
                        set: { owned in
                            settings.setEquipment(
                                option.value, owned: owned, order: vocabulary.equipment.map(\.value))
                        })
                ) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(option.label)
                        if let description = option.description {
                            Text(description)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        } header: {
            Text("What You Cook With")
        } footer: {
            Text("Weekday rules can ask for equipment you have, like the smoker on Sundays.")
        }
    }
}

// MARK: - Weekday rules

private struct WeekdayRulesSectionForm: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    var body: some View {
        Section {
            ForEach(PlanDay.allCases) { day in
                NavigationLink {
                    WeekdayRuleEditor(day: day, settings: $settings, vocabulary: vocabulary)
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        if let rule = settings.rule(for: day) {
                            Text(AutopilotFormat.ruleTitle(rule))
                            Text(AutopilotFormat.ruleSummary(rule, vocabulary: vocabulary))
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        } else {
                            Text(day.name())
                            Text("No rule")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        } footer: {
            Text("Habits for a day, like “Taco Tuesday” or “Sunday smoker night.” Autopilot prefers matching meals.")
        }
    }
}

/// Edits one day's rule. A rule is kept only once it asks for something.
struct WeekdayRuleEditor: View {
    let day: PlanDay
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    @State private var rule: AutopilotWeekdayRule

    init(day: PlanDay, settings: Binding<AutopilotSettings>, vocabulary: AutopilotVocabulary) {
        self.day = day
        _settings = settings
        self.vocabulary = vocabulary
        _rule = State(initialValue: settings.wrappedValue.rule(for: day) ?? AutopilotWeekdayRule(day: day))
    }

    private var limits: AutopilotLimits { vocabulary.limits }

    private var ownedMethods: [AutopilotOption] {
        vocabulary.equipment.filter { settings.equipment.contains($0.value) }
    }

    var body: some View {
        Form {
            if !rule.hasPreference {
                templates
            }
            Section {
                TextField("Name, like Smoker night", text: $rule.label)
                    .onChange(of: rule.label) { _, label in
                        if label.count > limits.maxLabelLength {
                            rule.label = String(label.prefix(limits.maxLabelLength))
                        }
                    }
            } header: {
                Text("Name")
            }
            Section("Proteins") {
                ValueChipGroup(
                    options: vocabulary.proteins, selection: $rule.proteins, maxCount: limits.maxRuleValues,
                    order: vocabulary.proteins.map(\.value))
            }
            Section {
                if ownedMethods.isEmpty {
                    Text("Add equipment, like a smoker or grill, to ask for it here.")
                        .foregroundStyle(.secondary)
                } else {
                    ValueChipGroup(
                        options: ownedMethods, selection: $rule.methods, maxCount: ownedMethods.count,
                        order: vocabulary.equipment.map(\.value))
                }
            } header: {
                Text("Cooking Method")
            }
            Section("Cuisines") {
                ValueChipGroup(
                    options: vocabulary.cuisines, selection: $rule.cuisines, maxCount: limits.maxRuleValues,
                    maxLength: limits.maxValueLength, addPrompt: "Add a cuisine", initialCount: 8)
            }
            Section("Food Types") {
                ValueChipGroup(
                    options: vocabulary.tags, selection: $rule.tags, maxCount: limits.maxRuleValues,
                    maxLength: limits.maxValueLength, addPrompt: "Add a food type", initialCount: 8)
            }
            Section {
                Picker("Cook Time", selection: $rule.timeBand) {
                    Text("No Preference").tag(AutopilotTimeBand?.none)
                    ForEach(AutopilotTimeBand.known, id: \.self) { band in
                        Text(AutopilotFormat.timeBandLabel(band, vocabulary: vocabulary)).tag(
                            AutopilotTimeBand?.some(band))
                    }
                }
                Picker("How Often", selection: $rule.frequency) {
                    ForEach([AutopilotRuleFrequency.everyWeek, .atMostOnce], id: \.self) { frequency in
                        Text(AutopilotFormat.frequencyLabel(frequency, vocabulary: vocabulary)).tag(frequency)
                    }
                }
            } footer: {
                Text(
                    "“Long cook OK” lifts the weeknight limit that day. “At most once a week” keeps it from repeating.")
            }
            if settings.rule(for: day) != nil {
                Section {
                    Button("Remove Rule", role: .destructive) {
                        rule = AutopilotWeekdayRule(day: day)
                    }
                }
            } else if !rule.label.isEmpty && !rule.hasPreference {
                Section {
                    Text("Choose at least one preference to keep this rule.")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .navigationTitle(day.name())
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: rule) { _, updated in
            settings.setRule(updated.hasPreference ? updated : nil, for: day)
        }
    }

    private var templates: some View {
        Section {
            if settings.equipment.contains("smoker") {
                template(.smokerNight(on: day))
            }
            template(.tacoNight(on: day))
            template(.quickNight(on: day))
        } header: {
            Text("Start From")
        }
    }

    private func template(_ template: AutopilotWeekdayRule) -> some View {
        Button {
            rule = template
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(template.label)
                    .foregroundStyle(Color.primary)
                Text(AutopilotFormat.ruleSummary(template, vocabulary: vocabulary))
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
        }
    }
}

// MARK: - Novelty

private struct NoveltySectionForm: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    var body: some View {
        Section {
            ForEach(vocabulary.novelty) { option in
                let isSelected = settings.novelty.rawValue == option.value
                Button {
                    settings.novelty = AutopilotNovelty(rawValue: option.value)
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(option.label)
                            if let description = option.description {
                                Text(description)
                                    .font(.subheadline)
                                    .foregroundStyle(Color.secondary)
                            }
                        }
                        Spacer()
                        if isSelected {
                            Image(systemName: "checkmark")
                                .foregroundStyle(.tint)
                                .accessibilityHidden(true)
                        }
                    }
                    // Keeps text gray and black inside the list button.
                    .foregroundStyle(Color.primary)
                    .contentShape(.rect)
                }
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
        } header: {
            Text("Favorites or Something New?")
        }
    }
}

#Preview("Cook Time") {
    @Previewable @State var settings = AutopilotPreviewData.profile?.settings ?? .defaults
    let session = HouseholdPreviewData.session()
    NavigationStack {
        Form {
            if let vocabulary = AutopilotPreviewData.vocabulary {
                AutopilotSectionForm(section: .taste, settings: $settings, vocabulary: vocabulary)
                AutopilotSectionForm(section: .cookTime, settings: $settings, vocabulary: vocabulary)
                AutopilotSectionForm(section: .weekdayRules, settings: $settings, vocabulary: vocabulary)
            }
        }
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
}
