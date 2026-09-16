import Foundation

/// Calendar dates in the API's `YYYY-MM-DD` form.
nonisolated enum PantryDate {
    /// Gregorian, whatever calendar the device uses, because the API's dates are Gregorian.
    static func calendar(timeZone: TimeZone) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        return calendar
    }

    /// Midnight at the start of the date in `timeZone`. `nil` unless `string` names a real date.
    static func date(from string: String, timeZone: TimeZone) -> Date? {
        let parts = string.split(separator: "-", omittingEmptySubsequences: false)
        guard
            parts.count == 3, parts[0].count == 4, parts[1].count == 2, parts[2].count == 2,
            parts.allSatisfy({ $0.allSatisfy { $0.isASCII && $0.isNumber } }),
            let year = Int(parts[0]), let month = Int(parts[1]), let day = Int(parts[2])
        else { return nil }
        let gregorian = Self.calendar(timeZone: timeZone)
        guard let date = gregorian.date(from: DateComponents(year: year, month: month, day: day)) else { return nil }
        // Rejects dates the calendar would roll over, such as February 30.
        let components = gregorian.dateComponents([.year, .month, .day], from: date)
        guard components.year == year, components.month == month, components.day == day else { return nil }
        return date
    }

    static func string(from date: Date, timeZone: TimeZone) -> String {
        let components = calendar(timeZone: timeZone).dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", components.year ?? 0, components.month ?? 0, components.day ?? 0)
    }
}

/// How soon an item expires, relative to today in the device's time zone.
nonisolated struct PantryExpiry: Equatable, Sendable {
    let date: Date
    /// Whole calendar days from today; negative once expired.
    let daysRemaining: Int
    let timeZone: TimeZone

    /// Days within which an expiry date is highlighted.
    static let soonDays = 3

    init?(expiresOn: String?, today: Date = .now, timeZone: TimeZone = .autoupdatingCurrent) {
        guard let expiresOn, let date = PantryDate.date(from: expiresOn, timeZone: timeZone) else { return nil }
        let calendar = PantryDate.calendar(timeZone: timeZone)
        guard let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: today), to: date).day else {
            return nil
        }
        self.date = date
        self.daysRemaining = days
        self.timeZone = timeZone
    }

    var isExpired: Bool { daysRemaining < 0 }
    var isSoon: Bool { (0...Self.soonDays).contains(daysRemaining) }

    /// "Expired", "Expires today", "Expires in 3 days", or a date for far-off expiries.
    func text(locale: Locale = .autoupdatingCurrent) -> String {
        switch daysRemaining {
        case ..<0:
            return String(localized: "Expired")
        case 0:
            return String(localized: "Expires today")
        case 1:
            return String(localized: "Expires tomorrow")
        case 2...30:
            return String(localized: "Expires in \(daysRemaining) days")
        default:
            let style = Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone)
            return String(localized: "Expires \(date.formatted(style))")
        }
    }
}

/// Display text for pantry values.
nonisolated enum PantryFormat {
    /// For example "1½ cups" or "2". `nil` when no amount was recorded.
    static func amount(_ item: PantryItem, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let quantity = RecipeFormat.quantity(item.quantity, value: item.quantityValue, locale: locale) else {
            return nil
        }
        let code = item.unit ?? PantryUnit.defaultCode
        let unit = RecipeFormat.unitLabel(code, sourceUnit: code, plural: RecipeFormat.isPlural(item.quantityValue))
        return unit.isEmpty ? quantity : "\(quantity) \(unit)"
    }
}

// MARK: - Grouping and filtering

/// The status segments above the pantry list.
nonisolated enum PantryStatusFilter: String, CaseIterable, Identifiable, Sendable {
    case all
    case low
    case out

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: String(localized: "All")
        case .low: String(localized: "Low")
        case .out: String(localized: "Out")
        }
    }

    func includes(_ status: PantryStatus) -> Bool {
        switch self {
        case .all: true
        case .low: status == .low
        case .out: status == .out
        }
    }
}

/// One grocery category of the pantry list.
nonisolated struct PantrySection: Equatable, Sendable, Identifiable {
    let category: String
    var items: [PantryItem]

    var id: String { category }
    var title: String { PantryCategory.title(category) }
}

/// Ordering, search, and grouping for the pantry list. The whole pantry is loaded, so these
/// run on device instead of sending the API's list filters.
nonisolated enum PantryList {
    /// Aisle order, then name ignoring case, then ID: the API's order, so an item applied
    /// after a change lands where a reload would put it.
    static func sorted(_ items: [PantryItem]) -> [PantryItem] {
        items.sorted(by: areInIncreasingOrder)
    }

    static func areInIncreasingOrder(_ lhs: PantryItem, _ rhs: PantryItem) -> Bool {
        let (leftRank, rightRank) = (PantryCategory.rank(lhs.category), PantryCategory.rank(rhs.category))
        if leftRank != rightRank {
            return leftRank < rightRank
        }
        if lhs.category != rhs.category {
            // Unknown categories: keep each one together.
            return lhs.category < rhs.category
        }
        let (leftName, rightName) = (lhs.displayName.lowercased(), rhs.displayName.lowercased())
        if leftName != rightName {
            return leftName < rightName
        }
        return lhs.id < rhs.id
    }

    /// Whether the display name or normalized key contains `search`, ignoring case and
    /// accents, so "jalapeno" finds "Jalapeño". Blank search matches everything.
    static func matches(_ item: PantryItem, search: String) -> Bool {
        let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else { return true }
        let options: String.CompareOptions = [.caseInsensitive, .diacriticInsensitive, .widthInsensitive]
        return item.displayName.range(of: query, options: options) != nil
            || item.key.range(of: query, options: options) != nil
    }

    /// Matching items grouped by category in aisle order. Empty categories are left out.
    static func sections(
        _ items: [PantryItem], search: String = "", status: PantryStatusFilter = .all
    ) -> [PantrySection] {
        let visible = sorted(items.filter { status.includes($0.status) && matches($0, search: search) })
        var sections: [PantrySection] = []
        for item in visible {
            if let last = sections.indices.last, sections[last].category == item.category {
                sections[last].items.append(item)
            } else {
                sections.append(PantrySection(category: item.category, items: [item]))
            }
        }
        return sections
    }

    /// The item an add would merge into: the same catalog ingredient, or the same name.
    /// A hint only; the API decides by normalized key.
    static func existingItem(in items: [PantryItem], named name: String, ingredientID: String?) -> PantryItem? {
        if let ingredientID, let match = items.first(where: { $0.ingredientID == ingredientID }) {
            return match
        }
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let options: String.CompareOptions = [.caseInsensitive, .diacriticInsensitive]
        return items.first {
            $0.displayName.compare(trimmed, options: options) == .orderedSame
                || $0.key.compare(trimmed, options: options) == .orderedSame
        }
    }
}
