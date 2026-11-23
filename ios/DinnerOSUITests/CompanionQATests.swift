import XCTest

/// Companions (decisions 715-719): make one that goes with Mexican meals and is only
/// suggested, plan a Mexican dinner, see the suggestion, add it, find it on the grocery list,
/// then delete the companion.
///
/// The dinner (next week, so the Menu shows its Grocery List button) is seeded through the local API (development sign-in, local Mongo only) with a
/// random name, so eight simulators can run this at once against one API. The companion has a
/// random name too, and stale ones from runs that died are cleared first.
final class CompanionQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testCompanionIsSuggestedAddedAndListed() async throws {
        let run = Int.random(in: 10000...99999)
        let dinner = "Black Bean Chili \(run)"
        let companion = "Tortilla Chips \(run)"
        let seed = try await CompanionSeed.prepare(dinner: dinner)

        let app = launchSignedIn(as: seed.own)
        // The dinner is next week, whose Menu has the Grocery List button.
        let next = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Next Week'")).firstMatch
        XCTAssertTrue(next.waitForExistence(timeout: 10), "No Next Week on the Menu")
        next.tap()

        // 1. Companions, from the Menu's ⋯ menu.
        app.buttons["Week Menu"].tap()
        let companionsItem = app.buttons["Companions"].firstMatch
        XCTAssertTrue(companionsItem.waitForExistence(timeout: 5), "No Companions in the Week Menu")
        companionsItem.tap()
        XCTAssertTrue(app.navigationBars["Companions"].waitForExistence(timeout: 10), "Companions didn’t open")
        snap(app, "Companions › List (\(mode))")

        // 2. A new companion: a name, the dinner's cuisine, Just Suggest It.
        app.buttons["Add a Companion"].firstMatch.tap()
        let name = app.textFields["CompanionName"]
        XCTAssertTrue(name.waitForExistence(timeout: 10), "The companion editor didn’t open")
        name.tap()
        name.typeText(companion)
        let cuisine = app.buttons[seed.cuisine].firstMatch
        reveal(cuisine, in: app)
        XCTAssertTrue(cuisine.waitForExistence(timeout: 10), "No \(seed.cuisine) chip")
        cuisine.tap()
        let suggest = app.buttons["Just Suggest It"].firstMatch
        reveal(suggest, in: app)
        if suggest.exists { suggest.tap() }
        snap(app, "Companions › New (\(mode))")
        app.buttons["CompanionSave"].tap()
        XCTAssertTrue(waitGone(app.buttons["CompanionSave"], timeout: 10), "The companion didn’t save")
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", companion)).firstMatch
        _ = row.waitForExistence(timeout: 10)
        reveal(row, in: app)
        XCTAssertTrue(row.exists, "\(companion) isn’t in the list")

        // 3. Back on the Menu, the Mexican dinner offers it.
        app.navigationBars.buttons.firstMatch.tap()
        let suggestion = app.descendants(matching: .any).matching(identifier: "CompanionSuggestion")
            .matching(NSPredicate(format: "label CONTAINS %@ AND label CONTAINS %@", companion, dinner)).firstMatch
        // The week reloads its suggestions after the save.
        _ = suggestion.waitForExistence(timeout: 5)
        reveal(suggestion, in: app, maxSwipes: 12)
        XCTAssertTrue(suggestion.waitForExistence(timeout: 5), "No suggestion for \(companion) with \(dinner)")
        snap(app, "Companions › Suggestion (\(mode))")
        suggestion.buttons["Add"].tap()
        XCTAssertTrue(waitGone(suggestion, timeout: 10), "The suggestion stayed after Add")

        // 4. It's on the week's grocery list.
        let list = app.buttons["Grocery List"].firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 10))
        list.tap()
        XCTAssertTrue(app.navigationBars["Grocery List"].waitForExistence(timeout: 10))
        let item = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", companion)).firstMatch
        reveal(item, in: app, maxSwipes: 12)
        XCTAssertTrue(item.waitForExistence(timeout: 10), "\(companion) isn’t on the grocery list")
        snap(app, "Companions › On the grocery list (\(mode))")
        app.navigationBars.buttons.firstMatch.tap()

        // 5. Delete it.
        app.buttons["Week Menu"].tap()
        app.buttons["Companions"].firstMatch.tap()
        _ = row.waitForExistence(timeout: 10)
        reveal(row, in: app)
        XCTAssertTrue(row.exists, "\(companion) isn’t in the list to delete")
        row.tap()
        let delete = app.buttons["CompanionDelete"]
        reveal(delete, in: app)
        XCTAssertTrue(delete.waitForExistence(timeout: 10), "No Delete Companion")
        delete.tap()
        let confirm = app.buttons.matching(NSPredicate(format: "label == 'Delete'")).allElementsBoundByIndex
            .first { $0.isHittable }
        XCTAssertNotNil(confirm, "No Delete in the confirmation")
        confirm?.tap()
        XCTAssertTrue(waitGone(row, timeout: 10), "\(companion) is still in the list")
        snap(app, "Companions › Deleted (\(mode))")

        try await seed.cleanUp()
    }
}

/// Seeds the dinner the test plans, through the development API the app uses.
private struct CompanionSeed {
    /// The test's own household (QAOwnHousehold): planning in the shared test household changed
    /// the Menu the screenshot tests were photographing (build 122).
    let own: QAOwnHousehold
    let api: URL
    let token: String
    let householdID: String
    let recipeID: String
    let week: String
    let entryID: String
    /// The cuisine's label, as its chip shows it.
    let cuisine: String

