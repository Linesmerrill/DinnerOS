import XCTest

/// Checks on the Shop tab's rows.
enum ShopQAChecks {
    /// Each Choose Product button on screen starts on its item's first line, beside the name, as
    /// in every other row: on the iPhones and the Duo the Lemon row's long note pushed it onto a
    /// line of its own under the text (Hark's review, build 121).
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
            let text = named
                .filter { $0.maxX <= frame.minX + 1 && $0.minY <= frame.maxY }
                .max { $0.minY < $1.minY }
            guard let text else {
                let shot = XCTAttachment(screenshot: app.screenshot())
                shot.name = "\(screen): \(name) row"
                shot.lifetime = .keepAlways
                XCTContext.runActivity(named: shot.name ?? "") { $0.add(shot) }
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
        }
        return problems
    }
}
