import XCTest

/// Next-step dots (decisions 736-739): a new household with Plus and no Autopilot sees a red dot
/// on the Household tab, the Planning row and Autopilot Preferences; setting Autopilot up clears
/// all three.
///
/// The person, household and Plus are made through the local API (development sign-in and the
/// development-only Plus route, local Mongo only) with a random name, so simulators side by side
/// never share one. QA_API_BASE (TEST_RUNNER_QA_API_BASE), defaulting to the QA matrix's 8082.
final class NextStepDotQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testAutopilotDotShowsAndClearsOnSetup() async throws {
        let n = Int.random(in: 10000...99999)
        let subject = "qa-nextstep-\(n)"
        let seed = try await NextStepSeed.prepare(subject: subject, household: "QA Next Step \(n)")
        let pending = try await seed.nextSteps()
        XCTAssertTrue(pending.contains("autopilotSetup"), "The API lists \(pending), not Autopilot setup")

        let app = XCUIApplication()
        applyOrientation()
        app.launch()
        if !app.textFields["Subject"].waitForExistence(timeout: 8) {
            if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
            signOut(app)
        }
        signIn(app, subject: subject)
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 20), "No Menu after sign-in")
        // A household with Plus and no Autopilot is offered setup once on the Menu (Later, so the
        // dots are what bring the member back to it), and the Plus thank-you shows once. Either
        // can come first.
        var quiet = 0
        for _ in 0..<12 where quiet < 3 {
            if app.buttons["Set Up Later"].waitForExistence(timeout: 2) {
                app.buttons["Set Up Later"].tap()
                quiet = 0
            } else if app.buttons["Thanks"].exists {
                app.buttons["Thanks"].tap()
                quiet = 0
            } else {
                quiet += 1
            }
        }

        // 1. The dot on the tab, the Planning row and Autopilot Preferences.
        let tab = app.buttons["Household"].firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 10), "No Household tab")
        XCTAssertTrue(waitFor(tab, value: true), "No dot on the Household tab (value \(String(describing: tab.value)))")
        snap(app, "Next steps › Tab dot (\(mode))")
        tab.tap()
        let planning = dot("planning", in: app)
        reveal(planning, in: app)
        XCTAssertTrue(planning.waitForExistence(timeout: 10), "No dot on the Planning row")
        XCTAssertEqual(planning.value as? String, "Needs attention")
        snap(app, "Next steps › Planning dot (\(mode))")
        planning.tap()
        let preferences = dot("autopilotPreferences", in: app)
        XCTAssertTrue(preferences.waitForExistence(timeout: 10), "No dot on Autopilot Preferences")
        XCTAssertFalse(dot("importReview", in: app).exists, "Import Review has a dot with nothing waiting")
        snap(app, "Next steps › Autopilot Preferences dot (\(mode))")

        // 2. Set Autopilot up, keeping the usual answers.
        preferences.tap()
        let setUp = app.buttons["Set Up Autopilot"]
        XCTAssertTrue(setUp.waitForExistence(timeout: 15), "Autopilot Preferences has no Set Up Autopilot")
        setUp.tap()
        for step in ["Next", "Next", "Finish"] {
            let button = app.buttons[step].firstMatch
            XCTAssertTrue(button.waitForExistence(timeout: 15), "Setup has no \(step)")
            button.tap()
        }
        XCTAssertTrue(app.staticTexts["You're All Set"].waitForExistence(timeout: 15), "Setup didn't finish")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.staticTexts["You're All Set"], timeout: 10), "Setup didn't close")
        XCTAssertTrue(waitGone(app.buttons["Set Up Autopilot"], timeout: 10), "Autopilot isn't set up")

        // 3. Every dot is gone: on the way back and on the tab.
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Planning"].waitForExistence(timeout: 10))
        XCTAssertTrue(waitGone(dot("autopilotPreferences", in: app)), "Autopilot Preferences kept its dot")
        snap(app, "Next steps › Cleared in Planning (\(mode))")
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(waitGone(dot("planning", in: app)), "The Planning row kept its dot")
        XCTAssertTrue(waitFor(tab, value: false), "The Household tab kept its dot")
        snap(app, "Next steps › Cleared (\(mode))")
        let after = try await seed.nextSteps()
        XCTAssertFalse(after.contains("autopilotSetup"), "The API still lists Autopilot setup")
    }

    @MainActor
    private func dot(_ row: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)["NextStepDot-\(row)"].firstMatch
    }

    /// Waits for the tab's badge to show (`true`) or go (`false`). A dot is a badge with no
    /// number, which XCTest reads as a non-empty value on the tab button.
    @MainActor
    private func waitFor(_ tab: XCUIElement, value shown: Bool, timeout: TimeInterval = 10) -> Bool {
        let predicate = NSPredicate { object, _ in
            guard let element = object as? XCUIElement else { return false }
            let value = (element.value as? String) ?? ""
            return value.isEmpty != shown
        }
        let wait = expectation(for: predicate, evaluatedWith: tab)
        return XCTWaiter.wait(for: [wait], timeout: timeout) == .completed
    }
}

/// A new person and household with Plus, through the local API.
private struct NextStepSeed {
    let token: String
    let householdID: String

    static var base: String { ProcessInfo.processInfo.environment["QA_API_BASE"] ?? "http://localhost:8082" }

    static func prepare(subject: String, household: String) async throws -> NextStepSeed {
        let login = try await call("POST", "/api/v1/auth/dev", token: nil, body: ["subject": subject])
        let token = try XCTUnwrap(login["accessToken"] as? String)
        let created = try await call(
            "POST", "/api/v1/households", token: token, body: ["name": household, "timeZone": "America/Denver"])
        let householdID = try XCTUnwrap((created["household"] as? [String: Any])?["id"] as? String)
        _ = try await call("POST", "/api/v1/households/\(householdID)/plus/dev-comp", token: token, body: nil)
        return NextStepSeed(token: token, householdID: householdID)
    }

    /// The pending steps' IDs.
    func nextSteps() async throws -> [String] {
        let list = try await Self.call("GET", "/api/v1/households/\(householdID)/next-steps", token: token, body: nil)
        return (list["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }
    }

    private static func call(_ method: String, _ path: String, token: String?, body: [String: Any]?) async throws
        -> [String: Any]
    {
        var request = URLRequest(url: URL(string: base + path)!)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "NextStepSeed", code: status,
                userInfo: [NSLocalizedDescriptionKey: "\(method) \(path) answered \(status)"])
        }
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }
}
