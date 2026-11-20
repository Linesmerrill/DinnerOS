import XCTest

/// An invite link from start to finish on the local API (decision 700): signed out, the
/// screen names who invited you, Accept keeps the code through sign-in and setup, and the new
/// household gets the checklist, the Menu card and Hide. A household that's already set up is
/// told codes are for new households, its own code says so, and a bad code gives the server's
/// words. `dinneros://referral/CODE` opens the same screen a dinneros.app/invite link does.
final class InviteLinkQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 900
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    private func link(_ code: String) throws -> URL {
        try XCTUnwrap(URL(string: "dinneros://referral/\(code)"))
    }

    /// Opens a link the way tapping one does, so a running app gets it where it is (signed in
    /// or not). `XCUIApplication.open` relaunches the app instead. iOS may ask to open it.
    @MainActor
    private func openLink(_ code: String) throws {
        XCUIDevice.shared.system.open(try link(code))
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let open = springboard.buttons["Open"]
        if open.waitForExistence(timeout: 3) { open.tap() }
    }

    @MainActor
    func testANewPersonAcceptsAnInviteLinkAndGetsTheChecklist() throws {
        // 1. The sharer's code.
        let app = launchSignedIn()
        signOut(app)
        signIn(app, subject: "dev-simulator")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The sharer didn't get to the Menu")
        openGiveAFreeMonth(app)
        let code = readCode(app)
        XCTAssertFalse(code.isEmpty, "Couldn't read the sharer's code")
        signOut(app)

        // 2. Signed out, the link opens the invite screen with the code filled in.
        try openLink(code)
        let screen = app.descendants(matching: .any)["InviteCodeScreen"].firstMatch
        XCTAssertTrue(screen.waitForExistence(timeout: 15), "The link didn't open the invite screen")
        let title = app.staticTexts.matching(NSPredicate(format: "label ENDSWITH 'invited you to DinnerOS'")).firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10), "No 'invited you' title")
        XCTAssertTrue(app.staticTexts["You both get a free month of Plus once you're set up."].exists)
        XCTAssertTrue(app.buttons["Accept"].exists && app.buttons["Not Now"].exists)
        for step in ["Create your household", "Add a recipe", "Plan a dinner"] {
            XCTAssertTrue(app.staticTexts[step].exists, "The invite screen is missing \(step)")
        }
        snap(app, "Invite link, new person (\(mode))")
        app.buttons["Accept"].tap()
        XCTAssertTrue(waitGone(screen, timeout: 5), "Accept didn't close the invite screen")

        // 3. Sign in: setup says the code is saved, then the household is made.
        let newName = "QA Link Kitchen \(Int(Date().timeIntervalSince1970) % 1_000_000)"
        signIn(app, subject: "qa-link-\(Int.random(in: 10000...99999))")
        let saved = app.descendants(matching: .any)["InviteCodeSaved"].firstMatch
        XCTAssertTrue(saved.waitForExistence(timeout: 15), "Setup didn't keep the invite code")
        snap(app, "Setup, invite code saved (\(mode))")
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText(newName)
        app.buttons["Create"].tap()
        if app.buttons["Start Planning"].waitForExistence(timeout: 15) { app.buttons["Start Planning"].tap() }

        // 4. The code is used for the new household and the checklist opens.
        let checklist = app.descendants(matching: .any)["ReferralChecklistSheet"].firstMatch
        XCTAssertTrue(checklist.waitForExistence(timeout: 20), "No checklist after setting up")
        XCTAssertTrue(app.staticTexts["Free month: 1 of 3 done"].waitForExistence(timeout: 10), "Wrong count")
        XCTAssertTrue(app.descendants(matching: .any)["ChecklistTimeLeft"].exists, "No time left")
        XCTAssertTrue(app.staticTexts["14 days left"].exists, "The time left isn't 14 days")
        // What Plus is, at the top, and the ways to add a recipe (decisions 706 and 708).
        let explainer = app.descendants(matching: .any)["PlusExplainer"].firstMatch
        XCTAssertTrue(explainer.exists, "The checklist doesn't say what Plus is")
        XCTAssertTrue(explainer.label.contains("What is DinnerOS Plus?"), "The explainer says \(explainer.label)")
        XCTAssertTrue(explainer.label.contains("Autopilot"), "The explainer doesn't mention Autopilot")
        let recipeStep = "Add a recipe: write your own, import one from a website, or pick one from Shared Recipes."
        XCTAssertTrue(app.staticTexts[recipeStep].exists, "The recipe step says something else")
        XCTAssertTrue(
            app.staticTexts["Add a recipe and plan a dinner to unlock your free month"].exists
                || app.descendants(matching: .any).matching(
                    NSPredicate(format: "label CONTAINS 'Add a recipe and plan a dinner to unlock your free month'")
                ).firstMatch.exists,
            "The checklist message says something else")
        snap(app, "Checklist after setup (\(mode))")
        // Add a Recipe opens the Add a Recipe choices.
        let go = app.buttons["ChecklistGo-recipes"]
        XCTAssertEqual(go.label, "Add a Recipe")
        go.tap()
        XCTAssertTrue(waitGone(checklist, timeout: 5), "Add a Recipe didn't close the checklist")
        XCTAssertTrue(app.navigationBars["Add a Recipe"].waitForExistence(timeout: 10), "Add a Recipe went elsewhere")
        for id in ["AddChoice-write", "AddChoice-import", "AddChoice-shared", "AddChoice-companion"] {
            XCTAssertTrue(app.buttons[id].exists, "The choices are missing \(id)")
        }
        snap(app, "Checklist, Add a Recipe opens the choices (\(mode))")
        app.navigationBars["Add a Recipe"].buttons["Cancel"].tap()
        XCTAssertTrue(waitGone(app.navigationBars["Add a Recipe"], timeout: 5), "Cancel didn't close the choices")

        // 5. The Menu card: the count, opens the checklist, Not Now closes it, then Hide.
        let card = app.buttons["ReferralMenuCard"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "No checklist card on the Menu")
        XCTAssertTrue(card.label.contains("Free month: 1 of 3 done"), "The card says \(card.label)")
        snap(app, "Menu, free month card (\(mode))")
        card.tap()
        XCTAssertTrue(checklist.waitForExistence(timeout: 10), "The card didn't open the checklist")
        let notNow = app.buttons["ChecklistNotNow"]
        XCTAssertTrue(notNow.waitForExistence(timeout: 5), "The checklist has no Not Now")
        XCTAssertEqual(notNow.label, "Not Now")
        notNow.tap()
        XCTAssertTrue(waitGone(checklist, timeout: 5), "Not Now didn't close the checklist")
        card.tap()
        XCTAssertTrue(checklist.waitForExistence(timeout: 10), "The card didn't open the checklist again")
        // Plan a Dinner opens Add Recipes on the Menu. On a small phone the Plus lines above the
        // steps push it below the fold.
        let planDinner = app.buttons["ChecklistGo-dinner"]
        reveal(planDinner, in: app, maxSwipes: 4)
        planDinner.tap()
        XCTAssertTrue(waitGone(checklist, timeout: 5))
        XCTAssertTrue(app.searchFields["Search recipes"].firstMatch.waitForExistence(timeout: 10), "No Add Recipes")
        snap(app, "Checklist, Plan a Dinner opens Add Recipes (\(mode))")
        if app.buttons["Done"].firstMatch.exists {
            app.buttons["Done"].firstMatch.tap()
        } else {
            app.swipeDown(velocity: .fast)
        }
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        app.buttons["Hide"].firstMatch.tap()
        XCTAssertTrue(waitGone(card, timeout: 5), "Hide didn't put the card away")

        // 6. Give a Free Month still has the checklist, and no Got a Code.
        openGiveAFreeMonth(app)
        let progress = app.descendants(matching: .any)["ReferralProgress"].firstMatch
        XCTAssertTrue(progress.waitForExistence(timeout: 10), "Give a Free Month lost the checklist")
        XCTAssertFalse(app.staticTexts["Got a Code?"].exists, "Got a Code still shows after using a code")
        snap(app, "Give a Free Month, checklist (\(mode))")

        // Leave the sharer signed in for the next test.
        signOut(app)
        signIn(app, subject: "dev-simulator")
    }

    @MainActor
    func testAHouseholdThatsSetUpIsToldCodesAreForNewHouseholds() throws {
        // Your own code.
        let app = launchSignedIn()
        signOut(app)
        signIn(app, subject: "dev-simulator")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30))
        openGiveAFreeMonth(app)
        let code = readCode(app)
        app.buttons["Menu"].firstMatch.tap()
        try openLink(code)
        let screen = app.descendants(matching: .any)["InviteCodeScreen"].firstMatch
        XCTAssertTrue(screen.waitForExistence(timeout: 15), "The link didn't open the invite screen")
        XCTAssertTrue(app.staticTexts["That's your own code"].waitForExistence(timeout: 10), "Own code not spotted")
        XCTAssertFalse(app.buttons["Accept"].exists, "A set-up household was offered Accept")
        snap(app, "Invite link, your own code (\(mode))")
        screen.buttons["Close"].tap()
        XCTAssertTrue(waitGone(screen, timeout: 5))
        signOut(app)

        // Someone else's code, for a household that's already set up.
        _ = signInAsNewPerson(app, householdName: "QA Set Up Kitchen \(Int.random(in: 100...999))")
        try openLink(code)
        XCTAssertTrue(screen.waitForExistence(timeout: 15), "The link didn't open the invite screen")
        XCTAssertTrue(
            app.staticTexts["This code is for new households"].waitForExistence(timeout: 10),
            "A set-up household wasn't told codes are for new households")
        XCTAssertFalse(app.buttons["Accept"].exists, "A set-up household was offered Accept")
        snap(app, "Invite link, household already set up (\(mode))")
        app.buttons["See Your Code"].tap()
        XCTAssertTrue(
            app.navigationBars["Give a Free Month"].waitForExistence(timeout: 10), "See Your Code went elsewhere")
        XCTAssertTrue(app.staticTexts["Your code"].waitForExistence(timeout: 10))
        snap(app, "Invite link, See Your Code (\(mode))")
        app.navigationBars.buttons.firstMatch.tap()
        screen.buttons["Close"].tap()
        XCTAssertTrue(waitGone(screen, timeout: 5))

        // A code that doesn't exist, opened from a cold start: the server's words.
        app.terminate()
        try openLink("ZZZZZZZZ")
        XCTAssertTrue(screen.waitForExistence(timeout: 30), "A cold start didn't open the invite screen")
        // A cold start in a QA build is signed out; the code check needs no sign-in.
        XCTAssertTrue(
            app.staticTexts["We couldn’t find that code. Check it and try again."].waitForExistence(timeout: 15),
            "A bad code didn't show the server's message")
        snap(app, "Invite link, code not found (\(mode))")
        screen.buttons["Close"].tap()
        XCTAssertTrue(waitGone(screen, timeout: 5))
    }
}
