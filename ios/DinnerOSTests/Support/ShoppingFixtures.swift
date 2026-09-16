import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's shopping responses. Item IDs are made up.
nonisolated enum ShoppingFixtures {
    static let providersJSON = Data(
        #"""
        {"items":[
          {"key":"walmart","name":"Walmart","affiliateTracked":false,
           "capabilities":{"handoff":"cart_link","pasteProductLink":true,"storeId":true,"productSearch":false,
                           "productLookup":false,"storeFinder":false,"cartWrite":false,"orderImport":false}}
        ]}
        """#.utf8)

    static func settingsJSON(provider: String?, storeID: String?) -> Data {
        let changed = provider != nil
        return Data(
            #"""
            {"provider":\#(string(provider)),"storeId":\#(string(storeID)),
             "updatedBy":\#(changed ? #""\#(Fixtures.user.id)""# : "null"),
             "updatedAt":\#(changed ? #""2026-09-15T19:04:46.249Z""# : "null")}
            """#.utf8)
    }

    static func amount(_ quantity: String, _ unit: String, text: String? = nil) -> String {
        let value = quantityValue(quantity)
        let display = text ?? "\(quantity) \(unit)"
        return #"{"quantity":"\#(quantity)","quantityValue":\#(value),"unit":"\#(unit)","text":"\#(display)"}"#
    }

    static func preferenceJSON(
        key: String, ingredientName: String, productID: String, displayName: String, size: String? = nil
    ) -> String {
        let ingredientID = key.hasPrefix("name:") ? "null" : #""\#(key)""#
        return #"""
            {"id":"pref-\#(productID)","provider":"walmart","ingredientKey":"\#(key)","ingredientId":\#(ingredientID),
             "ingredientName":"\#(ingredientName)","productId":"\#(productID)",
             "productUrl":"https://www.walmart.com/ip/\#(productID)","displayName":"\#(displayName)",
             "packageSize":\#(size ?? "null"),"createdBy":"\#(Fixtures.user.id)",
             "createdAt":"2026-09-15T19:04:46.269Z","updatedBy":"\#(Fixtures.user.id)",
             "updatedAt":"2026-09-15T19:04:46.269Z"}
            """#
    }

    /// The search the API suggests for a line. Produce is the interesting case: the bare
    /// ingredient name is the wrong search, so the qualifiers go in front of it.
    static func searchTermsJSON(
        _ name: String, qualifiers: [String] = [], avoid: [String] = [], why: String = ""
    ) -> String {
        let quoted = { (words: [String]) -> String in words.map { #""\#($0)""# }.joined(separator: ",") }
        let query = (qualifiers + [name]).joined(separator: " ")
        return #"""
            {"query":"\#(query)","qualifiers":[\#(quoted(qualifiers))],"avoid":[\#(quoted(avoid))],"why":"\#(why)"}
            """#
    }

    static func confirmationJSON(status: String, packages: Int? = nil) -> String {
        let confirmed = status == "confirmed"
        let skipped = status == "skipped"
        return #"""
            {"status":"\#(status)","packages":\#(packages.map(String.init) ?? "null"),
             "purchaseId":\#(confirmed ? #""purchase-1""# : "null"),
             "confirmedBy":\#(confirmed ? #""\#(Fixtures.user.id)""# : "null"),
             "confirmedAt":\#(confirmed ? #""2026-09-15T20:00:00Z""# : "null"),
             "skippedBy":\#(skipped ? #""\#(Fixtures.user.id)""# : "null"),
             "skippedAt":\#(skipped ? #""2026-09-15T20:00:00Z""# : "null")}
            """#
    }

    static func lineJSON(
        id: String, key: String, name: String, category: String = "produce", quantityText: String = "1",
        productID: String, displayName: String, size: String?, computed: Int, packages: Int? = nil,
        reason: String? = nil, reasonText: String? = nil, coverage: String = "",
        coverageRule: String = "per_amount", coversWeek: Bool = false, searchTerms: String? = nil,
        confirmation: String? = nil
    ) -> String {
        let ingredientID = key.hasPrefix("name:") ? "null" : #""\#(key)""#
        let count = packages ?? computed
        return #"""
            {"id":"\#(id)","ingredientKey":"\#(key)","ingredientId":\#(ingredientID),"name":"\#(name)",
             "category":"\#(category)","amounts":[],"quantityText":"\#(quantityText)","unquantified":false,
             "groceryStatus":"toBuy",
             "product":{"productId":"\#(productID)","displayName":"\#(displayName)",
                        "productUrl":"https://www.walmart.com/ip/\#(productID)","packageSize":\#(size ?? "null")},
             "computedPackages":\#(computed),"packages":\#(count),"packagesOverridden":\#(packages != nil),
             "checkAmount":\#(reason != nil),"reason":\#(string(reason)),"reasonText":\#(string(reasonText)),
             "coverageText":"\#(coverage)","coverage":"\#(coverageRule)","coversWeek":\#(coversWeek),
             "searchTerms":\#(searchTerms ?? searchTermsJSON(name)),
             "confirmation":\#(confirmation ?? "null")}
            """#
    }

    static func excludedJSON(
        key: String, name: String, reason: String, text: String, category: String = "produce",
        groceryStatus: String? = "toBuy", searchTerms: String? = nil
    ) -> String {
        let ingredientID = key.hasPrefix("name:") ? "null" : #""\#(key)""#
        return #"""
            {"ingredientKey":"\#(key)","ingredientId":\#(ingredientID),"name":"\#(name)","category":"\#(category)",
             "amounts":[],"quantityText":"","unquantified":false,"groceryStatus":\#(string(groceryStatus)),
             "reason":"\#(reason)","text":"\#(text)",
             "searchTerms":\#(searchTerms ?? searchTermsJSON(name))}
            """#
    }

    static func linkJSON(url: String, lineIDs: [String]) -> String {
        let ids = lineIDs.map { #""\#($0)""# }.joined(separator: ",")
        return #"{"url":"\#(url)","lineIds":[\#(ids)],"itemCount":\#(lineIDs.count)}"#
    }

    static func proposalFields(
        week: String = "2026-W38", storeID: String? = "5435", lines: [String], excluded: [String], links: [String],
        affiliateTracked: Bool = false
    ) -> String {
        #"""
        "provider":"walmart","week":"\#(week)","storeId":\#(string(storeID)),
        "lines":[\#(lines.joined(separator: ","))],"excluded":[\#(excluded.joined(separator: ","))],
        "cartLinks":[\#(links.joined(separator: ","))],"affiliateTracked":\#(affiliateTracked)
        """#
    }

    static func handoffJSON(id: String, status: String, fields: String) -> String {
        #"""
        {"id":"\#(id)","status":"\#(status)",\#(fields),"createdBy":"\#(Fixtures.user.id)",
         "createdAt":"2026-09-15T18:30:00Z","updatedAt":"2026-09-15T18:30:00.123456789Z"}
        """#
    }

    /// A match with every line state: a plain line, each check-amount reason, and every exclusion.
    static let everyStateProposal = Data(
        ("{"
            + proposalFields(
                storeID: "5435",
                lines: [
                    lineJSON(
                        id: "l1", key: "i-beef", name: "Ground Beef", category: "meat-seafood",
                        quantityText: "2 ¼ lb", productID: "100000001", displayName: "Test Brand ground beef",
                        size: amount("16", "oz"), computed: 3, coverage: "3 × 16 oz covers 36 oz"),
                    lineJSON(
                        id: "l2", key: "i-garlic", name: "Garlic", quantityText: "4 cloves", productID: "100000002",
                        displayName: "Test garlic", size: amount("1", "count", text: "1 ct"), computed: 1,
                        coverage: "1 × 1 ct covers this week (4 cloves)", coverageRule: "per_week", coversWeek: true,
                        searchTerms: searchTermsJSON(
                            "Garlic", qualifiers: ["fresh", "whole"], avoid: ["powder", "minced", "dried"],
                            why: "Produce: the fresh whole item, not a dried, powdered or prepared form.")),
                    lineJSON(
                        id: "l3", key: "i-beans", name: "Green Beans", productID: "100000003",
                        displayName: "Test green beans", size: nil, computed: 1, reason: "no_package_size",
                        reasonText: "Check amount: no package size saved", coverage: "1 package"),
                    lineJSON(
                        id: "l4", key: "i-rice", name: "Rice", category: "pantry", quantityText: "200 cup",
                        productID: "100000004", displayName: "Test rice", size: amount("1", "cup"), computed: 99,
                        packages: 12, reason: "package_count_capped", reasonText: "Check amount: capped at 99",
                        coverage: "12 × 1 cup covers 12 cup"),
                ],
                excluded: [
                    excludedJSON(
                        key: "name:flour tortillas", name: "Flour Tortillas", reason: "no_product",
                        text: "Choose a Walmart product", category: "bakery"),
                    excludedJSON(
                        key: "i-butter", name: "Butter", reason: "in_pantry", text: "In your pantry",
                        groceryStatus: "inPantry"),
                    excludedJSON(
                        key: "i-salt", name: "Salt", reason: "pantry_hint", text: "Probably at home",
                        groceryStatus: "pantryHint"),
                    excludedJSON(key: "i-stock", name: "Stock", reason: "house_made", text: "House-made"),
                    excludedJSON(key: "i-lime", name: "Lime", reason: "checked_off", text: "Checked off"),
                    excludedJSON(key: "i-onion", name: "Onion", reason: "excluded", text: "Left out"),
                    excludedJSON(key: "i-kale", name: "Kale", reason: "not_selected", text: "Not selected"),
                    excludedJSON(
                        key: "i-gone", name: "", reason: "not_on_list", text: "Not on the list", groceryStatus: nil),
                ],
                links: [
                    linkJSON(
                        url: "https://www.walmart.com/sc/cart/addToCart?items=100000001_3,100000002&storeId=5435",
                        lineIDs: ["l1", "l2"]),
                    linkJSON(
                        url: "https://www.walmart.com/sc/cart/addToCart?items=100000003,100000004_12&storeId=5435",
                        lineIDs: ["l3", "l4"]),
                ])
            + "}").utf8)

    static func string(_ value: String?) -> String {
        value.map { #""\#($0)""# } ?? "null"
    }

    static func quantityValue(_ quantity: String) -> String {
        let parts = quantity.split(separator: "/").compactMap { Double($0) }
        switch parts.count {
        case 1: return String(parts[0])
        case 2: return String(parts[0] / parts[1])
        default: return "0"
        }
    }

    static func body(of request: URLRequest) -> [String: Any] {
        PantryFixtures.body(of: request)
    }
}

/// An in-memory stand-in for the shopping endpoints with the API's matching rules, so store
/// tests exercise real state transitions through `APIClient` and `AuthSession`.
nonisolated final class FakeShoppingServer: Sendable {
    struct GroceryLine: Sendable {
        var key: String
        var name: String
        var category: String
        /// `toBuy`, `inPantry`, or `pantryHint`.
        var status = "toBuy"
        /// The package count the API would compute for the saved product.
        var computed = 1
    }

    struct Product: Sendable {
        var productID: String
        var displayName: String
        var ingredientName: String
        var sizeQuantity: String?
        var sizeUnit: String?
    }

    struct Line: Sendable {
        var id: String
        var key: String
        var packages: Int
        var status = "pending"
        var confirmedPackages: Int?
    }

    struct Handoff: Sendable {
        var id: String
        var week: String
        var lines: [Line]
        var excluded: [String]
        var isOpen: Bool { lines.contains { $0.status == "pending" } }
    }

    struct State: Sendable {
        var householdID = "household-1"
        var provider: String? = "walmart"
        var storeID: String? = "5435"
        var grocery: [GroceryLine] = FakeShoppingServer.sampleGrocery
        /// Saved products by ingredient key.
        var products: [String: Product] = FakeShoppingServer.sampleProducts
        /// Products in one cart link before another starts.
        var productsPerLink = 40
        var handoffs: [Handoff] = []
        var nextID = 1
        /// The household's grocery order day, or nil for no reminder.
        var orderDay: String? = "thu"
        /// Whether the order day has arrived. The real API derives this from today's date in
        /// the household's time zone; the fake states it so tests don't depend on the clock.
        var orderDayArrived = true
        /// Weeks a member marked ordered.
        var orderedWeeks: Set<String> = []
        /// The next this many confirmations answer `409 conflict`.
        var conflictsRemaining = 0
        /// `METHOD /path` (percent-encoded as sent) for every request, in order.
        var log: [String] = []
        /// The JSON body of every request that had one, by `METHOD /path`.
        var bodies: [String: [Data]] = [:]
    }

    /// Ground beef and cilantro have saved products; tortillas don't; salt and butter are at home.
    static let sampleGrocery = [
        GroceryLine(key: "i-beef", name: "Ground Beef", category: "meat-seafood", computed: 3),
        GroceryLine(key: "i-cilantro", name: "Cilantro", category: "produce"),
        GroceryLine(key: "name:flour tortillas", name: "Flour Tortillas", category: "bakery"),
        GroceryLine(key: "i-salt", name: "Salt", category: "spices", status: "pantryHint"),
        GroceryLine(key: "i-butter", name: "Butter", category: "dairy-eggs", status: "inPantry"),
    ]

    static let sampleProducts = [
        "i-beef": Product(
            productID: "100000001", displayName: "Test Brand ground beef", ingredientName: "Ground Beef",
            sizeQuantity: "16", sizeUnit: "oz"),
        "i-cilantro": Product(
            productID: "100000002", displayName: "Test cilantro", ingredientName: "Cilantro", sizeQuantity: "1",
            sizeUnit: "count"),
    ]

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    var handoffs: [Handoff] { state.withLock { $0.handoffs } }
    var products: [String: Product] { state.withLock { $0.products } }

    /// JSON bodies sent to `route` (`METHOD /path`), in order. `[String: Any]` isn't Sendable,
    /// so it's rebuilt from the recorded data.
    func bodies(_ route: String) -> [[String: Any]] {
        let recorded = state.withLock { $0.bodies[route] ?? [] }
        return recorded.map { (try? JSONSerialization.jsonObject(with: $0) as? [String: Any]) ?? [:] }
    }

    func update(_ change: @Sendable (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    /// The week's order state, shaped like the API's derived response.
    private static func orderReminderJSON(_ state: State, week: String) -> Data {
        let ordered = state.orderedWeeks.contains(week)
        let due = state.orderDay != nil && state.orderDayArrived
        let remind = due && !ordered
        let day = state.orderDay.map { #""\#($0)""# } ?? "null"
        let dueOn = state.orderDay == nil ? "null" : #""2026-09-17""#
        return Data(
            #"""
            {"week":"\#(week)","orderDay":\#(day),"dueOn":\#(dueOn),"due":\#(due),"remind":\#(remind),
             "ordered":\#(ordered),"orderedBy":\#(ordered ? #""\#(Fixtures.user.id)""# : "null"),
             "orderedAt":\#(ordered ? #""2026-09-17T18:00:00Z""# : "null")}
            """#.utf8)
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let queryItems = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let query = Dictionary(queryItems.map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { _, last in last })
        let body = ShoppingFixtures.body(of: request)
        let routeKey = "\(method) /\(route.joined(separator: "/"))"

        return state.withLock { state in
            state.log.append(routeKey + (query.isEmpty ? "" : "?" + (url.query(percentEncoded: true) ?? "")))
            if let data = request.httpBody {
                state.bodies[routeKey, default: []].append(data)
            }
            if route == ["shopping", "providers"], method == "GET" {
                return (200, ShoppingFixtures.providersJSON)
            }
            guard route.count >= 3, route[0] == "households", route[1] == state.householdID else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            let rest = Array(route.dropFirst(2))
            switch (method, rest.count) {
            case ("GET", 2) where rest == ["shopping", "settings"]:
                return (200, ShoppingFixtures.settingsJSON(provider: state.provider, storeID: state.storeID))
            case ("PUT", 2) where rest == ["shopping", "settings"]:
                state.provider = body["provider"] as? String
                state.storeID = body["storeId"] as? String
                return (200, ShoppingFixtures.settingsJSON(provider: state.provider, storeID: state.storeID))
            case ("GET", 3) where rest[0] == "shopping" && rest[2] == "preferences":
                let items = state.products.sorted { $0.value.ingredientName < $1.value.ingredientName }.map {
                    Self.preferenceJSON(key: $0.key, product: $0.value)
                }
                return (200, Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8))
            case ("PUT", 4) where rest[0] == "shopping" && rest[2] == "preferences":
                return Self.savePreference(body, key: rest[3].removingPercentEncoding ?? rest[3], state: &state)
            case ("DELETE", 4) where rest[0] == "shopping" && rest[2] == "preferences":
                let key = rest[3].removingPercentEncoding ?? rest[3]
                guard state.products.removeValue(forKey: key) != nil else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                return (204, Data())
            case ("GET", 4) where rest[0] == "shopping" && rest[1] == "weeks" && rest[3] == "order":
                return (200, Self.orderReminderJSON(state, week: rest[2]))
            case ("PUT", 4) where rest[0] == "shopping" && rest[1] == "weeks" && rest[3] == "order":
                let week = rest[2]
                if body["ordered"] as? Bool == true {
                    state.orderedWeeks.insert(week)
                } else {
                    state.orderedWeeks.remove(week)
                }
                return (200, Self.orderReminderJSON(state, week: week))
            case ("POST", 5) where rest[0] == "plans" && rest[2] == "shopping" && rest[4] == "match":
                let (lines, excluded) = Self.match(body, state: &state, numbering: false)
                return (200, Data(Self.proposalJSON(state, week: rest[1], lines: lines, excluded: excluded).utf8))
            case ("POST", 5) where rest[0] == "plans" && rest[2] == "shopping" && rest[4] == "handoffs":
                let (lines, excluded) = Self.match(body, state: &state, numbering: true)
                guard !lines.isEmpty else {
                    return (400, Fixtures.errorJSON(code: "validation_failed", message: "no line to hand off"))
                }
                let handoff = Handoff(id: "handoff-\(state.nextID)", week: rest[1], lines: lines, excluded: excluded)
                state.nextID += 1
                state.handoffs.append(handoff)
                return (201, Data(Self.handoffJSON(state, handoff).utf8))
            case ("GET", 2) where rest == ["shopping", "handoffs"]:
                let items = state.handoffs.reversed().filter { handoff in
                    (query["week"].map { $0 == handoff.week } ?? true)
                        && (query["status"].map { ($0 == "open") == handoff.isOpen } ?? true)
                }
                let json = items.map { Self.handoffJSON(state, $0) }.joined(separator: ",")
                return (200, Data(#"{"items":[\#(json)]}"#.utf8))
            case ("POST", 4) where rest[0] == "shopping" && rest[1] == "handoffs" && rest[3] == "confirm":
                if state.conflictsRemaining > 0 {
                    state.conflictsRemaining -= 1
                    return (409, Fixtures.errorJSON(code: "conflict"))
                }
                return Self.confirm(body, handoffID: rest[2], state: &state)
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    // MARK: - Rules

    /// Candidates are `toBuy` lines, or exactly the selected lines; checked-off keys are left out.
    private static func match(
        _ body: [String: Any], state: inout State, numbering: Bool
    ) -> (lines: [Line], excluded: [String]) {
        let checked = Set(body["checkedOffKeys"] as? [String] ?? [])
        let selection = body["lines"] as? [[String: Any]]
        let selectedKeys = selection.map { Set($0.compactMap { $0["ingredientKey"] as? String }) }
        var overrides: [String: Int] = [:]
        for entry in selection ?? [] {
            if let key = entry["ingredientKey"] as? String, let packages = entry["packages"] as? Int {
                overrides[key] = packages
            }
        }
        var lines: [Line] = []
        var excluded: [String] = []
        for grocery in state.grocery {
            let exclusion: (String, String)?
            if checked.contains(grocery.key) {
                exclusion = ("checked_off", "Checked off")
            } else if let selectedKeys, !selectedKeys.contains(grocery.key) {
                exclusion = ("not_selected", "Not selected")
            } else if selectedKeys == nil, grocery.status == "inPantry" {
                exclusion = ("in_pantry", "In your pantry")
            } else if selectedKeys == nil, grocery.status == "pantryHint" {
                exclusion = ("pantry_hint", "Probably at home")
            } else if state.products[grocery.key] == nil {
                exclusion = ("no_product", "Choose a Walmart product")
            } else {
                exclusion = nil
            }
            if let (reason, text) = exclusion {
                excluded.append(
                    ShoppingFixtures.excludedJSON(
                        key: grocery.key, name: grocery.name, reason: reason, text: text, category: grocery.category,
                        groceryStatus: grocery.status))
            } else {
                let packages = overrides[grocery.key] ?? grocery.computed
                lines.append(Line(id: "l\(lines.count + 1)", key: grocery.key, packages: packages))
            }
        }
        return (lines, excluded)
    }

    private static func savePreference(
        _ body: [String: Any], key: String, state: inout State
    ) -> (status: Int, body: Data) {
        let productID: String
        if let url = body["productUrl"] as? String {
            guard let id = ProductLink.walmartItemID(inURL: url) else {
                return (
                    400,
                    Fixtures.errorJSON(
                        code: "validation_failed", message: "productUrl must be a walmart.com/ip product link")
                )
            }
            productID = id
        } else if let id = body["productId"] as? String {
            productID = id
        } else {
            return (400, Fixtures.errorJSON(code: "validation_failed", message: "productUrl or productId is required"))
        }
        let size = body["packageSize"] as? [String: Any]
        let grocery = state.grocery.first { $0.key == key }
        let product = Product(
            productID: productID, displayName: body["displayName"] as? String ?? "",
            ingredientName: body["ingredientName"] as? String ?? grocery?.name ?? key,
            sizeQuantity: size?["quantity"] as? String, sizeUnit: size?["unit"] as? String)
        let created = state.products[key] == nil
        state.products[key] = product
        return (created ? 201 : 200, Data(preferenceJSON(key: key, product: product).utf8))
    }

    private static func confirm(
        _ body: [String: Any], handoffID: String, state: inout State
    ) -> (status: Int, body: Data) {
        guard let index = state.handoffs.firstIndex(where: { $0.id == handoffID }) else {
            return (404, Fixtures.errorJSON(code: "not_found"))
        }
        var handoff = state.handoffs[index]
        var requested: [String: Int?] = [:]
        if body["all"] as? Bool == true {
            for line in handoff.lines where line.status != "skipped" {
                requested[line.id] = .some(nil)
            }
        }
        for entry in body["lines"] as? [[String: Any]] ?? [] {
            guard let lineID = entry["lineId"] as? String else { continue }
            requested[lineID] = .some(entry["packages"] as? Int)
        }
        var purchases: [String] = []
        for lineIndex in handoff.lines.indices {
            var line = handoff.lines[lineIndex]
            if let packages = requested[line.id] {
                let created = line.status != "confirmed"
                if created {
                    line.status = "confirmed"
                    line.confirmedPackages = packages ?? line.packages
                }
                purchases.append(
                    purchaseJSON(line: line, handoffID: handoff.id, week: handoff.week, created: created, state: state))
            } else if body["skipRest"] as? Bool == true, line.status == "pending" {
                line.status = "skipped"
            }
            handoff.lines[lineIndex] = line
        }
        state.handoffs[index] = handoff
        let json = #"{"handoff":\#(handoffJSON(state, handoff)),"purchases":[\#(purchases.joined(separator: ","))]}"#
        return (200, Data(json.utf8))
    }

    // MARK: - JSON

    private static func preferenceJSON(key: String, product: Product) -> String {
        ShoppingFixtures.preferenceJSON(
            key: key, ingredientName: product.ingredientName, productID: product.productID,
            displayName: product.displayName, size: sizeJSON(product))
    }

    private static func sizeJSON(_ product: Product?) -> String? {
        guard let product, let quantity = product.sizeQuantity, let unit = product.sizeUnit else { return nil }
        return ShoppingFixtures.amount(quantity, unit)
    }

    /// Stored handoff lines carry a confirmation; match lines have `null`.
    private static func lineJSON(_ line: Line, state: State, stored: Bool) -> String {
        let grocery = state.grocery.first { $0.key == line.key }
        let product = state.products[line.key]
        let confirmation =
            stored ? ShoppingFixtures.confirmationJSON(status: line.status, packages: line.confirmedPackages) : nil
        return ShoppingFixtures.lineJSON(
            id: line.id, key: line.key, name: grocery?.name ?? line.key, category: grocery?.category ?? "other",
            productID: product?.productID ?? "0", displayName: product?.displayName ?? "",
            size: sizeJSON(product), computed: grocery?.computed ?? 1,
            packages: line.packages == grocery?.computed ? nil : line.packages,
            coverage: "\(line.packages) packages", confirmation: confirmation)
    }

    private static func linksJSON(_ lines: [Line], state: State) -> [String] {
        let size = max(state.productsPerLink, 1)
        return stride(from: 0, to: lines.count, by: size).map { start in
            let chunk = Array(lines[start..<min(start + size, lines.count)])
            let items = chunk.map { line -> String in
                let id = state.products[line.key]?.productID ?? "0"
                return line.packages > 1 ? "\(id)_\(line.packages)" : id
            }
            let store = state.storeID.map { "&storeId=\($0)" } ?? ""
            return ShoppingFixtures.linkJSON(
                url: "https://www.walmart.com/sc/cart/addToCart?items=\(items.joined(separator: ","))\(store)",
                lineIDs: chunk.map(\.id))
        }
    }

    private static func proposalJSON(_ state: State, week: String, lines: [Line], excluded: [String]) -> String {
        let lineJSON = lines.map { Self.lineJSON($0, state: state, stored: false) }
        return "{"
            + ShoppingFixtures.proposalFields(
                week: week, storeID: state.storeID, lines: lineJSON, excluded: excluded,
                links: linksJSON(lines, state: state))
            + "}"
    }

    private static func handoffJSON(_ state: State, _ handoff: Handoff) -> String {
        ShoppingFixtures.handoffJSON(
            id: handoff.id, status: handoff.isOpen ? "open" : "done",
            fields: ShoppingFixtures.proposalFields(
                week: handoff.week, storeID: state.storeID,
                lines: handoff.lines.map { lineJSON($0, state: state, stored: true) },
                excluded: handoff.excluded, links: linksJSON(handoff.lines, state: state)))
    }

    private static func purchaseJSON(
        line: Line, handoffID: String, week: String, created: Bool, state: State
    ) -> String {
        let product = state.products[line.key]
        let packages = line.confirmedPackages ?? line.packages
        return #"""
            {"lineId":"\#(line.id)","ingredientKey":"\#(line.key)","created":\#(created),
             "purchase":{"id":"purchase-\#(handoffID)-\#(line.id)","householdId":"\#(state.householdID)",
               "itemId":"item-\#(line.key)","source":"provider","quantity":"\#(packages)",
               "quantityValue":\#(packages),"unit":"package","unitSize":null,"week":"\#(week)",
               "clientPurchaseId":null,"recordedBy":"\#(Fixtures.user.id)","purchasedAt":"2026-09-15T20:00:00Z",
               "provider":{"key":"walmart","handoffId":"\#(handoffID)","lineId":"\#(line.id)",
                           "productId":"\#(product?.productID ?? "0")"}},
             "item":\#(PantryFixtures.butterUsageJSON)}
            """#
    }
}
