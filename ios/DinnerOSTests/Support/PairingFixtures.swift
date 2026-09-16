import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's add-on pairing responses. Made-up recipes and
/// grocery items only — no real household data.
nonisolated enum PairingFixtures {
    // MARK: Targets

    static func recipeTarget(id: String = "addon-1", name: String = "Sample Garlic Bread") -> String {
        #"""
        {"kind":"recipe","recipe":{"id":"\#(id)","name":"\#(name)","headline":"with Herb Butter",
         "imageUrl":"https://img.example.test/f_auto,q_auto,w_1200/\#(id).jpg","cookMinutes":15,
         "timesOrdered":203,"lastOrderedWeek":"2026-W37","isAddon":true,"tags":["SEO"]}}
        """#
    }

    static func groceryTarget(
        name: String = "Club Crackers", quantity: String = "1", unit: String = #""package""#
    ) -> String {
        #"{"kind":"grocery_item","groceryItem":{"name":"\#(name)","quantity":\#(quantity),"unit":\#(unit)}}"#
    }

    // MARK: Pairings

    /// One `AutopilotPairing`. `extra` adds fields a proposal slot's pairing carries
    /// (`id` and `included`).
    static func pairing(
        key: String = "recipe:addon-1", name: String = "Sample Garlic Bread", isRecipe: Bool = true,
        inPlan: Bool = false, source: String = "learned", frequency: String = "suggest",
        mealCategory: String = #""pasta""#, confidence: String = "0.95",
        learned: String = #"{"weeksTogether":186,"mealCategoryWeeks":195,"otherWeeks":35,"otherWeeksRate":0.49}"#,
        reason: String = "You usually have Sample Garlic Bread with pasta (95% of pasta weeks)",
        ruleID: String = "null", canMakeRule: Bool = true, servings: String = "2",
        quantityOverride: String? = nil, extra: String = ""
    ) -> String {
        let target =
            isRecipe
            ? recipeTarget(id: key.replacingOccurrences(of: "recipe:", with: ""), name: name)
            : groceryTarget(name: name, quantity: quantityOverride ?? "1")
        let extraFields = extra.isEmpty ? "" : "," + extra
        return #"""
            {"key":"\#(key)","target":\#(target),"servings":\#(isRecipe ? servings : "null"),
             "source":"\#(source)","frequency":"\#(frequency)","mealCategory":\#(mealCategory),
             "confidence":\#(isRecipe ? confidence : "null"),"learned":\#(isRecipe ? learned : "null"),
             "reason":"\#(reason)","ruleId":\#(ruleID),"inPlan":\#(inPlan),"canMakeRule":\#(canMakeRule)\#(extraFields)}
            """#
    }

    /// The rule-backed grocery pairing the owner's soup meals get.
    static func crackersPairing(inPlan: Bool = false, extra: String = "") -> String {
        pairing(
            key: "grocery:club crackers", name: "Club Crackers", isRecipe: false, inPlan: inPlan, source: "rule",
            mealCategory: #""soup""#, reason: "Your rule: soup → Club Crackers", ruleID: #""rule-2""#,
            canMakeRule: false, extra: extra)
    }

    // MARK: Responses

    /// `GET .../recipes/{id}/pairings?week=`: an add-on already in the week and a grocery item.
    static let recipePairingsJSON = #"""
        {"recipeId":"recipe-1","week":"2026-W38","entryId":"entry-1","mealCategories":["pasta"],
         "items":[\#(pairing(inPlan: true)),\#(crackersPairing())]}
        """#

    /// A meal with its open suggestions.
    static func meal(
        entryID: String = "entry-1", recipeID: String = "recipe-1", name: String = "Placeholder Pasta Bake",
        day: String = #""tue""#, mealCategories: String = #"["pasta"]"#, pairings: [String] = []
    ) -> String {
        #"""
        {"entryId":"\#(entryID)","day":\#(day),"date":"2026-09-15",
         "recipe":{"id":"\#(recipeID)","name":"\#(name)","imageUrl":"https://img.example.test/\#(recipeID).jpg"},
         "servings":2,"mealCategories":\#(mealCategories),"pairings":[\#(pairings.joined(separator: ","))]}
        """#
    }

    /// An accepted grocery item on the week's list.
    static func groceryLine(
        id: String = "item-1", key: String = "grocery:club crackers", name: String = "Club Crackers",
        entryID: String = "entry-2", recipeName: String = "Sample Soup"
    ) -> String {
        #"""
        {"id":"\#(id)","key":"\#(key)","groceryItem":{"name":"\#(name)","quantity":1,"unit":"package"},
         "entryId":"\#(entryID)","recipe":{"id":"recipe-2","name":"\#(recipeName)"},"source":"rule",
         "ruleId":"rule-2","text":"\#(name) for \#(recipeName)","addedBy":"\#(Fixtures.user.id)",
         "addedAt":"2026-09-14T19:10:00Z"}
        """#
    }

    static func weekPairings(
        week: String = "2026-W38", meals: [String] = [], groceryItems: [String] = []
    ) -> String {
        let (start, end) = PlanFixtures.dates(for: week)
        return #"""
            {"week":"\#(week)","startDate":"\#(start)","endDate":"\#(end)",
             "meals":[\#(meals.joined(separator: ","))],
             "groceryItems":[\#(groceryItems.joined(separator: ","))]}
            """#
    }

    /// A grocery list item carrying a pairing extra, for the grocery screen.
    static let groceryListWithExtra = Data(
        #"""
        {
          "week": "2026-W38", "status": "draft", "pantryApplied": false,
          "categories": [
            {"category": "pantry", "items": [
              {"ingredientKey": "name:club crackers", "name": "Club Crackers",
               "amounts": [{"quantity": "1", "quantityValue": 1, "unit": "package", "text": "1 package"}],
               "quantityText": "1 package", "unquantified": false, "status": "toBuy",
               "recipes": [{"id": "recipe-2", "name": "Sample Soup"}], "via": [],
               "extras": [{"id": "item-1", "origin": "pairing", "text": "Club Crackers for Sample Soup"},
                          {"id": "item-2", "origin": "future_origin", "text": "From somewhere new"},
                          {"origin": "pairing"}]},
              {"ingredientKey": "i-salt", "name": "Salt", "amounts": [], "quantityText": "", "unquantified": true,
               "status": "toBuy", "recipes": [{"id": "recipe-2", "name": "Sample Soup"}]}
            ]}
          ],
          "skipped": []
        }
        """#.utf8)
}

/// An in-memory stand-in for the pairings endpoints with the API's rules: accepted and
/// dismissed pairings drop out of later reads, accepting twice reports `alreadyAdded`, and
/// a grocery item can be taken back off the week.
nonisolated final class FakePairingsServer: Sendable {
    struct Suggestion: Sendable {
        var key: String
        var name: String
        var isRecipe = true
        /// The add-on's recipe id, for the plan entry an accept creates.
        var recipeID = "addon-1"
    }

    struct Meal: Sendable {
        var entryID: String
        var recipeID: String
        var name: String
        var day: String
        var mealCategory: String
        var suggestions: [Suggestion]
    }

    struct State: Sendable {
        var householdID = "household-1"
        var week = "2026-W38"
        var meals: [Meal] = [
            Meal(
                entryID: "entry-1", recipeID: "recipe-1", name: "Placeholder Pasta Bake", day: "tue",
                mealCategory: "pasta",
                suggestions: [Suggestion(key: "recipe:addon-1", name: "Sample Garlic Bread")]),
            Meal(
                entryID: "entry-2", recipeID: "recipe-2", name: "Sample Soup", day: "thu", mealCategory: "soup",
                suggestions: [
                    Suggestion(key: "grocery:club crackers", name: "Club Crackers", isRecipe: false)
                ]),
        ]
        /// Grocery items accepted onto the week's list, by item id.
        var groceryItems: [String: (key: String, name: String, entryID: String)] = [:]
        /// `<entryId>/<key>` of everything accepted, so a second accept is `alreadyAdded`.
        var accepted: Set<String> = []
        var dismissed: Set<String> = []
        var nextItemID = 1
        var planEntries: [String] = []
        var planStatus = "draft"
        /// The next this many requests answer with this status and code.
        var failure: (status: Int, code: String)?
        var log: [String] = []
        var bodies: [Data?] = []
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    var acceptedKeys: Set<String> { state.withLock { $0.accepted } }
    var dismissedKeys: Set<String> { state.withLock { $0.dismissed } }
    var groceryItemIDs: [String] { state.withLock { $0.groceryItems.keys.sorted() } }

    func update(_ change: @Sendable (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    func failNext(status: Int, code: String) {
        state.withLock { $0.failure = (status, code) }
    }

    /// The JSON body of the last request whose log line is `line`.
    func body(of line: String) -> [String: Any]? {
        let data: Data? = state.withLock { state in
            guard let index = state.log.lastIndex(of: line) else { return nil }
            return state.bodies[index]
        }
        return data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]

        return state.withLock { state in
            let query = url.query(percentEncoded: false).map { "?\($0)" } ?? ""
            state.log.append("\(method) /\(route.joined(separator: "/"))\(query)")
            state.bodies.append(request.httpBody)
            if let failure = state.failure {
                state.failure = nil
                return (failure.status, Fixtures.errorJSON(code: failure.code))
            }
            guard
                route.count >= 6, route[0] == "households", route[1] == state.householdID,
                route[2] == "autopilot", route[3] == "weeks", route[4] == state.week, route[5] == "pairings"
            else { return (404, Fixtures.errorJSON(code: "not_found")) }

            let tail = Array(route.dropFirst(6))
            switch (method, tail.first, tail.count) {
            case ("GET", nil, 0):
                let entryID = URLComponents(url: url, resolvingAgainstBaseURL: false)?
                    .queryItems?.first { $0.name == "entryId" }?.value
                return (200, Self.weekJSON(state, entryID: entryID))
            case ("POST", "accept", 1):
                return Self.accept(&state, body: body)
            case ("POST", "dismiss", 1):
                guard let entryID = body["entryId"] as? String, let key = body["key"] as? String else {
                    return (400, Fixtures.errorJSON(code: "validation_failed"))
                }
                state.dismissed.insert("\(entryID)/\(key)")
                return (200, Self.weekJSON(state, entryID: nil))
            case ("POST", "rules", 1):
                return Self.makeRule(state, body: body)
            case ("DELETE", "grocery-items", 2):
                guard state.groceryItems.removeValue(forKey: tail[1]) != nil else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                return (204, Data())
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    // MARK: Actions

    private static func accept(_ state: inout State, body: [String: Any]) -> (status: Int, body: Data) {
        guard let entryID = body["entryId"] as? String, let key = body["key"] as? String else {
            return (400, Fixtures.errorJSON(code: "validation_failed"))
        }
        guard
            let meal = state.meals.first(where: { $0.entryID == entryID }),
            let suggestion = meal.suggestions.first(where: { $0.key == key })
        else { return (404, Fixtures.errorJSON(code: "not_found")) }
        guard state.planStatus == "draft" else { return (409, Fixtures.errorJSON(code: "plan_finalized")) }

        let marker = "\(entryID)/\(key)"
        let alreadyAdded = state.accepted.contains(marker)
        var entryJSON = "null"
        var itemJSON = "null"
        if !alreadyAdded {
            state.accepted.insert(marker)
            if suggestion.isRecipe {
                let id = "addon-entry-\(state.accepted.count)"
                state.planEntries.append(
                    AutopilotFixtures.entry(
                        id: id, recipeID: suggestion.recipeID, name: suggestion.name, day: meal.day,
                        week: state.week, origin: "autopilot"))
                entryJSON = AutopilotFixtures.entry(
                    id: id, recipeID: suggestion.recipeID, name: suggestion.name, day: meal.day, week: state.week,
                    origin: "autopilot")
            } else {
                let id = "item-\(state.nextItemID)"
                state.nextItemID += 1
                state.groceryItems[id] = (key: key, name: suggestion.name, entryID: entryID)
                itemJSON = PairingFixtures.groceryLine(
                    id: id, key: key, name: suggestion.name, entryID: entryID, recipeName: meal.name)
            }
        }
        let plan = PlanFixtures.plan(
            householdID: state.householdID, week: state.week, status: state.planStatus, entries: state.planEntries)
        let pairingJSON = PairingFixtures.pairing(
            key: key, name: suggestion.name, isRecipe: suggestion.isRecipe, inPlan: true)
        let json = #"""
            {"status":"\#(alreadyAdded ? "alreadyAdded" : "added")","pairing":\#(pairingJSON),
             "entry":\#(entryJSON),"groceryItem":\#(itemJSON),"plan":\#(plan)}
            """#
        return (200, Data(json.utf8))
    }

    private static func makeRule(_ state: State, body: [String: Any]) -> (status: Int, body: Data) {
        guard body["key"] is String else {
            return (400, Fixtures.errorJSON(code: "validation_failed"))
        }
        let frequency = body["frequency"] as? String ?? "suggest"
        let rule = #"""
            {"id":"rule-new","label":"","when":{"mealCategories":["pasta"],"cuisines":[],"tags":[],"proteins":[]},
             "add":{"kind":"recipe","recipeId":"addon-1","recipeName":"Sample Garlic Bread","groceryItem":null},
             "frequency":"\#(frequency)"}
            """#
        let json = #"""
            {"status":"created","rule":\#(rule),
             "profile":\#(AutopilotFixtures.profile(householdID: state.householdID, configured: true))}
            """#
        return (200, Data(json.utf8))
    }

    // MARK: Reads

    /// The week's pairings, leaving out everything accepted or dismissed — the way the API
    /// does, so a second read never re-offers something the member decided on.
    private static func weekJSON(_ state: State, entryID: String?) -> Data {
        let meals =
            state.meals
            .filter { entryID == nil || $0.entryID == entryID }
            .map { meal in
                let open = meal.suggestions.filter { suggestion in
                    let marker = "\(meal.entryID)/\(suggestion.key)"
                    return !state.accepted.contains(marker) && !state.dismissed.contains(marker)
                }
                return PairingFixtures.meal(
                    entryID: meal.entryID, recipeID: meal.recipeID, name: meal.name, day: #""\#(meal.day)""#,
                    mealCategories: #"["\#(meal.mealCategory)"]"#,
                    pairings: open.map { suggestion in
                        PairingFixtures.pairing(
                            key: suggestion.key, name: suggestion.name, isRecipe: suggestion.isRecipe,
                            source: suggestion.isRecipe ? "learned" : "rule",
                            mealCategory: #""\#(meal.mealCategory)""#,
                            ruleID: suggestion.isRecipe ? "null" : #""rule-2""#,
                            canMakeRule: suggestion.isRecipe)
                    })
            }
        let items = state.groceryItems.sorted { $0.key < $1.key }
            .map { id, item in
                PairingFixtures.groceryLine(id: id, key: item.key, name: item.name, entryID: item.entryID)
            }
        return Data(PairingFixtures.weekPairings(week: state.week, meals: meals, groceryItems: items).utf8)
    }
}
