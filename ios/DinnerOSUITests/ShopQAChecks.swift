import XCTest

/// Checks on the Shop tab's rows.
enum ShopQAChecks {
    /// Rows whose Choose Product must stay beside the name on every device: the Lemon row's long
    /// note pushed it onto a line of its own on the iPhones and the Duo (Hark's review, build 121).
    static let besideNameRequired: Set<String> = ["Lemon"]

    /// The most lines a name or amount beside Choose Product may wrap to: "All-Purpose Flour /
    /// 2 Tbsp + 2 tsp" took 5 lines beside it on an iPhone SE (Hark's review, build 121).
    static let maxLinesBesideButton = 2

    /// Each Choose Product button on screen starts on its item's first line, beside the name, as
    /// in every other row, with the name and the amount beside it two lines at most each. A row
    /// whose text would need more puts the button on a line of its own under the text, which
    /// is fine except for the rows in `besideNameRequired`.
    @MainActor
    static func checkChooseProductBesideName(_ app: XCUIApplication, screen: String) -> [String] {
        let buttons = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Choose Product for '"))
        guard buttons.firstMatch.waitForExistence(timeout: 15) else {
            return ["\(screen): no Choose Product button to check"]
        }
        var problems: [String] = []
        for button in buttons.allElementsBoundByIndex where button.isHittable {
            let name = String(button.label.dropFirst("Choose Product for ".count))
            let frame = button.frame
            // The row's text: the name alone, or the name with its amount and note read together.
            let named = app.descendants(matching: .any)
                .matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", name, name + ","))
                .allElementsBoundByIndex.map(\.frame)
            let text =
                named
                .filter { $0.maxX <= frame.minX + 1 && $0.minY <= frame.maxY }
                .max { $0.minY < $1.minY }
            guard let text else {
                // The button on a line of its own, just under the row's text.
                let above = named.first {
                    $0.maxY <= frame.minY + 1 && frame.minY - $0.maxY < 40 && abs($0.minX - frame.minX) < 24
                }
                if above != nil, !besideNameRequired.contains(name) { continue }
                attachScreenshot(app, name: "\(screen): \(name) row")
                problems.append(
                    "\(screen): no text beside \(name)’s Choose Product button at \(frame.integral); "
                        + "named \(named.map { "\($0.integral)" })")
                continue
            }
            // On the name's line: the button starts no lower than a line under the name's top.
            if frame.minY > text.minY + 30 {
                problems.append(
                    "\(screen): \(name)’s Choose Product button is \(Int(frame.minY - text.minY)) points "
                        + "under the top of its text, not beside the name")
            }
            problems += wrapProblems(app, name: name, nameFrame: text, button: frame, screen: screen)
        }
        return problems
    }

    /// The name and the amount under it, when they sit beside the button as texts of their
    /// own: each wraps to `maxLinesBesideButton` lines at most.
    @MainActor
    private static func wrapProblems(
        _ app: XCUIApplication, name: String, nameFrame: CGRect, button: CGRect, screen: String
    ) -> [String] {
        // The name alone (a phone row); a label read together with its notes is a wide row.
        let nameText = app.staticTexts.matching(NSPredicate(format: "label == %@", name))
            .allElementsBoundByIndex.map(\.frame)
            .first { abs($0.minX - nameFrame.minX) < 1 && abs($0.minY - nameFrame.minY) < 1 }
        guard let nameText else { return [] }
        var problems: [String] = []
        // Body text is about 22 points a line at the default size, the amount's about 20.
        let nameLines = lines(nameText.height, lineHeight: 22)
        if nameLines > maxLinesBesideButton {
            attachScreenshot(app, name: "\(screen): \(name) name wraps")
            problems.append(
                "\(screen): \(name)’s name wraps to \(nameLines) lines beside its Choose Product button")
        }
        let amount = app.staticTexts.allElementsBoundByIndex.map(\.frame).first {
            abs($0.minX - nameText.minX) < 1 && $0.minY >= nameText.maxY - 1 && $0.minY - nameText.maxY < 8
                && $0.maxX <= button.minX + 1
        }
        if let amount {
            let amountLines = lines(amount.height, lineHeight: 20)
            if amountLines > maxLinesBesideButton {
                attachScreenshot(app, name: "\(screen): \(name) amount wraps")
                problems.append(
                    "\(screen): \(name)’s amount wraps to \(amountLines) lines beside its Choose Product button")
            }
        }
        return problems
    }

    private static func lines(_ height: CGFloat, lineHeight: CGFloat) -> Int {
        max(1, Int((height / lineHeight).rounded()))
    }

    @MainActor
    private static func attachScreenshot(_ app: XCUIApplication, name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        XCTContext.runActivity(named: name) { $0.add(shot) }
    }
}
