import XCTest

/// Checks that fail a run on what a screenshot can hide (build 123, Hark: "this cannot happen
/// again"). Each returns the problems it found, one line each, naming the screen and the item;
/// an empty list means it passed. AppSweepQATests runs them on the shared test household in every
/// matrix config; MenuCardsQATests runs the Menu ones on a household made like Merrill's week.
enum MenuQAChecks {
    static let cardPrefix = "menu.mealCard."
    static let addOnPrefix = "menu.mealCard.addOn."
    static let ratingRow = "menu.mealRating"
    static let asksSuffix = ".asks"
    static let photoPlaceholder = "photo.placeholder"
    /// Spans a whole meal card, held space included (`QAMarkers.mealCardFrame` in the app): a
    /// card's own frame covers only what VoiceOver reads.
    static let cardFrameMarker = "menu.mealCardFrame"
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

    /// (1) Every meal card shows the stars, a meal still ahead included, so the week shows how
    /// each was liked last time (Merrill, Nov 23, undoing build 121's stars only on cooked meals).
    @MainActor
    static func checkMealCardRatingRows(_ app: XCUIApplication, screen: String) -> [String] {
        var problems: [String] = []
        for card in cards(app) {
            let stars = card.descendants(matching: .any)[ratingRow]
            if !stars.exists {
                problems.append("\(screen): the card “\(name(of: card))” has no stars")
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

    /// (6) The meal cards in a row are one height, their stars level, and "Didn't make it?" (or
    /// "I Made It"), where a past meal shows it, at the card's left edge and level with any other
    /// (build 123 review, Hark's item 2: it sat at the right of the first card only, with an empty
    /// band beside it).
    ///
    /// Measured on one snapshot of the screen, like the blank-band check.
    @MainActor
    static func checkMealCardsLineUp(_ app: XCUIApplication, screen: String) -> [String] {
        guard let root = try? app.snapshot() else { return ["\(screen): couldn’t read the screen"] }
        var cards: [any XCUIElementSnapshot] = []
        func walk(_ element: any XCUIElementSnapshot) {
            if element.identifier.hasPrefix(cardPrefix) {
                if !element.frame.isEmpty { cards.append(element) }
                return
            }
            element.children.forEach(walk)
        }
        walk(root)
        return lineUpProblems(
            cards.map { card in
                LinedUpCard(
                    name: cardName(card.label.isEmpty ? card.identifier : card.label),
                    frame: firstDescendant(of: card, identifier: cardFrameMarker)?.frame ?? card.frame,
                    outcome: outcomeControl(in: card)?.frame,
                    stars: firstDescendant(of: card, identifier: ratingRow)?.frame)
            }, screen: screen)
    }

    /// A meal card as the line-up check sees it: its frame, its "Didn't make it?" control's, and
    /// its stars'.
    struct LinedUpCard {
        var name: String
        var frame: CGRect
        var outcome: CGRect?
        var stars: CGRect?
    }

    /// How far cards may differ, in points: rounding, not a missing row.
    static let lineUpTolerance: CGFloat = 2

    /// The line-up rules on measured cards, apart from XCTest so they're tested on their own.
    static func lineUpProblems(_ cards: [LinedUpCard], screen: String) -> [String] {
        guard let first = cards.first else { return [] }
        var problems: [String] = []
        for card in cards.dropFirst() where abs(card.frame.height - first.frame.height) > lineUpTolerance {
            problems.append(
                "\(screen): “\(card.name)” is \(Int(card.frame.height)) points tall, “\(first.name)” \(Int(first.frame.height))"
            )
        }
        problems += levelProblems(cards, row: \.stars, what: "The stars", screen: screen)
        let offering = cards.filter { $0.outcome != nil }
        guard let reference = offering.first, let referenceRow = reference.outcome else { return problems }
        let referenceTop = referenceRow.minY - reference.frame.minY
        for card in offering {
            guard let row = card.outcome else { continue }
            if row.minX - card.frame.minX > lineUpTolerance {
                problems.append(
                    "\(screen): “Didn’t make it?” on “\(card.name)” starts \(Int(row.minX - card.frame.minX)) points in from the card’s left edge"
                )
            }
            if abs(row.minY - card.frame.minY - referenceTop) > lineUpTolerance {
                problems.append("\(screen): “Didn’t make it?” on “\(card.name)” isn’t level with “\(reference.name)”")
            }
        }
        return problems
    }

    /// Cards whose `row` sits at a different height in the card from the first card's.
    static func levelProblems(
        _ cards: [LinedUpCard], row: KeyPath<LinedUpCard, CGRect?>, what: String, screen: String
    ) -> [String] {
        let placed = cards.compactMap { card in card[keyPath: row].map { (card, $0.minY - card.frame.minY) } }
        guard let (reference, top) = placed.first else { return [] }
        return placed.dropFirst().filter { abs($0.1 - top) > lineUpTolerance }.map {
            "\(screen): \(what) on “\($0.0.name)” aren’t level with “\(reference.name)”"
        }
    }

    static func firstDescendant(of element: any XCUIElementSnapshot, identifier: String) -> (any XCUIElementSnapshot)? {
        for child in element.children {
            if child.identifier == identifier { return child }
            if let found = firstDescendant(of: child, identifier: identifier) { return found }
        }
        return nil
    }

    /// The card's "Didn't make it?" menu ("Didn’t make Tacos") or its "I Made It" button.
    static func outcomeControl(in card: any XCUIElementSnapshot) -> (any XCUIElementSnapshot)? {
        for child in card.children {
            if child.label.hasPrefix("Didn’t make ") || child.label == "I Made It" { return child }
            if let found = outcomeControl(in: child) { return found }
        }
        return nil
    }

    /// (7) Typing a search, each result's round add button sits at the right margin (build 123
    /// review, Hark's item 7: it floated three-quarters across an iPad in portrait).
    @MainActor
    static func checkSearchAddAtMargin(_ app: XCUIApplication, recipe: String, screen: String) -> [String] {
        // The result's own button, on its row: a carousel higher up can hold the same recipe.
        let frame = app.frame
        let rows = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", recipe)).allElementsBoundByIndex
            .map(\.frame).filter { !$0.isEmpty && frame.intersects($0) }
        let adds = app.buttons.matching(
            NSPredicate(
                format: "label == %@ OR label == %@", "Add \(recipe) to your week", "Remove \(recipe) from your week")
        ).allElementsBoundByIndex.map(\.frame)
        guard !rows.isEmpty else { return [] }
        guard
            let add = adds.first(where: { add in rows.contains { abs($0.midY - add.midY) < $0.height / 2 + 1 } })
        else { return ["\(screen): no add button beside “\(recipe)”"] }
        return searchAddProblems(button: add, screen: frame, name: screen)
    }

    /// The add button's 44-point target ends at the 20-point margin; a little slack for rounding.
    static let searchAddMaxInset: CGFloat = 32

    static func searchAddProblems(button: CGRect, screen: CGRect, name: String) -> [String] {
        let inset = screen.maxX - button.maxX
        guard inset > searchAddMaxInset else { return [] }
        return ["\(name): the add button is \(Int(inset)) points in from the right edge, not at the margin"]
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

    /// (5) Shared Recipes' rows (build 123 review): no card named like a test run's recipe
    /// ("Lemon Herb Pilaf 24421") and none without a photo. Recipes from the test runs' own
    /// households filled Chef’s Picks and Trending with both.
    @MainActor
    static func checkSharedRecipeRows(_ app: XCUIApplication, screen: String) -> [String] {
        let frame = app.frame
        var problems: [String] = []
        for card in app.buttons.matching(identifier: "DiscoverCard").allElementsBoundByIndex
        where !card.frame.isEmpty && frame.intersects(card.frame) {
            let name = cardName(card.label)
            if endsInRunNumber(name) {
                problems.append("\(screen): “\(name)” is a test recipe, its name ends in a number")
            }
        }
        let placeholders = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == %@", photoPlaceholder)).allElementsBoundByIndex
            .filter { !$0.frame.isEmpty && frame.intersects($0.frame) }
        for marker in placeholders {
            problems.append("\(screen): no photo for “\(item(at: marker, in: app))”, showing the placeholder")
        }
        return problems
    }

    /// A card's name: its label up to the time and cuisine ("Saffron Pilaf, 25 min, Indian").
    static func cardName(_ label: String) -> String {
        label.components(separatedBy: ", ").first ?? label
    }

    /// Whether a name ends in a number of three digits or more after a word: a test run's
    /// recipe ("Mushroom Pilaf 90901"), never "Chili for 2" or "Serves 4".
    static func endsInRunNumber(_ name: String) -> Bool {
        name.trimmingCharacters(in: .whitespaces)
            .range(of: #"\p{L}\s+\d{3,}$"#, options: .regularExpression) != nil
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
