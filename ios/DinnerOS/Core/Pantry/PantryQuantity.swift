import Foundation

/// Why typed amount text can't be sent to the API.
nonisolated enum PantryQuantityError: Error, Equatable, Sendable {
    case negative
    case zero
    case invalid
    case tooLong
}

extension PantryQuantityError: LocalizedError {
    nonisolated var errorDescription: String? {
        switch self {
        case .negative: String(localized: "An amount can't be negative.")
        case .zero: String(localized: "An amount must be more than zero. Leave it empty if you didn't measure.")
        case .invalid: String(localized: "Enter an amount such as 2, 0.5, 1/2, or 1 1/2.")
        case .tooLong: String(localized: "That amount is too long.")
        }
    }
}

/// Converts between amounts people type and the API's exact quantity strings.
///
/// The API stores quantities as reduced fractions (`"3/2"`). Parsing on device lets the
/// form reject bad input before a round trip, and sends exactly what the API stores.
nonisolated enum PantryQuantity {
    /// Longer input is rejected rather than guessed at.
    static let maxInputLength = 32

    /// Parses `"2"`, `"0.5"`, `"1/2"`, `"1 1/2"`, `"½"`, or `"1½"` into `"n"` or `"n/d"`,
    /// reduced, so `"1 1/2"` becomes `"3/2"`. Whitespace-only text is no amount (`nil`).
    static func parse(_ text: String) throws(PantryQuantityError) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        guard trimmed.count <= maxInputLength else { throw .tooLong }

        var expanded = ""
        for character in trimmed {
            if let fraction = fractionGlyphs[character] {
                expanded += " \(fraction) "
            } else if character == "\u{2044}" {
                // FRACTION SLASH, as in "1⁄2".
                expanded.append("/")
            } else {
                expanded.append(character)
            }
        }

        var total = ExactFraction(reducing: 0, 1)
        for part in expanded.split(whereSeparator: \.isWhitespace) {
            if part.first == "-" || part.first == "\u{2212}" {
                throw .negative
            }
            guard let value = ExactFraction(part), let sum = total.adding(value) else { throw .invalid }
            total = sum
        }
        guard total.numerator > 0 else { throw .zero }
        return total.description
    }

    /// Editable text for an exact quantity that parses back to the same value:
    /// `"3/2"` → `"1 1/2"`. `nil` is empty text.
    static func editingText(_ exact: String?) -> String {
        guard let exact else { return "" }
        guard let value = ExactFraction(Substring(exact)) else { return exact }
        let whole = value.numerator / value.denominator
        let remainder = value.numerator % value.denominator
        if remainder == 0 {
            return String(whole)
        }
        return whole == 0 ? "\(remainder)/\(value.denominator)" : "\(whole) \(remainder)/\(value.denominator)"
    }

    private static let fractionGlyphs: [Character: String] = [
        "½": "1/2", "⅓": "1/3", "⅔": "2/3", "¼": "1/4", "¾": "3/4", "⅕": "1/5", "⅖": "2/5", "⅗": "3/5",
        "⅘": "4/5", "⅙": "1/6", "⅚": "5/6", "⅛": "1/8", "⅜": "3/8", "⅝": "5/8", "⅞": "7/8",
    ]

    /// A reduced, non-negative fraction. Arithmetic reports overflow instead of trapping.
    private nonisolated struct ExactFraction {
        let numerator: Int
        let denominator: Int

        /// `denominator` must be positive.
        init(reducing numerator: Int, _ denominator: Int) {
            let divisor = Self.gcd(numerator, denominator)
            self.numerator = numerator / divisor
            self.denominator = denominator / divisor
        }

        /// `"2"`, `"0.5"`, `".5"`, or `"3/4"`. ASCII digits only.
        init?(_ text: Substring) {
            let parts = text.split(separator: "/", omittingEmptySubsequences: false)
            switch parts.count {
            case 1:
                guard let decimal = Self.decimal(parts[0]) else { return nil }
                self = decimal
            case 2:
                guard let top = Self.integer(parts[0]), let bottom = Self.integer(parts[1]), bottom > 0 else {
                    return nil
                }
                self.init(reducing: top, bottom)
            default:
                return nil
            }
        }

        func adding(_ other: ExactFraction) -> ExactFraction? {
            let divisor = Self.gcd(denominator, other.denominator)
            let (left, leftOverflow) = numerator.multipliedReportingOverflow(by: other.denominator / divisor)
            let (right, rightOverflow) = other.numerator.multipliedReportingOverflow(by: denominator / divisor)
            let (sum, sumOverflow) = left.addingReportingOverflow(right)
            let (common, commonOverflow) = (denominator / divisor).multipliedReportingOverflow(by: other.denominator)
            guard !(leftOverflow || rightOverflow || sumOverflow || commonOverflow) else { return nil }
            return ExactFraction(reducing: sum, common)
        }

        var description: String {
            denominator == 1 ? String(numerator) : "\(numerator)/\(denominator)"
        }

        private static func integer(_ digits: Substring) -> Int? {
            guard !digits.isEmpty, digits.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
            return Int(digits)
        }

        private static func decimal(_ text: Substring) -> ExactFraction? {
            let pieces = text.split(separator: ".", omittingEmptySubsequences: false)
            guard (1...2).contains(pieces.count) else { return nil }
            let wholeDigits = pieces[0]
            let fractionDigits = pieces.count == 2 ? pieces[1] : ""
            guard !wholeDigits.isEmpty || !fractionDigits.isEmpty else { return nil }
            guard let whole = wholeDigits.isEmpty ? 0 : integer(wholeDigits) else { return nil }
            guard !fractionDigits.isEmpty else { return ExactFraction(reducing: whole, 1) }
            guard fractionDigits.count <= 18, let fraction = integer(fractionDigits) else { return nil }
            var scale = 1
            for _ in 0..<fractionDigits.count {
                scale *= 10
            }
            let (scaled, scaleOverflow) = whole.multipliedReportingOverflow(by: scale)
            let (sum, sumOverflow) = scaled.addingReportingOverflow(fraction)
            guard !scaleOverflow, !sumOverflow else { return nil }
            return ExactFraction(reducing: sum, scale)
        }

        private static func gcd(_ a: Int, _ b: Int) -> Int {
            var (a, b) = (a.magnitude, b.magnitude)
            while b != 0 {
                (a, b) = (b, a % b)
            }
            return a == 0 ? 1 : Int(a)
        }
    }
}
