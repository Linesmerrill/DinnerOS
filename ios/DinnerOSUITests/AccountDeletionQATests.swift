import XCTest

/// App Review 5.1.1(v): a new person signs in, makes a household with a recipe, deletes the
/// account from Household > Account, and lands back on sign-in. Signing in again with the same
/// name is a brand-new person with no household, so nothing was kept.
final class AccountDeletionQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    @MainActor
    func testDeletingTheAccountRemovesItAndReturnsToSignIn() throws {
        applyOrientation()
        let app = XCUIApplication()
        app.launch()
        let subject = QAPeople.subject()
        _ = signInAsNewPerson(app, householdName: QAPeople.kitchen(forSubject: subject), subject: subject)
        addPastedRecipe(app, name: "Tomato Lentil Soup")
        snap(app, "Account deletion › household with a recipe")

        // A new household can get the Autopilot questions over the Menu; put them off.
        let later = app.buttons["Set Up Later"].firstMatch
        if later.waitForExistence(timeout: 3), later.isHittable {
            later.tap()
            sleep(1)
        }
        // A tap while the recipe screen is still closing can be lost; try again until it opens.
        // An iPhone's tab bar, or the iPad's tabs at the top.
        let phoneTab = app.tabBars.buttons["Household"].firstMatch
        let householdTab =
            phoneTab.exists ? phoneTab : app.buttons.matching(NSPredicate(format: "label == 'Household'")).firstMatch
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
        snap(app, "Account deletion › confirmation")
        // The confirmation's Delete Account is the one that isn't the row just tapped.
        let confirm = app.buttons.matching(NSPredicate(format: "label == 'Delete Account'")).allElementsBoundByIndex
            .first { $0.frame != rowFrame && $0.isHittable }
        XCTAssertNotNil(confirm, "No Delete Account in the confirmation")
        confirm?.tap()
        XCTAssertTrue(app.textFields["Subject"].waitForExistence(timeout: 20), "Didn’t return to sign-in")
        snap(app, "Account deletion › back at sign-in")

        // The same name again is a new person: no household to open.
        signIn(app, subject: subject)
        XCTAssertTrue(
            app.staticTexts["Create a Household"].waitForExistence(timeout: 15),
            "Signing in again found the old household")
        snap(app, "Account deletion › signed in again with nothing kept")
    }
}
