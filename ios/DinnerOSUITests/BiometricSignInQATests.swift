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
        // Start signed in with it off.
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

        // Interrupted: the lock screen stays, and Sign in another way goes to the sign-in screen,
        // which keeps Sign In with Face ID (build 119) without asking by itself.
        app = relaunch(app, fake: "interrupt")
        let another = app.buttons["lock.anotherWay"]
        XCTAssertTrue(another.waitForExistence(timeout: 15), "No lock screen")
        XCTAssertTrue(app.buttons["lock.unlock"].exists)
        snap(app, "Face ID › Lock screen")
        another.tap()
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 15), "Sign in another way didn't go to sign-in")
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 5), "Sign in another way lost Face ID sign-in")

        // Signing in another way keeps it on: no offer.
        devSignIn(app).tap()
        XCTAssertFalse(
            app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 4),
            "Offered Face ID while it was on")
        XCTAssertEqual(toggleValue(app), true, "Signing in another way turned Face ID off")

        // Cancel at launch: the sign-in screen with every way to sign in, and no second ask.
        app = relaunch(app, fake: "cancel,match")
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "A cancel didn't go to sign-in")
        XCTAssertTrue(faceIDButton(app).exists, "A cancel lost Face ID sign-in")
        snap(app, "Face ID › Cancel goes to sign-in")
        faceIDButton(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Sign In with Face ID didn't sign in")

        // Faces changed since it was turned on: it's removed, with a message, then offered again.
        app = relaunch(app, fake: "changed")
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "Changed faces didn't go to sign-in")
        XCTAssertTrue(
            app.descendants(matching: .any)["signIn.biometricNotice"].waitForExistence(timeout: 5),
            "No message after faces changed")
        XCTAssertFalse(faceIDButton(app).exists, "Face ID sign-in stayed after faces changed")
        snap(app, "Face ID › Faces changed")
        devSignIn(app).tap()
        XCTAssertTrue(app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 15), "No offer")
        answerOffer(app, use: true)
        XCTAssertTrue(waitForToggle(app, true, opening: true), "Use Face ID in the offer didn't turn it on")
        // Turned on while this launch's fake faces were "changed": off again, so the next
        // launch's faces count as the enrolled ones.
        setToggle(app, on: false)

        // Away longer than the relock time (2 seconds here, 15 minutes for real): locked over
        // the app, then a match opens it where it was.
        app = relaunch(app, fake: "match", relockSeconds: 2)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "The app didn't open")
        setToggle(app, on: true)
        away(app)
        XCTAssertTrue(waitGone(app.buttons["lock.anotherWay"], timeout: 10), "The lock stayed after a match")
        XCTAssertTrue(
            app.switches["account.biometricSignIn"].firstMatch.waitForExistence(timeout: 10),
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
        setToggle(app, on: false)
        app.terminate()
    }

    /// The whole loop (build 119): sign in, turn Face ID on, sign out, and sign back in with it.
    /// A non-match keeps every other way to sign in; a new launch asks by itself once; turning
    /// it off removes the button.
    @MainActor
    func testFaceIDSignInLoop() throws {
        var app = launch(fake: "match")
        signInIfNeeded(app)
        answerOffer(app, use: false)
        setToggle(app, on: true)

        // Signing out keeps Face ID sign-in, and the screen doesn't ask by itself right after.
        signOut(app)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign In with Face ID after signing out")
        XCTAssertTrue(devSignIn(app).exists, "The other ways to sign in are gone")
        XCTAssertTrue(
            app.buttons.matching(NSPredicate(format: "label CONTAINS 'Apple'")).firstMatch.exists,
            "No Apple sign-in beside Face ID")
        XCTAssertLessThan(
            faceIDButton(app).frame.minY, devSignIn(app).frame.minY, "Face ID isn't the first way to sign in")
        XCTAssertFalse(householdTab(app).exists, "Signed in by itself after signing out")
        snap(app, "Sign-in › Face ID (\(mode))")

        // No match, then a match.
        app = relaunch(app, fake: "nomatch,match")
        // A new launch asks once by itself: here, no match. The screen stays as it was.
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 15), "No Sign In with Face ID at launch")
        sleep(2)
        XCTAssertFalse(householdTab(app).exists, "No match signed in")
        XCTAssertTrue(devSignIn(app).isHittable, "No match left no other way to sign in")
        XCTAssertTrue(faceIDButton(app).isEnabled)
        snap(app, "Sign-in › Face ID no match")
        faceIDButton(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A match didn't sign in")

        // Signed out on a past launch, a new launch asks by itself and a match signs in.
        signOut(app)
        app = relaunch(app, fake: "match")
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "The ask at launch didn't sign in")

        // A cancel: the button and the other ways stay.
        signOut(app)
        app = relaunch(app, fake: "cancel,match")
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 15))
        sleep(2)
        XCTAssertTrue(devSignIn(app).isHittable, "A cancel left no other way to sign in")
        devSignIn(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Signing in another way didn't work")

        // Turned off in Account: signing out shows no Face ID button.
        setToggle(app, on: false)
        signOut(app)
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 10))
        XCTAssertFalse(faceIDButton(app).waitForExistence(timeout: 3), "Face ID sign-in stayed after turning it off")
        app = relaunch(app, fake: "match")
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 15))
        XCTAssertFalse(faceIDButton(app).exists, "Face ID sign-in came back after a relaunch")
        app.terminate()
    }

    /// The sign-in screen with Face ID kept, for the screenshot checklist on every device.
    @MainActor
    func testSignInScreenWithFaceID() throws {
        let app = launch(fake: "match")
        signInIfNeeded(app)
        answerOffer(app, use: false)
        setToggle(app, on: true)
        signOut(app)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign In with Face ID after signing out")
        XCTAssertTrue(faceIDButton(app).isHittable, "Sign In with Face ID is off the screen")
        sleep(1)
        snap(app, "Sign-in › Face ID (\(mode))")
        assertSignInButtonsMatch(app)
        faceIDButton(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Sign In with Face ID didn't sign in")
        setToggle(app, on: false)
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
        XCTAssertTrue(faceIDButton(app).exists, "No match lost Face ID sign-in")

        // The sign-in screen's button (build 119): no match keeps the screen, a match signs in.
        faceIDButton(app).tap()
        sleep(2)
        faceMatches(false)
        let again = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons["Cancel"].firstMatch
        if again.waitForExistence(timeout: 8) { again.tap() } else { faceMatches(false) }
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 10), "No match left the sign-in screen")
        XCTAssertFalse(householdTab(app).exists, "No match signed in")
        faceIDButton(app).tap()
        sleep(2)
        faceMatches(true)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A matched face didn't sign in")

        // Signed out, the button signs in again; turned off, it's gone.
        signOut(app)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign In with Face ID after signing out")
        snap(app, "Simulator Face ID › Sign-in screen")
        faceIDButton(app).tap()
        sleep(2)
        faceMatches(true)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A matched face didn't sign in")
        setToggle(app, on: false)
        signOut(app)
        XCTAssertFalse(faceIDButton(app).waitForExistence(timeout: 3), "Face ID sign-in stayed after turning it off")
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

    /// The sign-in buttons share one size (build 120): Face ID, Apple and Google are the same
    /// height and width, and line up.
    @MainActor
    private func assertSignInButtonsMatch(_ app: XCUIApplication) {
        let apple = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Apple'")).firstMatch
        let google = app.buttons["signIn.google"].firstMatch
        var buttons = [("Face ID", faceIDButton(app)), ("Apple", apple)]
        if google.exists { buttons.append(("Google", google)) }
        XCTAssertTrue(apple.exists, "No Apple button")
        let reference = faceIDButton(app).frame
        XCTAssertEqual(reference.height, 50, accuracy: 6, "Sign In with Face ID is \(reference.height) pt tall")
        for (name, button) in buttons {
            let frame = button.frame
            XCTAssertEqual(
                frame.height, reference.height, accuracy: 1,
                "\(name) is \(frame.height) pt tall, Face ID \(reference.height)")
            XCTAssertEqual(
                frame.width, reference.width, accuracy: 1,
                "\(name) is \(frame.width) pt wide, Face ID \(reference.width)")
            XCTAssertEqual(frame.minX, reference.minX, accuracy: 1, "\(name) doesn't line up with Face ID")
        }
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    private func faceIDButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons["signIn.biometric"].firstMatch
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
