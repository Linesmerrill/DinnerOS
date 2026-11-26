import Darwin
import XCTest

/// Face ID sign-in (decision 760): the offer, the Account toggle, the unlock at launch, every way
/// back to normal sign-in, and no ask when the app comes back from the background.
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

        // A relaunch with a valid session opens the app with no check (decision 767).
        app = relaunch(app, fake: "interrupt")
        assertOpenWithoutFaceID(app, after: "a relaunch with Face ID on")

        // A session older builds kept only behind Face ID: the lock asks once and a match opens
        // the app; the next launch doesn't ask.
        app = relaunch(app, fake: "match", oldKeychain: true)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A match didn’t open the app")
        XCTAssertFalse(app.buttons["lock.anotherWay"].exists)
        app = relaunch(app, fake: "interrupt")
        assertOpenWithoutFaceID(app, after: "a relaunch after the one-time check")

        // Interrupted: the lock screen stays, and Sign in another way goes to the sign-in screen,
        // which keeps Sign In with Face ID (build 119) without asking by itself.
        app = relaunch(app, fake: "interrupt", oldKeychain: true)
        let another = app.buttons["lock.anotherWay"]
        XCTAssertTrue(another.waitForExistence(timeout: 15), "No lock screen")
        XCTAssertTrue(app.buttons["lock.unlock"].exists)
        snap(app, "Face ID › Lock screen")
        another.tap()
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 15), "Sign in another way didn’t go to sign-in")
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 5), "Sign in another way lost Face ID sign-in")

        // Signing in another way keeps it on: no offer.
        devSignIn(app).tap()
        XCTAssertFalse(
            app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 4),
            "Offered Face ID while it was on")
        XCTAssertEqual(toggleValue(app), true, "Signing in another way turned Face ID off")

        // Cancel at launch: the sign-in screen with every way to sign in, and no second ask.
        app = relaunch(app, fake: "cancel,match", oldKeychain: true)
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "A cancel didn’t go to sign-in")
        XCTAssertTrue(faceIDButton(app).exists, "A cancel lost Face ID sign-in")
        snap(app, "Face ID › Cancel goes to sign-in")
        faceIDButton(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Sign In with Face ID didn’t sign in")

        // Faces changed since it was turned on: it's removed, with a message, then offered again.
        app = relaunch(app, fake: "changed", oldKeychain: true)
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "Changed faces didn’t go to sign-in")
        XCTAssertTrue(
            app.descendants(matching: .any)["signIn.biometricNotice"].waitForExistence(timeout: 5),
            "No message after faces changed")
        XCTAssertFalse(faceIDButton(app).exists, "Face ID sign-in stayed after faces changed")
        snap(app, "Face ID › Faces changed")
        devSignIn(app).tap()
        XCTAssertTrue(app.alerts["Use Face ID to sign in next time?"].waitForExistence(timeout: 15), "No offer")
        answerOffer(app, use: true)
        XCTAssertTrue(waitForToggle(app, true, opening: true), "Use Face ID in the offer didn’t turn it on")
        // Turned on while this launch's fake faces were "changed": off again, so the next
        // launch's faces count as the enrolled ones.
        setToggle(app, on: false)

        app.terminate()
    }

    /// Face ID is only a sign-in step (Merrill, Nov 22 and Nov 25): with it on and signed in,
    /// coming back from the background, from the app switcher, and a relaunch (as after iOS ends
    /// the app in the background) all open the app with no Face ID check. Every fake check after
    /// turning it on is "interrupt", which leaves the lock screen up, so any ask shows.
    @MainActor
    func testComingBackFromTheBackgroundNeverAsksForFaceID() async throws {
        // A person of this run's own, signed in with email, so Account shows a real-looking
        // name and "Email" rather than the developer account (Hark's build 122 round 5, item 2).
        let person = try await EmailPerson.register()
        // Every check after the first is "interrupt", which would leave the lock screen up, so
        // any ask on the way back shows.
        var app = launch(fake: "match,interrupt")
        signInWithEmail(app, person)
        answerOffer(app, use: false)
        setToggle(app, on: true)
        let toggle = app.switches["account.biometricSignIn"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 5), "Not on the Account screen")
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for seconds: UInt32 in [8] {
            let failuresBefore = testRun?.totalFailureCount ?? 0
            defer {
                let passed = (testRun?.totalFailureCount ?? 0) == failuresBefore
                print("QA-FACEID-STEP: \(passed ? "passed" : "FAILED"), no Face ID prompt after \(seconds)s in the background (\(mode))")
            }
            XCUIDevice.shared.press(.home)
            sleep(seconds)
            app.activate()
            XCTAssertFalse(
                app.buttons["lock.anotherWay"].waitForExistence(timeout: 5), "Face ID was asked after \(seconds)s away")
            XCTAssertFalse(app.buttons["lock.unlock"].exists, "The lock screen showed after \(seconds)s away")
            XCTAssertFalse(springboard.alerts.firstMatch.exists, "A system prompt showed after \(seconds)s away")
            XCTAssertFalse(devSignIn(app).exists, "Coming back signed out")
            XCTAssertTrue(toggle.waitForExistence(timeout: 5), "Coming back didn’t return to the Account screen")
            XCTAssertTrue(waitForToggle(app, true), "Coming back turned Face ID off")
        }
        // Account shows the person and how they signed in (each row reads "Title, value").
        XCTAssertTrue(app.staticTexts["Name, \(person.name)"].exists, "Account doesn’t show \(person.name)")
        XCTAssertTrue(app.staticTexts["Email, \(person.email)"].exists, "Account doesn’t show their email")
        XCTAssertTrue(
            app.staticTexts["Signed in with, Email"].waitForExistence(timeout: 5),
            "Account doesn’t say they signed in with email")
        snap(app, "Face ID › Back from the background (\(mode))")
        // The end of Account: the last rows rest whole above the tab bar, not washed out by its
        // fade, and are seen whole in a shot of their own (Hark's review, build 121 round 2: Sign
        // Out and Delete Account under the fade as Account opens on an SE and the Duo).
        var fades = FadeQAChecks.checkRest(app, screen: "Account, as it opens", edges: .bottom)
        print("QA-INFO: \(fades)")
        FadeQAChecks.scrollToEnd(app) { capture(app, settle: false) }
        fades = FadeQAChecks.checkRest(app, screen: "Account, scrolled to the end", edges: .bottom)
        let end = capture(app)
        fades += FadeQAChecks.checkClear(
            app.buttons["Delete Account"].firstMatch, what: "Delete Account", in: app, screen: "Account, at the end",
            image: end.image)
        // Kept on every config: where Account fits, the same screen as it opened (an allowed
        // look-alike, QAShotCompare.allowedPairs).
        keep(end, "Face ID › Back from the background › end (\(mode))")
        XCTAssertTrue(fades.isEmpty, fades.joined(separator: "\n"))

        // The app switcher: up from the bottom edge and held, then back to the app.
        openAppSwitcher(app)
        app.activate()
        assertOpenWithoutFaceID(app, after: "the app switcher")

        // Ended while in the background, then opened again: a cold launch with a valid session.
        XCUIDevice.shared.press(.home)
        sleep(2)
        app = relaunch(app, fake: "interrupt")
        assertOpenWithoutFaceID(app, after: "a relaunch from the background")
        // Ended in front, then opened again.
        app = relaunch(app, fake: "interrupt")
        assertOpenWithoutFaceID(app, after: "a relaunch")
        // And back from the background once more after the relaunch.
        XCUIDevice.shared.press(.home)
        sleep(3)
        app.activate()
        assertOpenWithoutFaceID(app, after: "the background after a relaunch")
        setToggle(app, on: false)
        app.terminate()
    }

    /// Signed in and nothing asking: no lock screen, no system prompt, not signed out.
    @MainActor
    private func assertOpenWithoutFaceID(_ app: XCUIApplication, after: String) {
        let failuresBefore = testRun?.totalFailureCount ?? 0
        // One line per step in the log, so a review can see each one passed in each config.
        defer {
            let passed = (testRun?.totalFailureCount ?? 0) == failuresBefore
            print("QA-FACEID-STEP: \(passed ? "passed" : "FAILED"), no Face ID prompt after \(after) (\(mode))")
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        // Long enough for a launch's restore and any ask to show.
        XCTAssertFalse(app.buttons["lock.anotherWay"].waitForExistence(timeout: 6), "Face ID was asked after \(after)")
        XCTAssertFalse(app.buttons["lock.unlock"].exists, "The lock screen showed after \(after)")
        XCTAssertFalse(springboard.alerts.firstMatch.exists, "A system prompt showed after \(after)")
        XCTAssertFalse(devSignIn(app).exists, "Signed out after \(after)")
        XCTAssertFalse(faceIDButton(app).exists, "The Face ID sign-in screen showed after \(after)")
        if app.buttons["Start Planning"].exists { app.buttons["Start Planning"].tap() }
        if app.buttons["Thanks"].exists { app.buttons["Thanks"].tap() }
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "The app didn’t open after \(after)")
        XCTAssertTrue(waitForToggle(app, true, opening: true), "Face ID sign-in turned off after \(after)")
    }

    /// Up from the bottom edge, held, as a person opens the app switcher. The app goes inactive
    /// there, not to the background. On a phone with a home button (the small iPhone) the same
    /// swipe opens Control Center, which also leaves the app inactive; XCUITest can't press home
    /// twice fast enough for that phone's switcher.
    @MainActor
    private func openAppSwitcher(_ app: XCUIApplication) {
        let bottom = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.998))
        let middle = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6))
        bottom.press(forDuration: 0.05, thenDragTo: middle, withVelocity: .slow, thenHoldForDuration: 1.2)
        sleep(2)
        // Not in the screenshot set: a debug look that the switcher really opened.
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "debug app switcher (\(mode))"
        shot.lifetime = .keepAlways
        add(shot)
        print("QA-INFO: after the app switcher gesture the app is \(app.state.rawValue)")
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
            faceIDButton(app).frame.minY, devSignIn(app).frame.minY, "Face ID isn’t the first way to sign in")
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
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A match didn’t sign in")

        // Signed out on a past launch, a new launch asks by itself and a match signs in.
        signOut(app)
        app = relaunch(app, fake: "match")
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "The ask at launch didn’t sign in")

        // A cancel: the button and the other ways stay.
        signOut(app)
        app = relaunch(app, fake: "cancel,match")
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 15))
        sleep(2)
        XCTAssertTrue(devSignIn(app).isHittable, "A cancel left no other way to sign in")
        devSignIn(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Signing in another way didn’t work")

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
        // First the sign-in screen as everyone sees it, without Face ID and without the Debug
        // build's developer sign-in (build 121: the screenshots show the Release screen).
        var app = launch(fake: "match", hideDeveloper: true)
        let apple = app.buttons["signIn.apple"].firstMatch
        XCTAssertTrue(apple.waitForExistence(timeout: 10), "No Continue with Apple")
        XCTAssertFalse(faceIDButton(app).exists, "Face ID showed before it was turned on")
        XCTAssertFalse(devSignIn(app).exists, "Developer sign-in showed in a screenshot launch")
        assertFitsOnScreen(app)
        sleep(1)
        snap(app, "Sign-in (\(mode))")
        assertSignInButtonsMatch(app, faceID: false)

        app = relaunch(app, fake: "match")
        signInIfNeeded(app)
        answerOffer(app, use: false)
        setToggle(app, on: true)
        signOut(app)

        // Launched again signed out: the automatic Face ID prompt is cancelled, so the screen stays.
        app = relaunch(app, fake: "cancel,match", hideDeveloper: true)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign in with Face ID after signing out")
        XCTAssertTrue(faceIDButton(app).isHittable, "Sign in with Face ID is off the screen")
        XCTAssertEqual(faceIDButton(app).label, "Sign in with Face ID")
        sleep(2)
        assertFitsOnScreen(app)
        snap(app, "Sign-in › Face ID (\(mode))")
        assertSignInButtonsMatch(app, faceID: true)
        faceIDButton(app).tap()
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "Sign in with Face ID didn’t sign in")
        setToggle(app, on: false)
        app.terminate()
    }

    /// The privacy line and the Privacy Policy link fit on the screen without scrolling.
    @MainActor
    private func assertFitsOnScreen(_ app: XCUIApplication) {
        let privacy = app.links["Privacy Policy"].firstMatch
        guard privacy.waitForExistence(timeout: 3) else { return }
        XCTAssertLessThanOrEqual(privacy.frame.maxY, app.frame.maxY, "The Privacy Policy link is below the screen")
        let line = app.staticTexts["Only the people you invite can see your household\u{2019}s plans."].firstMatch
        let plain = app.staticTexts["Only the people you invite can see your household’s plans."].firstMatch
        let shown = line.exists ? line : plain
        if shown.exists {
            XCTAssertLessThanOrEqual(shown.frame.maxY, app.frame.maxY, "The privacy line is below the screen")
        }
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
        XCTAssertTrue(waitForToggle(app, true), "A matched face didn’t turn it on")
        snap(app, "Simulator Face ID › On")

        // A relaunch and a trip to the background with a valid session: no system check.
        app = relaunch(app, fake: nil)
        assertOpenWithoutFaceID(app, after: "a relaunch with the simulator’s Face ID")
        XCUIDevice.shared.press(.home)
        sleep(3)
        app.activate()
        assertOpenWithoutFaceID(app, after: "the background with the simulator’s Face ID")

        // Signed out, a new launch has no valid session: it asks by itself. No match, then
        // Cancel: the sign-in screen with every way to sign in.
        signOut(app)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign In with Face ID after signing out")
        app = relaunch(app, fake: nil)
        sleep(3)
        faceMatches(false)
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let cancel = springboard.buttons["Cancel"].firstMatch
        if cancel.waitForExistence(timeout: 8) {
            snap(app, "Simulator Face ID › No match")
            cancel.tap()
        } else {
            faceMatches(false)
        }
        XCTAssertTrue(devSignIn(app).waitForExistence(timeout: 20), "No match didn’t stay on sign-in")
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
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A matched face didn’t sign in")

        // Signed out, the button signs in again; turned off, it's gone.
        signOut(app)
        XCTAssertTrue(faceIDButton(app).waitForExistence(timeout: 10), "No Sign In with Face ID after signing out")
        snap(app, "Simulator Face ID › Sign-in screen")
        faceIDButton(app).tap()
        sleep(2)
        faceMatches(true)
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 20), "A matched face didn’t sign in")
        setToggle(app, on: false)
        signOut(app)
        XCTAssertFalse(faceIDButton(app).waitForExistence(timeout: 3), "Face ID sign-in stayed after turning it off")
        app.terminate()
    }

    // MARK: Steps

    @MainActor
    private func launch(fake: String?, hideDeveloper: Bool = false, oldKeychain: Bool = false) -> XCUIApplication {
        let app = XCUIApplication()
        if hideDeveloper { app.launchEnvironment["QA_HIDE_DEVELOPER_SIGN_IN"] = "1" }
        if oldKeychain { app.launchEnvironment["DINNEROS_QA_OLD_FACE_ID_KEYCHAIN"] = "1" }
        applyOrientation()
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        if let fake { app.launchEnvironment["DINNEROS_FAKE_BIOMETRICS"] = fake }
        // Where the fades are, for the check at the end of Account (`FadeQAChecks`).
        app.launchEnvironment["QA_MARKERS"] = "1"
        app.launch()
        assertFullScreen(app)
        return app
    }

    /// Relaunches; with `expectLock`, the lock screen must show first.
    @MainActor
    private func relaunch(
        _ app: XCUIApplication, fake: String?, expectLock: Bool = false, hideDeveloper: Bool = false,
        oldKeychain: Bool = false
    ) -> XCUIApplication {
        app.terminate()
        let next = launch(fake: fake, hideDeveloper: hideDeveloper, oldKeychain: oldKeychain)
        if expectLock {
            XCTAssertTrue(next.buttons["lock.anotherWay"].waitForExistence(timeout: 15), "No lock screen at launch")
        }
        return next
    }

    /// The sign-in buttons share one size (build 120) and one title font (build 121): Face ID,
    /// Apple and Google are the same height and width, line up, and their titles are drawn the
    /// same size (measured from the screenshot, so a style that changes the font fails here).
    @MainActor
    private func assertSignInButtonsMatch(_ app: XCUIApplication, faceID: Bool) {
        let apple = app.buttons["signIn.apple"].firstMatch
        let google = app.buttons["signIn.google"].firstMatch
        var buttons = [("Apple", apple)]
        if faceID { buttons.insert(("Face ID", faceIDButton(app)), at: 0) }
        if google.exists { buttons.append(("Google", google)) }
        XCTAssertTrue(apple.exists, "No Apple button")
        let reference = buttons[0].1.frame
        XCTAssertEqual(reference.height, 50, accuracy: 6, "\(buttons[0].0) is \(reference.height) pt tall")
        for (name, button) in buttons {
            let frame = button.frame
            XCTAssertEqual(
                frame.height, reference.height, accuracy: 1,
                "\(name) is \(frame.height) pt tall, \(buttons[0].0) \(reference.height)")
            XCTAssertEqual(
                frame.width, reference.width, accuracy: 1,
                "\(name) is \(frame.width) pt wide, \(buttons[0].0) \(reference.width)")
            XCTAssertEqual(frame.minX, reference.minX, accuracy: 1, "\(name) doesn’t line up with \(buttons[0].0)")
        }
        // The same upright image the screenshots keep (an iPad on its side included).
        guard let shot = UIImage(data: Self.uprightPNG(XCUIScreen.main.screenshot().image)) else {
            XCTFail("No screenshot to measure")
            return
        }
        var heights: [(String, CGFloat)] = []
        for (name, button) in buttons {
            guard let height = SignInTitleMeasure.firstLetterHeight(of: button.frame, in: shot, screen: app.frame)
            else {
                XCTFail("Couldn’t measure the \(name) title")
                continue
            }
            heights.append((name, height))
        }
        let summary = heights.map { "\($0.0) \(String(format: "%.1f", $0.1)) pt" }.joined(separator: ", ")
        let attachment = XCTAttachment(string: "Sign-in title letter heights: \(summary)")
        attachment.name = "debug sign-in title heights (\(mode))"
        attachment.lifetime = .keepAlways
        add(attachment)
        if let first = heights.first {
            for (name, height) in heights.dropFirst() {
                XCTAssertEqual(height, first.1, accuracy: 0.7, "\(name)'s title isn’t the same size: \(summary)")
            }
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
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 30), "The app didn’t open after sign-in")
    }

    /// Signs in through "Sign in with email" as a registered person, then past the welcome.
    @MainActor
    private func signInWithEmail(_ app: XCUIApplication, _ person: EmailPerson) {
        let open = app.buttons["signIn.email.open"]
        guard open.waitForExistence(timeout: 15) else {
            XCTFail("No Sign in with email: the local API needs build 123's development email accounts")
            return
        }
        open.tap()
        let field = app.textFields["signIn.email.address"]
        XCTAssertTrue(field.waitForExistence(timeout: 5), "No email field")
        field.tap()
        field.typeText(person.email)
        let secure = app.secureTextFields["signIn.email.password"]
        secure.tap()
        secure.typeText(person.password)
        app.buttons["signIn.email.submit"].tap()
        // The seeded household's first sign-in opens on the welcome, over the tabs.
        let start = app.buttons["Start Planning"].firstMatch
        let deadline = Date.now.addingTimeInterval(30)
        while Date.now < deadline {
            if start.exists {
                start.tap()
                break
            }
            if householdTab(app).exists, !start.waitForExistence(timeout: 2) { break }
        }
        if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(householdTab(app).waitForExistence(timeout: 30), "The app didn’t open after email sign-in")
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
        let account = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Account'")).firstMatch
        // The Face ID offer and the thanks sheet can come up late, after the steps that answer them
        // stopped waiting, and take the tap meant for the tab: answer them and tap again.
        for _ in 0..<3 where !app.navigationBars["Household"].exists {
            let offer = app.alerts["Use Face ID to sign in next time?"]
            if offer.exists { offer.buttons["Not Now"].tap() }
            // The one-time "Thanks for testing" sheet can also come up after signInIfNeeded looked.
            if app.buttons["Thanks"].exists { app.buttons["Thanks"].tap() }
            // A person new this run sees the welcome once, which can come up after a relaunch
            // (build 123, the Duo): Start Planning only closes it.
            if app.buttons["Start Planning"].exists { app.buttons["Start Planning"].tap() }
            (household.waitForExistence(timeout: 5) ? household : householdTab(app)).tap()
            _ = app.navigationBars["Household"].waitForExistence(timeout: 4)
        }
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
            // A tap as Account was still settling in left the toggle as it was (small-dark, build
            // 121 round 2): once more if it hasn't moved.
            if !waitForToggle(app, on, timeout: 8) {
                print("QA-NOTE: the Face ID toggle didn’t move on the first tap; tapping it again")
                app.switches["account.biometricSignIn"].firstMatch.switches.firstMatch.tap()
            }
        }
        XCTAssertTrue(waitForToggle(app, on), "The Face ID toggle didn’t turn \(on ? "on" : "off")")
    }

    @MainActor
    private func waitForToggle(
        _ app: XCUIApplication, _ on: Bool, opening: Bool = false, timeout: TimeInterval = 10
    ) -> Bool {
        if opening { openAccount(app) }
        let toggle = app.switches["account.biometricSignIn"].firstMatch
        let done = expectation(for: NSPredicate(format: "value == %@", on ? "1" : "0"), evaluatedWith: toggle)
        return XCTWaiter.wait(for: [done], timeout: timeout) == .completed
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

/// A person of one test run's own who signs in with email (Hark's build 122 round 5, item 2),
/// registered on the local development API (POST /auth/dev/email-account, local Mongo only)
/// rather than the one shared App Review account, so ten simulators can each have their own.
private struct EmailPerson {
    let name: String
    let email: String
    let password: String

    /// A realistic person ("jordan.m.avery@example.com", no run number) with a fresh password.
    static func register() async throws -> EmailPerson {
        let name = QAPeople.names.randomElement()!
        let person = EmailPerson(name: name, email: QAPeople.email(for: name), password: UUID().uuidString)
        var request = URLRequest(url: QAOwnHousehold.apiBase.appending(path: "auth/dev/email-account"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: [
            "email": person.email, "password": person.password, "displayName": person.name,
        ])
        let (_, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 204 else {
            throw NSError(
                domain: "EmailPerson", code: status,
                userInfo: [
                    NSLocalizedDescriptionKey:
                        "Couldn’t register an email account (\(status)): the local API needs build 123 "
                        + "(development email accounts) with developer sign-in on"
                ])
        }
        return person
    }
}
