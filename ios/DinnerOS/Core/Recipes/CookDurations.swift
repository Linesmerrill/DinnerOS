import Foundation

/// A cooking time written in a step: "8-10 minutes", "5 min", "1 hour".
nonisolated struct CookDuration: Equatable, Sendable {
    /// Where it is in the text.
    let range: Range<String.Index>
    /// The low end, in seconds; a timer starts here.
    let lowSeconds: Int
    /// The high end, in seconds; equal to `lowSeconds` when the step gives one number.
    let highSeconds: Int

    /// "8–10 min", "5 min", "1 hr 30 min".
    var text: String {
        let low = Self.words(lowSeconds)
        guard highSeconds != lowSeconds else { return low }
        if lowSeconds % 60 == 0, highSeconds % 60 == 0, lowSeconds < 3600, highSeconds < 3600 {
            return String(localized: "\(lowSeconds / 60)–\(highSeconds / 60) min")
        }
        return "\(low)–\(Self.words(highSeconds))"
    }

    static func words(_ seconds: Int) -> String {
        let hours = seconds / 3600, minutes = (seconds % 3600) / 60, rest = seconds % 60
        switch (hours, minutes, rest) {
        case (0, 0, let s): return String(localized: "\(s) sec")
        case (0, let m, 0): return String(localized: "\(m) min")
        case (let h, 0, 0): return String(localized: "\(h) hr")
        case (let h, let m, _) where h > 0: return String(localized: "\(h) hr \(m) min")
        default: return String(localized: "\(minutes) min \(rest) sec")
        }
    }
}

/// Finds the cooking times in a step's text.
nonisolated enum CookDurations {
    // "8-10 minutes", "8 to 10 min", "5 mins", "1½ hours", "30 seconds"
    private static let pattern = try? NSRegularExpression(
        pattern:
            #"(\d+(?:\.\d+)?|\d*[½¼¾])\s*(?:(?:-|–|—|to)\s*(\d+(?:\.\d+)?|\d*[½¼¾]))?\s*(hours?|hrs?|minutes?|mins?|seconds?|secs?)\b"#,
        options: [.caseInsensitive])

    static func find(in text: String) -> [CookDuration] {
        guard let pattern else { return [] }
        let ns = text as NSString
        return pattern.matches(in: text, range: NSRange(location: 0, length: ns.length)).compactMap { match in
            guard let range = Range(match.range, in: text),
                let low = number(ns.substring(with: match.range(at: 1)))
            else { return nil }
            let high =
                match.range(at: 2).location == NSNotFound ? low : number(ns.substring(with: match.range(at: 2))) ?? low
            let unit = ns.substring(with: match.range(at: 3)).lowercased()
            let scale: Double = unit.hasPrefix("h") ? 3600 : unit.hasPrefix("s") ? 1 : 60
            let lowSeconds = Int((low * scale).rounded()), highSeconds = Int((max(high, low) * scale).rounded())
            guard lowSeconds > 0, highSeconds <= 12 * 3600 else { return nil }
            return CookDuration(range: range, lowSeconds: lowSeconds, highSeconds: highSeconds)
        }
    }

    private static func number(_ text: String) -> Double? {
        let fractions: [Character: Double] = ["½": 0.5, "¼": 0.25, "¾": 0.75]
        if let last = text.last, let fraction = fractions[last] {
            return (Double(text.dropLast()) ?? 0) + fraction
        }
        return Double(text)
    }
}
