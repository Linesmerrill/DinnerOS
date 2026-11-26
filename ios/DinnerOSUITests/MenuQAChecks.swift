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
        // iPhone Duo's cover screen keeps its last 84 points for the button rail (x from 382, as
        // QAShotCompare.mask has it): the page's right margin is at the rail.
        var content = frame
        if Int(frame.width.rounded()) == 466 && Int(frame.height.rounded()) == 678 { content.size.width = 382 }
        return searchAddProblems(button: add, screen: content, name: screen)
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

    /// The ingredient thumbnails' no-photo tile (`QAMarkers.ingredientFallback`).
    static let ingredientFallback = "ingredient.fallback"

    /// Every ingredient on screen without a photo shows the lettered tile, never the old gray
    /// fork and knife or an empty plate (Hark's build 121 round 3: Pine Nuts). Each marker is
    /// labeled with the name and its letter ("Pine Nuts: P").
    @MainActor
    static func checkIngredientFallbacks(_ app: XCUIApplication, screen: String) -> [String] {
        let frame = app.frame
        var problems: [String] = []
        let markers = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == %@", ingredientFallback)).allElementsBoundByIndex
            .filter { !$0.frame.isEmpty && frame.intersects($0.frame) }
        for marker in markers {
            let parts = marker.label.components(separatedBy: ": ")
            let letter = parts.count > 1 ? parts[parts.count - 1] : ""
            if letter.count != 1 || !(letter.first?.isLetter ?? false) {
                problems.append("\(screen): “\(parts.first ?? marker.label)” has no letter on its no-photo tile")
            }
        }
        return problems
    }

    /// The seeded recipes' ingredients all have photos (Hark's build 121 review, round 5: Pine Nuts
    /// showed its letter tile): any no-photo tile on screen is a problem. A photo still loading
    /// gets up to `wait` seconds to come in.
    @MainActor
    static func checkIngredientPhotos(_ app: XCUIApplication, screen: String, wait: Int = 8) -> [String] {
        let frame = app.frame
        var names: [String] = []
        for attempt in 0...wait {
            names = app.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier == %@", ingredientFallback)).allElementsBoundByIndex
                .filter { !$0.frame.isEmpty && frame.intersects($0.frame) }
                .map { $0.label.components(separatedBy: ": ").first ?? $0.label }
            if names.isEmpty { return [] }
            if attempt < wait { sleep(1) }
        }
        return names.map { "\(screen): “\($0)” shows a letter tile, not a photo" }
    }

    // MARK: - Build 121 review round 2 (Hark's items 9 to 13)

    /// (8) Your Meals' "N meals planned" is the meal cards, add-ons not counted, and a card past
    /// the screen's edge always shows a peek of itself (item 10: on an iPad in landscape three
    /// cards filled the row exactly and the fourth meal had no sign of being there).
    @MainActor
    static func checkMealCountMatchesCards(_ app: XCUIApplication, screen: String) -> [String] {
        let subtitle = app.staticTexts.matching(NSPredicate(format: "label MATCHES %@", #"^\d+ meals? planned.*"#))
            .firstMatch
        guard subtitle.exists, let promised = mealCount(in: subtitle.label) else { return [] }
        let meals = cards(app).filter { !$0.identifier.hasPrefix(addOnPrefix) }
        var problems = countProblems(label: subtitle.label, promised: promised, mealCards: meals.count, screen: screen)
        problems += peekProblems(cards: cards(app).map(\.frame), screen: app.frame, name: screen)
        return problems
    }

    /// "3 meals planned · 1 add-on" → 3.
    static func mealCount(in label: String) -> Int? {
        guard let range = label.range(of: #"^\d+ meals?\b"#, options: .regularExpression) else { return nil }
        return Int(label[range].split(separator: " ").first ?? "")
    }

    static func countProblems(label: String, promised: Int, mealCards: Int, screen: String) -> [String] {
        promised == mealCards ? [] : ["\(screen): “\(label)” but \(mealCards) meal card(s), add-ons not counted"]
    }

    /// A card wholly past the screen's right edge needs a card cut by that edge, at least 20
    /// points of it showing, so the row says there's more.
    static func peekProblems(cards: [CGRect], screen: CGRect, name: String) -> [String] {
        let edge = screen.maxX
        let hidden = cards.filter { $0.minX >= edge - 1 }
        guard !hidden.isEmpty else { return [] }
        let peeking = cards.contains { $0.minX <= edge - 20 && $0.maxX > edge + 1 }
        return peeking ? [] : ["\(name): \(hidden.count) card(s) past the edge with none peeking in"]
    }

    /// (9) By Day shows the whole week from its first day: at rest the list starts at the top,
    /// nothing tucked under the week strip, and scrolling down meets all seven days in order
    /// (item 9: it opened on Monday, today, with Sunday's meal scrolled away under the strip).
    @MainActor
    static func checkByDayWholeWeek(_ app: XCUIApplication, screen: String) -> [String] {
        let strip = app.descendants(matching: .any)["menu.weekStrip"]
        guard strip.waitForExistence(timeout: 5) else { return ["\(screen): no week strip"] }
        let stripBottom = strip.frame.maxY
        func headers() -> [(id: String, frame: CGRect)] {
            app.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier BEGINSWITH 'byDay.'")).allElementsBoundByIndex
                .map { ($0.identifier, $0.frame) }
                .filter { !$0.1.isEmpty }
                .sorted { $0.1.minY < $1.1.minY }
        }
        let atRest = headers()
        var problems = byDayTopProblems(atRest, stripBottom: stripBottom, screen: screen)
        let firstCard = app.cells.allElementsBoundByIndex.map(\.frame).filter { !$0.isEmpty }.map(\.minY).min()
        problems += byDayGapProblems(firstCardTop: firstCard, stripBottom: stripBottom, screen: screen)
        var seen: [String] = atRest.map(\.id).filter { $0.hasPrefix("byDay.day.") }
        for _ in 0..<6 where seen.count < 7 {
            app.swipeUp()
            sleep(1)
            for header in headers() where header.id.hasPrefix("byDay.day.") && !seen.contains(header.id) {
                seen.append(header.id)
            }
        }
        problems += byDayWeekProblems(seen, screen: screen)
        for _ in 0..<6 { app.swipeDown() }
        return problems
    }

    /// At rest the first heading is the list's first, below the strip: the meals without a day,
    /// else the week's first day.
    static func byDayTopProblems(
        _ headers: [(id: String, frame: CGRect)], stripBottom: CGFloat, screen: String
    ) -> [String] {
        guard let first = headers.first else { return ["\(screen): no day headings"] }
        var problems: [String] = []
        if first.frame.minY < stripBottom - 1 {
            problems.append("\(screen): the first heading is under the week strip, the list isn't at its top")
        }
        let days = headers.filter { $0.id.hasPrefix("byDay.day.") }
        if headers.contains(where: { $0.id == "byDay.unscheduled" }), first.id != "byDay.unscheduled" {
            problems.append("\(screen): the meals without a day aren't at the top")
        }
        if days.isEmpty { problems.append("\(screen): no day showing at rest") }
        return problems
    }

    /// The first card keeps some room under the strip (on the Duo it touched the divider).
    static func byDayGapProblems(firstCardTop: CGFloat?, stripBottom: CGFloat, screen: String) -> [String] {
        guard let top = firstCardTop, top < stripBottom + 8 else { return [] }
        return ["\(screen): the first card is \(Int(top - stripBottom)) points under the week strip, touching it"]
    }

    /// Seven days, each once, in a week's order (any first day).
    static func byDayWeekProblems(_ seen: [String], screen: String) -> [String] {
        let order = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
        let days = seen.map { String($0.dropFirst("byDay.day.".count)) }
        guard days.count == 7, Set(days).count == 7, let start = order.firstIndex(of: days[0]) else {
            return ["\(screen): the week shows \(days.count) day(s) (\(days.joined(separator: ", "))), not 7"]
        }
        let expected = (0..<7).map { order[(start + $0) % 7] }
        return days == expected ? [] : ["\(screen): the days are out of order: \(days.joined(separator: ", "))"]
    }

    /// (10) On an iPad, Suggested for You's cards are the meal cards' width (item 12: one
    /// suggestion was 575 points wide under 360-point meal cards).
    @MainActor
    static func checkSuggestedCardWidths(_ app: XCUIApplication, screen: String) -> [String] {
        guard app.frame.width >= 600 else { return [] }
        let suggested = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Suggested for '"))
            .allElementsBoundByIndex.map(\.frame.width).filter { $0 > 0 }
        let meals = cards(app).map(\.frame.width).filter { $0 > 0 }
        return widthProblems(suggested: suggested, meals: meals, screen: screen)
    }

    static func widthProblems(suggested: [CGFloat], meals: [CGFloat], screen: String) -> [String] {
        guard let meal = meals.max(), meal > 0 else { return [] }
        return suggested.filter { abs($0 - meal) > meal * 0.1 }.map {
            "\(screen): a suggestion is \(Int($0)) points wide, the meal cards \(Int(meal))"
        }
    }

    /// (11) New to You never has a meal marked made (item 13).
    @MainActor
    static func checkNewToYouHasNoMadeMeal(_ app: XCUIApplication, screen: String) -> [String] {
        let made = cards(app).flatMap { card in
            card.buttons.matching(NSPredicate(format: "label CONTAINS ', Made'")).allElementsBoundByIndex
                .map { cardName($0.label) }
        }
        let section = app.descendants(matching: .any)["menu.section.new_to_you"]
        guard !made.isEmpty, section.exists else { return [] }
        let newToYou = section.buttons.allElementsBoundByIndex.map { cardName($0.label) }
        return madeInNewToYouProblems(made: made, newToYou: newToYou, screen: screen)
    }

    static func madeInNewToYouProblems(made: [String], newToYou: [String], screen: String) -> [String] {
        Set(made).intersection(newToYou).sorted().map { "\(screen): “\($0)” is marked made and still New to You" }
    }

    /// (12) Shared Recipes' rows that fit on screen sit centered, the same room on both sides
    /// (item 11: three recipes a row on an iPad in landscape stopped at 56% of the screen under
    /// a centered title).
    @MainActor
    static func checkSharedRecipesCentered(_ app: XCUIApplication, screen: String) -> [String] {
        let rows = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH 'DiscoverSection-'")).allElementsBoundByIndex
        let spans: [CGRect] = rows.compactMap { row in
            let frames = row.buttons.matching(identifier: "DiscoverCard").allElementsBoundByIndex.map(\.frame)
                .filter { !$0.isEmpty }
            return frames.dropFirst().reduce(frames.first) { $0?.union($1) }
        }
        return centeredProblems(spans: spans, screen: app.frame, name: screen)
    }

    /// The widest row that fits on screen is centered, and every row starts at its left edge: a
    /// shorter row lines up under it rather than being centered on its own.
    static func centeredProblems(spans: [CGRect], screen: CGRect, name: String) -> [String] {
        let fitting = spans.filter { $0.maxX <= screen.maxX + 1 }
        guard let widest = fitting.max(by: { $0.width < $1.width }) else { return [] }
        let left = widest.minX - screen.minX
        let right = screen.maxX - widest.maxX
        var problems: [String] = []
        if abs(left - right) > 24 {
            problems.append(
                "\(name): the recipes are off center, \(Int(left)) points on the left, \(Int(right)) on the right")
        }
        if spans.contains(where: { abs($0.minX - widest.minX) > 2 }) {
            problems.append("\(name): the rows of recipes don't start at one left edge")
        }
        return problems
    }

    @MainActor
    static func name(of card: XCUIElement) -> String {
        if !card.label.isEmpty { return card.label }
        return card.staticTexts.firstMatch.exists ? card.staticTexts.firstMatch.label : card.identifier
    }
}

// MARK: - Every ingredient row's thumbnail in a kept shot (Hark's qa/124 review, item 5)

extension MenuQAChecks {
    /// Drags the list up by `distance` from just under the screen's middle, clear of the bottom
    /// bar, slowly and held, so it stops where the finger does.
    @MainActor
    static func dragList(_ app: XCUIApplication, by distance: CGFloat) {
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.62))
        let to = from.withOffset(CGVector(dx: 0, dy: -distance))
        from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.4)
    }

    /// What the list shows at one place on the way down: the thumbnails resting whole in the
    /// clear band, and where each one is, to tell how far the list moved since.
    struct ListPlace {
        var shown: Set<String>
        var minY: [String: CGFloat]
    }

    /// The thumbnails resting whole between the bars and fades the app reports at the top and
    /// at the bottom. On the Duo the typed recipe's top shot ended at Lemon and its end shot began
    /// at Fresh Basil, so Pine Nuts, Olive Oil and Garlic were in no shot.
    @MainActor
    static func ingredientPlace(_ app: XCUIApplication) -> ListPlace {
        let screen = app.frame
        // Bars only: a row under a fade still shows (the fade is the bar's soft edge).
        let bars = (FadeQAChecks.covers(app) ?? []).filter { !$0.isFade }.map(\.frame)
        var place = ListPlace(shown: [], minY: [:])
        var notes: [String] = []
        defer { print("QA-NOTE: ingredient thumbnails whole: \(place.shown.sorted()), at: \(notes)") }
        for thumb in app.descendants(matching: .any).matching(identifier: QAMarkers.ingredientThumb)
            .allElementsBoundByIndex
        {
            let frame = thumb.frame
            // The thumbnail's own element (a row can carry the identifier too, with a longer label).
            guard !frame.isEmpty, frame.width < 120, screen.intersects(frame) else { continue }
            place.minY[thumb.label] = frame.minY
            // The bars over this thumbnail's column, on the iPad's two columns too.
            let over = bars.filter { $0.minX < frame.maxX && $0.maxX > frame.minX }
            let top = over.filter { $0.midY < screen.midY }.map(\.maxY).max() ?? screen.minY
            let bottom = over.filter { $0.midY >= screen.midY }.map(\.minY).min() ?? screen.maxY
            let whole = frame.minY >= top - 1 && frame.maxY <= bottom + 1
            if whole { place.shown.insert(thumb.label) }
            notes.append("\(thumb.label) \(Int(frame.minY)) in \(Int(top))-\(Int(bottom))")
        }
        return place
    }

    /// How far the list moved up from `a` to `b`: the middle of the moves of the thumbnails on both.
    static func moved(from a: ListPlace, to b: ListPlace) -> CGFloat? {
        let moves = a.minY.compactMap { name, y in b.minY[name].map { y - $0 } }.sorted()
        return moves.isEmpty ? nil : moves[moves.count / 2]
    }

    /// The places on the way (indexes into `onTheWay`, in list order) to keep as middle shots:
    /// each the one that shows the most of `expected` the top, the end and the places already
    /// picked don't, the nearer the list's middle on a tie. Usually one; a long list on a short
    /// screen (the SE) can need two. `missing` is what still shows in no shot.
    static func middlePicks(
        top: Set<String>, end: Set<String>, onTheWay: [Set<String>], expected: [String]
    ) -> (indexes: [Int], missing: [String]) {
        func lower(_ set: Set<String>) -> Set<String> { Set(set.map { $0.lowercased() }) }
        var left = Set(expected.map { $0.lowercased() }).subtracting(lower(top)).subtracting(lower(end))
        var picked: [Int] = []
        let middle = Double(onTheWay.count - 1) / 2
        while !left.isEmpty {
            var best: (index: Int, gain: Int, distance: Double)?
            for (i, shown) in onTheWay.enumerated() where !picked.contains(i) {
                let gain = left.intersection(lower(shown)).count
                let distance = abs(Double(i) - middle)
                if gain > 0, best == nil || gain > best!.gain || (gain == best!.gain && distance < best!.distance) {
                    best = (i, gain, distance)
                }
            }
            guard let best else { break }
            picked.append(best.index)
            left.subtract(lower(onTheWay[best.index]))
        }
        return (picked.sorted(), expected.filter { left.contains($0.lowercased()) })
    }
}
