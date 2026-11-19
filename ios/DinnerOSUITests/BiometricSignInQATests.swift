import Darwin
import XCTest

/// Face ID sign-in (decision 760): the offer, the Account toggle, the lock at launch and after
/// time away, and every way back to normal sign-in.
///
/// `testFakeFaceID` stands in for Face ID with DINNEROS_FAKE_BIOMETRICS (a debug build), so every
/// outcome is certain. `testSimulatorFaceID` uses the simulator's own Face ID: it enrolls a face
/// and answers each check with a match or no match, the same as Features > Face ID in the
/// Simulator menu.
final class BiometricSignInQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    // MARK: Fake Face ID

    @MainActor
    func testFakeFaceID() throws {
        // Start signed in with it off: a cancel at a leftover lock signs out.
        var app = launch(fake: "cancel")
        signInIfNeeded(app)
        answerOffer(app, use: false)
        setToggle(app, on: false)

        // On from the Account toggle.
        app = relaunch(app, fake: "match")
        setToggle(app, on: true)
        snap(app, "Face ID › Account toggle on")

        // Launch: the lock asks and a match opens the app.
        app = relaunch(app, fake: "match")
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A match didn't open the app")
        XCTAssertFalse(app.buttons["lock.anotherWay"].exists)

        // Interrupted: the lock screen stays, and Sign in another way signs out.
        app = relaunch(app, fake: "interrupt")
        let another = app.buttons["lock.anotherWay"]
        XCTAssertTrue(another.waitForExistence(timeout: 15), "No lock screen")
        XCTAssertTrue(app.buttons["lock.unlock"].exists)
        snap(app, "Face ID › Lock screen")
        another.tap()
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 15), "Sign in another way didn't sign out")

        // Signing in again offers it again (it was on). Not Now leaves it off.
        devSignIn(app).tap()
        XCTAssertTrue(app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 15), "No offer")
        snap(app, "Face ID › Offer after sign-in")
        answerOffer(app, use: false)
        XCTAssertEqual(toggleValue(app), false)
        // A check that doesn't finish leaves it off.
        app.switches["account.biometricSignIn"].firstMatch.switches.firstMatch.tap()
        sleep(2)
        XCTAssertEqual(toggleValue(app), false, "An unfinished check turned it on")

        // Cancel at launch: normal sign-in, never a second ask. Then Use Face ID in the offer.
        app = relaunch(app, fake: "match")
        setToggle(app, on: true)
        app = relaunch(app, fake: "cancel,match")
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "A cancel didn't go to sign-in")
        snap(app, "Face ID › Cancel goes to sign-in")
        devSignIn(app).tap()
        XCTAssertTrue(app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 15), "No offer")
        answerOffer(app, use: true)
        XCTAssertTrue(waitForToggle(app, true, opening: true), "Use Face ID in the offer didn't turn it on")

        // Faces changed since it was turned on: normal sign-in.
        app = relaunch(app, fake: "changed")
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "Changed faces didn't go to sign-in")
        devSignIn(app).tap()
        answerOffer(app, use: false)

        // Away longer than the relock time (2 seconds here, 15 minutes for real): locked over
        // the app, then a match opens it where it was.
        app = relaunch(app, fake: "match", relockSeconds: 2)
        setToggle(app, on: true)
        away(app)
        XCTAssertTrue(waitGone(app.buttons["lock.anotherWay"], timeout: 10), "The lock stayed after a match")
        XCTAssertTrue(app.switches["account.biometricSignIn"].firstMatch.waitForExistence(timeout: 10),
            "The app didn't open where it was")

        // Interrupted on the way back: the lock covers the app; Sign in another way signs out.
        app = relaunch(app, fake: "match,interrupt", relockSeconds: 2)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A match didn't open the app")
        away(app)
        XCTAssertTrue(app.buttons["lock.anotherWay"].waitForExistence(timeout: 15), "No lock after time away")
        snap(app, "Face ID › Locked after time away")
        app.buttons["lock.anotherWay"].tap()
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 15), "Sign in another way didn't sign out")
        devSignIn(app).tap()
        answerOffer(app, use: false)
        app.terminate()
    }

    // MARK: Simulator Face ID

    @MainActor
    func testSimulatorFaceID() throws {
        enrollFace(true)
        defer { enrollFace(false) }
        var app = launch(fake: nil)
        // A leftover lock: no match signs out.
        if app.buttons["lock.anotherWay"].waitForExistence(timeout: 6) {
            app.buttons["lock.anotherWay"].tap()
        }
        signInIfNeeded(app)
        answerOffer(app, use: false)
        setToggle(app, on: false)

        // Turning it on asks permission the first time, then for a face.
        let toggle = app.switches["account.biometricSignIn"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 5), "No Face ID toggle with a face enrolled")
        toggle.switches.firstMatch.tap()
        allowFaceIDIfAsked()
        sleep(2)
        faceMatches(true)
        XCTAssertTrue(waitForToggle(app, true), "A matched face didn't turn it on")
        snap(app, "Simulator Face ID › On")

        // Launch: the system check, then a match opens the app.
        app = relaunch(app, fake: nil, expectLock: true)
        sleep(2)
        faceMatches(true)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A matched face didn't open the app")

        // No match, then Cancel: normal sign-in.
        app = relaunch(app, fake: nil, expectLock: true)
        sleep(2)
        faceMatches(false)
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let cancel = springboard.buttons["Cancel"].firstMatch
        if cancel.waitForExistence(timeout: 8) {
            snap(app, "Simulator Face ID › No match")
            cancel.tap()
        } else {
            faceMatches(false)
        }
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "No match didn't go to sign-in")
        devSignIn(app).tap()
        answerOffer(app, use: false)
        app.terminate()
    }

    // MARK: Steps

    @MainActor
    private func launch(fake: String?, relockSeconds: Int? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        applyOrientation()
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        if let fake { app.launchEnvironment["DINNEROS_FAKE_BIOMETRICS"] = fake }
        if let relockSeconds { app.launchEnvironment["QA_RELOCK_SECONDS"] = String(relockSeconds) }
        app.launch()
        return app
    }

    /// Relaunches; with `expectLock`, the lock screen must show first.
    @MainActor
    private func relaunch(
        _ app: XCUIApplication, fake: String?, relockSeconds: Int? = nil, expectLock: Bool = false
    ) -> XCUIApplication {
        app.terminate()
        let next = launch(fake: fake, relockSeconds: relockSeconds)
        if expectLock {
            XCTAssertTrue(next.buttons["lock.anotherWay"].waitForExistence(timeout: 15), "No lock screen at launch")
        }
        return next
    }

    /// Home, a few seconds away, then back.
    @MainActor
    private func away(_ app: XCUIApplication) {
        XCUIDevice.shared.press(.home)
        sleep(5)
        app.activate()
    }

    @MainActor
    private func devSignIn(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
    }

    @MainActor
    private func signInIfNeeded(_ app: XCUIApplication) {
        if devSignIn(app).waitForExistence(timeout: 8) { devSignIn(app).tap() }
        if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 30), "The app didn't open after sign-in")
    }

    @MainActor
    private func answerOffer(_ app: XCUIApplication, use: Bool) {
        let offer = app.alerts["Use Face ID to sign in next time?"]
        guard offer.waitForExistence(timeout: 4) else { return }
        offer.buttons[use ? "Use Face ID" : "Not Now"].tap()
        sleep(1)
        if app.buttons["Thanks"].waitForExistence(timeout: 2) { app.buttons["Thanks"].tap() }
    }

    @MainActor
    private func householdTab(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == 'Household'")).firstMatch
    }

    /// Opens Household > Account, where the toggle is.
    @MainActor
    private func openAccount(_ app: XCUIApplication) {
        if app.switches["account.biometricSignIn"].exists { return }
        let household = app.buttons.matching(NSPredicate(format: "label == 'Household' AND identifier == 'person.2'"))
            .firstMatch
        (household.waitForExistence(timeout: 5) ? household : householdTab(app)).tap()
        let account = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Account'")).firstMatch
        reveal(account, in: app)
        account.tap()
        _ = app.switches["account.biometricSignIn"].firstMatch.waitForExistence(timeout: 8)
    }

    @MainActor
    private func toggleValue(_ app: XCUIApplication) -> Bool? {
        openAccount(app)
        let toggle = app.switches["account.biometricSignIn"].firstMatch
        guard toggle.waitForExistence(timeout: 5) else { return nil }
        return (toggle.value as? String) == "1"
    }

    @MainActor
    private func setToggle(_ app: XCUIApplication, on: Bool) {
        guard let value = toggleValue(app) else {
            XCTFail("No Face ID toggle in Account")
            return
        }
        if value != on {
            app.switches["account.biometricSignIn"].firstMatch.switches.firstMatch.tap()
        }
        XCTAssertTrue(waitForToggle(app, on), "The Face ID toggle didn't turn \(on ? "on" : "off")")
    }

    @MainActor
    private func waitForToggle(_ app: XCUIApplication, _ on: Bool, opening: Bool = false) -> Bool {
        if opening { openAccount(app) }
        let toggle = app.switches["account.biometricSignIn"].firstMatch
        let done = expectation(for: NSPredicate(format: "value == %@", on ? "1" : "0"), evaluatedWith: toggle)
        return XCTWaiter.wait(for: [done], timeout: 10) == .completed
    }

    /// The first use asks "Do you want to allow DinnerOS to use Face ID?".
    @MainActor
    private func allowFaceIDIfAsked() {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for title in ["OK", "Allow"] {
            let button = springboard.alerts.buttons[title].firstMatch
            if button.waitForExistence(timeout: 4) {
                button.tap()
                return
            }
        }
    }

    // MARK: Simulator biometrics

    /// Features > Face ID > Enrolled.
    private func enrollFace(_ enrolled: Bool) {
        let name = "com.apple.BiometricKit.enrollmentChanged"
        var token: Int32 = 0
        _ = Notify.registerCheck(name, &token)
        _ = Notify.setState(token, enrolled ? 1 : 0)
        _ = Notify.post(name)
    }

    /// Features > Face ID > Matching Face or Non-matching Face.
    private func faceMatches(_ matches: Bool) {
        _ = Notify.post(matches ? "com.apple.BiometricKit_Sim.pearl.match" : "com.apple.BiometricKit_Sim.pearl.nomatch")
    }
}

/// libnotify, which the Simulator's Face ID menu uses. Not in Swift's iOS headers, so looked up.
private enum Notify {
    nonisolated(unsafe) private static let handle = UnsafeMutableRawPointer(bitPattern: -2)  // RTLD_DEFAULT

    static let post: @convention(c) (UnsafePointer<CChar>) -> UInt32 = unsafeBitCast(
        dlsym(handle, "notify_post"), to: (@convention(c) (UnsafePointer<CChar>) -> UInt32).self)
    static let registerCheck: @convention(c) (UnsafePointer<CChar>, UnsafeMutablePointer<Int32>) -> UInt32 =
        unsafeBitCast(
            dlsym(handle, "notify_register_check"),
            to: (@convention(c) (UnsafePointer<CChar>, UnsafeMutablePointer<Int32>) -> UInt32).self)
    static let setState: @convention(c) (Int32, UInt64) -> UInt32 = unsafeBitCast(
        dlsym(handle, "notify_set_state"), to: (@convention(c) (Int32, UInt64) -> UInt32).self)
}
