import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's recipe responses. No real recipes or orders.
nonisolated enum RecipeFixtures {
    static func summary(
        id: String, name: String, isAddon: Bool = false, timesOrdered: Int = 0, lastOrderedWeek: String? = nil
    ) -> String {
        let week = lastOrderedWeek.map { #","lastOrderedWeek":"\#($0)""# } ?? ""
        return #"""
            {"id":"\#(id)","name":"\#(name)","headline":"with Test Sauce",
             "imageUrl":"https://img.example.test/\#(id).jpg","totalMinutes":30,
             "timesOrdered":\#(timesOrdered)\#(week),"isAddon":\#(isAddon),"tags":["Quick"]}
            """#
    }

    static func page(_ items: [String], nextCursor: String?) -> Data {
        let cursor = nextCursor.map { #","nextCursor":"\#($0)""# } ?? ""
        return Data(#"{"items":[\#(items.joined(separator: ","))]\#(cursor)}"#.utf8)
    }

    /// A summary with every `omitempty` field left out.
    static let minimalPage = Data(
        #"{"items":[{"id":"r-min","name":"Plain Toast","timesOrdered":0,"isAddon":true,"tags":[]}]}"#.utf8)

    /// A full recipe. Amounts for 4 servings are deliberately not double the 2-serving
    /// amounts, so tests catch arithmetic scaling.
    static func detail(servings: String = "[2,4]") -> Data {
        Data(
            #"""
            {
              "id": "recipe-1", "householdId": "household-1", "source": "hellofresh",
              "sourceRecipeId": "src-1", "sourceAliases": ["src-1-clone"],
              "name": "Test Kitchen Tacos", "headline": "with Synthetic Salsa",
              "imageUrl": "https://img.example.test/tacos.jpg",
              "isAddon": false, "servings": \#(servings), "prepMinutes": 10, "totalMinutes": 30, "difficulty": 1,
              "cuisines": ["Mexican"], "tags": ["Quick"], "utensils": ["Skillet"], "allergens": ["Milk", "Wheat"],
              "nutritionPerServing": [
                {"name": "Calories", "amount": 640, "unit": "kcal"},
                {"name": "Protein", "amount": 32.5, "unit": "g"}
              ],
              "ingredients": [
                {"ingredientId": "i-cheddar", "name": "Cheddar", "category": "dairy-eggs", "pantryStaple": false,
                 "amounts": [
                   {"servings": 2, "quantity": "1/2", "quantityValue": 0.5, "unit": "oz", "sourceUnit": "ounce",
                    "rawText": "½ ounce Cheddar"},
                   {"servings": 4, "quantity": "3/4", "quantityValue": 0.75, "unit": "oz", "sourceUnit": "ounce",
                    "rawText": "¾ ounce Cheddar"}
                 ]},
                {"ingredientId": "i-garlic", "name": "Garlic", "category": "produce", "pantryStaple": false,
                 "amounts": [
                   {"servings": 2, "quantity": "1", "quantityValue": 1, "unit": "clove", "sourceUnit": "clove",
                    "rawText": "1 clove Garlic"},
                   {"servings": 4, "quantity": "2", "quantityValue": 2, "unit": "clove", "sourceUnit": "clove",
                    "rawText": "2 clove Garlic"}
                 ]},
                {"ingredientId": "i-tortilla", "name": "Flour Tortillas", "category": "bakery", "pantryStaple": false,
                 "amounts": [
                   {"servings": 2, "quantity": "6", "quantityValue": 6, "unit": "count", "sourceUnit": "unit",
                    "rawText": "6 unit Flour Tortillas"},
                   {"servings": 4, "quantity": "12", "quantityValue": 12, "unit": "count", "sourceUnit": "unit",
                    "rawText": "12 unit Flour Tortillas"}
                 ]},
                {"ingredientId": "i-salt", "name": "Salt", "category": "spices", "pantryStaple": true,
                 "amounts": [
                   {"servings": 2, "quantity": null, "quantityValue": null, "unit": "", "sourceUnit": "",
                    "rawText": "Salt"},
                   {"servings": 4, "quantity": null, "quantityValue": null, "unit": "", "sourceUnit": "",
                    "rawText": "Salt"}
                 ]},
                {"ingredientId": "i-paste", "name": "Test Paste", "category": "other", "pantryStaple": false,
                 "amounts": [
                   {"servings": 2, "quantity": "3/2", "quantityValue": 1.5, "unit": "tbsp", "sourceUnit": "tablespoon",
                    "rawText": "1½ tablespoon Test Paste"}
                 ]}
              ],
              "steps": [
                {"index": 1, "text": "Warm the tortillas.", "imageUrl": "https://img.example.test/step-1.jpg"},
                {"index": 2, "text": "• Fill.\n• Serve."}
              ],
              "orderWeeks": ["2026-W12", "2026-W37"], "timesOrdered": 2, "lastOrderedWeek": "2026-W37",
              "createdAt": "2026-09-14T18:30:00.123456789Z", "updatedAt": "2026-09-14T18:30:00Z"
            }
            """#.utf8)
    }
}

/// An in-memory stand-in for the recipe endpoints with cursor paging, search, the
/// add-on filter, and injectable failures.
nonisolated final class FakeRecipeServer: Sendable {
    struct Entry: Sendable {
        var id: String
        var name: String
        var isAddon = false
    }

    struct State: Sendable {
        var recipes: [String: [Entry]] = [:]
        /// The next this many requests answer `500`.
        var failuresRemaining = 0
        /// Query parameters of every list request, in order.
        var listQueries: [[String: String]] = []
        var detailRequests = 0
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var listQueries: [[String: String]] { state.withLock { $0.listQueries } }
    var detailRequests: Int { state.withLock { $0.detailRequests } }

    func failNext(_ count: Int = 1) {
        state.withLock { $0.failuresRemaining = count }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let query = Dictionary(
            (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []).map {
                ($0.name, $0.value ?? "")
            },
            uniquingKeysWith: { _, last in last })

        return state.withLock { state in
            guard route.count >= 3, route[0] == "households", route[2] == "recipes" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            if route.count == 3 {
                state.listQueries.append(query)
            } else {
                state.detailRequests += 1
            }
            if state.failuresRemaining > 0 {
                state.failuresRemaining -= 1
                return (500, Fixtures.errorJSON(code: "internal"))
            }
            guard let entries = state.recipes[route[1]] else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            if route.count == 4 {
                return entries.contains { $0.id == route[3] }
                    ? (200, RecipeFixtures.detail()) : (404, Fixtures.errorJSON(code: "not_found"))
            }

            var matches = entries
            if let search = query["q"], !search.isEmpty {
                matches = matches.filter { $0.name.localizedCaseInsensitiveContains(search) }
            }
            if let addons = query["addons"] {
                matches = matches.filter { $0.isAddon == (addons == "true") }
            }
            let start = query["cursor"].flatMap { Int($0) } ?? 0
            let limit = query["limit"].flatMap { Int($0) } ?? 50
            let end = min(start + limit, matches.count)
            let items = matches[min(start, end)..<end].map {
                RecipeFixtures.summary(id: $0.id, name: $0.name, isAddon: $0.isAddon)
            }
            return (200, RecipeFixtures.page(items, nextCursor: end < matches.count ? String(end) : nil))
        }
    }
}
