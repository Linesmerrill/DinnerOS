import SwiftUI

/// Step text with its cooking times drawn as timers: green, bold, a small timer glyph, and a
/// link the cooking screen opens as "start a timer for this".
enum CookTimerText {
    static let scheme = "dinneros-timer"

    static func text(_ string: String, step: Int) -> Text {
        let durations = CookDurations.find(in: string)
        guard !durations.isEmpty else { return Text(verbatim: string) }
        var out = Text(verbatim: "")
        var cursor = string.startIndex
        for duration in durations {
            out = out + Text(verbatim: String(string[cursor..<duration.range.lowerBound]))
            var link = AttributedString(String(string[duration.range]))
            link.link = url(step: step, duration: duration)
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

    static func url(step: Int, duration: CookDuration) -> URL? {
        var components = URLComponents()
        components.scheme = scheme
        components.host = "start"
        components.queryItems = [
            URLQueryItem(name: "step", value: String(step)),
            URLQueryItem(name: "low", value: String(duration.lowSeconds)),
            URLQueryItem(name: "high", value: String(duration.highSeconds)),
        ]
        return components.url
    }

    /// The timer a tapped link asks for, or `nil` for any other link.
    static func request(from url: URL) -> CookTimerRequest? {
        guard url.scheme == scheme, let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else { return nil }
        func value(_ name: String) -> Int? { items.first { $0.name == name }?.value.flatMap(Int.init) }
        guard let step = value("step"), let low = value("low") else { return nil }
        return CookTimerRequest(step: step, lowSeconds: low, highSeconds: value("high") ?? low)
    }
}

/// A tapped time, waiting for the cook to adjust it and start.
struct CookTimerRequest: Identifiable, Equatable {
    let step: Int
    let lowSeconds: Int
    let highSeconds: Int
    var id: String { "\(step)-\(lowSeconds)-\(highSeconds)" }
}
