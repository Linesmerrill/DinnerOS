import SwiftUI

/// Step text with its cooking times drawn as timers: green, bold, a small timer glyph, and a
/// link the cooking screen opens as "start a timer for this".
enum CookTimerText {
    static let scheme = "dinneros-timer"

    /// `subject` is what the step is cooking by then ("Zucchini"), which names the timer. `server`
    /// is the server's timer for a time, when it named one; its name and start time win.
    static func text(
        _ string: String, step: Int, subject: (String) -> String? = { _ in nil },
        server: (CookDuration, String) -> InstructionTimer? = { _, _ in nil }
    ) -> Text {
        let durations = CookDurations.find(in: string)
        guard !durations.isEmpty else { return Text(verbatim: string) }
        var out = Text(verbatim: "")
        var cursor = string.startIndex
        for duration in durations {
            out = out + Text(verbatim: String(string[cursor..<duration.range.lowerBound]))
            // The stopwatch is part of the link and nothing in it can break, so a time that wraps
            // moves to the next line whole and any of the green is a tap target.
            let written = String(string[duration.range])
            var glyph = AttributedString("\u{23F1}\u{FE0E}")
            glyph.font = .title2.weight(.black)
            var link =
                glyph
                + AttributedString(
                    "\u{00A0}"
                        + written.replacingOccurrences(of: " ", with: "\u{00A0}").replacingOccurrences(
                            of: "-", with: "\u{2011}"))
            let before = String(string[string.startIndex..<duration.range.lowerBound])
            if let timer = server(duration, written) {
                link.link = url(step: step, duration: duration, subject: timer.subject, start: timer.startSeconds)
            } else {
                link.link = url(step: step, duration: duration, subject: subject(before))
            }
            link.foregroundColor = .accentColor
            out = out + Text(link).fontWeight(.bold)
            cursor = duration.range.upperBound
        }
        return out + Text(verbatim: String(string[cursor...]))
    }

    static func url(step: Int, duration: CookDuration, subject: String? = nil, start: Int? = nil) -> URL? {
        var components = URLComponents()
        components.scheme = scheme
        components.host = "start"
        components.queryItems = [
            URLQueryItem(name: "step", value: String(step)),
            URLQueryItem(name: "low", value: String(duration.lowSeconds)),
            URLQueryItem(name: "high", value: String(duration.highSeconds)),
        ]
        if let subject, !subject.isEmpty {
            components.queryItems?.append(URLQueryItem(name: "what", value: subject))
        }
        if let start, start > 0 {
            components.queryItems?.append(URLQueryItem(name: "start", value: String(start)))
        }
        return components.url
    }

    /// The timer a tapped link asks for, or `nil` for any other link.
    static func request(from url: URL) -> CookTimerRequest? {
        guard url.scheme == scheme, let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else { return nil }
        func value(_ name: String) -> Int? { items.first { $0.name == name }?.value.flatMap(Int.init) }
        guard let step = value("step"), let low = value("low") else { return nil }
        let what = items.first { $0.name == "what" }?.value
        return CookTimerRequest(
            step: step, lowSeconds: low, highSeconds: value("high") ?? low, subject: what, startSeconds: value("start"))
    }
}

/// A tapped time, waiting for the cook to adjust it and start.
struct CookTimerRequest: Identifiable, Equatable {
    let step: Int
    let lowSeconds: Int
    let highSeconds: Int
    /// What it's for, "Zucchini"; `nil` when the step doesn't say.
    var subject: String? = nil
    /// Where the server says to start it; `nil` from an older server (the high end is used).
    var startSeconds: Int? = nil
    var id: String { "\(step)-\(lowSeconds)-\(highSeconds)-\(subject ?? "")" }
}

/// Names a timer the way you'd ask a speaker for one: "a timer for the pasta".
enum CookTimerSubject {
    /// Things a step cooks that aren't always an ingredient the recipe lists ("the pasta" when
    /// the list says "Rigatoni").
    private static let nouns = [
        "pasta", "rice", "tortillas", "water", "sauce", "potatoes", "vegetables", "veggies", "noodles",
        "chicken", "beef", "pork", "steak", "salmon", "fish", "shrimp", "bread", "croutons", "eggs",
        "onion", "onions", "broccoli", "carrots", "green beans", "peppers", "mushrooms", "couscous",
    ]

    /// Seasonings and liquids a step adds along the way; never what a timer is for.
    private static let seasonings: Set<String> = [
        "salt", "pepper", "black pepper", "kosher salt", "oil", "olive oil", "cooking oil", "vegetable oil",
        "water", "sugar", "garlic powder", "chili flakes",
    ]
    private static let proteins = ["beef", "pork", "chicken", "turkey", "sausage", "lamb", "steak", "shrimp", "salmon"]

    /// What a timer is for, from the sentence up to the time and the step's ingredients:
    /// something the sentence cooks ("pasta", "meat" as the step's meat), else the last
    /// ingredient the sentence names that isn't a seasoning, else the step's last such one.
    static func pick(sentence text: String, ingredients: [String]) -> String? {
        let sentence = text.split(whereSeparator: { ".;\n".contains($0) }).last.map(String.init) ?? text
        let lower = sentence.lowercased()
        let cooking = ingredients.filter { !seasonings.contains($0.lowercased()) }
        if lower.range(of: #"\bmeat\b"#, options: .regularExpression) != nil {
            let meat = cooking.last { name in proteins.contains { name.lowercased().contains($0) } }
            return meat.map(capitalized) ?? "Meat"
        }
        if let noun = noun(in: sentence) { return noun }
        // The last ingredient named before the time: in this sentence first, else earlier in
        // the step ("…add rigatoni to pot. Cook until al dente, 9-11 minutes" is Rigatoni).
        func lastNamed(in text: String) -> String? {
            let lower = text.lowercased()
            let found = cooking.compactMap { name -> (String, String.Index)? in
                let forms = [name.lowercased()] + name.lowercased().split(separator: " ").map(String.init)
                let ranges = forms.filter { $0.count > 3 }.compactMap { lower.range(of: $0, options: .backwards) }
                return ranges.map(\.lowerBound).max().map { (name, $0) }
            }
            return found.max(by: { $0.1 < $1.1 }).map { capitalized($0.0) }
        }
        return lastNamed(in: sentence) ?? lastNamed(in: text)
    }

    private static func capitalized(_ name: String) -> String {
        name.split(separator: " ").map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined(separator: " ")
    }

    /// The last of those in the sentence before the time, capitalized, or `nil`.
    static func noun(in text: String) -> String? {
        let sentence = text.split(whereSeparator: { ".;\n".contains($0) }).last.map(String.init) ?? text
        let lower = sentence.lowercased()
        var best: (String, String.Index)?
        for noun in nouns {
            guard let range = lower.range(of: noun, options: .backwards) else { continue }
            let atWordStart =
                range.lowerBound == lower.startIndex || !lower[lower.index(before: range.lowerBound)].isLetter
            let atWordEnd = range.upperBound == lower.endIndex || !lower[range.upperBound].isLetter
            guard atWordStart, atWordEnd else { continue }
            if let current = best, current.1 >= range.lowerBound { continue }
            best = (noun, range.lowerBound)
        }
        return best.map { $0.0.prefix(1).uppercased() + $0.0.dropFirst() }
    }
}
