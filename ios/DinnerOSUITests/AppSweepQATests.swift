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
            guard button.isSelected || app.navigationBars[tab].exists || app.staticTexts[tab].exists else {
                problems.append("\(tab): tapping the tab didn't open it")
                continue
            }
            snap(app, "\(tab) (\(mode))")
            checkScreen(app, "\(tab) (\(mode))")
            visitRows(app, tab: tab, mode: mode)
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    /// Opens each row on the screen that leads somewhere, checks it, and comes back.
    @MainActor
    private func visitRows(_ app: XCUIApplication, tab: String, mode: String) {
        let rows = app.collectionViews.firstMatch.exists ? app.collectionViews.firstMatch.cells : app.cells
        let count = min(rows.count, 25)
        var seen = Set<String>()
        for index in 0..<count {
            let row = rows.element(boundBy: index)
            guard row.exists, row.isHittable else { continue }
            let label = row.label.isEmpty ? row.staticTexts.firstMatch.label : row.label
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
            row.tap()
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
            app.swipeDown(velocity: .fast)
            sleep(1)
            if app.navigationBars.count > 1 { problems.append("\(name): no way back") }
        }
    }
}
