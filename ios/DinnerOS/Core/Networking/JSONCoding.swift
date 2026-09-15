import Foundation

/// JSON coders matching the API's conventions: camelCase keys and ISO 8601 dates.
nonisolated enum JSONCoding {
    static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let string = try container.decode(String.self)
            guard let date = parseDate(string) else {
                throw DecodingError.dataCorruptedError(
                    in: container, debugDescription: "Expected an ISO 8601 date-time")
            }
            return date
        }
        return decoder
    }

    static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(date.formatted(fractionalSecondsStyle))
        }
        return encoder
    }

    private static let fractionalSecondsStyle = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
    private static let wholeSecondsStyle = Date.ISO8601FormatStyle()

    /// Parses RFC 3339 date-times with or without fractional seconds.
    ///
    /// Go encodes `time.Time` with up to nine fractional digits, while Foundation's
    /// ISO 8601 parser handles millisecond precision, so extra digits are truncated.
    static func parseDate(_ string: String) -> Date? {
        let normalized = normalizingFractionalSeconds(string)
        if let date = try? Date(normalized, strategy: fractionalSecondsStyle) {
            return date
        }
        return try? Date(normalized, strategy: wholeSecondsStyle)
    }

    private static func normalizingFractionalSeconds(_ string: String) -> String {
        guard
            let timeSeparator = string.firstIndex(where: { $0 == "T" || $0 == "t" }),
            let dot = string[timeSeparator...].firstIndex(of: ".")
        else { return string }

        let fractionStart = string.index(after: dot)
        let fractionEnd = string[fractionStart...].firstIndex { !("0"..."9").contains($0) } ?? string.endIndex
        let digits = string[fractionStart..<fractionEnd]
        guard !digits.isEmpty else { return string }

        let milliseconds = String(digits.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0)
        return String(string[..<fractionStart]) + milliseconds + String(string[fractionEnd...])
    }
}
