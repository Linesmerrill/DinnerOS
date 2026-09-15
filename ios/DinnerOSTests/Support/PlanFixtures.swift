import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's plan and grocery responses. No real recipes.
nonisolated enum PlanFixtures {
    static func entry(
        id: String, recipeID: String = "recipe-1", name: String = "Test Kitchen Tacos", day: String? = "tue",
        date: String? = "2026-09-15", servings: Int = 2, note: String = "", imageURL: String? = nil
    ) -> String {
        let dayField = day.map { #""\#($0)""# } ?? "null"
        let dateField = date.map { #""\#($0)""# } ?? "null"
        let image = imageURL.map { #","imageUrl":"\#($0)""# } ?? ""
        return #"""
            {"id":"\#(id)","recipe":{"id":"\#(recipeID)","name":"\#(name)"\#(image)},
             "day":\#(dayField),"date":\#(dateField),"servings":\#(servings),"note":"\#(note)",
             "addedBy":"66e5a1f2c3b4a5d6e7f80912","addedAt":"2026-09-14T18:30:00.123456789Z"}
            """#
    }

    static func plan(
        householdID: String = "household-1", week: String = "2026-W38", status: String = "draft",
        entries: [String] = [], stored: Bool = true
    ) -> String {
        let (start, end) = dates(for: week)
        let timestamps =
            stored
            ? #""createdAt":"2026-09-14T18:30:00Z","updatedAt":"2026-09-14T19:05:00Z""#
            : #""createdAt":null,"updatedAt":null"#
        return #"""
            {"householdId":"\#(householdID)","week":"\#(week)","startDate":"\#(start)","endDate":"\#(end)",
             "status":"\#(status)","entries":[\#(entries.joined(separator: ","))],\#(timestamps)}
            """#
    }

    /// The documented grocery list example, plus a pantry hint and a skipped entry.
    static let groceryList = Data(
        #"""
        {
          "week": "2026-W38", "status": "draft", "pantryApplied": false,
          "categories": [
            {"category": "produce", "items": [
              {"ingredientKey": "i-onion", "name": "Yellow Onion",
               "amounts": [
                 {"quantity": "3/2", "quantityValue": 1.5, "unit": "count", "text": "1 ½"},
                 {"quantity": "8", "quantityValue": 8, "unit": "oz", "text": "8 oz"}
               ],
               "quantityText": "1 ½ + 8 oz", "unquantified": false, "status": "toBuy",
               "recipes": [{"id": "recipe-1", "name": "Test Kitchen Tacos"}, {"id": "recipe-2", "name": "Sample Soup"}]}
            ]},
            {"category": "spices", "items": [
              {"ingredientKey": "i-salt", "name": "Salt", "amounts": [], "quantityText": "", "unquantified": true,
               "status": "pantryHint", "recipes": [{"id": "recipe-1", "name": "Test Kitchen Tacos"}]},
              {"ingredientKey": "i-pepper", "name": "Black Pepper",
               "amounts": [{"quantity": "1", "quantityValue": 1, "unit": "tsp", "text": "1 tsp"}],
               "quantityText": "1 tsp", "unquantified": true, "status": "inPantry",
               "recipes": [{"id": "recipe-2", "name": "Sample Soup"}]}
            ]}
          ],
          "skipped": [
            {"entryId": "entry-9", "recipeId": "recipe-9", "recipeName": "Retired Stew", "reason": "recipeUnavailable"}
          ]
        }
        """#.utf8)

    static func dates(for week: String) -> (String, String) {
        guard let parsed = ISOWeek(week), let start = parsed.startDate, let end = parsed.endDate else {
            return ("", "")
        }
        let style = Date.ISO8601FormatStyle(timeZone: .gmt).year().month().day()
        return (start.formatted(style), end.formatted(style))
    }
}

/// An in-memory stand-in for the plan endpoints with the API's rules: servings must be a
/// recipe's authored size, finalized plans reject changes, and a week holds 50 entries.
nonisolated final class FakePlanServer: Sendable {
    struct Entry: Sendable {
        var id: String
        var recipeID: String
        var day: String?
        var servings: Int
        var note: String
    }

    struct Week: Sendable {
        var status = "draft"
        var entries: [Entry] = []
    }

    struct State: Sendable {
        var householdID = "household-1"
        /// Recipe ID to authored serving sizes.
        var recipes: [String: [Int]] = ["recipe-1": [2, 4], "recipe-2": [2]]
        var weeks: [String: Week] = [:]
        var nextID = 1
        /// The next this many requests answer `500`.
        var failuresRemaining = 0
        /// Method and path of every request, in order.
        var log: [String] = []
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    func week(_ week: String) -> Week? { state.withLock { $0.weeks[week] } }

    func update(_ change: @Sendable (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    func failNext(_ count: Int = 1) {
        state.withLock { $0.failuresRemaining = count }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]

        return state.withLock { state in
            state.log.append("\(method) /\(route.joined(separator: "/"))")
            if state.failuresRemaining > 0 {
                state.failuresRemaining -= 1
                return (500, Fixtures.errorJSON(code: "internal"))
            }
            guard route.count >= 4, route[0] == "households", route[1] == state.householdID, route[2] == "plans" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            let weekKey = route[3]
            guard ISOWeek(weekKey) != nil else {
                return (400, Fixtures.errorJSON(code: "validation_failed", message: "invalid week"))
            }
            var week = state.weeks[weekKey] ?? Week()
            let tail = Array(route.dropFirst(4))

            switch (method, tail.first, tail.count) {
            case ("GET", nil, 0):
                return (200, Self.planJSON(state, weekKey, week, stored: state.weeks[weekKey] != nil))
            case ("GET", "grocery", 1):
                return (200, PlanFixtures.groceryList)
            case ("PUT", "status", 1):
                guard let status = body["status"] as? String, ["draft", "finalized"].contains(status) else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "invalid status"))
                }
                week.status = status
                state.weeks[weekKey] = week
                return (200, Self.planJSON(state, weekKey, week, stored: true))
            case ("POST", "entries", 1):
                guard week.status == "draft" else { return (409, Fixtures.errorJSON(code: "plan_finalized")) }
                guard week.entries.count < 50 else { return (409, Fixtures.errorJSON(code: "plan_full")) }
                guard
                    let recipeID = body["recipeId"] as? String, let sizes = state.recipes[recipeID],
                    let servings = body["servings"] as? Int, sizes.contains(servings)
                else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "servings must be offered"))
                }
                let entry = Entry(
                    id: "entry-\(state.nextID)", recipeID: recipeID, day: body["day"] as? String, servings: servings,
                    note: body["note"] as? String ?? "")
                state.nextID += 1
                week.entries.append(entry)
                state.weeks[weekKey] = week
                let json = #"{"entry":\#(Self.entryJSON(entry)),"plan":\#(Self.planString(state, weekKey, week))}"#
                return (201, Data(json.utf8))
            case ("PATCH", "entries", 2), ("DELETE", "entries", 2):
                guard let index = week.entries.firstIndex(where: { $0.id == tail[1] }) else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                guard week.status == "draft" else { return (409, Fixtures.errorJSON(code: "plan_finalized")) }
                if method == "DELETE" {
                    week.entries.remove(at: index)
                    state.weeks[weekKey] = week
                    return (204, Data())
                }
                if body.keys.contains("day") {
                    week.entries[index].day = body["day"] as? String
                }
                if let servings = body["servings"] as? Int {
                    week.entries[index].servings = servings
                }
                if let note = body["note"] as? String {
                    week.entries[index].note = note
                }
                state.weeks[weekKey] = week
                return (200, Self.planJSON(state, weekKey, week, stored: true))
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private static func entryJSON(_ entry: Entry) -> String {
        PlanFixtures.entry(
            id: entry.id, recipeID: entry.recipeID, name: "Recipe \(entry.recipeID)", day: entry.day,
            date: entry.day.map { _ in "2026-09-15" }, servings: entry.servings, note: entry.note)
    }

    private static func planString(_ state: State, _ key: String, _ week: Week, stored: Bool = true) -> String {
        PlanFixtures.plan(
            householdID: state.householdID, week: key, status: week.status, entries: week.entries.map(entryJSON),
            stored: stored)
    }

    private static func planJSON(_ state: State, _ key: String, _ week: Week, stored: Bool) -> Data {
        Data(planString(state, key, week, stored: stored).utf8)
    }
}
