import XCTest

/// Walks every tab and every screen one tap away, in light and dark, keeping a screenshot of
/// each and failing on what a person would trip over: a toolbar button with nothing on it, a
/// button with no name for VoiceOver, or a screen with no way back.
final class AppSweepQATests: XCTestCase {
    private var problems: [String] = []

    override func setUp() {
        continueAfterFailure = true
    }

    @MainActor func testSweepLight() throws { try sweep(dark: false) }
    @MainActor func testSweepDark() throws { try sweep(dark: true) }

    @MainActor
    private func sweep(dark: Bool) throws {
        let app = launchSignedIn(dark: dark)
        let mode = dark ? "dark" : "light"
        for tab in ["Menu", "Shop", "Pantry", "Household"] {
            let button = app.tabBars.buttons[tab].exists ? app.tabBars.buttons[tab] : app.buttons[tab].firstMatch
            guard button.waitForExistence(timeout: 5) else {
                problems.append("\(tab): tab missing")
                continue
            }
            button.tap()
            sleep(2)
            var opened = false
            for _ in 0..<5 where !opened {
                opened = button.isSelected || app.navigationBars[tab].exists || app.staticTexts[tab].exists
                if !opened { sleep(1) }
            }
            guard opened else {
                snap(app, "\(tab) didn’t open (\(mode))", unique: false)
                problems.append("\(tab): tapping the tab didn’t open it")
                continue
            }
            snap(app, "\(tab) (\(mode))")
            checkScreen(app, "\(tab) (\(mode))")
            checkNothingMissing(app, tab: tab, mode: mode)
            visitRows(app, tab: tab, mode: mode)
            if let row = Self.rowsToShow[tab] { snapRow(app, row, tab: tab, mode: mode) }
            if tab == "Household" { checkVersion(app, mode: mode) }
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    /// A row on each of these tabs whose amount is checked in every config (Hark's build 121
    /// review, item 18): Butter's percentage and amount on Pantry, the Cream Sauce Base's flour on
    /// Shop. Below the first screen on a phone, so the sweep scrolls to them; every config keeps
    /// the shot, so all have the same set.
    static let rowsToShow = ["Pantry": "Butter", "Shop": "All-Purpose Flour"]

    /// Brings `row` into view and keeps "Pantry › scrolled to Butter", in every config: a row
    /// already in view on the tab (an iPad) is kept as it is, as the tab shot's allowed
    /// look-alike (`QAShotCompare.allowedPairs`); one only partly in view is scrolled up.
    @MainActor
    private func snapRow(_ app: XCUIApplication, _ row: String, tab: String, mode: String) {
        let text = app.staticTexts[row].firstMatch
        if !Self.fullyInView(text, in: app) { reveal(text, in: app, maxSwipes: 12) }
        guard text.exists, Self.fullyInView(text, in: app) else {
            problems.append("\(tab) (\(mode)): no \(row) row in view to show")
            return
        }
        let short = row.split(separator: " ").last.map(String.init) ?? row
        snap(app, "\(tab) › scrolled to \(short) (\(mode))")
    }

    /// Whether `element` sits in the top 70% of the screen, clear of a pinned bottom bar and the
    /// tab bar, with room under it for its amount lines.
    @MainActor
    private static func fullyInView(_ element: XCUIElement, in app: XCUIApplication) -> Bool {
        guard element.exists else { return false }
        let frame = element.frame
        let screen = app.frame
        return !frame.isEmpty && frame.minY > screen.minY + screen.height * 0.08
            && frame.maxY < screen.minY + screen.height * 0.7
    }

    /// Build 123 (Hark: "this cannot happen again"): content that should be there and isn't. On
    /// Menu, every meal card shows its stars (a meal still ahead included, Merrill, Nov 23), the
    /// add-ons the subtitle counts have their cards, the cards line up with no empty band under
    /// them, and "Didn't make it?" sits on the left; on Menu, a recipe
    /// page, Shop and Pantry, nothing is still loading after 10 seconds (`MenuQAChecks`).
    @MainActor
    private func checkNothingMissing(_ app: XCUIApplication, tab: String, mode: String) {
        let screen = "\(tab) (\(mode))"
        switch tab {
        case "Menu":
            problems += MenuQAChecks.checkMealCardRatingRows(app, screen: screen)
            problems += MenuQAChecks.checkAddOnCards(app, screen: screen)
            problems += MenuQAChecks.checkNoBlankBandUnderMeals(app, screen: screen)
            problems += MenuQAChecks.checkMealCardsLineUp(app, screen: screen)
            problems += MenuQAChecks.checkLoaded(app, screen: screen)
            checkRecipePageLoaded(app, mode: mode)
        case "Shop", "Pantry":
            problems += MenuQAChecks.checkLoaded(app, screen: screen)
        default:
            break
        }
    }

    /// Household's last line names the build as TestFlight numbers it ("Version 1.0 (121)"), the
    /// number the matrix built with (BUILD_NUMBER), so a screenshot can be matched to a phone. The
    /// Household shot shows it.
    @MainActor
    private func checkVersion(_ app: XCUIApplication, mode: String) {
        let build = ProcessInfo.processInfo.environment["QA_BUILD_NUMBER"] ?? ""
        let version = app.staticTexts["household.version"].firstMatch
        reveal(version, in: app, maxSwipes: 12)
        guard version.waitForExistence(timeout: 5) else {
            problems.append("Household (\(mode)): no version line")
            return
        }
        if build.isEmpty || !version.label.hasSuffix("(\(build))") {
            problems.append("Household (\(mode)): shows “\(version.label)”, want build \(build)")
        }
    }

    /// Opens the first planned meal's recipe page from Your Meals, checks it loaded, and comes
    /// back to the Menu.
    @MainActor
    private func checkRecipePageLoaded(_ app: XCUIApplication, mode: String) {
        let cards = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", MenuQAChecks.cardPrefix))
        let open = cards.buttons.firstMatch
        guard open.exists, open.isHittable else {
            problems.append("Menu (\(mode)): no meal card to open a recipe page from")
            return
        }
        let name = open.label.components(separatedBy: ",").first ?? open.label
        open.tap()
        sleep(1)
        problems += MenuQAChecks.checkLoaded(app, screen: "Recipe page › \(name) (\(mode))")
        backButton(app).tap()
        sleep(1)
    }

    /// Opens each row on the screen that leads somewhere, checks it, and comes back.
    @MainActor
    private func visitRows(_ app: XCUIApplication, tab: String, mode: String) {
        let rows = app.collectionViews.firstMatch.exists ? app.collectionViews.firstMatch.cells : app.cells
        let count = min(rows.count, 25)
        var seen = Set<String>()
        let screen = app.frame
        for index in 0..<count {
            // A snapshot rather than the live row: the list can change while the sweep walks it
            // (a row loads in, or coming back re-renders it), and asking a vanished row for its
            // label fails the test. A snapshot just throws, and the row is skipped.
            guard let row = try? rows.element(boundBy: index).snapshot() else { continue }
            let center = CGPoint(x: row.frame.midX, y: row.frame.midY)
            guard !row.frame.isEmpty, screen.insetBy(dx: 0, dy: screen.height * 0.1).contains(center) else { continue }
            let firstText = row.children.first { $0.elementType == .staticText }?.label ?? ""
            let label = row.label.isEmpty ? firstText : row.label
            guard !label.isEmpty, seen.insert(label).inserted else { continue }
            // Rows that act right away (sign out, delete) aren't part of a sweep.
            let lower = label.lowercased()
            // Nor are ones that hand off to a store or another app.
            if ["sign out", "delete", "leave", "remove", "reset", "walmart", "instacart", "open in"]
                .contains(where: lower.contains)
            {
                continue
            }
            let before = app.navigationBars.firstMatch.identifier
            app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: center.x, dy: center.y)).tap()
            sleep(1)
            let name = "\(tab) › \(label) (\(mode))"
            // A full-screen cover hides the tab bar without a new bar title or sheet.
            let tabBarCovered = !app.buttons[tab].firstMatch.isHittable
            let opened = app.navigationBars.firstMatch.identifier != before || app.sheets.count > 0 || tabBarCovered
            if opened {
                snap(app, name)
                checkScreen(app, name)
                goBack(app, name)
                // Back on the tab, or the rest of this sweep would be checking the wrong screen.
                if !app.tabBars.firstMatch.waitForExistence(timeout: 3) && !app.buttons[tab].firstMatch.isHittable {
                    problems.append("\(name): no way back to \(tab)")
                    app.terminate()
                    _ = launchSignedIn(dark: mode == "dark")
                    app.buttons[tab].firstMatch.tap()
                    return
                }
            }
        }
    }

