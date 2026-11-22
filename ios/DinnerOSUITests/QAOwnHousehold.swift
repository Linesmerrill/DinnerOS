import XCTest

/// A household of one test's own on the local development API (local Mongo only).
///
/// The screenshot tests (AppSweepQATests, ScreensQATests, StartCookingQATests,
/// BiometricSignInQATests) photograph the shared test household, the one the developer sign-in
/// opens by default. Tests that add, plan or delete recipes used to do it there too, while
/// the screenshots were being taken on the other simulators, so the Menu showed three Chicken
/// Bowls and "41 meals" in one shot and "42" in the next (build 122, Hark's review items 12
/// and 24). They now sign in as a person of their own with a household of their own, made
/// here through the API, and leave the shared one alone.
struct QAOwnHousehold {
    let subject: String
    let householdID: String
    let token: String
    let timeZone: String
    let weekStartsOn: String

    /// `QA_API_BASE` (the matrix's `TEST_RUNNER_QA_API_BASE`), else the matrix's 8082.
    static var apiBase: URL {
        let env = ProcessInfo.processInfo.environment
        let base = env["QA_API_BASE"] ?? env["QA_API_BASE_URL"] ?? "http://localhost:8082"
        return URL(string: base)!.appending(path: "api/v1")
    }

    /// Signs a new person in on the API and gives them a household with one typed recipe, so the
    /// Menu opens on a library rather than the empty-library card. `prefix` names the test in
    /// the subject ("qa-own-addrecipe-12345").
    static func make(_ prefix: String, recipe: String? = nil) async throws -> QAOwnHousehold {
        // The app lowercases a developer subject, so it's made lowercase here too.
        let subject = "qa-own-\(prefix.lowercased())-\(Int.random(in: 10000...99999))"
        let login = try await call("POST", "auth/dev", token: nil, body: ["subject": subject])
        let token = try XCTUnwrap(login["accessToken"] as? String, "The development sign-in gave no token")
        let timeZone = "America/Phoenix"
        let created = try await call(
            "POST", "households", token: token,
            body: ["name": "QA Own Kitchen", "timeZone": timeZone, "defaultServings": 2])
        let household = (created["household"] as? [String: Any]) ?? created
        let householdID = try XCTUnwrap(household["id"] as? String, "The new household has no id")
        let own = QAOwnHousehold(
            subject: subject, householdID: householdID, token: token, timeZone: timeZone,
            weekStartsOn: household["weekStartsOn"] as? String ?? "sun")
        _ = try await own.addRecipe(recipe ?? "QA Lemon Rice \(Int.random(in: 100...999))")
        return own
    }

    /// A typed recipe with ingredients, so a week it's planned in has a grocery list. Returns
    /// its id.
    @discardableResult
    func addRecipe(_ name: String, cuisine: String = "Italian") async throws -> String {
        let recipe = try await Self.call(
            "POST", "households/\(householdID)/recipes", token: token,
            body: [
                "name": name, "servings": 2, "cuisines": [cuisine], "tags": [String](), "warnings": [String](),
                "ingredients": [
                    ["name": "Jasmine rice", "quantity": "1", "unit": "cup"],
                    ["name": "Carrots", "quantity": "6", "unit": "oz"],
                    ["name": "Lemon", "quantity": "1"],
                ],
                "steps": ["Cook the rice.", "Roast the carrots.", "Squeeze the lemon over both."],
                "fromUrl": false,
            ])
        return try XCTUnwrap(recipe["id"] as? String, "\(name) wasn’t saved")
    }

    /// A typed recipe with the given ingredients (name, quantity, unit) and steps. Returns its id.
    @discardableResult
    func addRecipe(
        _ name: String, ingredients: [(String, String, String?)], steps: [String], cuisine: String = "Italian"
    ) async throws -> String {
        let lines: [[String: String]] = ingredients.map { name, quantity, unit in
            var line = ["name": name, "quantity": quantity]
            if let unit { line["unit"] = unit }
            return line
        }
        let recipe = try await Self.call(
            "POST", "households/\(householdID)/recipes", token: token,
            body: [
                "name": name, "servings": 2, "cuisines": [cuisine], "tags": [String](), "warnings": [String](),
                "ingredients": lines, "steps": steps, "fromUrl": false,
            ])
        return try XCTUnwrap(recipe["id"] as? String, "\(name) wasn’t saved")
    }

    /// The grocery items of the week `weeksAhead` from this one: key and name, with the
    /// component each is part of, if any.
    func groceryItems(weeksAhead: Int) async throws -> [(key: String, name: String, component: String?)] {
        let list = try await Self.call(
            "GET", "households/\(householdID)/plans/\(week(weeksAhead: weeksAhead))/grocery", token: token)
        let categories = list["categories"] as? [[String: Any]] ?? []
        return categories.flatMap { ($0["items"] as? [[String: Any]]) ?? [] }.compactMap { item in
            guard let key = item["ingredientKey"] as? String, let name = item["name"] as? String else { return nil }
            let shares = item["shares"] as? [[String: Any]] ?? []
            let component = shares.lazy.compactMap { ($0["component"] as? [String: Any])?["specialtyName"] as? String }
                .first
            return (key, name, component)
        }
    }

