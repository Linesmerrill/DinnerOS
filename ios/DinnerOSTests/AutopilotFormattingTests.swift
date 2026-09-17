import Foundation
import Testing

@testable import DinnerOS

struct AutopilotFormattingTests {
    private let locale = Locale(identifier: "en_US")

    private func vocabulary() throws -> AutopilotVocabulary {
        try JSONCoding.makeDecoder().decode(AutopilotVocabulary.self, from: AutopilotFixtures.vocabulary)
    }

    private func context(_ json: String) throws -> AutopilotWeekContext {
        try JSONCoding.makeDecoder().decode(AutopilotWeekContext.self, from: Data(json.utf8))
    }

    @Test func attributionNamesTheMemberAndDate() throws {
        let members = [
            HouseholdMember(userID: "user-ada", displayName: "Ada Lovelace", role: .admin, joinedAt: .now),
            HouseholdMember(userID: "user-anon", displayName: "", role: .member, joinedAt: .now),
        ]
        let date = try #require(JSONCoding.parseDate("2026-09-14T18:30:00Z"))

        func text(_ userID: String) -> String? {
            AutopilotFormat.attribution(
                AutopilotSectionChange(updatedBy: userID, updatedAt: date), members: members,
                currentUserID: "user-charles", locale: locale, timeZone: .gmt)
        }

        #expect(text("user-ada") == "Changed by Ada Lovelace · Sep 14, 2026")
        #expect(text("user-charles") == "Changed by You · Sep 14, 2026")
        #expect(text("user-anon") == "Changed by Unnamed member · Sep 14, 2026")
        #expect(text("user-gone") == "Changed by A former member · Sep 14, 2026")
        #expect(AutopilotFormat.attribution(nil, members: members, currentUserID: nil) == nil)
    }

    @Test func contextSummaryListsWhatsSpecial() throws {
        #expect(try AutopilotFormat.contextSummary(context(AutopilotFixtures.context())) != nil)
        let summary = try AutopilotFormat.contextSummary(context(AutopilotFixtures.context()))
        #expect(summary?.hasPrefix("Busy weeknights · Up to 20") == true)
        #expect(summary?.hasSuffix("2 day changes") == true)
        #expect(try AutopilotFormat.contextSummary(context(AutopilotFixtures.context(configured: false))) == nil)
        #expect(AutopilotFormat.contextSummary(nil) == nil)

        let skipped = AutopilotFixtures.context().replacingOccurrences(
            of: #""skip":false,"busy""#, with: #""skip":true,"busy""#)
        #expect(try AutopilotFormat.contextSummary(context(skipped)) == "Skipping this week")
    }

    @Test func ruleSummaryReadsLikeTheOwnersExample() throws {
        let vocabulary = try vocabulary()
        let smoker = AutopilotWeekdayRule.smokerNight(on: .sun)

        #expect(
            AutopilotFormat.ruleSummary(smoker, vocabulary: vocabulary)
                == "Chicken or Pork · Smoker · Long cook OK · Every week")
        #expect(AutopilotFormat.ruleTitle(smoker, locale: locale) == "Sunday: Smoker night")
        #expect(
            AutopilotFormat.ruleTitle(AutopilotWeekdayRule(day: .tue, cuisines: ["mexican"]), locale: locale)
                == "Tuesday")
        #expect(
            AutopilotFormat.ruleSummary(.tacoNight(on: .tue), vocabulary: vocabulary)
                == "Mexican · At most once a week")
    }

    @Test func historyLinesDescribeEachChange() throws {
        let vocabulary = try vocabulary()
        let items = try JSONCoding.makeDecoder().decode(AutopilotHistory.self, from: AutopilotFixtures.history).items

        #expect(
            AutopilotFormat.historyTitle(items[0], vocabulary: vocabulary, recipeName: nil, weekStartsOn: .mon)
                == "Cook-Time Mix")
        #expect(AutopilotFormat.historyLines(items[0], vocabulary: vocabulary) == ["Max long meals: 2 → 1"])
        #expect(
            AutopilotFormat.historyLines(items[1], vocabulary: vocabulary)
                == ["Liked cuisines: added Thai; removed italian"])
        #expect(
            AutopilotFormat.historyTitle(
                items[2], vocabulary: vocabulary, recipeName: nil, weekStartsOn: .mon, locale: locale)
                == "Week of Sep 14, 2026")
        #expect(
            AutopilotFormat.historyTitle(
                items[2], vocabulary: vocabulary, recipeName: nil, weekStartsOn: .sun, locale: locale)
                == "Week of Sep 13, 2026")
        #expect(AutopilotFormat.historyLines(items[2], vocabulary: vocabulary) == ["Time limit: not set → 20"])
        #expect(AutopilotFormat.historyLines(items[3], vocabulary: vocabulary) == ["Cleared"])
        #expect(
            AutopilotFormat.historyTitle(
                items[4], vocabulary: vocabulary, recipeName: "Pork Shoulder", weekStartsOn: .mon)
                == "Good for Smoker: Pork Shoulder")
        #expect(AutopilotFormat.historyLines(items[4], vocabulary: vocabulary) == ["Automatic → Yes"])
        #expect(AutopilotFormat.fieldName("weekdayRules.sun") == "Sunday rule")
        #expect(AutopilotFormat.fieldName("days.fri") == "Friday")
        #expect(AutopilotFormat.fieldName("something.new") == "something.new")
    }

    @Test func methodSettingsNameTheAutomaticAnswer() {
        #expect(AutopilotFormat.methodSettingTitle(.automatic, heuristicSuits: true) == "Automatic (Yes)")
        #expect(AutopilotFormat.methodSettingTitle(.automatic, heuristicSuits: false) == "Automatic (No)")
        #expect(AutopilotFormat.methodSettingTitle(.no, heuristicSuits: true) == "No")
        #expect(AutopilotMethodSetting.yes.overrideValue == true)
        #expect(AutopilotMethodSetting.automatic.overrideValue == nil)
    }

    @Test func slotDatesAndCookTimes() {
        #expect(AutopilotFormat.dayTitle(date: "2026-09-20", day: .sun, locale: locale) == "Sunday, Sep 20")
        #expect(AutopilotFormat.dayTitle(date: "not a date", day: .sun, locale: locale) == "Sunday")
        #expect(AutopilotFormat.cookTime(nil) == "Time unknown")
        #expect(AutopilotFormat.cookTime(0) == "Time unknown")
    }
}
