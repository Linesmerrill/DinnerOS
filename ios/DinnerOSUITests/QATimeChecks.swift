import Foundation
import XCTest

/// A recipe's time read off the screen, so a test can check the Menu card, the recipe page and the
/// cooking screen agree (Hark's build 121 review, round 3: one pasta read 20, 30 and 35 min).
enum QATimeChecks {
    /// The minutes in a shown or spoken time: "20 min", "20 minutes", "1 hr 5 min",
    /// "1 hour, 5 minutes". Nil when the text has no time.
    static func minutes(in text: String) -> Int? {
        func number(before units: String) -> Int? {
            guard let regex = try? NSRegularExpression(pattern: "(\\d+)\\s*(?:\(units))\\b", options: .caseInsensitive),
                let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
                let range = Range(match.range(at: 1), in: text)
            else { return nil }
            return Int(text[range])
        }
        let hours = number(before: "hr|hrs|hour|hours")
        let minutes = number(before: "min|mins|minute|minutes")
        if hours == nil && minutes == nil { return nil }
        return (hours ?? 0) * 60 + (minutes ?? 0)
    }

    /// What each screen showed that isn't `expected` minutes, one line per screen.
    static func problems(_ shown: [(screen: String, text: String?)], expected: Int, recipe: String) -> [String] {
        shown.compactMap { screen, text in
            guard let text else { return "\(screen) shows no time for \(recipe)" }
            let minutes = minutes(in: text)
            return minutes == expected
                ? nil : "\(screen) shows \(recipe) as “\(text)”, not \(expected) minutes like the other screens"
        }
    }
}
