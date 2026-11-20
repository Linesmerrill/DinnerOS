import XCTest

/// Shared Recipes as Discover: the server's rows show, a filter chip narrows Browse All, search
/// works, saving from a row works and the household gets the recipe, and an add-on the household
/// has never shows anywhere in Shared Recipes.
///
/// Each run makes its own household on the local development server (a random sign-in subject),
/// so eight simulators can run it at once against one API. The API's address comes from
/// QA_API_BASE (TEST_RUNNER_QA_API_BASE), defaulting to the QA matrix's port 8082.
final class DiscoverQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testDiscoverRowsFiltersSearchSaveAndNoAddOns() async throws {
        let n = Int.random(in: 10000...99999)
        let subject = "qa-discover-\(n)"
        let addonName = "QA Addon Garlic Bread \(n)"
        let api = try await QAAPI.signIn(subject: subject)
        let householdID = try await api.createHousehold(name: "QA Discover \(n)")
        try await api.importAddon(householdID: householdID, name: addonName, sourceID: "qa-addon-\(n)")

        let app = XCUIApplication()
        applyOrientation()
        app.launch()
        if !app.textFields["Subject"].waitForExistence(timeout: 8) {
            if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
            signOut(app)
        }
        signIn(app, subject: subject)
        if app.buttons["Start Planning"].waitForExistence(timeout: 10) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 20), "No Menu after sign-in")

        // Shared Recipes, from its row on the Menu.
        let entry = app.staticTexts["Shared Recipes"].firstMatch
        reveal(entry, in: app, maxSwipes: 10)
        entry.tap()
        XCTAssertTrue(app.navigationBars["Shared Recipes"].waitForExistence(timeout: 10), "Shared Recipes didn't open")

        // 1. Rows show, with cards.
        let picks = app.descendants(matching: .any)["DiscoverSection-chefs-picks"].firstMatch
        XCTAssertTrue(picks.waitForExistence(timeout: 15), "No Chef's Picks row")
        XCTAssertTrue(picks.buttons["DiscoverCard"].firstMatch.waitForExistence(timeout: 5), "The row has no cards")
        XCTAssertTrue(app.staticTexts["Chef’s Picks This Week"].exists)
        snap(app, "Discover 01 rows")
        assertNoAddOn(app, addonName, "the rows")

        // 2. Save from a row: the card turns into a check, and the household has the recipe.
        let save = picks.buttons["DiscoverCardSave"].firstMatch
        XCTAssertTrue(save.waitForExistence(timeout: 5), "No save button on a card")
        let savedName = save.label.replacingOccurrences(of: "Add ", with: "")
            .replacingOccurrences(of: " to your library", with: "")
        save.tap()
        XCTAssertTrue(
            picks.descendants(matching: .any)["DiscoverCardSaved"].firstMatch.waitForExistence(timeout: 10),
            "Saving from the row didn't mark the card")
        snap(app, "Discover 02 saved from a row")
        let saved = try await api.recipes(householdID: householdID).first { $0.name == savedName }
        XCTAssertNotNil(saved, "\(savedName) isn't in the household's recipes after saving")

        // 3. Browse All and its chips.
        let browse = app.descendants(matching: .any)["DiscoverBrowseAll"].firstMatch
        let timeChip = app.buttons["DiscoverFilter-time"].firstMatch
        reveal(timeChip, in: app, maxSwipes: 12)
        XCTAssertTrue(app.buttons["DiscoverFilter-servings"].exists, "No Servings chip")
        let cardsBefore = browseCards(app)
        snap(app, "Discover 03 browse all")
        timeChip.tap()
        app.buttons["30 min or less"].firstMatch.tap()
        let chosen = expectation(
            for: NSPredicate(format: "label CONTAINS '30 min or less'"),
            evaluatedWith: app.buttons["DiscoverFilter-time"].firstMatch)
        await fulfillment(of: [chosen], timeout: 5)
        sleep(2)
        let cardsAfter = browseCards(app)
        snap(app, "Discover 04 filtered to 30 min")
        for label in cardsAfter {
            XCTAssertFalse(label.contains(" hr"), "\(label) is over 30 minutes")
            if let minutes = Self.minutes(in: label) {
                XCTAssertLessThanOrEqual(minutes, 30, "\(label) is over 30 minutes")
            }
        }
        if cardsBefore.contains(where: { label in label.contains(" hr") || (Self.minutes(in: label) ?? 0) > 30 }) {
            XCTAssertLessThan(cardsAfter.count, cardsBefore.count, "The time chip didn't narrow Browse All")
        }
        app.buttons["DiscoverFilterClear"].firstMatch.tap()

        // 4. Search finds a recipe by name, and finds no add-on.
        let search = app.textFields["DiscoverSearch"].firstMatch
        reveal(search, in: app, maxSwipes: 6)
        search.tap()
        search.typeText(savedName)
        let hit = browse.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", savedName)).firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 10), "Search didn't find \(savedName)")
        snap(app, "Discover 05 search")
        clearSearch(app, search)
        // Search matches any of the words, so other breads may show; the add-on never does.
        search.typeText(addonName)
        sleep(3)
        assertNoAddOn(app, addonName, "search")
        snap(app, "Discover 06 add-on not found")
        clearSearch(app, search)

        // 5. See All opens the whole row, still with no add-on.
        let seeAll = app.buttons["DiscoverSeeAll-chefs-picks"].firstMatch
        // Chef's Picks is the first row; the search can leave the list scrolled below it.
        for _ in 0..<4 where !seeAll.exists { scrollBack(app) }
        reveal(seeAll, in: app, maxSwipes: 12)
        seeAll.tap()
        XCTAssertTrue(
            app.navigationBars["Chef’s Picks This Week"].waitForExistence(timeout: 10), "See All went elsewhere")
        XCTAssertTrue(app.buttons["DiscoverCard"].firstMatch.waitForExistence(timeout: 10))
        assertNoAddOn(app, addonName, "See All")
        snap(app, "Discover 07 see all")
        backButton(app).tap()

        // The recipe saved here goes away again; the add-on stays exactly as it was.
        if let saved { try await api.deleteRecipe(householdID: householdID, id: saved.id) }
        let left = try await api.recipes(householdID: householdID)
        XCTAssertFalse(left.contains { $0.name == savedName }, "The saved recipe wasn't deleted")
        XCTAssertTrue(left.contains { $0.name == addonName }, "The household's add-on changed")
    }

    @MainActor
    private func assertNoAddOn(_ app: XCUIApplication, _ name: String, _ where: String) {
        let any = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", name))
        XCTAssertEqual(any.count, 0, "The add-on showed in \(`where`)")
    }

    /// The labels of the cards in Browse All that are on screen.
    @MainActor
    private func browseCards(_ app: XCUIApplication) -> [String] {
        let browse = app.descendants(matching: .any)["DiscoverBrowseAll"].firstMatch
        return browse.buttons.matching(identifier: "DiscoverCard").allElementsBoundByIndex.map(\.label)
    }

    @MainActor
    private func clearSearch(_ app: XCUIApplication, _ field: XCUIElement) {
        let clear = app.buttons["Clear search"].firstMatch
        if clear.exists { clear.tap() }
        if !app.keyboards.firstMatch.exists { field.tap() }
    }

    /// The minutes in a card's label ("Quick Tacos, 20 min, Mexican" is 20).
    static func minutes(in label: String) -> Int? {
        for part in label.components(separatedBy: ", ") where part.hasSuffix(" min") && !part.contains("hr") {
            return Int(part.dropLast(4))
        }
        return nil
    }
}

