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
        let dinner = "QA Companion Supper \(run)"
        let companion = "QA Companion Chips \(run)"
        let seed = try await CompanionSeed.prepare(dinner: dinner)

        let app = launchSignedIn()
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
    let api: URL
    let token: String
    let householdID: String
    let recipeID: String
    let week: String
    let entryID: String
    /// The cuisine's label, as its chip shows it.
    let cuisine: String

    /// `QA_API_BASE_URL` (from `TEST_RUNNER_QA_API_BASE_URL`), else the matrix's 8082.
    static var apiBase: URL {
        let base = ProcessInfo.processInfo.environment["QA_API_BASE_URL"] ?? "http://localhost:8082"
        return URL(string: base)!.appending(path: "api/v1")
    }

    static func prepare(dinner: String) async throws -> CompanionSeed {
        let api = apiBase
        let login = try await call(api, "POST", "auth/dev", token: nil, body: ["subject": "dev-simulator"])
        let token = try XCTUnwrap(login["accessToken"] as? String)
        let households = try await call(api, "GET", "households", token: token)
        let first = try XCTUnwrap((households["items"] as? [[String: Any]])?.first)
        let household = try XCTUnwrap(first["household"] as? [String: Any])
        let householdID = try XCTUnwrap(household["id"] as? String)

        // A meal shows at most three suggestions. Companions from runs that died (older than
        // any run can be) are cleared, and the cuisine is the one other runs use least, so
        // runs side by side don't crowd each other's dinner.
        let companions = try await call(api, "GET", "households/\(householdID)/companions", token: token)
        let stale = Date.now.addingTimeInterval(-12 * 60)
        var used: [String: Int] = [:]
        for c in companions["items"] as? [[String: Any]] ?? [] {
            guard let id = c["id"] as? String, (c["name"] as? String)?.hasPrefix("QA Companion ") == true,
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
        let week = nextWeek(
            timeZone: household["timeZone"] as? String, weekStartsOn: household["weekStartsOn"] as? String)
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
        let plan = try await call(
            api, "POST", "households/\(householdID)/plans/\(week)/entries", token: token,
            body: ["recipeId": recipeID, "day": NSNull(), "servings": 2])
        let entryID = (plan["entry"] as? [String: Any])?["id"] as? String ?? plan["id"] as? String ?? ""
        return CompanionSeed(
            api: api, token: token, householdID: householdID, recipeID: recipeID, week: week, entryID: entryID,
            cuisine: pick.1)
    }

    /// Takes the seeded dinner off the week and deletes it, so the shared household stays small.
    func cleanUp() async throws {
        if !entryID.isEmpty {
            _ = try? await Self.call(
                api, "DELETE", "households/\(householdID)/plans/\(week)/entries/\(entryID)", token: token)
        }
        _ = try? await Self.call(api, "DELETE", "households/\(householdID)/recipes/\(recipeID)", token: token)
    }

    /// The week after the one the app shows today: the ISO week of a week from today moved back
    /// to the household's first day (`planning.WeekOfOn`).
    static func nextWeek(timeZone: String?, weekStartsOn: String?) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone.flatMap(TimeZone.init(identifier:)) ?? .current
        let parts = calendar.dateComponents([.year, .month, .day], from: .now.addingTimeInterval(7 * 24 * 3600))
        var iso = Calendar(identifier: .iso8601)
        iso.timeZone = TimeZone(identifier: "UTC")!
        let date = iso.date(from: parts)!
        let order = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
        let index = order.firstIndex(of: weekStartsOn ?? "mon") ?? 0
        let offset = index > 3 ? index - 7 : index
        let shifted = iso.date(byAdding: .day, value: -offset, to: date)!
        let year = iso.component(.yearForWeekOfYear, from: shifted)
        let number = iso.component(.weekOfYear, from: shifted)
        return String(format: "%04d-W%02d", year, number)
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
