import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's specialty ingredient and grocery list responses. Generic
/// names only.
nonisolated enum SpecialtyFixtures {
    static let storeOptionJSON = #"""
        {"id":"southwest-spice-blend.store","specialtyId":"southwest-spice-blend","source":"curated",
         "type":"store_alternative","name":"Southwest blend from the spice rack","notes":"","isDefault":false,
         "per":{"quantity":"1","quantityValue":1,"unit":"tbsp","text":"1 tbsp"},
         "ingredients":[
           {"name":"Chili Powder","quantity":"3/2","quantityValue":1.5,"unit":"tsp","text":"1 ½ tsp Chili Powder",
            "category":null},
           {"name":"Ground Cumin","quantity":"3/4","quantityValue":0.75,"unit":"tsp","text":"¾ tsp Ground Cumin",
            "category":"spices"}],
         "steps":[],"yield":null,"shelfLifeDays":null,"basedOnOptionId":null,
         "summary":"1 tbsp = 1 ½ tsp Chili Powder + ¾ tsp Ground Cumin",
         "createdBy":null,"updatedBy":null,"createdAt":null,"updatedAt":null}
        """#

    static let batchOptionJSON = #"""
        {"id":"southwest-spice-blend.batch","specialtyId":"southwest-spice-blend","source":"curated",
         "type":"house_made_batch","name":"Southwest spice blend (house blend)","notes":"Keep away from heat.",
         "isDefault":true,"per":null,
         "ingredients":[
           {"name":"Chili Powder","quantity":"3","quantityValue":3,"unit":"tbsp","text":"3 tbsp Chili Powder",
            "category":null},
           {"name":"Ground Cumin","quantity":"2","quantityValue":2,"unit":"tbsp","text":"2 tbsp Ground Cumin",
            "category":null},
           {"name":"Salt","quantity":null,"quantityValue":null,"unit":null,"text":"Salt, to taste","category":"spices"}],
         "steps":["Stir everything together in a bowl.","Store in an airtight jar."],
         "yield":{"quantity":"12","quantityValue":12,"unit":"tbsp","text":"12 tbsp"},"shelfLifeDays":180,
         "basedOnOptionId":null,"summary":"Makes about 12 tbsp and keeps 180 days.",
         "createdBy":null,"updatedBy":null,"createdAt":null,"updatedAt":null}
        """#

    static let householdOptionJSON = #"""
        {"id":"66e5a1f2c3b4a5d6e7f80c01","specialtyId":"southwest-spice-blend","source":"household",
         "type":"house_made_batch","name":"Mild southwest blend","notes":"No cayenne.","isDefault":false,"per":null,
         "ingredients":[
           {"name":"Chili Powder","quantity":"3/2","quantityValue":1.5,"unit":"tbsp","text":"1 ½ tbsp Chili Powder",
            "category":null}],
         "steps":["Mix."],"yield":{"quantity":"3","quantityValue":3,"unit":"tbsp","text":"3 tbsp"},"shelfLifeDays":90,
         "basedOnOptionId":"southwest-spice-blend.batch","summary":"Makes about 3 tbsp and keeps 90 days.",
         "createdBy":"user-1","updatedBy":"user-1","createdAt":"2026-09-15T18:30:00Z",
         "updatedAt":"2026-09-15T18:30:00.123456789Z"}
        """#

    /// A batch is chosen and in the pantry; the household added its own copy.
    static let southwestJSON = #"""
        {"id":"southwest-spice-blend","key":"southwest spice blend","name":"Southwest Spice Blend",
         "aliases":["Southwestern Spice Blend"],"category":"spices","ingredientIds":["i-southwest"],"recipeCount":52,
         "unitSizes":[{"per":"count","quantity":"1","quantityValue":1,"unit":"tbsp","text":"1 tbsp"}],
         "defaultOptionId":"southwest-spice-blend.batch","retired":false,
         "choice":{"optionId":"southwest-spice-blend.batch","type":"house_made_batch",
           "optionName":"Southwest spice blend (house blend)","chosenBy":"user-1","chosenAt":"2026-09-15T18:30:00Z"},
         "options":[\#(storeOptionJSON),\#(batchOptionJSON),\#(householdOptionJSON)],
         "batch":{"pantryItemId":"item-southwest","status":"in_stock",
           "remaining":{"quantity":"9","quantityValue":9,"unit":"tbsp","text":"9 tbsp"},"percentRemaining":75,
           "expiresOn":"2027-03-14"}}
        """#

    static let defaultsJSON = Data(#"{"items":[\#(southwestJSON)],"skipped":2}"#.utf8)

    static let batchResponseJSON = Data(
        #"""
        {"purchase":{"id":"purchase-9","householdId":"household-1","itemId":"item-southwest","source":"house_made",
          "quantity":"12","quantityValue":12,"unit":"tbsp","unitSize":null,"week":null,
          "clientPurchaseId":"client-1","recordedBy":"user-1","purchasedAt":"2026-09-15T18:30:00Z"},
         "item":{"id":"item-southwest","householdId":"household-1","ingredientId":"i-southwest",
          "key":"southwest spice blend","displayName":"Southwest Spice Blend (house-made)","category":"spices",
          "quantity":"12","quantityValue":12,"unit":"tbsp","status":"in_stock","isStaple":false,
          "expiresOn":"2027-03-14","note":"","statusSource":"person","lowThresholdPercent":null,
          "unitSize":{"per":"count","quantity":"1","quantityValue":1,"unit":"tbsp"},"estimate":null,
          "updatedBy":"user-1","createdAt":"2026-09-15T18:30:00Z","updatedAt":"2026-09-15T18:30:00Z"},
         "option":\#(batchOptionJSON)}
        """#.utf8)

    static func list(_ items: [String]) -> Data {
        Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    private static let batchVia = #"""
        {"kind":"house_made_batch","specialtyId":"southwest-spice-blend","specialtyKey":"southwest spice blend",
         "specialtyName":"Southwest Spice Blend","optionId":"southwest-spice-blend.batch",
         "optionName":"Southwest spice blend (house blend)",
         "yield":{"quantity":"12","quantityValue":12,"unit":"tbsp","text":"12 tbsp"},"batches":1,
         "recipes":[{"id":"recipe-1","name":"Chili Bowls"}],
         "text":"to make Southwest Spice Blend (makes about 12 tbsp)"}
        """#

    private static let storeVia = #"""
        {"kind":"store_alternative","specialtyId":"tex-mex-paste","specialtyKey":"tex mex paste",
         "specialtyName":"Tex-Mex Paste","optionId":"tex-mex-paste.store","optionName":"Tomato paste and chili spices",
         "yield":null,"batches":null,"recipes":[{"id":"recipe-3","name":"Smoky Pork Tacos"}],
         "text":"for Tex-Mex Paste in Smoky Pork Tacos"}
        """#

    private static func amount(_ quantity: String, _ value: Double, _ unit: String, _ text: String) -> String {
        #"{"quantity":"\#(quantity)","quantityValue":\#(value),"unit":"\#(unit)","text":"\#(text)"}"#
    }

    private static func item(
        key: String, name: String, amount: String, quantityText: String, status: String = "toBuy",
        recipes: String, specialty: Bool = false, detail: String = "null", via: [String] = []
    ) -> String {
        #"""
        {"ingredientKey":"\#(key)","name":"\#(name)","amounts":[\#(amount)],"quantityText":"\#(quantityText)",
         "unquantified":false,"status":"\#(status)","recipes":\#(recipes),"specialty":\#(specialty),
         "specialtyDetail":\#(detail),"via":[\#(via.joined(separator: ","))]}
        """#
    }

    /// One of each case: an ordinary line; batch ingredients (one only for the batch, one a
    /// recipe also uses directly, one shared with a store alternative); a store alternative's
    /// ingredient; a line without a choice; a house-made batch in the pantry.
    static let groceryList = Data(
        #"""
        {"week":"2026-W38","status":"draft","pantryApplied":true,"specialtiesApplied":true,
         "categories":[
          {"category":"produce","items":[
            \#(item(key: "i-onion", name: "Yellow Onion", amount: amount("1", 1, "count", "1"), quantityText: "1",
                recipes: #"[{"id":"recipe-3","name":"Smoky Pork Tacos"}]"#))]},
          {"category":"spices","items":[
            \#(item(key: "i-chili-powder", name: "Chili Powder", amount: amount("4", 4, "tbsp", "4 tbsp"),
                quantityText: "4 tbsp",
                recipes: #"[{"id":"recipe-1","name":"Chili Bowls"},{"id":"recipe-3","name":"Smoky Pork Tacos"}]"#,
                via: [batchVia])),
            \#(item(key: "i-cumin", name: "Ground Cumin", amount: amount("2", 2, "tbsp", "2 tbsp"),
                quantityText: "2 tbsp", recipes: #"[{"id":"recipe-1","name":"Chili Bowls"}]"#, via: [batchVia])),
            \#(item(key: "i-paprika", name: "Smoked Paprika", amount: amount("1", 1, "tbsp", "1 tbsp"),
                quantityText: "1 tbsp",
                recipes: #"[{"id":"recipe-1","name":"Chili Bowls"},{"id":"recipe-3","name":"Smoky Pork Tacos"}]"#,
                via: [batchVia, storeVia]))]},
          {"category":"condiments","items":[
            \#(item(key: "i-tomato-paste", name: "Tomato Paste", amount: amount("7/3", 2.3333333333333335, "tbsp", "2 ⅓ tbsp"),
                quantityText: "2 ⅓ tbsp", recipes: #"[{"id":"recipe-3","name":"Smoky Pork Tacos"}]"#, via: [storeVia])),
            \#(item(key: "name:sweet soy glaze", name: "Sweet Soy Glaze", amount: amount("2", 2, "tbsp", "2 tbsp"),
                quantityText: "2 tbsp", recipes: #"[{"id":"recipe-4","name":"Teriyaki Bowls"}]"#, specialty: true,
                detail: #"""
                {"id":"sweet-soy-glaze","key":"sweet soy glaze","name":"Sweet Soy Glaze","choiceType":null,
                 "optionId":null,"houseMade":false,
                 "suggestedOptions":[
                   {"id":"sweet-soy-glaze.store","type":"store_alternative","name":"Soy and honey","isDefault":true},
                   {"id":"sweet-soy-glaze.batch","type":"house_made_batch","name":"Sweet soy glaze (house batch)",
                    "isDefault":false}],
                 "text":"Specialty ingredient: choose a store alternative or a house-made batch"}
                """#)),
            \#(item(key: "name:fry seasoning", name: "Fry Seasoning", amount: amount("1", 1, "tbsp", "1 tbsp"),
                quantityText: "1 tbsp", status: "inPantry", recipes: #"[{"id":"recipe-5","name":"Crispy Potatoes"}]"#,
                specialty: true,
                detail: #"""
                {"id":"fry-seasoning","key":"fry seasoning","name":"Fry Seasoning","choiceType":"house_made_batch",
                 "optionId":"fry-seasoning.batch","houseMade":true,"suggestedOptions":[],"text":"In pantry (house-made)"}
                """#))]}
         ],
         "batches":[
          {"specialtyId":"southwest-spice-blend","specialtyKey":"southwest spice blend",
           "specialtyName":"Southwest Spice Blend","optionId":"southwest-spice-blend.batch",
           "optionName":"Southwest spice blend (house blend)","yield":\#(amount("12", 12, "tbsp", "12 tbsp")),
           "status":"make","reason":"missing","batches":1,"pantryItemId":null,"remaining":null,
           "needed":\#(amount("2", 2, "tbsp", "2 tbsp")),"recipes":[{"id":"recipe-1","name":"Chili Bowls"}],
           "text":"Make a batch (makes about 12 tbsp)"},
          {"specialtyId":"fry-seasoning","specialtyKey":"fry seasoning","specialtyName":"Fry Seasoning",
           "optionId":"fry-seasoning.batch","optionName":"Fry seasoning (house blend)",
           "yield":\#(amount("6", 6, "tbsp", "6 tbsp")),"status":"inPantry","reason":"enough","batches":0,
           "pantryItemId":"item-fry","remaining":\#(amount("8", 8, "tbsp", "8 tbsp")),
           "needed":\#(amount("1", 1, "tbsp", "1 tbsp")),"recipes":[{"id":"recipe-5","name":"Crispy Potatoes"}],
           "text":"In pantry (house-made)"}
         ],
         "skipped":[]}
        """#.utf8)
}

/// An in-memory stand-in for the specialty ingredient endpoints with the API's rules: lists are
/// most used first, defaults never change a choice, only batch options record batches, and a
/// repeated `clientPurchaseId` records nothing new.
nonisolated final class FakeSpecialtyServer: Sendable {
    struct Option: Sendable, Equatable {
        var id: String
        var type: String
        var name: String
        var source = "curated"
        var isDefault = false
        var basedOnOptionID: String?
        var ingredientNames: [String] = []
    }

    struct Specialty: Sendable {
        var id: String
        var name: String
        var recipeCount: Int
        var defaultOptionID: String
        var options: [Option]
        /// An option ID or `as_is`.
        var choice: String?
        /// The batch item's tablespoons; `nil` without a batch in the pantry.
        var batchRemaining: Int?
    }

    struct RecordedBatch: Sendable, Equatable {
        var specialtyID: String
        var optionID: String
        var batches: Int
        var clientPurchaseID: String?
    }

    struct SentRequest: Sendable {
        let line: String
        let body: Data?
    }

    /// Chicken stock is kept as is; the glaze and the blend have no choice; Tuscan heat is unused.
    static let sample: [Specialty] = [
        Specialty(
            id: "sweet-soy-glaze", name: "Sweet Soy Glaze", recipeCount: 12, defaultOptionID: "sweet-soy-glaze.store",
            options: [
                Option(
                    id: "sweet-soy-glaze.store", type: "store_alternative", name: "Soy and honey", isDefault: true,
                    ingredientNames: ["Soy Sauce", "Honey"]),
                Option(
                    id: "sweet-soy-glaze.batch", type: "house_made_batch", name: "Sweet soy glaze (house batch)",
                    ingredientNames: ["Soy Sauce", "Brown Sugar"]),
            ]),
        Specialty(
            id: "southwest-spice-blend", name: "Southwest Spice Blend", recipeCount: 30,
            defaultOptionID: "southwest-spice-blend.batch",
            options: [
                Option(
                    id: "southwest-spice-blend.store", type: "store_alternative",
                    name: "Southwest blend from the spice rack", ingredientNames: ["Chili Powder"]),
                Option(
                    id: "southwest-spice-blend.batch", type: "house_made_batch",
                    name: "Southwest spice blend (house blend)", isDefault: true,
                    ingredientNames: ["Chili Powder", "Ground Cumin"]),
            ]),
        Specialty(
            id: "tuscan-heat-spice", name: "Tuscan Heat Spice", recipeCount: 0,
            defaultOptionID: "tuscan-heat-spice.store",
            options: [
                Option(
                    id: "tuscan-heat-spice.store", type: "store_alternative",
                    name: "Italian seasoning with chili flakes",
                    isDefault: true, ingredientNames: ["Italian Seasoning"])
            ]),
        Specialty(
            id: "chicken-stock-concentrate", name: "Chicken Stock Concentrate", recipeCount: 50,
            defaultOptionID: "chicken-stock-concentrate.store",
            options: [
                Option(
                    id: "chicken-stock-concentrate.store", type: "store_alternative", name: "Chicken bouillon base",
                    isDefault: true, ingredientNames: ["Chicken Bouillon Base"])
            ],
            choice: "as_is"),
    ]

    struct State: Sendable {
        var householdID = "household-1"
        var specialties = FakeSpecialtyServer.sample
        var nextID = 1
        var requests: [SentRequest] = []
        /// Changes answer `403`, as for a member without `pantry.edit`.
        var forbidsChanges = false
        /// The next choice answers `500`.
        var failsNextChoice = false
        /// The next batch is recorded, but its response is a `500`, as if it were lost.
        var losesNextBatchResponse = false
        var batches: [RecordedBatch] = []
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    /// `METHOD /path` for every request, in order.
    var log: [String] { state.withLock { $0.requests.map(\.line) } }
    var batches: [RecordedBatch] { state.withLock { $0.batches } }

    func choice(for specialtyID: String) -> String? {
        state.withLock { state in state.specialties.first { $0.id == specialtyID }?.choice }
    }

    func forbidChanges() {
        state.withLock { $0.forbidsChanges = true }
    }

    func failNextChoice() {
        state.withLock { $0.failsNextChoice = true }
    }

    func loseNextBatchResponse() {
        state.withLock { $0.losesNextBatchResponse = true }
    }

    /// The JSON bodies of requests whose `METHOD /path` ends with `suffix`, in order.
    func bodies(endingWith suffix: String) -> [[String: Any]] {
        let requests = state.withLock { $0.requests }
        return requests.filter { $0.line.hasSuffix(suffix) }.compactMap { request in
            request.body.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let queryItems = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let includesAll = queryItems.contains { $0.name == "all" && $0.value == "true" }
        let body = PantryFixtures.body(of: request)
        let httpBody = request.httpBody

        return state.withLock { state in
            state.requests.append(SentRequest(line: "\(method) /\(route.joined(separator: "/"))", body: httpBody))
            guard
                route.count >= 3, route[0] == "households", route[1] == state.householdID,
                route[2] == "specialty-ingredients"
            else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            if method != "GET", state.forbidsChanges {
                return (403, Fixtures.errorJSON(code: "forbidden"))
            }
            let rest = Array(route.dropFirst(3))
            if rest.isEmpty, method == "GET" {
                let items = Self.mostUsedFirst(state.specialties).filter { includesAll || $0.recipeCount > 0 }
                return (200, Data(#"{"items":[\#(items.map(Self.json).joined(separator: ","))]}"#.utf8))
            }
            if rest == ["choices", "defaults"], method == "POST" {
                return Self.applyDefaults(&state)
            }
            guard let specialtyID = rest.first,
                let index = state.specialties.firstIndex(where: { $0.id == specialtyID })
            else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            let tail = Array(rest.dropFirst())

            switch method {
            case "GET" where tail.isEmpty:
                return (200, Data(Self.json(state.specialties[index]).utf8))
            case "PUT" where tail == ["choice"]:
                if state.failsNextChoice {
                    state.failsNextChoice = false
                    return (500, Fixtures.errorJSON(code: "internal"))
                }
                guard
                    let optionID = body["optionId"] as? String,
                    optionID == "as_is" || state.specialties[index].options.contains(where: { $0.id == optionID })
                else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "unknown option"))
                }
                state.specialties[index].choice = optionID
                return (200, Data(Self.json(state.specialties[index]).utf8))
            case "DELETE" where tail == ["choice"]:
                state.specialties[index].choice = nil
                return (204, Data())
            case "POST" where tail == ["options"]:
                guard let type = body["type"] as? String, let name = body["name"] as? String else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "type and name"))
                }
                let option = Option(
                    id: "option-\(state.nextID)", type: type, name: name, source: "household",
                    basedOnOptionID: body["basedOnOptionId"] as? String, ingredientNames: Self.ingredientNames(body))
                state.nextID += 1
                state.specialties[index].options.append(option)
                return (201, Data(Self.json(option, specialtyID: specialtyID).utf8))
            case "PUT" where tail.count == 2 && tail[0] == "options":
                guard
                    let optionIndex = state.specialties[index].options.firstIndex(where: {
                        $0.id == tail[1] && $0.source == "household"
                    })
                else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                if let name = body["name"] as? String {
                    state.specialties[index].options[optionIndex].name = name
                }
                state.specialties[index].options[optionIndex].ingredientNames = Self.ingredientNames(body)
                return (
                    200, Data(Self.json(state.specialties[index].options[optionIndex], specialtyID: specialtyID).utf8)
                )
            case "DELETE" where tail.count == 2 && tail[0] == "options":
                guard state.specialties[index].options.contains(where: { $0.id == tail[1] && $0.source == "household" })
                else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                state.specialties[index].options.removeAll { $0.id == tail[1] }
                if state.specialties[index].choice == tail[1] {
                    state.specialties[index].choice = nil
                }
                return (204, Data())
            case "POST" where tail == ["batches"]:
                return Self.recordBatch(body, index: index, state: &state)
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    // MARK: - Endpoints

    private static func mostUsedFirst(_ specialties: [Specialty]) -> [Specialty] {
        specialties.sorted { first, second in
            first.recipeCount != second.recipeCount ? first.recipeCount > second.recipeCount : first.name < second.name
        }
    }

    private static func ingredientNames(_ body: [String: Any]) -> [String] {
        (body["ingredients"] as? [[String: Any]] ?? []).compactMap { $0["name"] as? String }
    }

    private static func applyDefaults(_ state: inout State) -> (status: Int, body: Data) {
        var chosen: [String] = []
        var skipped = 0
        for specialty in mostUsedFirst(state.specialties) where specialty.recipeCount > 0 {
            guard let index = state.specialties.firstIndex(where: { $0.id == specialty.id }) else { continue }
            if state.specialties[index].choice == nil {
                state.specialties[index].choice = state.specialties[index].defaultOptionID
                chosen.append(json(state.specialties[index]))
            } else {
                skipped += 1
            }
        }
        return (200, Data(#"{"items":[\#(chosen.joined(separator: ","))],"skipped":\#(skipped)}"#.utf8))
    }

    private static func recordBatch(
        _ body: [String: Any], index: Int, state: inout State
    ) -> (status: Int, body: Data) {
        let specialty = state.specialties[index]
        let clientID = body["clientPurchaseId"] as? String
        if let clientID, let existing = state.batches.first(where: { $0.clientPurchaseID == clientID }) {
            return (200, batchResponse(existing, specialty: specialty, householdID: state.householdID))
        }
        guard
            let optionID = (body["optionId"] as? String) ?? specialty.choice,
            let option = specialty.options.first(where: { $0.id == optionID }), option.type == "house_made_batch"
        else {
            return (400, Fixtures.errorJSON(code: "validation_failed", message: "choose a house-made batch"))
        }
        let batch = RecordedBatch(
            specialtyID: specialty.id, optionID: optionID, batches: body["batches"] as? Int ?? 1,
            clientPurchaseID: clientID)
        state.batches.append(batch)
        state.specialties[index].batchRemaining = 12 * batch.batches
        if state.losesNextBatchResponse {
            state.losesNextBatchResponse = false
            return (500, Fixtures.errorJSON(code: "internal"))
        }
        return (201, batchResponse(batch, specialty: state.specialties[index], householdID: state.householdID))
    }

    // MARK: - JSON

    private static func batchResponse(_ batch: RecordedBatch, specialty: Specialty, householdID: String) -> Data {
        let itemID = "item-batch-\(specialty.id)"
        let quantity = String(12 * batch.batches)
        var item = FakePantryServer.Item(
            id: itemID, name: "\(specialty.name) (house-made)", category: "spices", quantity: quantity, unit: "tbsp")
        item.estimatePercent = 100
        let purchase = FakePantryServer.Purchase(
            id: "purchase-\(batch.clientPurchaseID ?? specialty.id)", householdID: householdID, itemID: itemID,
            source: "house_made", quantity: quantity, unit: "tbsp", clientPurchaseID: batch.clientPurchaseID)
        let option = specialty.options.first { $0.id == batch.optionID }.map { json($0, specialtyID: specialty.id) }
        return Data(
            #"{"purchase":\#(FakePantryServer.json(purchase)),"item":\#(FakePantryServer.json(item, householdID: householdID)),"option":\#(option ?? "null")}"#
                .utf8)
    }

    static func json(_ specialty: Specialty) -> String {
        var choice = "null"
        if let optionID = specialty.choice {
            let option = specialty.options.first { $0.id == optionID }
            let type = option?.type ?? "as_is"
            let name = option.map { #""\#($0.name)""# } ?? "null"
            choice =
                #"{"optionId":"\#(optionID)","type":"\#(type)","optionName":\#(name),"#
                + #""chosenBy":"\#(Fixtures.user.id)","chosenAt":"2026-09-15T18:30:00Z"}"#
        }
        let batch =
            specialty.batchRemaining.map { remaining in
                #"{"pantryItemId":"item-batch-\#(specialty.id)","status":"in_stock","#
                    + #""remaining":{"quantity":"\#(remaining)","quantityValue":\#(remaining),"unit":"tbsp","#
                    + #""text":"\#(remaining) tbsp"},"percentRemaining":100,"expiresOn":"2027-03-14"}"#
            } ?? "null"
        let options = specialty.options.map { json($0, specialtyID: specialty.id) }.joined(separator: ",")
        return "{"
            + #""id":"\#(specialty.id)","key":"\#(specialty.name.lowercased())","name":"\#(specialty.name)","#
            + #""aliases":[],"category":"spices","ingredientIds":["i-\#(specialty.id)"],"#
            + #""recipeCount":\#(specialty.recipeCount),"#
            + #""unitSizes":[{"per":"count","quantity":"1","quantityValue":1,"unit":"tbsp","text":"1 tbsp"}],"#
            + #""defaultOptionId":"\#(specialty.defaultOptionID)","retired":false,"choice":\#(choice),"#
            + #""options":[\#(options)],"batch":\#(batch)"#
            + "}"
    }

    static func json(_ option: Option, specialtyID: String) -> String {
        let isBatch = option.type == "house_made_batch"
        let ingredients = option.ingredientNames.map { name in
            #"{"name":"\#(name)","quantity":"1","quantityValue":1,"unit":"tsp","text":"1 tsp \#(name)","category":null}"#
        }
        let per = isBatch ? "null" : #"{"quantity":"1","quantityValue":1,"unit":"tbsp","text":"1 tbsp"}"#
        let yield = isBatch ? #"{"quantity":"12","quantityValue":12,"unit":"tbsp","text":"12 tbsp"}"# : "null"
        let basedOn = option.basedOnOptionID.map { #""\#($0)""# } ?? "null"
        return "{"
            + #""id":"\#(option.id)","specialtyId":"\#(specialtyID)","source":"\#(option.source)","#
            + #""type":"\#(option.type)","name":"\#(option.name)","notes":"","isDefault":\#(option.isDefault),"#
            + #""per":\#(per),"ingredients":[\#(ingredients.joined(separator: ","))],"#
            + #""steps":\#(isBatch ? #"["Mix everything."]"# : "[]"),"yield":\#(yield),"#
            + #""shelfLifeDays":\#(isBatch ? "180" : "null"),"basedOnOptionId":\#(basedOn),"#
            + #""summary":"\#(option.name)","createdBy":null,"updatedBy":null,"createdAt":null,"updatedAt":null"#
            + "}"
    }
}