    static func prepare(dinner: String) async throws -> CompanionSeed {
        let api = QAOwnHousehold.apiBase
        let own = try await QAOwnHousehold.make("companion")
        let token = own.token
        let householdID = own.householdID

        // A meal shows at most three suggestions. Companions from runs that died (older than
        // any run can be) are cleared, and the cuisine is the one other runs use least, so
        // runs side by side don't crowd each other's dinner.
        let companions = try await call(api, "GET", "households/\(householdID)/companions", token: token)
        let stale = Date.now.addingTimeInterval(-12 * 60)
        var used: [String: Int] = [:]
        for c in companions["items"] as? [[String: Any]] ?? [] {
            guard let id = c["id"] as? String, (c["name"] as? String)?.hasPrefix("Tortilla Chips ") == true,
                let created = (c["createdAt"] as? String).flatMap(parseDate)
            else { continue }
            if created < stale {
                _ = try? await call(api, "DELETE", "households/\(householdID)/companions/\(id)", token: token)
            } else {
                for v in c["cuisines"] as? [String] ?? [] { used[v, default: 0] += 1 }
            }
        }
        let vocabulary = try await call(api, "GET", "households/\(householdID)/autopilot/vocabulary", token: token)
        let known = (vocabulary["cuisines"] as? [[String: Any]] ?? []).compactMap { option -> (String, String)? in
            guard let value = option["value"] as? String, let label = option["label"] as? String else { return nil }
            return (value, label)
        }
        let preferred = ["mexican", "italian", "indian", "thai", "chinese", "japanese", "greek", "french", "korean"]
        let choices = known.filter { preferred.contains($0.0) }.shuffled()
        let pick = choices.min { used[$0.0, default: 0] < used[$1.0, default: 0] } ?? ("mexican", "Mexican")

        let recipe = try await call(
            api, "POST", "households/\(householdID)/recipes", token: token,
            body: [
                "name": dinner, "servings": 2, "cuisines": [pick.1], "tags": [String](), "warnings": [String](),
                "ingredients": [["name": "Black beans", "quantity": "1"]], "steps": ["Warm the beans."],
                "fromUrl": false,
            ])
        let recipeID = try XCTUnwrap(recipe["id"] as? String)
        let week = own.week(weeksAhead: 1)
        // The shared test household's next week fills up over many runs (other tests plan there
        // too) and a week holds at most 50 dinners: make room by taking off the oldest.
        if let current = try? await call(api, "GET", "households/\(householdID)/plans/\(week)", token: token) {
            let entries = ((current["plan"] as? [String: Any]) ?? current)["entries"] as? [[String: Any]] ?? []
            if entries.count > 40 {
                let oldest = entries.sorted { ($0["addedAt"] as? String ?? "") < ($1["addedAt"] as? String ?? "") }
                for entry in oldest.prefix(entries.count - 40) {
                    guard let id = entry["id"] as? String else { continue }
                    _ = try? await call(
                        api, "DELETE", "households/\(householdID)/plans/\(week)/entries/\(id)", token: token)
                }
            }
        }
        // A week's list holds at most 50 paired grocery items, and every run adds one: take off
        // the ones runs that died left behind (the same 12 minutes as their companions).
        await pruneGroceryItems(api: api, token: token, householdID: householdID, week: week) { item in
            let name = (item["groceryItem"] as? [String: Any])?["name"] as? String ?? ""
            let added = (item["addedAt"] as? String).flatMap(parseDate) ?? .now
            return name.hasPrefix("Tortilla Chips ") && added < stale
        }
        let plan = try await call(
            api, "POST", "households/\(householdID)/plans/\(week)/entries", token: token,
            body: ["recipeId": recipeID, "day": NSNull(), "servings": 2])
        let entryID = (plan["entry"] as? [String: Any])?["id"] as? String ?? plan["id"] as? String ?? ""
        return CompanionSeed(
            own: own, api: api, token: token, householdID: householdID, recipeID: recipeID, week: week,
            entryID: entryID,
            cuisine: pick.1)
    }

    /// Takes the seeded dinner and its companion off the week and deletes the dinner, so the
    /// shared household stays small.
    func cleanUp() async throws {
        let entryID = entryID
        await Self.pruneGroceryItems(api: api, token: token, householdID: householdID, week: week) { item in
            item["entryId"] as? String == entryID
        }
        if !entryID.isEmpty {
            _ = try? await Self.call(
                api, "DELETE", "households/\(householdID)/plans/\(week)/entries/\(entryID)", token: token)
        }
        _ = try? await Self.call(api, "DELETE", "households/\(householdID)/recipes/\(recipeID)", token: token)
    }

    /// Deletes the week's paired grocery items that `matches` picks.
    static func pruneGroceryItems(
        api: URL, token: String, householdID: String, week: String, matches: ([String: Any]) -> Bool
    ) async {
        let base = "households/\(householdID)/autopilot/weeks/\(week)/pairings"
        guard let pairings = try? await call(api, "GET", base, token: token) else { return }
        for item in pairings["groceryItems"] as? [[String: Any]] ?? [] where matches(item) {
            guard let id = item["id"] as? String else { continue }
            _ = try? await call(api, "DELETE", "\(base)/grocery-items/\(id)", token: token)
        }
    }

    private static func parseDate(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f.date(from: s) ?? ISO8601DateFormatter().date(from: s)
    }

    @discardableResult
    static func call(
        _ api: URL, _ method: String, _ path: String, token: String?, body: [String: Any]? = nil
    ) async throws -> [String: Any] {
        var request = URLRequest(url: api.appending(path: path))
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "CompanionSeed", code: status,
                userInfo: [NSLocalizedDescriptionKey: "\(method) \(path) = \(status)"])
        }
        guard !data.isEmpty else { return [:] }
        return (try JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
    }
}