/// Plain calls to the local development API, to set up a household the app then signs in to.
struct QAAPI {
    let base: URL
    let token: String

    struct Recipe: Decodable {
        let id: String
        let name: String
    }

    static var baseURL: URL {
        URL(string: ProcessInfo.processInfo.environment["QA_API_BASE"] ?? "http://localhost:8082")!
    }

    static func signIn(subject: String) async throws -> QAAPI {
        struct Tokens: Decodable { let accessToken: String }
        let tokens: Tokens = try await send(
            base: baseURL, token: nil, method: "POST", path: "/api/v1/auth/dev",
            body: ["subject": subject, "displayName": "QA Discover"])
        return QAAPI(base: baseURL, token: tokens.accessToken)
    }

    func createHousehold(name: String) async throws -> String {
        struct Created: Decodable {
            struct Household: Decodable { let id: String }
            let household: Household
        }
        let created: Created = try await Self.send(
            base: base, token: token, method: "POST", path: "/api/v1/households",
            body: ["name": name, "timeZone": "America/Denver"])
        return created.household.id
    }

    /// One add-on, the way a meal-kit import brings them in. No real recipe text.
    func importAddon(householdID: String, name: String, sourceID: String) async throws {
        struct Result: Decodable { let created: Int }
        let file: [String: Any] = [
            "version": 1, "source": "hellofresh", "generatedAt": "2026-11-16T00:00:00Z", "review": [],
            "recipes": [
                [
                    "source": "hellofresh", "sourceRecipeId": sourceID, "sourceUrl": "", "name": name, "isAddon": true,
                    "servings": [2], "totalMinutes": 10, "cuisines": ["Italian"], "orderWeeks": [],
                    "ingredients": [["name": "bread", "amounts": [["servings": 2, "quantity": 1, "unit": "cup"]]]],
                    "steps": [["index": 1, "text": "Toast it."]],
                ]
            ],
        ]
        let result: Result = try await Self.send(
            base: base, token: token, method: "POST", path: "/api/v1/households/\(householdID)/recipes/import",
            body: file)
        XCTAssertEqual(result.created, 1, "The add-on wasn't imported")
    }

