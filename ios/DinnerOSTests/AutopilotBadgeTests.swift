import Foundation
import Testing

@testable import DinnerOS

struct AutopilotBadgeTests {
    private func slot(badges: String?) throws -> AutopilotSlot {
        let badgesField = badges.map { #","badges":\#($0)"# } ?? ""
        let json = Data(
            #"""
            {"id":"tue","day":"tue","date":"2026-10-06","recipe":{"id":"r1","name":"Beef Stew"},"servings":2,
             "cookMinutes":45,"timeBand":"medium","score":1.1,"signals":{},"reasons":[],"swapCount":0\#(badgesField)}
            """#.utf8)
        return try JSONDecoder().decode(AutopilotSlot.self, from: json)
    }

    @Test func weatherBadgeDecodes() throws {
        let decoded = try slot(
            badges: #"""
                [{"code":"weather","label":"Cold and rainy","symbol":"cloud.rain",
                  "detail":"Picked for the weather: Tuesday looks cold and rainy, so a warm, comforting dinner."}]
                """#)
        #expect(decoded.badges.count == 1)
        #expect(decoded.badges[0].isWeather)
        #expect(decoded.badges[0].label == "Cold and rainy")
        #expect(decoded.badges[0].symbol == "cloud.rain")
    }

    @Test func olderServersAndBadEntriesCostOnlyTheBadge() throws {
        #expect(try slot(badges: nil).badges.isEmpty)
        // One badge this build can't read is dropped; the slot and the other badge stay.
        let decoded = try slot(badges: #"[{"code":"weather"},{"code":"x","label":"Hot"}]"#)
        #expect(decoded.badges.map(\.label) == ["Hot"])
        #expect(decoded.badges[0].detail.isEmpty)
    }
}

struct PlanDayShortNameTests {
    @Test func englishShortNamesTellEveryDayApart() {
        let english = Locale(identifier: "en_US")
        let names = PlanDay.allCases.map { $0.shortName(locale: english) }
        #expect(names == ["M", "T", "W", "Th", "F", "Sa", "Su"])
    }

    @Test func otherLanguagesUseTheirOwnShortDays() {
        let names = PlanDay.allCases.map { $0.shortName(locale: Locale(identifier: "fr_FR")) }
        #expect(Set(names).count == 7)
        #expect(names.allSatisfy { !$0.isEmpty && $0.count <= 5 })
    }
}

struct MenuPlannedCardsTests {
    @Test func aPlannedMealNoSectionShowsStillHasItsCard() throws {
        let data = MenuFixtures.menu(
            sections: [],
            planned: [MenuFixtures.card(id: "r-rigatoni", name: "Rigatoni", inPlan: true, entryIDs: ["e1"])])
        let menu = try JSONCoding.makeDecoder().decode(WeekMenu.self, from: data)
        #expect(menu.planned.map(\.recipe.id) == ["r-rigatoni"])
    }

    @Test func olderServersSendNoPlannedCards() throws {
        let data = Data(
            #"{"week":"2026-W38","weekStart":"2026-09-14","weekEnd":"2026-09-20","timing":"current","plan":null,"proposal":null,"sections":[]}"#
                .utf8)
        #expect(try JSONCoding.makeDecoder().decode(WeekMenu.self, from: data).planned.isEmpty)
    }
}