    @MainActor
    private func checkScreen(_ app: XCUIApplication, _ name: String) {
        let screen = app.frame
        // On screen, without asking XCTest whether it can be tapped: that throws for the
        // off-screen items of a lazy row.
        func shown(_ element: XCUIElement) -> Bool {
            // Gone since the list was taken (a toast going away): nothing to check.
            guard element.exists else { return false }
            let frame = element.frame
            return !frame.isEmpty && screen.intersects(frame)
        }
        func blank(_ element: XCUIElement) -> Bool {
            element.label.trimmingCharacters(in: .whitespaces).isEmpty
        }
        for bar in app.navigationBars.allElementsBoundByIndex {
            for button in bar.buttons.allElementsBoundByIndex where shown(button) && blank(button) {
                problems.append("\(name): a toolbar button has no label (blank slot?)")
            }
        }
        // A menu's button is an empty wrapper around its label, so its name sits on the text or
        // icon inside it. VoiceOver reads that one.
        func namedInside(_ element: XCUIElement) -> Bool {
            element.descendants(matching: .any).allElementsBoundByIndex.contains { !blank($0) }
        }
        let unnamed = app.buttons.allElementsBoundByIndex.filter {
            shown($0) && blank($0) && $0.identifier.isEmpty && !namedInside($0)
        }
        if !unnamed.isEmpty {
            let places = unnamed.map {
                "at \(Int($0.frame.midX)),\(Int($0.frame.midY)) size \(Int($0.frame.width))x\(Int($0.frame.height))"
            }
            problems.append("\(name): \(unnamed.count) button(s) with no name, \(places.joined(separator: "; "))")
        }
    }

    @MainActor
    private func goBack(_ app: XCUIApplication, _ name: String) {
        let back = app.navigationBars.buttons.element(boundBy: 0)
        for title in ["Cancel", "Done", "Close"] where app.buttons[title].firstMatch.isHittable {
            app.buttons[title].firstMatch.tap()
            sleep(1)
            return
        }
        if back.exists, back.isHittable {
            back.tap()
            sleep(1)
        } else {
            pullDown(app)
            sleep(1)
            if app.navigationBars.count > 1 { problems.append("\(name): no way back") }
        }
    }
}
