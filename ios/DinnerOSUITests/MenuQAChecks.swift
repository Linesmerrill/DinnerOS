import XCTest

/// Checks that fail a run on what a screenshot can hide (build 123, Hark: "this cannot happen
/// again"). Each returns the problems it found, one line each, naming the screen and the item;
/// an empty list means it passed. AppSweepQATests runs them on the shared test household in every
/// matrix config; MenuCardsQATests runs the Menu ones on a household made like Merrill's week.
enum MenuQAChecks {
    static let cardPrefix = "menu.mealCard."
    static let addOnPrefix = "menu.mealCard.addOn."
    static let ratingRow = "menu.mealCard.rating"
    static let asksSuffix = ".asks"
    static let photoPlaceholder = "photo.placeholder"
    /// Room allowed between the last meal card's content and what comes next, beyond the Menu's
    /// own 24-point gap between sections. Build 122's empty band was over 100 points.
    static let allowedExtraGap: CGFloat = 40
    static let sectionSpacing: CGFloat = 24

    @MainActor
    static func cards(_ app: XCUIApplication) -> [XCUIElement] {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", cardPrefix))
            .allElementsBoundByIndex
    }

    /// (1) Every card whose meal should ask how it was (made, or its night over) shows the stars.
    /// A meal still ahead shows none, by design (build 121, Hark's item 12).
    @MainActor
    static func checkMealCardRatingRows(_ app: XCUIApplication, screen: String) -> [String] {
        var problems: [String] = []
        for card in cards(app) where card.identifier.hasSuffix(asksSuffix) {
            let stars = card.descendants(matching: .any)[ratingRow]
            if !stars.exists {
                problems.append("\(screen): the card “\(name(of: card))” should ask “How was it?” but has no stars")
            }
        }
        return problems
    }

    /// (2) The Your Meals subtitle's "N add-ons" matches the add-on cards in the carousel.
    @MainActor
    static func checkAddOnCards(_ app: XCUIApplication, screen: String) -> [String] {
        let subtitle = app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' add-on'")).firstMatch
        guard subtitle.exists else { return [] }
        let promised = addOnCount(in: subtitle.label)
        let shown = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", addOnPrefix)).count
        guard shown < promised else { return [] }
        return ["\(screen): “\(subtitle.label)” but \(shown) add-on card(s) show"]
    }