    /// "Have It This Week" for one grocery item.
    func haveThisWeek(key: String, name: String, weeksAhead: Int) async throws {
        _ = try await Self.call(
            "POST", "households/\(householdID)/grocery-skips", token: token,
            body: ["ingredientKey": key, "name": name, "scope": "week", "week": week(weeksAhead: weeksAhead)])
    }

    /// Shops at Walmart, as the shared test household does, so the Shop tab shows its list and
    /// export rather than the store setup.
    func shopAtWalmart() async throws {
        _ = try await Self.call(
            "PUT", "households/\(householdID)/shopping/settings", token: token, body: ["provider": "walmart"])
    }

    /// Plans `recipeID` in the week `weeksAhead` from this one (0 is this week).
    func plan(_ recipeID: String, weeksAhead: Int) async throws {
        _ = try await Self.call(
            "POST", "households/\(householdID)/plans/\(week(weeksAhead: weeksAhead))/entries", token: token,
            body: ["recipeId": recipeID, "day": NSNull(), "servings": 2])
    }

    /// The ISO week the app shows `weeksAhead` weeks from today: today's date in the household's
    /// time zone, moved back to its first day (`planning.WeekOfOn`).
    func week(weeksAhead: Int, now: Date = .now) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: timeZone) ?? .current
        let day = now.addingTimeInterval(Double(weeksAhead) * 7 * 24 * 3600)
        let parts = calendar.dateComponents([.year, .month, .day], from: day)
        var iso = Calendar(identifier: .iso8601)
        iso.timeZone = TimeZone(identifier: "UTC")!
        let date = iso.date(from: parts)!
        let order = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
        let index = order.firstIndex(of: weekStartsOn) ?? 0
        let offset = index > 3 ? index - 7 : index
        let shifted = iso.date(byAdding: .day, value: -offset, to: date)!
        return String(
            format: "%04d-W%02d", iso.component(.yearForWeekOfYear, from: shifted),
            iso.component(.weekOfYear, from: shifted))
    }

    @discardableResult
    static func call(_ method: String, _ path: String, token: String?, body: [String: Any]? = nil) async throws
        -> [String: Any]
    {
        var request = URLRequest(url: apiBase.appending(path: path))
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "QAOwnHousehold", code: status,
                userInfo: [
                    NSLocalizedDescriptionKey:
                        "\(method) \(path) answered \(status): \(String(decoding: data, as: UTF8.self))"
                ])
        }
        guard !data.isEmpty else { return [:] }
        return (try JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
    }
}

/// Carries a result out of a detached task.
private final class QABox<T>: @unchecked Sendable {
    var value: Result<T, Error>?
}

extension XCTestCase {
    /// `QAOwnHousehold.make`, for a test that isn't async (an async test can't use XCTest's
    /// waits). `setup` plans or adds whatever else the test needs.
    @MainActor
    func ownHousehold(
        _ prefix: String, setup: @escaping @Sendable (QAOwnHousehold) async throws -> Void = { _ in }
    ) throws -> QAOwnHousehold {
        let box = QABox<QAOwnHousehold>()
        let done = expectation(description: "A household of the test’s own")
        Task.detached {
            do {
                let own = try await QAOwnHousehold.make(prefix)
                try await setup(own)
                box.value = .success(own)
            } catch {
                box.value = .failure(error)
            }
            done.fulfill()
        }
        wait(for: [done], timeout: 90)
        return try XCTUnwrap(box.value, "The household wasn’t made in time").get()
    }

    /// `launchSignedIn()`, signed in as `household`'s person instead of the shared test household:
    /// the developer sign-in starts on their subject (`QA_DEV_SUBJECT`). A new household's
    /// one-time welcome is closed.
    @MainActor
    func launchSignedIn(as household: QAOwnHousehold) -> XCUIApplication {
        let app = XCUIApplication()
        applyOrientation()
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        app.launchEnvironment["QA_DEV_SUBJECT"] = household.subject
        app.launch()
        assertFullScreen(app)
        let devSignIn = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        if devSignIn.waitForExistence(timeout: 8) {
            devSignIn.tap()
        } else {
            // Still signed in from an earlier test in this session: sign out and in as them.
            if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
            signOut(app)
            signIn(app, subject: household.subject)
        }
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
        if app.buttons["Start Planning"].waitForExistence(timeout: 10) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn’t open after sign-in")
        closeNewsBanner(app)
        return app
    }
}
