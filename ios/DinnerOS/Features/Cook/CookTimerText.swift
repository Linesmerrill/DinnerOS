import SwiftUI

/// Step text with its cooking times drawn as timers: green, bold, a small timer glyph, and a
/// link the cooking screen opens as "start a timer for this".
enum CookTimerText {
    static let scheme = "dinneros-timer"

    /// `subject` is what the step is cooking by then ("Zucchini"), which names the timer.
    static func text(_ string: String, step: Int, subject: String? = nil) -> Text {
        let durations = CookDurations.find(in: string)
        guard !durations.isEmpty else { return Text(verbatim: string) }
        var out = Text(verbatim: "")
        var cursor = string.startIndex
        for duration in durations {
            out = out + Text(verbatim: String(string[cursor..<duration.range.lowerBound]))
            var link = AttributedString(String(string[duration.range]))
            let before = String(string[string.startIndex..<duration.range.lowerBound])
            link.link = url(step: step, duration: duration, subject: CookTimerSubject.noun(in: before) ?? subject)
            link.foregroundColor = .accentColor
            link.font = .body.bold()
            out =
                out
                + Text(Image(systemName: "timer")).foregroundStyle(Color.accentColor).fontWeight(.semibold)
                + Text(verbatim: "\u{2009}")
                + Text(link).fontWeight(.bold)
            cursor = duration.range.upperBound
        }
        return out + Text(verbatim: String(string[cursor...]))
    }

    static func url(step: Int, duration: CookDuration, subject: String? = nil) -> URL? {
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
        return components.url
    }

    /// The timer a tapped link asks for, or `nil` for any other link.
    static func request(from url: URL) -> CookTimerRequest? {
        guard url.scheme == scheme, let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else { return nil }
        func value(_ name: String) -> Int? { items.first { $0.name == name }?.value.flatMap(Int.init) }
        guard let step = value("step"), let low = value("low") else { return nil }
        let what = items.first { $0.name == "what" }?.value
        return CookTimerRequest(step: step, lowSeconds: low, highSeconds: value("high") ?? low, subject: what)
    }
}

/// A tapped time, waiting for the cook to adjust it and start.
struct CookTimerRequest: Identifiable, Equatable {
    let step: Int
    let lowSeconds: Int
    let highSeconds: Int
    /// What it's for, "Zucchini"; `nil` when the step doesn't say.
    var subject: String? = nil
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