    /// "5 meals planned · 1 add-on" → 1.
    static func addOnCount(in label: String) -> Int {
        guard let range = label.range(of: #"(\d+) add-ons?"#, options: .regularExpression) else { return 0 }
        return Int(label[range].split(separator: " ").first ?? "") ?? 0
    }

    /// (4) No empty band under Your Meals at rest: what comes next (Shared Recipes, or the next
    /// section) starts right after the lowest thing any meal card shows.
    ///
    /// Measured on one snapshot of the screen: asking XCTest for each element's frame took 17 s.
    @MainActor
    static func checkNoBlankBandUnderMeals(_ app: XCUIApplication, screen: String) -> [String] {
        guard let root = try? app.snapshot() else { return ["\(screen): couldn’t read the screen"] }
        var cards: [any XCUIElementSnapshot] = []
        var texts: [CGRect] = []
        func walk(_ element: any XCUIElementSnapshot) {
            if element.identifier.hasPrefix(cardPrefix) {
                cards.append(element)
                return
            }
            if element.elementType == .staticText
                || (element.elementType == .button && element.label.hasPrefix("Shared Recipes"))
            {
                texts.append(element.frame)
            }
            element.children.forEach(walk)
        }
        walk(root)
        guard !cards.isEmpty else { return [] }
        let contentBottom = cards.map { contentBottom(of: $0) }.max() ?? 0
        let carouselTop = cards.map(\.frame.minY).min() ?? 0
        // Whatever comes first under the cards: Autopilot's suggestions, Shared Recipes' row, or
        // the next section's title.
        guard
            let next = texts.filter({ !$0.isEmpty && $0.minY > carouselTop && $0.minY >= contentBottom - 1 })
                .min(by: { $0.minY < $1.minY })
        else {
            return ["\(screen): nothing found under Your Meals to measure the gap against"]
        }
        let gap = next.minY - contentBottom
        let limit = sectionSpacing + allowedExtraGap
        guard gap > limit else { return [] }
        return ["\(screen): \(Int(gap)) points of blank space under the meal cards (allowed \(Int(limit)))"]
    }

    /// The lowest edge of what a card shows: its photo and title, its servings control, its
    /// "Didn't make it?" and its stars. Space held for a row that isn't shown is in the tree too,
    /// as an element with no name, value or identifier (build 122's band was two of them), so
    /// only elements that say something count.
    static func contentBottom(of card: any XCUIElementSnapshot) -> CGFloat {
        func says(_ element: any XCUIElementSnapshot) -> Bool {
            !element.label.isEmpty || !element.identifier.isEmpty || !((element.value as? String) ?? "").isEmpty
        }
        func bottoms(_ element: any XCUIElementSnapshot) -> [CGFloat] {
            element.children.flatMap { child in
                (child.frame.isEmpty || !says(child) ? [] : [child.frame.maxY]) + bottoms(child)
            }
        }
        return bottoms(card).max() ?? card.frame.maxY
    }

    /// (3) Nothing still loading after `timeout`: no spinner on screen, no photo showing its
    /// no-photo art, no row without a word on it.
    @MainActor
    static func checkLoaded(_ app: XCUIApplication, screen: String, timeout: TimeInterval = 10) -> [String] {
        let frame = app.frame
        func onScreen(_ rect: CGRect) -> Bool { !rect.isEmpty && frame.intersects(rect) }
        let deadline = Date().addingTimeInterval(timeout)
        var spinners: [CGRect] = []
        repeat {
            spinners = app.activityIndicators.allElementsBoundByIndex.map(\.frame).filter(onScreen)
            if spinners.isEmpty { break }
            sleep(1)
        } while Date() < deadline
        var problems: [String] = []
        if !spinners.isEmpty {
            let places = spinners.map { "\(Int($0.midX)),\(Int($0.midY))" }.joined(separator: "; ")
            problems.append("\(screen): still loading after \(Int(timeout)) s (spinner at \(places))")
        }
        let placeholders = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == %@", photoPlaceholder)).allElementsBoundByIndex
            .filter { onScreen($0.frame) }
        for marker in placeholders {
            problems.append("\(screen): no photo for “\(item(at: marker, in: app))”, showing the placeholder")
        }
        for cell in app.cells.allElementsBoundByIndex where onScreen(cell.frame) {
            // Anything with a name or a value counts: text, a picker's segments, a field.
            guard let snapshot = try? cell.snapshot() else { continue }
            func words(_ element: any XCUIElementSnapshot) -> String {
                element.label + ((element.value as? String) ?? "") + element.children.map(words).joined()
            }
            if words(snapshot).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                problems.append("\(screen): an empty row at \(Int(cell.frame.midX)),\(Int(cell.frame.midY))")
            }
        }
        return problems
    }

    /// The marker's own label (a recipe photo's name), else the text beside it on its row (an
    /// ingredient's name).
    @MainActor
    static func item(at marker: XCUIElement, in app: XCUIApplication) -> String {
        if !marker.label.isEmpty, marker.label != "ingredient" { return marker.label }
        let row = marker.frame
        let beside = app.staticTexts.allElementsBoundByIndex
            .first { $0.frame.minX >= row.maxX - 1 && abs($0.frame.midY - row.midY) < row.height }
        return beside?.label ?? "an item at \(Int(row.midX)),\(Int(row.midY))"
    }

    @MainActor
    static func name(of card: XCUIElement) -> String {
        if !card.label.isEmpty { return card.label }
        return card.staticTexts.firstMatch.exists ? card.staticTexts.firstMatch.label : card.identifier
    }
}