    func recipes(householdID: String) async throws -> [Recipe] {
        struct List: Decodable { let items: [Recipe] }
        let list: List = try await Self.send(
            base: base, token: token, method: "GET", path: "/api/v1/households/\(householdID)/recipes?limit=100",
            body: nil)
        return list.items
    }

    /// Types one recipe and shares it to Shared Recipes, as a cook in another household would.
    /// Made-up text only.
    func shareTypedRecipe(householdID: String, name: String) async throws {
        struct Sharing: Decodable { let inCatalog: Bool }
        struct Ignored: Decodable {}
        let _: Ignored = try await Self.send(
            base: base, token: token, method: "PATCH", path: "/api/v1/households/\(householdID)",
            body: ["catalogSharing": "chosen"])
        let created: Recipe = try await Self.send(
            base: base, token: token, method: "POST", path: "/api/v1/households/\(householdID)/recipes",
            body: [
                "name": name, "servings": 2,
                "ingredients": [["name": "rice"], ["name": "peas"]],
                "steps": ["Cook the rice.", "Stir in the peas."],
            ] as [String: Any])
        let shared: Sharing = try await Self.send(
            base: base, token: token, method: "PUT",
            path: "/api/v1/households/\(householdID)/recipes/\(created.id)/sharing",
            body: ["sharedToCatalog": true])
        XCTAssertTrue(shared.inCatalog, "\(name) didn't reach Shared Recipes")
    }

    func deleteRecipe(householdID: String, id: String) async throws {
        var request = URLRequest(
            url: URL(string: base.absoluteString + "/api/v1/households/\(householdID)/recipes/\(id)")!)
        request.httpMethod = "DELETE"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (_, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        XCTAssertTrue((200..<300).contains(status), "Deleting the saved recipe answered \(status)")
    }

    private static func send<T: Decodable>(
        base: URL, token: String?, method: String, path: String, body: Any?
    ) async throws -> T {
        var request = URLRequest(url: URL(string: base.absoluteString + path)!)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "QAAPI", code: status,
                userInfo: [
                    NSLocalizedDescriptionKey:
                        "\(method) \(path) answered \(status): \(String(decoding: data, as: UTF8.self))"
                ])
        }
        return try JSONDecoder().decode(T.self, from: data)
    }
}
