import XCTest

/// The App Review account (decisions 752, 759): sign in with email and password, find the recipes
/// we wrote for it and no starter library, open the purchase screen (no Plus on the house), then
/// delete the account so the next run starts from a fresh household.
///
/// Needs an API started with REVIEW_ACCOUNT_EMAIL and REVIEW_ACCOUNT_PASSWORD and PLUS_ENABLED=true,
/// and the same two values given to the test as TEST_RUNNER_QA_REVIEW_EMAIL and
/// TEST_RUNNER_QA_REVIEW_PASSWORD (scripts/qa-matrix.sh passes them on). QA_API_BASE defaults to
/// the matrix's 8082. Only one run at a time: there is one review account.
final class ReviewAccountQATests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var base: String { env["QA_API_BASE"] ?? "http://localhost:8082" }

    /// The recipes in api/internal/reviewaccount/recipes.json.
    private let seeded = [
        "Salmon with Asparagus and Rice", "Beef and Broccoli", "Spaghetti Pomodoro", "Shredded Beef Tacos",
        "Chicken Tikka Masala", "Margherita Pizza", "Chicken Skewers with Garlic Sauce", "Turkey and Bean Chili",
    ]

    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    @MainActor
    func testReviewAccountSignsInToItsOwnRecipesAndThePaywall() async throws {
        let email = try XCTUnwrap(env["QA_REVIEW_EMAIL"], "Set TEST_RUNNER_QA_REVIEW_EMAIL")
        let password = try XCTUnwrap(env["QA_REVIEW_PASSWORD"], "Set TEST_RUNNER_QA_REVIEW_PASSWORD")
        applyOrientation()
        let app = XCUIApplication()
        app.launch()

        // Apple first, then the small email link under it.
        let open = app.buttons["signIn.email.open"]
        XCTAssertTrue(
            open.waitForExistence(timeout: 15), "No Sign in with email: is the review account set on the API?")
        let apple = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Apple'")).firstMatch
        if apple.exists { XCTAssertLessThan(apple.frame.minY, open.frame.minY, "Sign in with Apple should come first") }
        snap(app, "Review account › sign-in")
        open.tap()

        let field = app.textFields["signIn.email.address"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        // A wrong password first: one plain message, and still on sign-in.
        field.tap()
        field.typeText(email)
        let secure = app.secureTextFields["signIn.email.password"]
        secure.tap()
        secure.typeText("not-the-password")
        app.buttons["signIn.email.submit"].tap()
        XCTAssertTrue(
            app.staticTexts["That email and password didn’t work."].waitForExistence(timeout: 10),
            "A wrong password didn’t say so plainly")
        snap(app, "Review account › wrong password")

        secure.tap()
        secure.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 20))
        secure.typeText(password)
        app.buttons["signIn.email.submit"].tap()

        // Signed in to the seeded household: the welcome, not Create a Household. The welcome
        // covers the Menu, so it goes first.
        let start = app.buttons["Start Planning"].firstMatch
        if start.waitForExistence(timeout: 20) {
            snap(app, "Review account › welcome")
            start.tap()
        }
        XCTAssertFalse(app.staticTexts["Create a Household"].exists, "The review account has no household")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 20), "The Menu didn’t open")
        for title in ["Thanks", "Skip"] where app.buttons[title].firstMatch.exists {
            app.buttons[title].firstMatch.tap()
        }
        sleep(2)
        snap(app, "Review account › Menu")
        let first = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", seeded[0])).firstMatch
        reveal(first, in: app, maxSwipes: 8)
        XCTAssertTrue(first.exists, "\(seeded[0]) isn’t on the Menu")
        scrollBack(app)

        // The library holds exactly our five: nothing from the starter library.
        let names = try await libraryNames(email: email, password: password)
        XCTAssertEqual(Set(names), Set(seeded), "The review household’s recipes")

        // No Plus on the house: Autopilot leads to the purchase screen.
        let autopilot = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Plan with Autopilot'")).firstMatch
        reveal(autopilot, in: app, maxSwipes: 6)
        if autopilot.exists {
            autopilot.tap()
            XCTAssertTrue(
                app.staticTexts["DinnerOS Plus"].firstMatch.waitForExistence(timeout: 10), "No purchase screen")
            sleep(2)
            snap(app, "Review account › purchase screen")
            close(app)
        } else {
            XCTFail("No Plan with Autopilot on the Menu")
        }

        deleteAccount(app)
        XCTAssertTrue(app.buttons["signIn.email.open"].waitForExistence(timeout: 20), "Didn’t return to sign-in")
        snap(app, "Review account › deleted, back at sign-in")
    }

    // MARK: Steps

    @MainActor
    private func deleteAccount(_ app: XCUIApplication) {
        // A tab on the iPhone; on the iPad it can be in the sidebar instead.
        let tab = app.tabBars.buttons["Household"].firstMatch
        let householdTab = tab.exists ? tab : app.buttons["Household"].firstMatch
        for _ in 0..<3 where !app.navigationBars["Household"].exists {
            householdTab.tap()
            _ = app.navigationBars["Household"].waitForExistence(timeout: 3)
        }
        let account = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'Account'"))
            .firstMatch
        reveal(account, in: app, maxSwipes: 6)
        account.tap()
        let delete = app.buttons["Delete Account"].firstMatch
        reveal(delete, in: app, maxSwipes: 6)
        let rowFrame = delete.frame
        delete.tap()
        sleep(1)
        let confirm = app.buttons.matching(NSPredicate(format: "label == 'Delete Account'")).allElementsBoundByIndex
            .first { $0.frame != rowFrame && $0.isHittable }
        XCTAssertNotNil(confirm, "No Delete Account in the confirmation")
        confirm?.tap()
    }

    @MainActor
    private func close(_ app: XCUIApplication) {
        for title in ["Not Now", "Cancel", "Done", "Close"] where app.buttons[title].firstMatch.exists {
            let button = app.buttons[title].firstMatch
            if button.isHittable {
                button.tap()
                sleep(1)
                return
            }
        }
        app.swipeDown(velocity: .fast)
        sleep(1)
    }

    /// The review household's recipe names, read from the API with the same email and password.
    private func libraryNames(email: String, password: String) async throws -> [String] {
        let session = try await call(
            "POST", "/api/v1/auth/review-sign-in", token: nil, body: ["email": email, "password": password])
        let token = try XCTUnwrap(session["accessToken"] as? String)
        let list = try await call("GET", "/api/v1/households", token: token, body: nil)
        let items = list["items"] as? [[String: Any]] ?? []
        XCTAssertEqual(items.count, 1, "The review account should have one household")
        let id = try XCTUnwrap((items.first?["household"] as? [String: Any])?["id"] as? String)
        let recipes = try await call("GET", "/api/v1/households/\(id)/recipes?limit=50", token: token, body: nil)
        let plus = try await call("GET", "/api/v1/households/\(id)/plus", token: token, body: nil)
        XCTAssertEqual(plus["active"] as? Bool, false, "The review household has Plus")
        XCTAssertNil(plus["comp"], "The review household has Plus on the house")
        return (recipes["items"] as? [[String: Any]] ?? []).compactMap { $0["name"] as? String }
    }

    private func call(_ method: String, _ path: String, token: String?, body: [String: Any]?) async throws
        -> [String: Any]
    {
        var request = URLRequest(url: try XCTUnwrap(URL(string: base + path)))
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "ReviewAccountQATests", code: status,
                userInfo: [NSLocalizedDescriptionKey: "\(method) \(path) answered \(status)"])
        }
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }
}
