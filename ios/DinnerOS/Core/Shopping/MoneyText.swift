import Foundation

/// US dollar amounts as integer cents, the only way DinnerOS stores money.
nonisolated enum MoneyText {
    /// The API's largest price or total: $10,000.00.
    static let maxCents = 1_000_000
    static let range = 0...maxCents

    /// "$4.98" for 498, formatted for `locale` but always in US dollars.
    static func format(_ cents: Int, locale: Locale = .autoupdatingCurrent) -> String {
        let amount = Decimal(cents) / 100
        return amount.formatted(.currency(code: "USD").locale(locale))
    }

    /// "4.98" for 498: what a price field starts with when editing.
    static func editingText(_ cents: Int) -> String {
        let dollars = cents / 100
        let remainder = abs(cents % 100)
        return remainder < 10 ? "\(dollars).0\(remainder)" : "\(dollars).\(remainder)"
    }

    /// Cents for text a member typed or OCR read: "4.98", "$4.98", "4", "$1,234.50", "4.5".
    /// `nil` for empty text, anything negative, more than two decimals, or anything else.
    static func cents(from text: String) -> Int? {
        var trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("$") {
            trimmed = String(trimmed.dropFirst()).trimmingCharacters(in: .whitespaces)
        }
        guard !trimmed.isEmpty else { return nil }
        let parts = trimmed.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count <= 2 else { return nil }
        let wholeText = parts[0].replacingOccurrences(of: ",", with: "")
        guard parts[0].isEmpty || isGroupedDigits(String(parts[0])) else { return nil }
        let fraction = parts.count == 2 ? String(parts[1]) : ""
        guard fraction.count <= 2, fraction.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        guard !(wholeText.isEmpty && fraction.isEmpty) else { return nil }
        guard wholeText.count <= 9, let whole = Int(wholeText.isEmpty ? "0" : wholeText) else { return nil }
        let paddedFraction = fraction.padding(toLength: 2, withPad: "0", startingAt: 0)
        guard let cents = Int(paddedFraction) else { return nil }
        return whole * 100 + cents
    }

    /// Why a price field's text can't be saved, or `nil` when it's empty or valid.
    static func error(_ text: String) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        guard let cents = cents(from: trimmed) else {
            return String(localized: "Enter a price such as 4.98.")
        }
        guard range.contains(cents) else {
            return String(localized: "A price can be at most \(format(maxCents)).")
        }
        return nil
    }

    /// "1234" or "1,234": digits, optionally grouped by thousands.
    private static func isGroupedDigits(_ text: String) -> Bool {
        guard text.allSatisfy({ ($0.isASCII && $0.isNumber) || $0 == "," }) else { return false }
        guard text.contains(",") else { return true }
        let groups = text.split(separator: ",", omittingEmptySubsequences: false)
        guard let first = groups.first, (1...3).contains(first.count) else { return false }
        return groups.dropFirst().allSatisfy { $0.count == 3 }
    }
}

nonisolated extension FieldChange where Value == Int {
    /// What a price field's `text` means against the price it started with: blank means
    /// "don't send", while clearing a price that was there sends `null`. `nil` when invalid.
    static func price(text: String, original: Int?) -> FieldChange<Int>? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return original == nil ? .keep : .clear
        }
        guard MoneyText.error(trimmed) == nil, let cents = MoneyText.cents(from: trimmed) else { return nil }
        return cents == original ? .keep : .set(cents)
    }
}
