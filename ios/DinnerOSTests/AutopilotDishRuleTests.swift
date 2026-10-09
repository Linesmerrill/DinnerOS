import Foundation
import Testing

@testable import DinnerOS

/// A weekday rule can name dishes ("tacos on Tuesday"), any cuisine.
struct AutopilotDishRuleTests {
    @Test func aRuleFromAnOlderServerHasNoDishes() throws {
        let json = Data(
            #"{"day":"tue","label":"Taco night","cuisines":["mexican"],"tags":[],"proteins":[],"methods":[],"timeBand":null,"frequency":"every_week"}"#
                .utf8)
        let rule = try JSONDecoder().decode(AutopilotWeekdayRule.self, from: json)
        #expect(rule.dishes.isEmpty)
        #expect(rule.cuisines == ["mexican"])
    }

    @Test func dishesRoundTripAndCountAsAPreference() throws {
        var rule = AutopilotWeekdayRule(day: .tue, label: "Taco night")
        #expect(!rule.hasPreference)
        rule.dishes = ["taco", "enchilada"]
        #expect(rule.hasPreference)
        let data = try JSONEncoder().encode(rule)
        let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(object["dishes"] as? [String] == ["taco", "enchilada"])
        #expect(try JSONDecoder().decode(AutopilotWeekdayRule.self, from: data).dishes == ["taco", "enchilada"])
    }

    @Test func tacoAndPastaNightsNameTheDish() {
        #expect(AutopilotWeekdayRule.tacoNight(on: .tue).dishes == ["taco"])
        #expect(AutopilotWeekdayRule.pastaNight(on: .mon).dishes == ["pasta"])
        let summary = AutopilotFormat.ruleSummary(.tacoNight(on: .tue), vocabulary: nil)
        #expect(summary.hasPrefix("Taco"))
    }
}
