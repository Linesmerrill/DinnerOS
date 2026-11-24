import XCTest

/// Screens the tab sweep doesn't reach by tapping rows: a brand-new person's first run, the
/// paywall a household without Plus sees, recipe pages of every kind, cooking, and the sheets
/// behind buttons. Each step keeps a screenshot and checks for blank or unnamed buttons.
/// Light or dark comes from the simulator (`xcrun simctl ui <device> appearance dark`).
final class ScreensQATests: XCTestCase {
    private var problems: [String] = []

    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    // MARK: A new person

    @MainActor
    func testNewPersonFirstRun() throws {
        applyOrientation()
        let app = XCUIApplication()
        // Marks a photo showing its placeholder, for the Shared Recipes check.
        app.launchEnvironment["QA_MARKERS"] = "1"
        app.launch()
        let subject = app.textFields["Subject"]
        if subject.waitForExistence(timeout: 8) {
            subject.tap()
            subject.press(forDuration: 1.0)
            if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
            subject.typeText(QAPeople.subject("Maya Rivera"))
        } else {
            XCTFail("Already signed in: uninstall the app before this test")
            return
        }
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch.tap()

        XCTAssertTrue(app.staticTexts["Set Up Your Household"].waitForExistence(timeout: 15))
        shot(app, "First run › Set Up Your Household")
        app.staticTexts["Join with a Code"].tap()
        // The pushed screen, settled: its code field is on screen and the push has finished.
        let codeField = app.textFields.firstMatch
        _ = codeField.waitForExistence(timeout: 5)
        waitStill(app)
        shot(app, "First run › Join with a Code")
        back(app)
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        shot(app, "First run › New Household")
        name.tap()
        name.typeText("Rivera Kitchen")
        app.buttons["Create"].tap()

        XCTAssertTrue(app.staticTexts["Welcome to DinnerOS"].waitForExistence(timeout: 15))
        shot(app, "First run › Welcome")

        // A starter recipe from Shared Recipes, opened from the welcome screen.
        app.staticTexts["Shared Recipes"].firstMatch.tap()
        sleep(3)
        shot(app, "Shared Recipes")
        problems += MenuQAChecks.checkSharedRecipeRows(app, screen: "Shared Recipes")
        // A recipe card in one of the rows (decision 710).
        let first = app.buttons["DiscoverCard"].firstMatch
        if first.waitForExistence(timeout: 5) {
            first.tap()
            sleep(3)
            waitForPushToSettle(app)
            shot(app, "Recipe page › starter (Shared Recipes)")
            back(app)
        } else {
            problems.append("Shared Recipes: nothing to open")
        }
        back(app)

        if app.buttons["Start Planning"].waitForExistence(timeout: 5) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 10))
        shot(app, "Menu › new household")
        for tab in ["Shop", "Pantry"] {
            app.buttons[tab].firstMatch.tap()
            sleep(2)
            shot(app, "\(tab) › new household")
        }

        // The paywall: this household has no Plus.
        app.buttons["Household"].firstMatch.tap()
        sleep(2)
        // A household without Plus: "No Plus yet" and the way to Plus. On an iPad it differs from
        // the sweep's Household tab in under 1% of the screen, so the pair is allowed
        // (QAShotCompare.allowedPairs; build 122 round 4, Hark's item 15a).
        shot(app, "Household › new household")
        let plus = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Plus'")).firstMatch
        if plus.waitForExistence(timeout: 5) {
            plus.tap()
            sleep(3)
            shot(app, "Plus paywall")
            back(app)
        } else {
            problems.append("Household: no way to Plus for a household without it")
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    // MARK: The main test household

    @MainActor
    func testDeeperScreens() throws {
        var app = launchSignedIn()
        let env = ProcessInfo.processInfo.environment

        // Recipe pages: typed by hand, imported from a meal kit. Found the way a person would,
        // by searching All Meals (XCTest's own link opening signed the app out).
        for (kind, key) in [("typed by hand", "QA_MANUAL_NAME"), ("imported", "QA_IMPORTED_NAME")] {
            guard let recipe = env[key] else {
                problems.append("Recipe page › \(kind): no name given")
                continue
            }
            // A fresh Menu each time, so the search starts from the top.
            if kind != "typed by hand" { app = launchSignedIn() }
            let field = app.textFields["Search meals"].firstMatch
            if !field.exists {
                let search = app.buttons["Search meals"].firstMatch
                // All Meals is the Menu's last section and the list is lazy: on a large iPhone
                // with a full week it can take more than 12 swipes to reach (build 121).
                reveal(search, in: app, maxSwipes: 24)
                // Missed it (the lazy list skipped past, or a carousel took the swipe): from the
                // top once more.
                if !search.exists {
                    scrollToTop(app)
                    scrollToTop(app)
                    reveal(search, in: app, maxSwipes: 24)
                }
                search.tap()
            }
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            field.tap()
            let typed = (field.value as? String) ?? ""
            if !typed.isEmpty, typed != "Search meals" {
                field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed.count))
            }
            field.typeText(recipe)
            // The results, loaded: no gray loading card in the screenshot.
            _ = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", recipe)).firstMatch
                .waitForExistence(timeout: 10)
            snap(app, "Menu › search typed, \(kind) (\(mode))")
            problems += MenuQAChecks.checkSearchAddAtMargin(
                app, recipe: recipe, screen: "Menu › search typed, \(kind) (\(mode))")
            // Put the keyboard away with Done, then tap the result card that's on screen: the same
            // name can also sit in a carousel further up.
            let done = app.toolbars.buttons["Done"].firstMatch
            if done.waitForExistence(timeout: 2) { done.tap() }
            _ = waitGone(app.keyboards.firstMatch, timeout: 3)
            snap(app, "Menu › search after Done, \(kind) (\(mode))")
            // The week strip keeps the week you're on (it once ran back to weeks from 2025).
            // Expanded, this week's pill; collapsed, the one-line "This Week · Nov 15–21".
            let thisWeek = app.descendants(matching: .any)
                .matching(NSPredicate(format: "label BEGINSWITH 'This Week'")).firstMatch
            if !(thisWeek.exists && app.frame.intersects(thisWeek.frame) && !thisWeek.frame.isEmpty) {
                problems.append("Menu › search: the week strip lost this week")
            }
            let cards = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", recipe))
            var card: XCUIElement?
            for attempt in 0..<12 where card == nil {
                card = cards.allElementsBoundByIndex.first { $0.isHittable }
                if card == nil {
                    // The result can sit below the fold under the bars: bring it up once.
                    if attempt == 5, cards.firstMatch.exists { reveal(cards.firstMatch, in: app, maxSwipes: 6) }
                    sleep(1)
                }
            }
            guard let card else {
                snap(app, "Recipe page › \(kind): search result missing (\(mode))", unique: false)
                problems.append("Recipe page › \(kind): \(recipe) isn’t in All Meals")
                continue
            }
            card.tap()
            XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 15), "\(kind) didn’t open")
            waitForPushToSettle(app)
            shot(app, "Recipe page › \(kind)")
            // Each tab, picked by its accessibility identifier and checked selected (build 121: a
            // tap by label landed on Customize, Description or a rating tag), then scrolled so its
            // content is on screen. A copy of a screen already kept fails in `keep`.
            for (tab, id) in [("Ingredients", "recipeTab.ingredients"), ("Nutrition", "recipeTab.nutrition")] {
                let byID = app.descendants(matching: .any)[id].firstMatch
                let picker = byID.exists ? byID : app.buttons[tab].firstMatch
                guard picker.exists else { continue }
                reveal(picker, in: app)
                picker.tap()
                sleep(1)
                if !(picker.isSelected || (picker.value as? String)?.contains("elected") == true) {
                    picker.tap()
                    sleep(1)
                }
                XCTAssertTrue(
                    picker.isSelected || (picker.value as? String)?.contains("elected") == true,
                    "Recipe page › \(kind): the \(tab) tab isn’t selected")
                pinPicker(picker, in: app)
                shot(app, "Recipe page › \(kind) › \(tab)")
                scrollToTop(app)
            }
            let more = app.buttons["More"].firstMatch
            if more.exists {
                more.tap()
                // The menu is open: one of its items is on screen.
                let item = app.buttons.matching(
                    NSPredicate(format: "label IN %@", ["Delete Recipe", "Edit Recipe", "Share", "Report a Problem"])
                ).firstMatch
                if !item.waitForExistence(timeout: 4) {
                    problems.append("Recipe page › \(kind): the More menu didn’t open")
                }
                waitStill(app)
                shot(app, "Recipe page › \(kind) › More menu")
                dismissMenu(app)
            }
            // The page as it opens (its first tab), so the scrolled shot isn't the Nutrition tab
            // again: on a short page both scroll to the same bottom (build 122).
            let firstTab = app.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier IN %@", ["recipeTab.description", "recipeTab.customize"]))
                .firstMatch
            if firstTab.exists {
                reveal(firstTab, in: app)
                // Checked selected, as the tabs above: a tap that missed left the Nutrition tab
                // up, and its scrolled shot was the Nutrition shot again (build 122, large-dark).
                for _ in 0..<3 where !Self.isSelected(firstTab) {
                    firstTab.tap()
                    sleep(1)
                }
                if !Self.isSelected(firstTab) {
                    problems.append("Recipe page › \(kind): its first tab isn’t selected")
                }
            }
            scrollToTop(app)
            var top = capture(app)
            scrollContent(app)
            var scrolled = capture(app)
            if scrolled.isSameScreen(as: top) {
                scrollContent(app)
                scrolled = capture(app)
            }
            // A first tab that fits on the screen (the typed recipe's description on the upright
            // iPad): the Ingredients tab from the page's top instead, so every config has this shot
            // (Hark's build 123 review, item 3).
            let ingredientsTab = app.descendants(matching: .any)["recipeTab.ingredients"].firstMatch
            if scrolled.isSameScreen(as: top), ingredientsTab.exists {
                ingredientsTab.tap()
                sleep(1)
                scrollToTop(app)
                top = capture(app)
                scrollContent(app)
                scrollContent(app)
                scrolled = capture(app)
            }
            // A page that fits on the screen (a short recipe on an iPad) has nothing to scroll: no
            // shot, rather than a copy of the top (Hark's review, build 122 item 13c).
            if keepIfScrolled(scrolled, "Recipe page › \(kind) › scrolled (\(mode))", before: top) {
                checkToolbar(app, "Recipe page › \(kind) › scrolled")
            }
            // Start Cooking stays on screen everywhere since build 119 (StartCookingQATests); back
            // to the top in case it ever scrolls away again.
            let startCooking = app.buttons["Start Cooking"].firstMatch
            for _ in 0..<3 where !startCooking.exists { pullDown(app) }
            startCooking.tap()
            // The walkthrough shows the first time cooking opens: a screen of its own, then Skip,
            // so the cooking shot is this recipe's cooking screen (Hark's review, build 122 13b).
            if app.staticTexts["Welcome to Cooking with DinnerOS"].waitForExistence(timeout: 4) {
                waitStill(app)
                shot(app, "Cooking › Welcome")
                app.buttons["Skip"].firstMatch.tap()
                _ = waitGone(app.staticTexts["Welcome to Cooking with DinnerOS"], timeout: 5)
            }
            if openedCooking(recipe, in: app) {
                waitStill(app)
                shot(app, "Cooking › \(kind)")
                checkStepsShowAtRest(app, "Cooking › \(kind)")
                // The end of the ingredient list, so every ingredient can be seen in some shot
                // (Hark's build 123 review, item 10), with every row on the way drawn where it rests.
                let top = capture(app)
                let pad = UIDevice.current.userInterfaceIdiom == .pad
                problems += CookingQAChecks.checkEveryIngredient(
                    app, expected: [], screen: "Cooking › \(kind)", column: pad ? 0.2 : 0.5
                ) { capture(app, settle: false) }
                keepIfScrolled(capture(app), "Cooking › \(kind) › end of ingredients (\(mode))", before: top)
            } else {
                snap(app, "Cooking › \(kind): not the cooking screen (\(mode))", unique: false)
                problems.append("Cooking › \(kind): Start Cooking didn’t open \(recipe)’s cooking screen")
            }
            if app.buttons["Done"].firstMatch.waitForExistence(timeout: 3) { app.buttons["Done"].firstMatch.tap() }
            sleep(1)
            back(app)
        }

        // Back from a recipe found by search, the search keyboard comes back over the tab bar.
        // Done above it must put it away.
        if app.keyboards.firstMatch.exists {
            snap(app, "Menu › search keyboard (\(mode))")
            let done = app.toolbars.buttons["Done"].firstMatch
            if done.exists { done.tap() }
            if !waitGone(app.keyboards.firstMatch) {
                problems.append("Menu › search: no way to put the keyboard away")
            }
        }

        // Menu: notifications, the ⋯ menu, setup again. A fresh launch, so no search text is left
        // over from the recipe search above.
        app = launchSignedIn()
        app.buttons["Menu"].firstMatch.tap()
        sleep(1)
        menuByDay(app)
        let bell = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'notification'")).firstMatch
        if bell.exists {
            bell.tap()
            sleep(1)
            shot(app, "Menu › Notifications")
            back(app)
        }
        app.buttons["Week Menu"].tap()
        if !app.buttons["Run Setup Again"].waitForExistence(timeout: 4) {
            problems.append("Menu: the ⋯ menu didn’t open")
        }
        waitStill(app)
        shot(app, "Menu › ⋯ menu")
        if app.buttons["Run Setup Again"].exists {
            // The sheet is up only once its way out shows: a tap the menu swallowed left the menu
            // on screen, and the shot was the ⋯ menu again (build 122, ipadL-dark).
            let later = app.buttons["Set Up Later"].firstMatch
            app.buttons["Run Setup Again"].tap()
            if !later.waitForExistence(timeout: 6), app.buttons["Run Setup Again"].exists {
                app.buttons["Run Setup Again"].tap()
            }
            if later.waitForExistence(timeout: 6) {
                waitStill(app)
                shot(app, "Autopilot setup")
            } else {
                snap(app, "Autopilot setup: didn’t open (\(mode))", unique: false)
                problems.append("Menu: Run Setup Again didn’t open Autopilot setup")
            }
            back(app)
        } else {
            pullDown(app)
        }

        // Shop: choosing products in Walmart, given time to load.
        app.buttons["Shop"].firstMatch.tap()
        sleep(1)
        let choose = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Choose' AND label CONTAINS 'Walmart'")).firstMatch
        // The Shop can still be loading its list; a missing button fails the run instead of
        // quietly dropping the shot (build 122: iPad landscape light published without it).
        if choose.waitForExistence(timeout: 15) {
            choose.tap()
            sleep(10)
            shot(app, "Shop › Choose in Walmart (after 10 s)")
            back(app)
        } else {
            snap(app, "Shop: no Choose in Walmart button (\(mode))", unique: false)
            problems.append("Shop: no Choose in Walmart button")
        }

        pantryAddAnItem(app)

        // Household: inviting someone.
        app.buttons["Household"].firstMatch.tap()
        sleep(1)
        let members = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'Members'"))
            .firstMatch
        guard members.waitForExistence(timeout: 5) else {
            problems.append("Household: no Members row")
            XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
            return
        }
        members.tap()
        let invite = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add Someone'")).firstMatch
        if invite.waitForExistence(timeout: 5) {
            invite.tap()
            sleep(1)
            shot(app, "Household › Members › Add Someone")
            back(app)
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    /// The Menu switched to By Day, kept only when it isn't the Menu again (Hark's review, build
    /// 122 item 13d), then switched back.
    @MainActor
    private func menuByDay(_ app: XCUIApplication) {
        let segment = app.segmentedControls.buttons["By Day"].firstMatch
        let byDay = segment.waitForExistence(timeout: 5) ? segment : app.buttons["By Day"].firstMatch
        guard byDay.exists else {
            problems.append("Menu: no By Day switch")
            return
        }
        let menu = capture(app)
        byDay.tap()
        sleep(2)
        let shown = capture(app)
        if shown.isSameScreen(as: menu) {
            snap(app, "Menu › By Day: still the Menu (\(mode))", unique: false)
            problems.append("Menu › By Day: switching to By Day left the Menu on screen")
        } else {
            keep(shown, "Menu › By Day (\(mode))")
            checkToolbar(app, "Menu › By Day")
        }
        let back = app.segmentedControls.buttons["Menu"].firstMatch
        if back.exists { back.tap() }
        sleep(1)
    }

    /// Pantry › Add Item as it opens, with the keyboard up on the Name field (Hark's review, build
    /// 121 item 3: the keyboard mustn't cover it; build 122 round 4: Name comes first). The + button is "Add Item"; "Add an Item" is
    /// the empty pantry's button (build 122 looked only for that, and the shot went missing).
    @MainActor
    private func pantryAddAnItem(_ app: XCUIApplication) {
        app.buttons["Pantry"].firstMatch.tap()
        let add = app.buttons["Add Item"].firstMatch
        let addEmpty = app.buttons["Add an Item"].firstMatch
        _ = add.waitForExistence(timeout: 8)
        let button = add.exists ? add : addEmpty
        guard button.exists else {
            snap(app, "Pantry: no Add Item button (\(mode))", unique: false)
            problems.append("Pantry: no Add Item button")
            return
        }
        button.tap()
        // The sheet opens on the name, with the keyboard up (Hark's review, build 122 round 4).
        let name = app.textFields["pantry.name"].firstMatch
        guard name.waitForExistence(timeout: 5) else {
            problems.append("Pantry › Add an Item: no Name field")
            back(app)
            return
        }
        let keyboard = app.keyboards.firstMatch
        let focused = { (name.value(forKey: "hasKeyboardFocus") as? Bool) == true }
        guard keyboard.waitForExistence(timeout: 5), focused() else {
            snap(app, "Pantry › Add an Item: Name not focused (\(mode))", unique: false)
            problems.append("Pantry › Add an Item: the sheet didn’t open with the keyboard up on Name")
            back(app)
            return
        }
        waitStill(app)
        if name.frame.maxY > keyboard.frame.minY {
            problems.append("Pantry › Add an Item: the keyboard covers the Name field")
        }
        shot(app, "Pantry › Add an Item")
        back(app)
    }

    // MARK: Helpers

    /// A segment or tab that says it's the selected one.
    @MainActor
    private static func isSelected(_ element: XCUIElement) -> Bool {
        element.isSelected || (element.value as? String)?.contains("elected") == true
    }

    /// The cooking screen as it opens on an iPad (the steps beside the ingredients): the Steps
    /// heading and Step 1 are on screen, below the bar, and drawn whole, not faded out. Hark's
    /// review, build 122 round 5: the steps started at Step 2 under a blank band, and the photo
    /// and Serves line were half faded. A phone shows the steps on a tab of their own.
    @MainActor
    private func checkStepsShowAtRest(_ app: XCUIApplication, _ name: String) {
        guard !app.segmentedControls.buttons["Steps"].firstMatch.exists else { return }
        let screen = app.frame
        // The ingredients pane is the left 38% or 320 points; the steps are to its right.
        let stepsLeft = max(320, screen.width * 0.38)
        let barBottom = max(
            app.buttons["Done"].firstMatch.frame.maxY,
            app.buttons["How the Cooking Screen Works"].firstMatch.frame.maxY)
        guard let image = capture(app).image else {
            problems.append("\(name): no screenshot to check the steps in")
            return
        }
        func inStepsPane(_ predicate: NSPredicate) -> XCUIElement? {
            let matches = app.descendants(matching: .any).matching(predicate).allElementsBoundByIndex
            return matches.first { $0.exists && $0.frame.minX >= stepsLeft - 1 }
        }
        let checks: [(what: String, element: XCUIElement?, contrast: Int)] = [
            ("the Steps heading", inStepsPane(NSPredicate(format: "label == 'Steps'")), 90),
            ("Step 1", inStepsPane(NSPredicate(format: "label == 'Step 1' OR label BEGINSWITH 'Step 1,'")), 150),
        ]
        for check in checks {
            guard let element = check.element else {
                problems.append("\(name): \(check.what) isn’t on the screen")
                continue
            }
            let frame = element.frame
            guard element.isHittable, screen.contains(frame), frame.minY >= barBottom else {
                problems.append("\(name): \(check.what) is off the screen or under the bar, at \(frame)")
                continue
            }
            let range = QAShotCompare.brightnessRange(image, in: frame)
            if range < check.contrast {
                problems.append(
                    "\(name): \(check.what) is faded out (brightness range \(range), want \(check.contrast)+)")
            }
        }
    }

    @MainActor
    private func shot(_ app: XCUIApplication, _ name: String) {
        snap(app, "\(name) (\(mode))")
        checkToolbar(app, name)
    }

    @MainActor
    private func checkToolbar(_ app: XCUIApplication, _ name: String) {
        // One snapshot of the whole screen: on iOS 27 the bar's buttons can change while they are
        // read one query at a time, and a button that left mid-read failed the test.
        guard let root = try? app.snapshot() else { return }
        let screen = root.frame
        for bar in Self.all(in: root, of: .navigationBar) {
            for button in Self.all(in: bar, of: .button) {
                let frame = button.frame
                guard !frame.isEmpty, screen.intersects(frame) else { continue }
                let named =
                    !button.label.trimmingCharacters(in: .whitespaces).isEmpty
                    || Self.all(in: button, of: .any).contains { !$0.label.isEmpty }
                if !named { problems.append("\(name): a toolbar button has no name (blank slot?)") }
            }
        }
    }

    /// Every element of a type under a snapshot (`.any`: every element), not counting the root.
    private static func all(in root: any XCUIElementSnapshot, of type: XCUIElement.ElementType)
        -> [any XCUIElementSnapshot]
    {
        root.children.flatMap { child in
            (type == .any || child.elementType == type ? [child] : []) + all(in: child, of: type)
        }
    }

    /// Waits for a push, sheet or menu animation to finish.
    @MainActor
    private func waitStill(_ app: XCUIApplication) {
        sleep(1)
        settleForScreenshot(app)
    }

    /// Scrolls the page's content up by about half a screen, in the main scroll view (on an iPad
    /// a swipe in the middle of the screen can land outside it).
    @MainActor
    private func scrollContent(_ app: XCUIApplication) {
        let scroll = app.scrollViews.allElementsBoundByIndex
            .filter { $0.exists && !$0.frame.isEmpty }
            .max { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }
        if let scroll {
            let start = scroll.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75))
            let end = scroll.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
            start.press(forDuration: 0.05, thenDragTo: end)
        } else {
            app.swipeUp(velocity: .slow)
        }
        sleep(1)
    }

    /// Drags the page up just far enough that the tab picker reaches the top and pins, so a
    /// tab's shot starts at its first row. A fixed scroll ran past the start of a short tab and
    /// left its first four rows off an iPhone SE (Hark's build 122 round 5, item 10).
    @MainActor
    private func pinPicker(_ picker: XCUIElement, in app: XCUIApplication) {
        let bar = app.navigationBars.firstMatch
        let top = bar.exists && !bar.frame.isEmpty ? bar.frame.maxY : app.frame.height * 0.12
        let distance = picker.frame.minY - top
        guard distance > 8 else { return }
        let start = app.coordinate(withNormalizedOffset: .zero)
            .withOffset(CGVector(dx: app.frame.width * 0.3, dy: min(picker.frame.maxY + 40, app.frame.height * 0.8)))
        let end = start.withOffset(CGVector(dx: 0, dy: -distance))
        // Slow, and held at the end, so the page stops where the drag does.
        start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.3)
        sleep(1)
    }

    @MainActor
    private func scrollToTop(_ app: XCUIApplication) {
        // One long drag from near the top: enough for a recipe page, and cheaper than three.
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95))
        start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .fast, thenHoldForDuration: 0)
        start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .fast, thenHoldForDuration: 0)
    }

    /// Closes an open menu by tapping the top corner of the screen, away from its items.
    @MainActor
    private func dismissMenu(_ app: XCUIApplication) {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.02, dy: 0.5)).tap()
        sleep(1)
    }

    /// Cancel, Done or Close if the screen has one, else the back button, else swipe the sheet away.
    @MainActor
    private func back(_ app: XCUIApplication) {
        for title in ["Set Up Later", "Cancel", "Done", "Close", "Not Now"] where app.buttons[title].firstMatch.exists {
            let button = app.buttons[title].firstMatch
            if button.isHittable {
                button.tap()
                sleep(1)
                return
            }
        }
        let back = backButton(app)
        if back.exists, back.isHittable {
            back.tap()
        } else {
            pullDown(app)
        }
        sleep(1)
    }
}
