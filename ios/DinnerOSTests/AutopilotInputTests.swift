import Foundation
import Testing

@testable import DinnerOS

struct AutopilotInputTests {
    private let limits = AutopilotLimits.defaults

    @Test func freeTextIsNormalizedAndBounded() {
        var list: [String] = []

        let results = ["  Korean   BBQ ", "korean bbq", "   ", "mediterranean", "thai", "greek"].map {
            AutopilotInput.add($0, to: &list, maxCount: 2, maxLength: 12)
        }

        #expect(results == [.added, .duplicate, .empty, .tooLong, .added, .full])
        #expect(list == ["korean bbq", "thai"])
    }

    @Test func fixedValuesKeepVocabularyOrder() {
        var list: [String] = []
        let order = ["chicken", "beef", "pork"]

        AutopilotInput.set("pork", included: true, in: &list, order: order)
        AutopilotInput.set("chicken", included: true, in: &list, order: order)
        AutopilotInput.set("mystery", included: true, in: &list, order: order)
        #expect(list == ["chicken", "pork", "mystery"])

        AutopilotInput.set("pork", included: false, in: &list, order: order)
        #expect(list == ["chicken", "mystery"])
    }

    @Test func likingDislikingAndExcludingAreExclusive() {
        var settings = AutopilotSettings.defaults

        settings.setPreference(.liked, for: "mexican", kind: .cuisine, limits: limits)
        #expect(settings.preference(for: "mexican", kind: .cuisine) == .liked)

        settings.setPreference(.disliked, for: "mexican", kind: .cuisine, limits: limits)
        #expect(settings.taste.likes.cuisines.isEmpty)
        #expect(settings.taste.dislikes.cuisines == ["mexican"])
        #expect(settings.preference(for: "mexican", kind: .cuisine).next == .neutral)

        settings.setPreference(.liked, for: "pork", kind: .protein, limits: limits)
        settings.setExcluded(true, value: "pork", kind: .protein, limits: limits)
        #expect(settings.taste.likes.proteins.isEmpty)
        #expect(settings.isExcluded("pork", kind: .protein))

        settings.setPreference(.liked, for: "pork", kind: .protein, limits: limits)
        #expect(!settings.isExcluded("pork", kind: .protein))
        #expect(settings.taste.likes.proteins == ["pork"])

        settings.setPreference(.neutral, for: "pork", kind: .protein, limits: limits)
        #expect(settings.preference(for: "pork", kind: .protein) == .neutral)
    }

    @Test func aFullListRefusesAnotherValue() {
        var settings = AutopilotSettings.defaults
        var tight = limits
        tight.maxListValues = 1

        let first = settings.setPreference(.liked, for: "thai", kind: .cuisine, limits: tight)
        let second = settings.setPreference(.liked, for: "greek", kind: .cuisine, limits: tight)
        #expect(first)
        #expect(!second)
        #expect(settings.taste.likes.cuisines == ["thai"])
        // Re-liking a value already there isn't adding.
        let again = settings.setPreference(.liked, for: "thai", kind: .cuisine, limits: tight)
        #expect(again)
        let excluded = settings.setExcluded(true, value: "spicy", kind: .tag, limits: tight)
        let overflow = settings.setExcluded(true, value: "fried", kind: .tag, limits: tight)
        #expect(excluded)
        #expect(!overflow)
    }

    @Test func planDaysKeepOneAndMealsNeverExceedThem() {
        var settings = AutopilotSettings.defaults
        #expect(settings.schedule.mealsPerWeek == 4)

        for day in [PlanDay.mon, .tue, .wed, .thu] {
            settings.setPlanDay(day, included: false)
        }
        #expect(settings.schedule.planDays == [.fri])
        #expect(settings.schedule.mealsPerWeek == 1)

        settings.setPlanDay(.fri, included: false)
        #expect(settings.schedule.planDays == [.fri])

        settings.setPlanDay(.sun, included: true)
        settings.setPlanDay(.mon, included: true)
        #expect(settings.schedule.planDays == [.mon, .fri, .sun])

        settings.setWeeknight(.fri, included: false)
        settings.setWeeknight(.sun, included: true)
        #expect(settings.schedule.weeknights == [.mon, .tue, .wed, .thu, .sun])
    }

    @Test func removingEquipmentStripsRuleMethodsAndDropsEmptyRules() {
        var settings = AutopilotSettings.defaults
        settings.setEquipment("smoker", owned: true, order: ["smoker", "grill"])
        settings.setEquipment("grill", owned: true, order: ["smoker", "grill"])
        #expect(settings.equipment == ["smoker", "grill"])
        settings.setRule(.smokerNight(on: .sun), for: .sun)
        settings.setRule(AutopilotWeekdayRule(day: .sat, methods: ["grill"]), for: .sat)
        #expect(settings.weekdayRules.map(\.day) == [.sat, .sun])

        settings.setEquipment("grill", owned: false, order: ["smoker", "grill"])
        #expect(settings.rule(for: .sat) == nil)

        settings.setEquipment("smoker", owned: false, order: ["smoker", "grill"])
        let sunday = settings.rule(for: .sun)
        #expect(sunday?.methods == [])
        #expect(sunday?.proteins == ["chicken", "pork"])
        #expect(settings.validationMessage == nil)
    }

    @Test func validationCatchesWhatTheAPIRejects() {
        var settings = AutopilotSettings.defaults
        #expect(settings.validationMessage == nil)

        settings.cookTime.mediumMaxMinutes = settings.cookTime.quickMaxMinutes
        #expect(settings.validationMessage?.contains("medium") == true)
        settings.cookTime = AutopilotCookTime()

        settings.schedule.mealsPerWeek = 6
        #expect(settings.validationMessage?.contains("Meals per week") == true)
        settings.schedule.mealsPerWeek = 4

        settings.weekdayRules = [AutopilotWeekdayRule(day: .tue)]
        #expect(settings.validationMessage?.contains("needs at least one preference") == true)

        settings.weekdayRules = [.smokerNight(on: .sun)]
        #expect(settings.validationMessage?.contains("equipment") == true)
    }

    @Test func skippingAStepRestoresThatSectionOnly() {
        var edited = AutopilotSettings.defaults
        edited.cookTime.maxLongPerWeek = 0
        edited.novelty = .adventurous

        let skipped = edited.replacing(.cookTime, from: .defaults)

        #expect(skipped.cookTime == AutopilotCookTime())
        #expect(skipped.novelty == .adventurous)
        #expect(edited.sectionsDiffering(from: .defaults) == [.cookTime, .novelty])
    }

    @Test func ruleTemplatesAreValidRules() {
        #expect(AutopilotWeekdayRule.smokerNight(on: .sun).hasPreference)
        #expect(AutopilotWeekdayRule.tacoNight(on: .tue).frequency == .atMostOnce)
        #expect(AutopilotWeekdayRule.quickNight(on: .wed).timeBand == .quick)
        #expect(!AutopilotWeekdayRule(day: .mon, label: "Just a label").hasPreference)
    }
}
