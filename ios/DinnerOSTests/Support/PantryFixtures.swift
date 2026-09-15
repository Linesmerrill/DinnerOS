import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's pantry and ingredient catalog responses.
nonisolated enum PantryFixtures {
    /// An item with every field set, from before usage tracking (no usage fields).
    static let oliveOilJSON = #"""
        {"id":"item-oil","householdId":"household-1","ingredientId":"i-olive-oil","key":"olive oil",
         "displayName":"Olive Oil","category":"pantry","quantity":"3/2","quantityValue":1.5,"unit":"cup",
         "status":"in_stock","isStaple":true,"expiresOn":"2027-03-01","note":"big tin",
         "updatedBy":"user-1","createdAt":"2026-09-15T18:30:00.123456789Z","updatedAt":"2026-09-15T18:30:00Z"}
        """#

    /// A free-text item with no amount, catalog link, or expiry.
    static let zaatarJSON = #"""
        {"id":"item-zaatar","householdId":"household-1","ingredientId":null,"key":"za'atar",
         "displayName":"Za'atar","category":"spices","quantity":null,"quantityValue":null,"unit":null,
         "status":"low","isStaple":false,"expiresOn":null,"note":"",
         "updatedBy":"user-1","createdAt":"2026-09-15T18:31:00Z","updatedAt":"2026-09-15T18:31:00Z"}
        """#

    /// A tracked item the estimate marked low, with its own threshold and a package size.
    static let butterUsageJSON = #"""
        {"id":"item-butter","householdId":"household-1","ingredientId":"i-butter","key":"butter",
         "displayName":"Butter","category":"dairy-eggs","quantity":"5","quantityValue":5,"unit":"tbsp",
         "status":"low","isStaple":false,"expiresOn":null,"note":"",
         "statusSource":"estimate","lowThresholdPercent":60,
         "unitSize":{"per":"package","quantity":"8","quantityValue":8,"unit":"oz"},
         "estimate":{"cycleId":"66e5a1f2c3b4a5d6e7f80f01","cycleSource":"grocery_list",
           "cycleStartedAt":"2026-09-15T18:30:00Z","adjustedAt":null,"unit":"tbsp",
           "startAmount":{"quantity":"16","quantityValue":16},"remaining":{"quantity":"5","quantityValue":5},
           "percentRemaining":31,"percentUsed":69,"recipeUse":{"count":2,"quantity":"6","quantityValue":6},
           "otherUse":{"quantity":"5","quantityValue":5},
           "dailyRate":{"quantity":"1","quantityValue":1,"basedOnSegments":3},"skippedRecipes":1,
           "lowThresholdPercent":60,"thresholdSource":"item","belowThreshold":true,
           "summary":"About 31% left: 2 recipes used 6 tbsp, plus about 1 tbsp a day of other use.",
           "estimatedAt":"2026-09-20T18:30:00Z"},
         "updatedBy":"user-1","createdAt":"2026-09-15T18:30:00Z","updatedAt":"2026-09-20T18:30:00Z"}
        """#

    /// A newer server's item whose estimate this build can't read.
    static let unreadableEstimateJSON = #"""
        {"id":"item-rice","householdId":"household-1","ingredientId":null,"key":"rice",
         "displayName":"Rice","category":"pantry","quantity":"2","quantityValue":2,"unit":"cup",
         "status":"in_stock","isStaple":false,"expiresOn":null,"note":"",
         "statusSource":"robot","lowThresholdPercent":null,"unitSize":null,"estimate":{"cycleId":5},
         "updatedBy":"user-1","createdAt":"2026-09-15T18:30:00Z","updatedAt":"2026-09-15T18:30:00Z"}
        """#

    static let purchaseResponseJSON = Data(
        #"""
        {"purchase":{"id":"purchase-1","householdId":"household-1","itemId":"item-butter","source":"grocery_list",
          "quantity":"1","quantityValue":1,"unit":"cup","unitSize":null,"week":"2026-W38",
          "clientPurchaseId":"client-1","recordedBy":"user-1","purchasedAt":"2026-09-15T18:30:00Z"},
         "item":\#(butterUsageJSON)}
        """#.utf8)

    static let purchaseHistoryJSON = Data(
        #"""
        {"items":[
          {"id":"purchase-2","householdId":"household-1","itemId":"item-butter","source":"manual",
           "quantity":"2","quantityValue":2,"unit":"package",
           "unitSize":{"per":"package","quantity":"8","quantityValue":8,"unit":"oz"},"week":null,
           "clientPurchaseId":"client-2","recordedBy":"user-1","purchasedAt":"2026-09-18T10:00:00Z"},
          {"id":"purchase-1","householdId":"household-1","itemId":"item-butter","source":"grocery_list",
           "quantity":null,"quantityValue":null,"unit":null,"unitSize":null,"week":"2026-W38",
           "clientPurchaseId":null,"recordedBy":"user-2","purchasedAt":"2026-09-15T18:30:00Z"}
        ]}
        """#.utf8)

    static let settingsJSON = Data(
        #"{"lowThresholdPercent":80,"defaultLowThresholdPercent":80,"updatedBy":null,"updatedAt":null}"#.utf8)

    static func list(_ items: [String]) -> Data {
        Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    static let catalogJSON = Data(
        #"""
        {"items":[
          {"id":"i-oil","key":"oil","name":"Oil","category":"pantry","categoryConfident":true},
          {"id":"i-olive-oil","key":"olive oil","name":"Olive Oil","category":"pantry","categoryConfident":true,
           "imageUrl":"https://img.example.test/olive-oil.png"}
        ]}
        """#.utf8)

    /// A Swift value, for tests that don't go through JSON.
    static func item(
        id: String = "item-1", name: String = "Butter", category: String = "dairy-eggs", quantity: String? = nil,
        quantityValue: Double? = nil, unit: String? = nil, status: PantryStatus = .inStock, isStaple: Bool = false,
        expiresOn: String? = nil, note: String = "", ingredientID: String? = nil,
        statusSource: PantryStatusSource = .person, lowThresholdPercent: Int? = nil, unitSize: PantryUnitSize? = nil,
        estimate: PantryEstimate? = nil
    ) -> PantryItem {
        PantryItem(
            id: id, householdID: "household-1", ingredientID: ingredientID, key: name.lowercased(),
            displayName: name, category: category, quantity: quantity, quantityValue: quantityValue, unit: unit,
            status: status, isStaple: isStaple, expiresOn: expiresOn, note: note, updatedBy: Fixtures.user.id,
            createdAt: Date(timeIntervalSince1970: 1_757_000_000),
            updatedAt: Date(timeIntervalSince1970: 1_757_000_000),
            statusSource: statusSource, lowThresholdPercent: lowThresholdPercent, unitSize: unitSize, estimate: estimate
        )
    }

    /// An estimate in tablespoons: 5 of 16 left by default.
    static func estimate(
        percentRemaining: Int = 31, belowThreshold: Bool = false, recipeCount: Int = 2,
        dailyRate: PantryDailyRate? = PantryDailyRate(quantity: "1", quantityValue: 1, basedOnSegments: 3),
        skippedRecipes: Int = 0, lowThresholdPercent: Int = 80, thresholdSource: PantryThresholdSource = .household
    ) -> PantryEstimate {
        PantryEstimate(
            cycleID: "cycle-1", cycleSource: "grocery_list", cycleStartedAt: Date(timeIntervalSince1970: 1_757_000_000),
            adjustedAt: nil, unit: "tbsp", startAmount: PantryAmount(quantity: "16", quantityValue: 16),
            remaining: PantryAmount(quantity: "5", quantityValue: 5), percentRemaining: percentRemaining,
            percentUsed: 100 - percentRemaining,
            recipeUse: PantryRecipeUse(
                count: recipeCount, quantity: String(recipeCount * 3), quantityValue: Double(recipeCount * 3)),
            otherUse: PantryAmount(quantity: "5", quantityValue: 5), dailyRate: dailyRate,
            skippedRecipes: skippedRecipes, lowThresholdPercent: lowThresholdPercent, thresholdSource: thresholdSource,
            belowThreshold: belowThreshold, summary: "About \(percentRemaining)% left.",
            estimatedAt: Date(timeIntervalSince1970: 1_757_000_000))
    }

    /// Parses a JSON request body.
    static func body(of request: URLRequest) -> [String: Any] {
        guard let data = request.httpBody else { return [:] }
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }
}

/// An in-memory stand-in for the pantry and ingredient catalog endpoints, so store tests
/// exercise real state transitions through `APIClient` and `AuthSession`.
///
/// Lists come back in insertion order, not aisle order, so tests prove the store sorts.
nonisolated final class FakePantryServer: Sendable {
    struct Item: Sendable, Equatable {
        var id: String
        var ingredientID: String?
        var key: String
        var name: String
        var category: String
        var quantity: String?
        var unit: String?
        var status: String
        var isStaple: Bool
        var expiresOn: String?
        var note = ""
        var statusSource = "person"
        var lowThresholdPercent: Int?
        /// A tracked item's estimate; set to 100 by a purchase with an amount.
        var estimatePercent: Int?
        var unitSizeQuantity: String?
        var unitSizeUnit: String?

        init(
            id: String, name: String, category: String, quantity: String? = nil, unit: String? = nil,
            status: String = "in_stock", isStaple: Bool = false, expiresOn: String? = nil, ingredientID: String? = nil
        ) {
            self.id = id
            self.ingredientID = ingredientID
            self.key = name.lowercased()
            self.name = name
            self.category = category
            self.quantity = quantity
            self.unit = unit
            self.status = status
            self.isStaple = isStaple
            self.expiresOn = expiresOn
        }
    }

    /// A recorded purchase, as the server saw the request.
    struct Purchase: Sendable, Equatable {
        var id: String
        var householdID: String
        var itemID: String
        var source: String
        var ingredientID: String?
        var name: String?
        var quantity: String?
        var unit: String?
        var unitSizeQuantity: String?
        var unitSizeUnit: String?
        var week: String?
        var clientPurchaseID: String?
    }

    struct CatalogEntry: Sendable {
        let id: String
        let key: String
        let name: String
        let category: String
    }

    static let catalog = [
        CatalogEntry(id: "i-salt", key: "salt", name: "Salt", category: "spices"),
        CatalogEntry(id: "i-black-pepper", key: "black pepper", name: "Black Pepper", category: "spices"),
        CatalogEntry(id: "i-oil", key: "oil", name: "Oil", category: "pantry"),
        CatalogEntry(id: "i-olive-oil", key: "olive oil", name: "Olive Oil", category: "pantry"),
        CatalogEntry(id: "i-apples", key: "apples", name: "Apples", category: "produce"),
        CatalogEntry(id: "i-onion", key: "yellow onion", name: "Yellow Onion", category: "produce"),
        CatalogEntry(id: "i-pepper", key: "pepper", name: "Pepper", category: "spices"),
    ]

    /// A short synthetic stand-in for the API's default staples.
    static let defaultStapleKeys = ["salt", "black pepper"]

    /// Deliberately not in aisle order.
    static let sampleItems = [
        Item(id: "item-oil", name: "Olive Oil", category: "pantry", quantity: "3/2", unit: "cup", isStaple: true),
        Item(id: "item-zaatar", name: "Za'atar", category: "spices", status: "out"),
        Item(id: "item-carrots", name: "Carrots", category: "produce", status: "low"),
        Item(id: "item-butter", name: "Butter", category: "dairy-eggs", quantity: "1", unit: "lb"),
    ]

    struct State: Sendable {
        var pantries: [String: [Item]] = [:]
        var nextID = 1
        /// The next this many requests answer `500`.
        var failuresRemaining = 0
        /// `METHOD /path` for every request, in order.
        var log: [String] = []
        /// The item count of every bulk request.
        var bulkSizes: [Int] = []
        var purchases: [Purchase] = []
        /// Household thresholds; 80 when unset.
        var thresholds: [String: Int] = [:]
        /// Purchases answer `403`, as for a member without `pantry.edit`.
        var forbidsPurchases = false
        /// The next purchase is recorded, but its response is a `500`, as if it were lost.
        var losesNextPurchaseResponse = false
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    var bulkSizes: [Int] { state.withLock { $0.bulkSizes } }
    var purchases: [Purchase] { state.withLock { $0.purchases } }

    func items(in householdID: String) -> [Item] {
        state.withLock { $0.pantries[householdID] ?? [] }
    }

    func threshold(for householdID: String) -> Int {
        state.withLock { $0.thresholds[householdID] ?? 80 }
    }

    func failNext(_ count: Int = 1) {
        state.withLock { $0.failuresRemaining = count }
    }

    func forbidPurchases() {
        state.withLock { $0.forbidsPurchases = true }
    }

    func loseNextPurchaseResponse() {
        state.withLock { $0.losesNextPurchaseResponse = true }
    }

    /// Another member deletes an item.
    func remove(_ itemID: String, from householdID: String) {
        state.withLock { $0.pantries[householdID]?.removeAll { $0.id == itemID } }
    }

    /// Another member adds an item.
    func insert(_ item: Item, into householdID: String) {
        state.withLock { $0.pantries[householdID, default: []].append(item) }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let query = Dictionary(
            (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []).map {
                ($0.name, $0.value ?? "")
            },
            uniquingKeysWith: { _, last in last })
        let body = PantryFixtures.body(of: request)

        return state.withLock { state in
            state.log.append("\(method) /\(route.joined(separator: "/"))")
            if state.failuresRemaining > 0 {
                state.failuresRemaining -= 1
                return (500, Fixtures.errorJSON(code: "internal"))
            }
            if route == ["ingredients"] {
                return (200, Self.search(query["q"] ?? "", limit: Int(query["limit"] ?? "") ?? 20))
            }
            guard route.count >= 3, route[0] == "households", route[2] == "pantry" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            let householdID = route[1]
            let rest = Array(route.dropFirst(3))
            let threshold = state.thresholds[householdID] ?? 80
            var items = state.pantries[householdID] ?? []
            defer { state.pantries[householdID] = items }

            switch method {
            case "GET" where rest.isEmpty:
                return (200, Self.list(items, householdID: householdID, threshold: threshold))
            case "POST" where rest.isEmpty:
                return Self.add(body, to: &items, householdID: householdID, nextID: &state.nextID)
            case "POST" where rest == ["bulk"]:
                let entries = body["items"] as? [[String: Any]] ?? []
                state.bulkSizes.append(entries.count)
                return Self.bulk(entries, items: &items, householdID: householdID)
            case "POST" where rest == ["staples", "defaults"]:
                return Self.addDefaults(to: &items, householdID: householdID, nextID: &state.nextID)
            case "POST" where rest == ["purchases"]:
                guard !state.forbidsPurchases else { return (403, Fixtures.errorJSON(code: "forbidden")) }
                return Self.recordPurchase(body, state: &state, items: &items, householdID: householdID)
            case "GET" where rest.count == 2 && rest[1] == "purchases":
                guard items.contains(where: { $0.id == rest[0] }) else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                let history = state.purchases.filter { $0.householdID == householdID && $0.itemID == rest[0] }
                return (200, Data(#"{"items":[\#(history.reversed().map(Self.json).joined(separator: ","))]}"#.utf8))
            case "GET" where rest == ["settings"]:
                return (200, Self.settingsJSON(threshold, changed: state.thresholds[householdID] != nil))
            case "PUT" where rest == ["settings"]:
                guard let percent = body["lowThresholdPercent"] as? Int, (1...100).contains(percent) else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "invalid threshold"))
                }
                state.thresholds[householdID] = percent
                return (200, Self.settingsJSON(percent, changed: true))
            case "PATCH" where rest.count == 1:
                guard let index = items.firstIndex(where: { $0.id == rest[0] }) else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                Self.apply(body, to: &items[index])
                return (200, Data(Self.json(items[index], householdID: householdID, threshold: threshold).utf8))
            case "DELETE" where rest.count == 1:
                guard items.contains(where: { $0.id == rest[0] }) else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                items.removeAll { $0.id == rest[0] }
                return (204, Data())
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    // MARK: - Endpoints

    private static func add(
        _ body: [String: Any], to items: inout [Item], householdID: String, nextID: inout Int
    ) -> (status: Int, body: Data) {
        let byID = (body["ingredientId"] as? String).flatMap { id in catalog.first { $0.id == id } }
        let byName = (body["name"] as? String).flatMap { name in catalog.first { $0.key == name.lowercased() } }
        let entry = byID ?? byName
        guard let name = (body["name"] as? String) ?? entry?.name else {
            return (400, Fixtures.errorJSON(code: "validation_failed", message: "name or ingredientId is required"))
        }
        let key = entry?.key ?? name.lowercased()
        var fields = body
        fields["status"] = body["status"] ?? "in_stock"

        if let index = items.firstIndex(where: { $0.key == key }) {
            apply(fields, to: &items[index])
            return (200, Data(json(items[index], householdID: householdID).utf8))
        }
        var item = Item(
            id: "item-new-\(nextID)", name: name, category: entry?.category ?? (body["category"] as? String) ?? "other",
            ingredientID: entry?.id)
        item.key = key
        nextID += 1
        apply(fields, to: &item)
        items.append(item)
        return (201, Data(json(item, householdID: householdID).utf8))
    }

    /// Applies an add or update body the way the API does.
    private static func apply(_ body: [String: Any], to item: inout Item) {
        if let status = body["status"] as? String {
            item.status = status
            item.statusSource = "person"
        }
        if let quantity = body["quantity"] as? String {
            if quantity.isEmpty {
                item.quantity = nil
                item.unit = nil
            } else {
                item.quantity = quantity
                item.unit = body["unit"] as? String ?? item.unit ?? "count"
            }
        }
        if let isStaple = body["isStaple"] as? Bool {
            item.isStaple = isStaple
        }
        if let expiresOn = body["expiresOn"] as? String {
            item.expiresOn = expiresOn.isEmpty ? nil : expiresOn
        }
        if let note = body["note"] as? String {
            item.note = note
        }
        if let threshold = body["lowThresholdPercent"] {
            // `null` arrives as NSNull and returns the item to the household's threshold.
            item.lowThresholdPercent = threshold as? Int
        }
        if item.status == "out" {
            item.quantity = nil
            item.unit = nil
        }
        if item.quantity == nil {
            item.estimatePercent = nil
        }
    }

    private static func recordPurchase(
        _ body: [String: Any], state: inout State, items: inout [Item], householdID: String
    ) -> (status: Int, body: Data) {
        let threshold = state.thresholds[householdID] ?? 80
        let clientID = body["clientPurchaseId"] as? String
        if let clientID,
            let existing = state.purchases.first(where: {
                $0.householdID == householdID && $0.clientPurchaseID == clientID
            }),
            let item = items.first(where: { $0.id == existing.itemID })
        {
            return (200, purchaseResponse(existing, item, householdID: householdID, threshold: threshold))
        }

        let index: Int
        if let itemID = body["itemId"] as? String {
            guard let found = items.firstIndex(where: { $0.id == itemID }) else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            index = found
        } else {
            let byID = (body["ingredientId"] as? String).flatMap { id in catalog.first { $0.id == id } }
            if body["ingredientId"] != nil, byID == nil {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            guard let name = byID?.name ?? (body["name"] as? String) else {
                return (400, Fixtures.errorJSON(code: "validation_failed", message: "itemId, ingredientId, or name"))
            }
            let key = byID?.key ?? name.lowercased()
            if let found = items.firstIndex(where: { $0.key == key }) {
                index = found
            } else {
                var item = Item(
                    id: "item-new-\(state.nextID)", name: name, category: byID?.category ?? "other",
                    ingredientID: byID?.id)
                item.key = key
                state.nextID += 1
                items.append(item)
                index = items.count - 1
            }
        }

        let quantity = body["quantity"] as? String
        let unit = quantity == nil ? nil : (body["unit"] as? String ?? "count")
        let size = body["unitSize"] as? [String: Any]
        items[index].status = "in_stock"
        items[index].statusSource = "person"
        items[index].quantity = quantity
        items[index].unit = unit
        items[index].estimatePercent = quantity == nil ? nil : 100
        if let size {
            items[index].unitSizeQuantity = size["quantity"] as? String
            items[index].unitSizeUnit = size["unit"] as? String
        }

        let purchase = Purchase(
            id: "purchase-\(state.nextID)", householdID: householdID, itemID: items[index].id,
            source: body["source"] as? String ?? "", ingredientID: body["ingredientId"] as? String,
            name: body["name"] as? String, quantity: quantity, unit: unit,
            unitSizeQuantity: size?["quantity"] as? String, unitSizeUnit: size?["unit"] as? String,
            week: body["week"] as? String, clientPurchaseID: clientID)
        state.nextID += 1
        state.purchases.append(purchase)
        if state.losesNextPurchaseResponse {
            state.losesNextPurchaseResponse = false
            return (500, Fixtures.errorJSON(code: "internal"))
        }
        return (201, purchaseResponse(purchase, items[index], householdID: householdID, threshold: threshold))
    }

    private static func bulk(
        _ entries: [[String: Any]], items: inout [Item], householdID: String
    ) -> (status: Int, body: Data) {
        var updated: [String] = []
        var missing: [String] = []
        for entry in entries {
            guard let id = entry["id"] as? String, let status = entry["status"] as? String else { continue }
            guard let index = items.firstIndex(where: { $0.id == id }) else {
                missing.append("\"\(id)\"")
                continue
            }
            apply(["status": status], to: &items[index])
            updated.append(json(items[index], householdID: householdID))
        }
        return (
            200,
            Data(#"{"items":[\#(updated.joined(separator: ","))],"missing":[\#(missing.joined(separator: ","))]}"#.utf8)
        )
    }

    private static func addDefaults(
        to items: inout [Item], householdID: String, nextID: inout Int
    ) -> (status: Int, body: Data) {
        var added: [String] = []
        var skipped = 0
        for key in defaultStapleKeys {
            guard let entry = catalog.first(where: { $0.key == key }) else { continue }
            if items.contains(where: { $0.key == key }) {
                skipped += 1
                continue
            }
            let item = Item(
                id: "item-new-\(nextID)", name: entry.name, category: entry.category, isStaple: true,
                ingredientID: entry.id)
            nextID += 1
            items.append(item)
            added.append(json(item, householdID: householdID))
        }
        return (200, Data(#"{"items":[\#(added.joined(separator: ","))],"skipped":\#(skipped)}"#.utf8))
    }

    private static func search(_ query: String, limit: Int) -> Data {
        let needle = query.lowercased()
        let matches = catalog.filter { entry in
            entry.key.split(separator: " ").contains { $0.hasPrefix(needle) }
        }
        let items = matches.prefix(limit).map {
            #"{"id":"\#($0.id)","key":"\#($0.key)","name":"\#($0.name)","# + #""category":"\#($0.category)","#
                + #""categoryConfident":true}"#
        }
        return Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    // MARK: - JSON

    private static func string(_ value: String?) -> String {
        value.map { "\"\($0)\"" } ?? "null"
    }

    private static func list(_ items: [Item], householdID: String, threshold: Int) -> Data {
        PantryFixtures.list(items.map { json($0, householdID: householdID, threshold: threshold) })
    }

    private static func settingsJSON(_ percent: Int, changed: Bool) -> Data {
        let updatedBy = changed ? #""\#(Fixtures.user.id)""# : "null"
        let updatedAt = changed ? #""2026-09-15T18:30:00Z""# : "null"
        return Data(
            #"{"lowThresholdPercent":\#(percent),"defaultLowThresholdPercent":80,"updatedBy":\#(updatedBy),"updatedAt":\#(updatedAt)}"#
                .utf8)
    }

    private static func purchaseResponse(
        _ purchase: Purchase, _ item: Item, householdID: String, threshold: Int
    ) -> Data {
        Data(
            #"{"purchase":\#(json(purchase)),"item":\#(json(item, householdID: householdID, threshold: threshold))}"#
                .utf8)
    }

    static func json(_ purchase: Purchase) -> String {
        let value = purchase.quantity.flatMap(quantityValue).map { String($0) } ?? "null"
        let unitSize =
            purchase.unitSizeQuantity.map { quantity in
                #"{"per":\#(string(purchase.unit)),"quantity":"\#(quantity)","quantityValue":\#(quantityValue(quantity) ?? 0),"unit":\#(string(purchase.unitSizeUnit))}"#
            } ?? "null"
        return "{"
            + #""id":"\#(purchase.id)","householdId":"\#(purchase.householdID)","itemId":"\#(purchase.itemID)","#
            + #""source":"\#(purchase.source)","quantity":\#(string(purchase.quantity)),"quantityValue":\#(value),"#
            + #""unit":\#(string(purchase.unit)),"unitSize":\#(unitSize),"week":\#(string(purchase.week)),"#
            + #""clientPurchaseId":\#(string(purchase.clientPurchaseID)),"recordedBy":"\#(Fixtures.user.id)","#
            + #""purchasedAt":"2026-09-15T18:30:00Z""#
            + "}"
    }

    static func json(_ item: Item, householdID: String, threshold: Int = 80) -> String {
        let value = item.quantity.flatMap(quantityValue).map { String($0) } ?? "null"
        let threshold = item.lowThresholdPercent ?? threshold
        var estimate = "null"
        if let percent = item.estimatePercent, let quantity = item.quantity, let unit = item.unit {
            let amount = #"{"quantity":"\#(quantity)","quantityValue":\#(value)}"#
            estimate =
                #"{"cycleId":"cycle-\#(item.id)","cycleSource":"grocery_list","cycleStartedAt":"2026-09-15T18:30:00Z","#
                + #""adjustedAt":null,"unit":"\#(unit)","startAmount":\#(amount),"remaining":\#(amount),"#
                + #""percentRemaining":\#(percent),"percentUsed":\#(100 - percent),"#
                + #""recipeUse":{"count":0,"quantity":"0","quantityValue":0},"#
                + #""otherUse":{"quantity":"0","quantityValue":0},"dailyRate":null,"skippedRecipes":0,"#
                + #""lowThresholdPercent":\#(threshold),"#
                + #""thresholdSource":"\#(item.lowThresholdPercent == nil ? "household" : "item")","#
                + #""belowThreshold":false,"summary":"About \#(percent)% left.","estimatedAt":"2026-09-15T18:30:00Z"}"#
        }
        let unitSize =
            item.unitSizeQuantity.map { quantity in
                #"{"per":\#(string(item.unit)),"quantity":"\#(quantity)","quantityValue":\#(quantityValue(quantity) ?? 0),"unit":\#(string(item.unitSizeUnit))}"#
            } ?? "null"
        return "{"
            + #""id":"\#(item.id)","householdId":"\#(householdID)","ingredientId":\#(string(item.ingredientID)),"#
            + #""key":"\#(item.key)","displayName":"\#(item.name)","category":"\#(item.category)","#
            + #""quantity":\#(string(item.quantity)),"quantityValue":\#(value),"unit":\#(string(item.unit)),"#
            + #""status":"\#(item.status)","isStaple":\#(item.isStaple),"expiresOn":\#(string(item.expiresOn)),"#
            + #""note":"\#(item.note)","statusSource":"\#(item.statusSource)","#
            + #""lowThresholdPercent":\#(item.lowThresholdPercent.map(String.init) ?? "null"),"#
            + #""unitSize":\#(unitSize),"estimate":\#(estimate),"updatedBy":"\#(Fixtures.user.id)","#
            + #""createdAt":"2026-09-15T18:30:00Z","updatedAt":"2026-09-15T18:30:00Z""#
            + "}"
    }

    private static func quantityValue(_ quantity: String) -> Double? {
        let parts = quantity.split(separator: "/").compactMap { Double($0) }
        switch parts.count {
        case 1: return parts[0]
        case 2: return parts[0] / parts[1]
        default: return nil
        }
    }
}
