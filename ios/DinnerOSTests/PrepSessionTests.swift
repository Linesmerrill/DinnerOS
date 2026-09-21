import Foundation
import Testing

@testable import DinnerOS

/// The prep plan as the app reads it: the week's checklist, the portioning advice, and the
/// rule that a thaw time always belongs to the portion count beside it
/// (docs/shopping-providers.md#the-prep-plan).
struct PrepSessionTests {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONCoding.makeDecoder().decode(type, from: Data(json.utf8))
    }

    @Test func aSessionCarriesItsCardsAndCounts() throws {
        let session = try decode(PrepSession.self, Self.sessionJSON)
        #expect(session.state == .ready)
        #expect(session.pending == 1)
        #expect(session.done == 1)
        #expect(session.hasWork)
        #expect(session.cards.count == 2)
        // The checklist resumes at the first card nobody has answered.
        #expect(session.cards.firstIndex { !$0.isAnswered } == 1)
    }

    @Test func aCardNamesTheMealsItsReserveIsFor() throws {
        let session = try decode(PrepSession.self, Self.sessionJSON)
        let pork = try #require(session.cards.last)
        #expect(pork.name == "Pork Loin")
        #expect(pork.status == .pending)
        #expect(pork.meals.map(\.recipeName) == ["Tuscan Pork"])
        #expect(pork.meals.first?.planDay == .thu)
        #expect(pork.meals.first?.past == false)
        #expect(pork.instruction.contains("Keep 10 oz out"))
    }

    /// The card promises only a reminder that exists. `thaw` means the hourly sweep will
    /// fire; nothing ever claims a general "we'll remember".
    @Test func aCardOnlyPromisesARealReminder() throws {
        let session = try decode(PrepSession.self, Self.sessionJSON)
        let pork = try #require(session.cards.last)
        #expect(pork.reminder == .thaw)
        #expect(pork.reminderText.contains("Thursday"))
        let beef = try #require(session.cards.first)
        #expect(beef.reminder == .list)
    }

    /// The bug this feature exists to fix: the thaw estimate has to follow the chosen count,
    /// never the whole bag.
    @Test func everyPortionCountCarriesItsOwnThawTime() throws {
        let session = try decode(PrepSession.self, Self.sessionJSON)
        let plan = try #require(session.cards.last?.portions)
        #expect(plan.portions == 5)
        #expect(plan.portionSizeText == "10.8 oz")
        #expect(plan.thaw.hours == 3)
        #expect(plan.basis == .meal)
        #expect(plan.reservedText == "10 oz")
        let whole = try #require(plan.option(1))
        // The old "Freeze the Rest" button recorded one 54 oz portion and quoted this.
        #expect(whole.thaw.hours == 17)
        let halves = try #require(plan.option(2))
        #expect(halves.sizeText == "27 oz")
        #expect(halves.thaw.hours == 8)
        #expect(plan.option(9) == nil)
    }

    @Test func aFinishedCardSaysWhatItRecorded() throws {
        let session = try decode(PrepSession.self, Self.sessionJSON)
        let beef = try #require(session.cards.first)
        #expect(beef.status == .done)
        #expect(beef.isAnswered)
        #expect(beef.frozenPortions == 4)
        #expect(beef.frozenItemID == "66e5a1f2c3b4a5d6e7f84002")
    }

    /// An empty week is an ordinary outcome, and the headline says so rather than leaving a
    /// blank screen.
    @Test func nothingToPrepIsAnAnswerNotAnError() throws {
        let session = try decode(PrepSession.self, Self.emptySessionJSON)
        #expect(session.state == .nothingToPrep)
        #expect(session.isEmpty)
        #expect(!session.hasWork)
        #expect(session.headline.contains("Nothing to prep"))
    }

    @Test func finishingACardReturnsTheSessionAroundIt() throws {
        let result = try decode(PrepCardResult.self, Self.resultJSON)
        #expect(result.card.status == .done)
        #expect(result.card.frozenPortions == 4)
        #expect(result.session.state == .finished)
        #expect(result.session.pending == 0)
    }

    // MARK: - Fixtures

    private static let sessionJSON = #"""
        {"week":"2026-W38","state":"ready","headline":"One thing to put away.","pending":1,
         "done":1,"skipped":0,"updatedAt":"2026-09-15T18:30:00.000Z",
         "cards":[
           {"id":"66e5a1f2c3b4a5d6e7f89001:l1","kind":"bulk_pack","handoffId":"66e5a1f2c3b4a5d6e7f89001",
            "lineId":"l1","status":"done","ingredientKey":"name:ground beef","ingredientId":null,
            "name":"Ground Beef","category":"meat-seafood","productName":"Valley Ridge Ground Beef",
            "unit":"oz","bought":"128","boughtValue":128,"needed":"36","neededValue":36,
            "surplus":"92","surplusValue":92,"surplusPercent":71,
            "surplusText":"This week uses 36 oz of 128 oz","freezable":true,"frozen":true,
            "instruction":"Keep 36 oz out, then cut the rest into 4 portions of about 23 oz and freeze them.",
            "reminder":"list","reminderText":"Next time a meal needs it, your list will say \"Grab from the freezer\".",
            "meals":[],
            "portions":{"unit":"oz","reserved":"36","reservedValue":36,"reservedText":"36 oz",
                        "surplus":"92","surplusValue":92,"meals":0,"typicalMeal":"36",
                        "typicalMealValue":36,"typicalMealText":"36 oz","basis":"week","portions":4,
                        "portionSize":"23","portionSizeValue":23,"portionSizeText":"23 oz",
                        "thaw":{"hours":7,"measured":true,"portionOunces":23,"summary":"about 7 hours"},
                        "options":[{"portions":1,"size":"92","sizeValue":92,"sizeText":"92 oz",
                                    "thaw":{"hours":29,"measured":true,"portionOunces":92,"summary":"about a day"}}]},
            "frozenItemId":"66e5a1f2c3b4a5d6e7f84002","frozenPortions":4,
            "answeredBy":"66e5a1f2c3b4a5d6e7f80c01","answeredAt":"2026-09-15T18:30:00.000Z",
            "suggestions":[]},
           {"id":"66e5a1f2c3b4a5d6e7f89001:l2","kind":"bulk_pack","handoffId":"66e5a1f2c3b4a5d6e7f89001",
            "lineId":"l2","status":"pending","ingredientKey":"name:pork loin","ingredientId":null,
            "name":"Pork Loin","category":"meat-seafood","productName":"Valley Ridge Pork Loin",
            "unit":"oz","bought":"64","boughtValue":64,"needed":"10","neededValue":10,
            "surplus":"54","surplusValue":54,"surplusPercent":84,
            "surplusText":"This week uses 10 oz of 64 oz","freezable":true,"frozen":false,
            "instruction":"Keep 10 oz out for Thursday's Tuscan Pork, then cut the rest into 5 portions of about 10.8 oz and freeze them.",
            "reminder":"thaw",
            "reminderText":"We'll remind you the morning of any day a planned meal needs it. Thursday is the next one.",
            "meals":[{"recipeId":"66e5a1f2c3b4a5d6e7f81001","recipeName":"Tuscan Pork","day":"thu",
                      "date":"2026-09-17","past":false}],
            "portions":{"unit":"oz","reserved":"10","reservedValue":10,"reservedText":"10 oz",
                        "surplus":"54","surplusValue":54,"meals":1,"typicalMeal":"10",
                        "typicalMealValue":10,"typicalMealText":"10 oz","basis":"meal","portions":5,
                        "portionSize":"54/5","portionSizeValue":10.8,"portionSizeText":"10.8 oz",
                        "thaw":{"hours":3,"measured":true,"portionOunces":10.8,"summary":"about 3 hours"},
                        "options":[{"portions":1,"size":"54","sizeValue":54,"sizeText":"54 oz",
                                    "thaw":{"hours":17,"measured":true,"portionOunces":54,"summary":"about 17 hours"}},
                                   {"portions":2,"size":"27","sizeValue":27,"sizeText":"27 oz",
                                    "thaw":{"hours":8,"measured":true,"portionOunces":27,"summary":"about 8 hours"}}]},
            "frozenItemId":null,"frozenPortions":0,"answeredBy":null,"answeredAt":null,
            "suggestions":[{"recipeId":"66e5a1f2c3b4a5d6e7f81006","recipeName":"Pork Fried Rice",
                            "imageUrl":null,"day":"sat","servings":2,"cookMinutes":25,
                            "reasons":["Uses what you already bought"]}]}]}
        """#

    private static let resultJSON = #"""
        {"card":{"id":"66e5a1f2c3b4a5d6e7f89001:l2","kind":"bulk_pack",
                 "handoffId":"66e5a1f2c3b4a5d6e7f89001","lineId":"l2","status":"done",
                 "ingredientKey":"name:pork loin","ingredientId":null,"name":"Pork Loin",
                 "category":"meat-seafood","productName":"Valley Ridge Pork Loin","unit":"oz",
                 "bought":"64","boughtValue":64,"needed":"10","neededValue":10,"surplus":"54",
                 "surplusValue":54,"surplusPercent":84,
                 "surplusText":"This week uses 10 oz of 64 oz","freezable":true,"frozen":true,
                 "instruction":"Keep 10 oz out.","reminder":"thaw","reminderText":"We'll remind you.",
                 "meals":[],"portions":null,"frozenItemId":"66e5a1f2c3b4a5d6e7f84003",
                 "frozenPortions":4,"answeredBy":"66e5a1f2c3b4a5d6e7f80c01",
                 "answeredAt":"2026-09-15T18:30:00.000Z","suggestions":[]},
         "session":{"week":"2026-W38","state":"finished","headline":"Everything's put away.",
                    "pending":0,"done":2,"skipped":0,"updatedAt":"2026-09-15T18:30:00.000Z",
                    "cards":[]}}
        """#

    private static let emptySessionJSON = #"""
        {"week":"2026-W38","state":"nothing_to_prep",
         "headline":"Nothing to prep this week — every pack was about the size the week needs.",
         "pending":0,"done":0,"skipped":0,"updatedAt":null,"cards":[]}
        """#
}
