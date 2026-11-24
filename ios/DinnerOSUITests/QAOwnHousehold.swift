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
    /// Menu opens on a library rather than the empty-library card. The person is a realistic one
    /// ("maya-rivera-48213", shown as Maya Rivera of Rivera Kitchen; build 123: names and
    /// avatars show on screen); `prefix`, the test's name, goes in the log. An empty `recipe` adds
    /// none, for a test that brings its own; `name` replaces the household's name.
    static func make(_ prefix: String, recipe: String? = nil, name: String? = nil) async throws
        -> QAOwnHousehold
    {
        let subject = QAPeople.subject()
        let displayName = QAPeople.displayName(forSubject: subject)
        print("QA-OWN: \(prefix) signs in as \(subject)")
        let login = try await call(
            "POST", "auth/dev", token: nil,
            body: [
                "subject": subject, "displayName": displayName, "email": QAPeople.email(for: displayName),
            ])
        let token = try XCTUnwrap(login["accessToken"] as? String, "The development sign-in gave no token")
        let timeZone = "America/Phoenix"
        let created = try await call(
            "POST", "households", token: token,
            body: ["name": name ?? QAPeople.kitchen(forSubject: subject), "timeZone": timeZone, "defaultServings": 2])
        let household = (created["household"] as? [String: Any]) ?? created
        let householdID = try XCTUnwrap(household["id"] as? String, "The new household has no id")
        let own = QAOwnHousehold(
            subject: subject, householdID: householdID, token: token, timeZone: timeZone,
            weekStartsOn: household["weekStartsOn"] as? String ?? "sun")
        if recipe != "" {
            _ = try await own.addRecipe(recipe ?? "Lemon Rice \(Int.random(in: 100...999))")
        }
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

    /// Adds an item to the pantry, in stock.
    func addToPantry(_ name: String, quantity: String, unit: String) async throws {
        _ = try await Self.call(
            "POST", "households/\(householdID)/pantry", token: token,
            body: ["name": name, "quantity": quantity, "unit": unit, "status": "in_stock"])
    }

    /// Saves the Walmart product for a grocery item, with its package size.
    func chooseWalmartProduct(key: String, productID: String, name: String, size: String, unit: String) async throws {
        let encoded = key.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? key
        _ = try await Self.call(
            "PUT", "households/\(householdID)/shopping/walmart/preferences/\(encoded)", token: token,
            body: [
                "productId": productID, "displayName": name, "packageSize": ["quantity": size, "unit": unit],
                "ingredientName": name,
            ])
    }

    /// Sends the week's list to Walmart, as Add to Shopping Cart does.
    func sendToWalmart(weeksAhead: Int) async throws {
        _ = try await Self.call(
            "POST", "households/\(householdID)/plans/\(week(weeksAhead: weeksAhead))/shopping/walmart/handoffs",
            token: token, body: [String: String]())
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

    /// Plans `recipeID` on `day` ("mon"…"sun") of the week `weeksAhead` from this one. Returns the
    /// plan entry's id and date.
    @discardableResult
    func plan(_ recipeID: String, weeksAhead: Int, day: String) async throws -> (id: String, date: String) {
        let added = try await Self.call(
            "POST", "households/\(householdID)/plans/\(week(weeksAhead: weeksAhead))/entries", token: token,
            body: ["recipeId": recipeID, "day": day, "servings": 2])
        let entry = try XCTUnwrap(added["entry"] as? [String: Any], "The planned meal came back without its entry")
        return (try XCTUnwrap(entry["id"] as? String), entry["date"] as? String ?? "")
    }

    /// Meal-kit recipes with photos, as a meal-kit import brings them: the only way a household's
    /// recipe is an add-on (garlic bread), which typing one in can't make. Returns name → id.
    @discardableResult
    func importRecipes(_ recipes: [(name: String, imageURL: String, isAddon: Bool)]) async throws -> [String: String] {
        let stamp = ISO8601DateFormatter().string(from: .now)
        let file: [String: Any] = [
            "version": 1, "source": "hellofresh", "generatedAt": stamp, "review": [[String: Any]](),
            "recipes": recipes.enumerated().map { index, recipe -> [String: Any] in
                [
                    "source": "hellofresh", "sourceRecipeId": "\(subject)-\(index)", "sourceUrl": "",
                    "name": recipe.name, "imageUrl": recipe.imageURL, "isAddon": recipe.isAddon,
                    "servings": [2, 4], "totalMinutes": 30, "cuisines": ["italian"], "tags": [String](),
                    "orderWeeks": [String](),
                    "ingredients": [
                        [
                            "sourceIngredientId": "\(subject)-garlic", "name": "Garlic", "pantryStaple": false,
                            "amounts": [
                                [
                                    "servings": 2, "quantity": 2, "unit": "clove", "sourceUnit": "clove",
                                    "rawText": "2 cloves",
                                ]
                            ],
                        ]
                    ],
                    "steps": [["index": 1, "text": "Cook it, then serve."]],
                ]
            },
        ]
        _ = try await Self.call("POST", "households/\(householdID)/recipes/import", token: token, body: file)
        // A new household's first page holds them all (`appending(path:)` would escape a query).
        let listed = try await Self.call("GET", "households/\(householdID)/recipes", token: token)
        let items = listed["items"] as? [[String: Any]] ?? []
        var ids: [String: String] = [:]
        for item in items {
            if let name = item["name"] as? String, let id = item["id"] as? String { ids[name] = id }
        }
        return ids
    }

    /// A meal-kit recipe with its whole ingredient list and steps, as an import brings it, for a
    /// test that checks the cooking screen lists every ingredient. Returns its id.
    func importRecipe(_ recipe: QARecipeFixture) async throws -> String {
        let file: [String: Any] = [
            "version": 1, "source": "hellofresh", "generatedAt": ISO8601DateFormatter().string(from: .now),
            "review": [[String: Any]](),
            "recipes": [
                [
                    "source": "hellofresh", "sourceRecipeId": "\(subject)-full", "sourceUrl": "",
                    "name": recipe.name, "imageUrl": recipe.imageURL, "isAddon": false,
                    "servings": [2, 4], "totalMinutes": 35, "cuisines": ["italian"], "tags": [String](),
                    "orderWeeks": [String](),
                    "ingredients": recipe.ingredients.enumerated().map { index, item -> [String: Any] in
                        var amounts: [[String: Any]] = []
                        if let quantity = item.quantity {
                            amounts = [2, 4].map { servings in
                                let scaled = quantity * Double(servings) / 2
                                // The importer's units ("oz") for the meal kit's ("ounce").
                                let units = ["unit": "count", "ounce": "oz", "tablespoon": "tbsp", "teaspoon": "tsp"]
                                return [
                                    "servings": servings, "quantity": scaled, "unit": units[item.unit] ?? item.unit,
                                    "sourceUnit": item.unit, "rawText": "\(scaled) \(item.unit)",
                                ]
                            }
                        }
                        return [
                            "sourceIngredientId": "\(subject)-\(index)", "name": item.name,
                            "pantryStaple": item.quantity == nil, "amounts": amounts,
                        ]
                    },
                    "steps": recipe.steps.enumerated().map { ["index": $0.offset + 1, "text": $0.element] },
                ]
            ],
        ]
        _ = try await Self.call("POST", "households/\(householdID)/recipes/import", token: token, body: file)
        let listed = try await Self.call("GET", "households/\(householdID)/recipes", token: token)
        let items = listed["items"] as? [[String: Any]] ?? []
        let id = items.first { $0["name"] as? String == recipe.name }?["id"] as? String
        return try XCTUnwrap(id, "\(recipe.name) wasn’t imported")
    }

    /// Says a planned meal was made, as "I Made It" does: the plan then carries the answer, and the
    /// Menu asks how it was.
    func markMade(entryID: String, recipeID: String, date: String, weeksAhead: Int) async throws {
        let event: [String: Any] = [
            "clientEventId": UUID().uuidString, "type": "recipe.cooked", "recipeId": recipeID,
            "week": week(weeksAhead: weeksAhead), "occurredAt": ISO8601DateFormatter().string(from: .now),
            "payload": ["entryId": entryID, "date": date, "servings": 2],
        ]
        _ = try await Self.call("POST", "households/\(householdID)/events", token: token, body: ["events": [event]])
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
        _ prefix: String, recipe: String? = nil, name: String? = nil,
        setup: @escaping @Sendable (QAOwnHousehold) async throws -> Void = { _ in }
    ) throws -> QAOwnHousehold {
        let box = QABox<QAOwnHousehold>()
        let done = expectation(description: "A household of the test’s own")
        Task.detached {
            do {
                let own = try await QAOwnHousehold.make(prefix, recipe: recipe, name: name)
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
        app.launchEnvironment["QA_MARKERS"] = "1"
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

/// A whole recipe for `QAOwnHousehold.importRecipe`: each ingredient with its amount for 2 (none
/// for a pantry staple), and the steps.
struct QARecipeFixture {
    struct Ingredient {
        let name: String
        let quantity: Double?
        let unit: String
    }

    let name: String
    let imageURL: String
    let ingredients: [Ingredient]
    let steps: [String]

    /// Merrill's dinner on Nov 23 (TestFlight 120): a pasta with a photo, 13 ingredients, salt
    /// and pepper with no amount, and butter that a late step adds "to the pan with the sauce".
    /// The steps are written for the test, in the meal kit's style.
    static let lemonPenne = QARecipeFixture(
        name: "Chicken Penne al Limone",
        imageURL: "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/image/"
            + "63853599929fb724f40ed140-e3d4003e.jpg",
        ingredients: [
            .init(name: "Garlic", quantity: 1, unit: "clove"),
            .init(name: "Lemon", quantity: 1, unit: "unit"),
            .init(name: "Penne Pasta", quantity: 6, unit: "ounce"),
            .init(name: "Cream Sauce Base", quantity: 4, unit: "ounce"),
            .init(name: "Zucchini", quantity: 1, unit: "unit"),
            .init(name: "Parsley", quantity: 0.25, unit: "ounce"),
            .init(name: "Chicken Breast Strips", quantity: 10, unit: "ounce"),
            .init(name: "Parmesan Cheese", quantity: 3, unit: "tablespoon"),
            .init(name: "Olive Oil", quantity: 1, unit: "teaspoon"),
            .init(name: "Butter", quantity: 2, unit: "tablespoon"),
            .init(name: "Salt", quantity: nil, unit: ""),
            .init(name: "Pepper", quantity: nil, unit: ""),
            .init(name: "Cooking Oil", quantity: 2, unit: "teaspoon"),
        ],
        steps: [
            "Bring a large pot of salted water to a boil. Thinly slice 1 clove garlic. Quarter 1 zucchini "
                + "lengthwise, then cut into pieces. Zest and quarter 1 lemon. Chop ¼ oz parsley.",
            "Warm a drizzle of olive oil in a large pan over medium heat. Add garlic and cook until golden, "
                + "1 minute. Move garlic to a plate.",
            "Add 6 oz penne to the boiling water and cook until al dente, 9 to 11 minutes. Keep 1 cup pasta "
                + "cooking water, then drain. Meanwhile, heat a drizzle of oil in the same pan. Add zucchini "
                + "and cook until golden, 5 minutes. Season with salt and pepper.",
            "Pat 10 oz chicken dry and season with salt and pepper. Heat a drizzle of oil in the pan. Add "
                + "chicken and cook until cooked through, 4 to 6 minutes.",
            "Stir in 4 oz cream sauce base, ¼ cup pasta cooking water, and a squeeze of lemon. Cook until "
                + "thickened, 2 minutes.",
            "Add drained penne, garlic, zucchini, 1 ½ Tbsp Parmesan, half the parsley, and 2 Tbsp butter "
                + "to the pan with the sauce. Toss and season with salt and pepper. Top with the rest of "
                + "the Parmesan and parsley.",
        ])
}
