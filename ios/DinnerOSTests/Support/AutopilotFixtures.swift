import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's Autopilot responses. No real recipes or households.
nonisolated enum AutopilotFixtures {
    static let otherMemberID = "66e5a1f2c3b4a5d6e7f80917"

    /// A profile with every section. Configured, it carries the documented example values
    /// and attribution on every section; unconfigured, the defaults with null metadata.
    static func profile(householdID: String = "household-1", configured: Bool = true) -> String {
        guard configured else {
            return #"""
                {"householdId":"\#(householdID)","configured":false,
                 "taste":{"likes":{"cuisines":[],"tags":[],"proteins":[]},"dislikes":{"cuisines":[],"tags":[],"proteins":[]}},
                 "restrictions":{"diets":[],"allergens":[],"excludedIngredients":[],"excludedCuisines":[],
                                 "excludedProteins":[],"excludedTags":[],"noSpicy":false},
                 "schedule":{"planDays":["mon","tue","wed","thu","fri"],"weeknights":["mon","tue","wed","thu"],
                             "mealsPerWeek":4,"defaultServings":null,"weeknightMaxMinutes":null},
                 "cookTime":{"quickMaxMinutes":20,"mediumMaxMinutes":35,"maxLongPerWeek":2,"minQuickPerWeek":0,
                             "avoidConsecutiveLong":true},
                 "novelty":"balanced","equipment":[],"weekdayRules":[],
                 "sections":{"taste":null,"restrictions":null,"schedule":null,"cookTime":null,"novelty":null,
                             "equipment":null,"weekdayRules":null},
                 "effective":{"defaultServings":2},
                 "createdBy":null,"createdAt":null,"updatedBy":null,"updatedAt":null}
                """#
        }
        let change = #"{"updatedBy":"\#(Fixtures.user.id)","updatedAt":"2026-09-14T18:30:00Z"}"#
        return #"""
            {"householdId":"\#(householdID)","configured":true,
             "taste":{"likes":{"cuisines":["mexican","thai"],"tags":["comfort food"],"proteins":["chicken","pork"]},
                      "dislikes":{"cuisines":[],"tags":[],"proteins":["lamb"]}},
             "restrictions":{"diets":[],"allergens":["peanuts"],"excludedIngredients":["cilantro"],"excludedCuisines":[],
                             "excludedProteins":[],"excludedTags":[],"noSpicy":false},
             "schedule":{"planDays":["mon","tue","wed","thu","fri","sun"],"weeknights":["mon","tue","wed","thu"],
                         "mealsPerWeek":5,"defaultServings":null,"weeknightMaxMinutes":35},
             "cookTime":{"quickMaxMinutes":20,"mediumMaxMinutes":35,"maxLongPerWeek":1,"minQuickPerWeek":2,
                         "avoidConsecutiveLong":true},
             "novelty":"balanced","equipment":["smoker"],
             "weekdayRules":[
               {"day":"tue","label":"Taco Tuesday","cuisines":["mexican"],"tags":[],"proteins":[],"methods":[],
                "timeBand":null,"frequency":"every_week"},
               {"day":"sun","label":"Sunday smoker night","cuisines":[],"tags":[],"proteins":["chicken","pork"],
                "methods":["smoker"],"timeBand":"long","frequency":"at_most_once"}
             ],
             "sections":{"taste":\#(change),"restrictions":\#(change),"schedule":\#(change),
                         "cookTime":{"updatedBy":"\#(otherMemberID)","updatedAt":"2026-09-15T08:10:00.123456789Z"},
                         "novelty":\#(change),"equipment":\#(change),"weekdayRules":\#(change)},
             "effective":{"defaultServings":2},
             "createdBy":"\#(Fixtures.user.id)","createdAt":"2026-09-14T18:30:00Z",
             "updatedBy":"\#(otherMemberID)","updatedAt":"2026-09-15T08:10:00Z"}
            """#
    }

    static let vocabulary = Data(
        #"""
        {
          "cuisines": [{"value":"mexican","label":"Mexican","recipeCount":42},{"value":"thai","label":"Thai","recipeCount":0}],
          "tags": [{"value":"comfort food","label":"Comfort Food","recipeCount":12}],
          "proteins": [{"value":"chicken","label":"Chicken","recipeCount":120},{"value":"pork","label":"Pork","recipeCount":64},
                       {"value":"tofu","label":"Tofu & tempeh","recipeCount":8}],
          "diets": [{"value":"vegetarian","label":"Vegetarian","description":"No meat or fish"}],
          "allergens": [{"value":"peanuts","label":"Peanuts"},{"value":"milk","label":"Milk"}],
          "equipment": [{"value":"smoker","label":"Smoker","description":"Whole or large cuts of chicken, pork, beef, or turkey"},
                        {"value":"grill","label":"Grill"}],
          "novelty": [{"value":"favorites","label":"Mostly favorites"},{"value":"balanced","label":"A mix","description":"Favorites with something new now and then"}],
          "timeBands": [{"value":"quick","label":"Quick"},{"value":"medium","label":"Medium"},{"value":"long","label":"Long cook OK","description":"Longer than the medium limit"}],
          "frequencies": [{"value":"every_week","label":"Every week"},{"value":"at_most_once","label":"At most once a week"}],
          "days": [{"value":"mon","label":"Monday"},{"value":"sun","label":"Sunday"}],
          "catalogRecipeCount": 429,
          "limits": {"maxListValues":30,"maxExcludedIngredients":50,"maxValueLength":40,"maxIngredientLength":60,
                     "maxRuleValues":10,"maxLabelLength":40,"maxNoteLength":500,"minCookMinutes":5,"maxCookMinutes":480,
                     "maxServings":12}
        }
        """#.utf8)

    static func context(householdID: String = "household-1", week: String = "2026-W38", configured: Bool = true)
        -> String
    {
        let (start, end) = PlanFixtures.dates(for: week)
        guard configured else {
            return #"""
                {"householdId":"\#(householdID)","week":"\#(week)","startDate":"\#(start)","endDate":"\#(end)",
                 "configured":false,"skip":false,"busy":false,"mealsPerWeek":null,"maxMinutes":null,"servings":null,
                 "days":[],"note":"","updatedBy":null,"updatedAt":null}
                """#
        }
        return #"""
            {"householdId":"\#(householdID)","week":"\#(week)","startDate":"\#(start)","endDate":"\#(end)",
             "configured":true,"skip":false,"busy":true,"mealsPerWeek":null,"maxMinutes":20,"servings":null,
             "days":[{"day":"fri","skip":false,"maxMinutes":null,"servings":6},
                     {"day":"sat","skip":true,"maxMinutes":null,"servings":null}],
             "note":"Grandparents visiting Friday","updatedBy":"\#(Fixtures.user.id)","updatedAt":"2026-09-14T19:00:00Z"}
            """#
    }

    struct Slot: Sendable {
        var day: String
        var recipeID: String
        var name: String
        var cookMinutes: Int? = 18
        var timeBand = "quick"
        var reasons: [String] = ["Ready in 18 min"]
        var swapCount = 0
    }

    static let defaultSlots = [
        Slot(day: "mon", recipeID: "recipe-1", name: "Test Kitchen Tacos", reasons: ["Taco night · Mexican"]),
        Slot(day: "wed", recipeID: "recipe-2", name: "Sample Soup", cookMinutes: nil, timeBand: "medium", reasons: []),
        Slot(
            day: "sun", recipeID: "recipe-3", name: "Placeholder Pork Shoulder", cookMinutes: 240, timeBand: "long",
            reasons: ["Sunday smoker night · Pork · Long cook OK", "Rated 4.5★ by your household"]),
    ]

    static func date(week: String, day: String) -> String {
        guard let parsed = ISOWeek(week), let planDay = PlanDay(rawValue: day), let date = planDay.date(in: parsed)
        else { return "" }
        return date.formatted(Date.ISO8601FormatStyle(timeZone: .gmt).year().month().day())
    }

    static func slot(_ slot: Slot, week: String = "2026-W38") -> String {
        let minutes = slot.cookMinutes.map(String.init) ?? "null"
        let reasons = slot.reasons.enumerated()
            .map { #"{"code":"\#($0.offset == 0 ? "rule" : "rating")","text":"\#($0.element)"}"# }
            .joined(separator: ",")
        return #"""
            {"id":"\#(slot.day)","day":"\#(slot.day)","date":"\#(date(week: week, day: slot.day))",
             "recipe":{"id":"\#(slot.recipeID)","name":"\#(slot.name)","imageUrl":"https://img.example.test/\#(slot.recipeID).jpg"},
             "servings":2,"cookMinutes":\#(minutes),"timeBand":"\#(slot.timeBand)","score":1.042,
             "signals":{"rating":1,"rule":1,"variety":-0.15},"reasons":[\#(reasons)],"swapCount":\#(slot.swapCount)}
            """#
    }

    static func proposal(
        id: String = "proposal-1", householdID: String = "household-1", week: String = "2026-W38",
        status: String = "proposed", version: Int = 1, slots: [Slot] = defaultSlots,
        unfilled: [String] = [], messages: [String] = [], excluded: [String] = []
    ) -> String {
        let (start, end) = PlanFixtures.dates(for: week)
        let unfilledJSON = unfilled.map {
            #"{"day":"\#($0)","date":"\#(date(week: week, day: $0))","code":"no_quick_candidates","text":"No quick recipe fits."}"#
        }
        let messagesJSON = messages.map { #"{"code":"not_enough_candidates","text":"\#($0)"}"# }
        let excludedJSON = excluded.map { #""\#($0)""# }
        let decided = status == "proposed" ? "null" : #""\#(Fixtures.user.id)""#
        let decidedAt = status == "proposed" ? "null" : #""2026-09-14T19:05:00Z""#
        return #"""
            {"id":"\#(id)","householdId":"\#(householdID)","week":"\#(week)","startDate":"\#(start)","endDate":"\#(end)",
             "status":"\#(status)","version":\#(version),"attempt":1,"modelVersion":"baseline-2026.1",
             "inputsHash":"9f2c4b1d0a7e6c35","requestedMeals":4,"plannedMeals":\#(slots.count),"candidateCount":120,
             "coldStart":false,"slots":[\#(slots.map { slot($0, week: week) }.joined(separator: ","))],
             "unfilled":[\#(unfilledJSON.joined(separator: ","))],"messages":[\#(messagesJSON.joined(separator: ","))],
             "objective":{"meals":2.91,"variety":-0.15,"cookTime":0,"rules":0,"novelty":0,"total":2.76},
             "swapCount":0,"excludedSlotIds":[\#(excludedJSON.joined(separator: ","))],
             "generatedBy":"\#(Fixtures.user.id)","generatedAt":"2026-09-14T19:02:00Z",
             "updatedAt":"2026-09-14T19:03:10.123456789Z","decidedBy":\#(decided),"decidedAt":\#(decidedAt)}
            """#
    }

    static func entry(
        id: String, recipeID: String, name: String, day: String, week: String = "2026-W38", origin: String
    ) -> String {
        #"""
        {"id":"\#(id)","recipe":{"id":"\#(recipeID)","name":"\#(name)"},"day":"\#(day)",
         "date":"\#(date(week: week, day: day))","servings":2,"note":"","addedBy":"\#(Fixtures.user.id)",
         "addedAt":"2026-09-14T19:05:00Z","origin":"\#(origin)"}
        """#
    }

    static let attributes = Data(
        #"""
        {"recipeId":"recipe-3","cookMinutes":90,"timeBand":"long","cuisines":["north american"],"cuisineRegions":["american"],"tags":[],"proteins":["pork"],
         "allergens":[],"diets":["gluten-free","dairy-free"],"spicy":false,
         "methods":[
           {"method":"smoker","label":"Smoker","suits":false,"source":"override","heuristicSuits":true,"evidence":"Pork Shoulder"},
           {"method":"grill","label":"Grill","suits":false,"source":"heuristic","heuristicSuits":false}
         ],
         "override":{"recipeId":"recipe-3","methods":{"smoker":false},"updatedBy":"\#(Fixtures.user.id)",
                     "updatedAt":"2026-09-14T18:45:00Z"}}
        """#.utf8)

    static let history = Data(
        #"""
        {"items":[
          {"type":"autopilot.preferences_updated","userId":"66e5a1f2c3b4a5d6e7f80917","occurredAt":"2026-09-15T08:10:00Z",
           "sections":["cookTime"],"changes":[{"field":"cookTime.maxLongPerWeek","from":"2","to":"1"}]},
          {"type":"autopilot.preferences_updated","userId":"66e5a1f2c3b4a5d6e7f80912","occurredAt":"2026-09-14T18:30:00Z",
           "sections":["taste"],"changes":[{"field":"taste.likes.cuisines","added":["thai"],"removed":["italian"]}]},
          {"type":"autopilot.week_context_updated","userId":"66e5a1f2c3b4a5d6e7f80912","occurredAt":"2026-09-14T19:00:00Z",
           "week":"2026-W38","changes":[{"field":"maxMinutes","to":"20"}]},
          {"type":"autopilot.week_context_updated","userId":"66e5a1f2c3b4a5d6e7f80912","occurredAt":"2026-09-14T19:30:00Z",
           "week":"2026-W38","cleared":true},
          {"type":"autopilot.recipe_override_updated","userId":"66e5a1f2c3b4a5d6e7f80912","occurredAt":"2026-09-14T18:45:00Z",
           "recipeId":"recipe-3","method":"smoker","value":"yes","previous":"auto"}
        ]}
        """#.utf8)
}

/// An in-memory stand-in for the Autopilot endpoints with the API's proposal rules:
/// versions must match, only a proposed week changes, swaps walk a list of alternatives,
/// and accepting skips days the plan already has.
nonisolated final class FakeAutopilotServer: Sendable {
    struct Proposal: Sendable {
        var id: String
        var week: String
        var status = "proposed"
        var version: Int
        var slots: [AutopilotFixtures.Slot]
    }

    struct State: Sendable {
        var householdID = "household-1"
        var profile = AutopilotFixtures.profile(configured: false)
        /// Saved week contexts by week.
        var contexts: [String: String] = [:]
        var proposal: Proposal?
        var generatedCount = 0
        /// Meals a swap offers, in order.
        var alternatives: [AutopilotFixtures.Slot] = [
            AutopilotFixtures.Slot(day: "", recipeID: "recipe-4", name: "Swapped Stir Fry", cookMinutes: 15)
        ]
        /// Days the plan already has an entry on.
        var plannedDays: Set<String> = []
        var planStatus = "draft"
        /// Method, path (without `/api/v1/`), and body of every request, in order.
        var log: [String] = []
        var bodies: [Data?] = []
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    var proposal: Proposal? { state.withLock { $0.proposal } }

    /// The JSON body of the last request whose log line is `line`.
    func body(of line: String) -> [String: Any]? {
        let data: Data? = state.withLock { state in
            guard let index = state.log.lastIndex(of: line) else { return nil }
            return state.bodies[index]
        }
        return data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    }

    func update(_ change: @Sendable (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]

        return state.withLock { state in
            state.log.append("\(method) /\(route.joined(separator: "/"))")
            state.bodies.append(request.httpBody)
            guard route.count >= 4, route[0] == "households", route[1] == state.householdID, route[2] == "autopilot"
            else { return (404, Fixtures.errorJSON(code: "not_found")) }
            let tail = Array(route.dropFirst(3))

            switch (method, tail) {
            case ("GET", ["profile"]):
                return (200, Data(state.profile.utf8))
            case ("PUT", ["profile"]), ("PATCH", ["profile"]):
                state.profile = Self.merge(body, into: state.profile)
                return (200, Data(state.profile.utf8))
            case ("GET", ["vocabulary"]):
                return (200, AutopilotFixtures.vocabulary)
            case ("GET", ["profile", "history"]):
                return (200, AutopilotFixtures.history)
            default:
                break
            }

            guard tail.count >= 3, tail[0] == "weeks" else { return (404, Fixtures.errorJSON(code: "not_found")) }
            let week = tail[1]
            switch (method, Array(tail.dropFirst(2))) {
            case ("GET", ["context"]):
                let context =
                    state.contexts[week]
                    ?? AutopilotFixtures.context(householdID: state.householdID, week: week, configured: false)
                return (200, Data(context.utf8))
            case ("PUT", ["context"]):
                let saved = Self.merge(
                    body,
                    into: AutopilotFixtures.context(householdID: state.householdID, week: week, configured: false),
                    configured: true)
                state.contexts[week] = saved
                return (200, Data(saved.utf8))
            case ("DELETE", ["context"]):
                state.contexts[week] = nil
                return (204, Data())
            case ("POST", ["generate"]):
                guard state.planStatus == "draft" else { return (409, Fixtures.errorJSON(code: "plan_finalized")) }
                state.generatedCount += 1
                let version = (state.proposal?.version ?? 0) + 1
                state.proposal = Proposal(
                    id: "proposal-\(state.generatedCount)", week: week, version: version,
                    slots: AutopilotFixtures.defaultSlots)
                return (201, Self.proposalJSON(state))
            case ("GET", ["proposal"]):
                guard let proposal = state.proposal, proposal.week == week else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                _ = proposal
                return (200, Self.proposalJSON(state))
            case ("POST", let action) where action.first == "proposal":
                return Self.changeProposal(&state, action: Array(action.dropFirst()), body: body)
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private static func changeProposal(
        _ state: inout State, action: [String], body: [String: Any]
    ) -> (status: Int, body: Data) {
        guard var proposal = state.proposal else { return (404, Fixtures.errorJSON(code: "not_found")) }
        guard proposal.status == "proposed" else { return (409, Fixtures.errorJSON(code: "proposal_not_pending")) }
        guard body["version"] as? Int == proposal.version else {
            return (409, Fixtures.errorJSON(code: "proposal_changed"))
        }
        switch action {
        case let slotAction where slotAction.count == 3 && slotAction[0] == "slots" && slotAction[2] == "swap":
            guard let index = proposal.slots.firstIndex(where: { $0.day == slotAction[1] }) else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            guard !state.alternatives.isEmpty else {
                return (409, Fixtures.errorJSON(code: "no_alternative", message: "No other recipe fits Sunday."))
            }
            var replacement = state.alternatives.removeFirst()
            replacement.day = proposal.slots[index].day
            replacement.swapCount = proposal.slots[index].swapCount + 1
            proposal.slots[index] = replacement
            proposal.version += 1
            state.proposal = proposal
            return (200, proposalJSON(state))
        case ["accept"]:
            let excluded = Set(body["excludeSlotIds"] as? [String] ?? [])
            let included = proposal.slots.filter { !excluded.contains($0.day) }
            let added = included.filter { !state.plannedDays.contains($0.day) }
            let skipped = included.filter { state.plannedDays.contains($0.day) }
            guard !added.isEmpty else { return (409, Fixtures.errorJSON(code: "nothing_to_accept")) }
            proposal.status = "accepted"
            proposal.version += 1
            state.proposal = proposal
            state.plannedDays.formUnion(added.map(\.day))
            let entries = added.enumerated().map {
                AutopilotFixtures.entry(
                    id: "entry-\($0.offset + 1)", recipeID: $0.element.recipeID, name: $0.element.name,
                    day: $0.element.day, week: proposal.week, origin: "autopilot")
            }
            let plan = PlanFixtures.plan(householdID: state.householdID, week: proposal.week, entries: entries)
            let skippedJSON = skipped.map { #"{"slotId":"\#($0.day)","day":"\#($0.day)","reason":"dayTaken"}"# }
            let json = #"""
                {"proposal":\#(String(decoding: proposalJSON(state), as: UTF8.self)),"plan":\#(plan),
                 "added":[\#(entries.joined(separator: ","))],"skipped":[\#(skippedJSON.joined(separator: ","))]}
                """#
            return (200, Data(json.utf8))
        case ["reject"]:
            proposal.status = "rejected"
            proposal.version += 1
            state.proposal = proposal
            return (200, proposalJSON(state))
        default:
            return (404, Fixtures.errorJSON(code: "not_found"))
        }
    }

    private static func proposalJSON(_ state: State) -> Data {
        guard let proposal = state.proposal else { return Data() }
        return Data(
            AutopilotFixtures.proposal(
                id: proposal.id, householdID: state.householdID, week: proposal.week, status: proposal.status,
                version: proposal.version, slots: proposal.slots
            ).utf8)
    }

    /// Replaces `document`'s top-level keys with `body`'s, as the API does for whole sections.
    private static func merge(_ body: [String: Any], into document: String, configured: Bool = true) -> String {
        guard
            var object = (try? JSONSerialization.jsonObject(with: Data(document.utf8))) as? [String: Any]
        else { return document }
        for (key, value) in body {
            object[key] = value
            if var sections = object["sections"] as? [String: Any], sections.keys.contains(key) {
                sections[key] = ["updatedBy": Fixtures.user.id, "updatedAt": "2026-09-15T10:00:00Z"]
                object["sections"] = sections
            }
        }
        object["configured"] = configured
        object["updatedBy"] = Fixtures.user.id
        object["updatedAt"] = "2026-09-15T10:00:00Z"
        guard let data = try? JSONSerialization.data(withJSONObject: object) else { return document }
        return String(decoding: data, as: UTF8.self)
    }
}
