import XCTest

/// Checks that a screen's rows come to rest clear of what the app draws over its scrolling content:
/// the fades at pinned bars' edges, the bars themselves and the tab bar's scrim (Hark's review,
/// build 121 round 2: Sign Out and Delete Account washed out above the tab bar, a recipe's first
/// Nutrition row half under the pinned picker, Add Prices ghosted under the Walmart note).
///
/// The app reports those covers by frame (`QACovers`, with `QA_MARKERS=1`): one element,
/// "qa.covers", whose value lists them in screen points. A row is clear when it's on the screen,
/// overlaps none of them and none of the tab bar, and its pixels are drawn at full strength.
enum FadeQAChecks {
    static let identifier = "qa.covers"
    /// Edges that touch, or overlap by less than this, don't count: a row resting right on a
    /// fade's edge is clear of it.
    static let slack: CGFloat = 1.5
    /// How far a clear row's darkest and lightest pixels differ at least: text on a card differs
    /// by 110 or more, text under half a fade by 60 or less.
    static let drawnContrast = 80

    /// A fade or a pinned bar, where the app reports it.
    struct Cover: Equatable {
        var isFade: Bool
        var frame: CGRect
    }

    /// The covers in "f:x,y,w,h;b:x,y,w,h" (`QACovers.encode`: `f` a fade, `b` a bar); anything
    /// else is left out.
    static func decode(_ value: String) -> [Cover] {
        value.split(separator: ";").compactMap { part in
            let pieces = part.split(separator: ":", maxSplits: 1)
            guard pieces.count == 2, pieces[0] == "f" || pieces[0] == "b" else { return nil }
            let numbers = pieces[1].split(separator: ",").compactMap { Double($0.trimmingCharacters(in: .whitespaces)) }
            guard numbers.count == 4 else { return nil }
            return Cover(
                isFade: pieces[0] == "f",
                frame: CGRect(x: numbers[0], y: numbers[1], width: numbers[2], height: numbers[3]))
        }
    }

    /// Whether `a` and `b` overlap by more than `slack` each way: edges that touch don't count.
    static func overlaps(_ a: CGRect, _ b: CGRect) -> Bool {
        let overlap = a.intersection(b)
        return !overlap.isNull && overlap.width > slack && overlap.height > slack
    }

    /// The cover `frame` (what's on screen of a row) rests part way under, or nil when it's
    /// clear: anything in a fade shows washed out, and anything part way under a bar is cut by
    /// its edge. Wholly under bars (a bar's own button, a row scrolled away under it) is fine.
    static func cover(over frame: CGRect, in covers: [Cover]) -> Cover? {
        if let fade = covers.first(where: { $0.isFade && overlaps($0.frame, frame) }) { return fade }
        let bars = covers.filter { !$0.isFade && overlaps($0.frame, frame) }
        guard let bar = bars.first else { return nil }
        let hidden = bars.reduce(CGRect.null) { $0.union($1.frame) }.insetBy(dx: -slack, dy: -slack)
        return hidden.contains(frame) ? nil : bar
    }

    /// The covers the app reports now and the tab bar, or nil when the app reports none (no
    /// `QA_MARKERS=1`).
    @MainActor
    static func covers(_ app: XCUIApplication) -> [Cover]? {
        let element = app.descendants(matching: .any)[identifier].firstMatch
        guard element.exists else { return nil }
        // Only what's across the screen: a bar laid out off it (at the window's corner, in a
        // screen not showing) covers nothing.
        let screen = app.frame
        var covers = decode(element.value as? String ?? "").filter {
            $0.frame.minX >= screen.minX - 1 && $0.frame.maxX <= screen.maxX + 1 && $0.frame.intersects(screen)
        }
        let tabBar = app.tabBars.firstMatch
        if tabBar.exists, !tabBar.frame.isEmpty { covers.append(Cover(isFade: false, frame: tabBar.frame)) }
        return covers
    }

    /// Problems with `element` (`what`, for the message) as it rests: it must be wholly on the
    /// screen, clear of every fade and bar, and (given the screenshot) drawn at full strength.
    @MainActor
    static func checkClear(
        _ element: XCUIElement, what: String, in app: XCUIApplication, screen: String, image: QAShotImage? = nil
    ) -> [String] {
        guard element.exists else { return ["\(screen): \(what) isn’t on the screen"] }
        let frame = element.frame
        guard !frame.isEmpty, app.frame.insetBy(dx: -slack, dy: -slack).contains(frame) else {
            return ["\(screen): \(what) is partly off the screen, at \(describe(frame))"]
        }
        guard let covers = covers(app) else {
            return ["\(screen): the app doesn’t report its fades (launch it with QA_MARKERS=1)"]
        }
        log(screen: screen, covers: covers, what: [(what, frame)])
        if let cover = covers.first(where: { overlaps($0.frame, frame) }) {
            return [
                "\(screen): \(what) rests under a \(cover.isFade ? "fade" : "bar"), at \(describe(frame)) "
                    + "(it covers \(describe(cover.frame)))"
            ]
        }
        if let image {
            let range = QAShotCompare.brightnessRange(image, in: frame.insetBy(dx: 0, dy: 1))
            if range < drawnContrast {
                return ["\(screen): \(what) is drawn faded (brightness range \(range), want \(drawnContrast)+)"]
            }
        }
        return []
    }

    /// Scrolls the screen's main list to its end and checks that `element`, its last row, comes
    /// to rest whole and clear of the bottom bars and their fades.
    @MainActor
    static func checkClearAtEnd(
        _ element: XCUIElement, what: String, in app: XCUIApplication, screen: String,
        capture: () -> QAShot
    ) -> [String] {
        scrollToEnd(app, look: capture)
        return checkClear(
            element, what: what, in: app, screen: "\(screen), scrolled to the end", image: capture().image)
    }

    /// Drags the largest scrolling view up until the screen stops changing (the end of the
    /// list), then gives it a moment to settle. `look` takes a screenshot without settling.
    @MainActor
    static func scrollToEnd(_ app: XCUIApplication, maxDrags: Int = 10, look: () -> QAShot) {
        var last = look()
        for _ in 0..<maxDrags {
            let scroll =
                (app.scrollViews.allElementsBoundByIndex + app.collectionViews.allElementsBoundByIndex
                + app.tables.allElementsBoundByIndex)
                .filter { $0.exists && !$0.frame.isEmpty }
                .max { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }
            let start = (scroll ?? app).coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
            let end = (scroll ?? app).coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.15))
            start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .fast, thenHoldForDuration: 0)
            sleep(2)
            let now = look()
            if now.isSameScreen(as: last) { break }
            last = now
        }
        // A list that came to rest with a row in a fade moves to show it whole.
        sleep(1)
    }

    /// Drags the screen's list so `element` comes to rest just under the bars at the top (and
    /// their fades), the room under it shown whole.
    @MainActor
    static func bringUnderTopBars(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<2 {
            let screen = app.frame
            let top =
                (covers(app) ?? []).filter { $0.frame.midY < screen.midY }.map(\.frame.maxY).max()
                ?? screen.minY + screen.height * 0.15
            let distance = element.frame.minY - (top + 8)
            // Close enough: a short second drag left a row above the card in the bar's fade, and
            // the list moved on to the next row, carrying the card up under the iPad's week bar
            // (the full matrix, build 121 round 2).
            guard abs(distance) > 40 else { return }
            let y = min(max(element.frame.minY, top + 40), screen.maxY - 120)
            let start = app.coordinate(withNormalizedOffset: .zero)
                .withOffset(CGVector(dx: screen.width * 0.5, dy: distance > 0 ? y : max(y + distance, top + 40)))
            let end = start.withOffset(CGVector(dx: 0, dy: -distance))
            // Slow, and held at the end, so the list stops where the drag does.
            start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.3)
            sleep(2)
        }
    }

    /// Which covers a check looks at: the ones over the top of the screen (a pinned header, the
    /// tab picker), the ones along the bottom (a bottom bar, the tab bar), or both.
    enum Edges {
        case top, bottom, both

        func includes(_ cover: Cover, screen: CGRect) -> Bool {
            switch self {
            case .both: true
            case .top: cover.frame.midY < screen.midY
            case .bottom: cover.frame.midY >= screen.midY
            }
        }
    }

    /// The text and buttons in `elements` (label and frame) that rest part way under a cover
    /// (`cover(over:in:)`), judged by what of each is on the screen.
    static func halfCovered(
        _ elements: [(label: String, frame: CGRect)], covers: [Cover], screen: CGRect, edges: Edges = .both
    ) -> [(label: String, frame: CGRect, cover: Cover)] {
        let covers = covers.filter { edges.includes($0, screen: screen) }
        return elements.compactMap { element in
            let shown = element.frame.intersection(screen)
            guard !shown.isNull, shown.width > slack, shown.height > slack,
                let over = cover(over: shown, in: covers)
            else { return nil }
            return (element.label, element.frame, over)
        }
    }

    /// Problems on the screen as it rests: any text or button part way under a fade or a pinned
    /// bar (washed out, or cut by the bar's edge) along `edges`.
    @MainActor
    static func checkRest(_ app: XCUIApplication, screen: String, edges: Edges = .both) -> [String] {
        guard let covers = covers(app) else {
            return ["\(screen): the app doesn’t report its fades (launch it with QA_MARKERS=1)"]
        }
        guard let root = try? app.snapshot() else { return [] }
        var elements: [(label: String, frame: CGRect)] = []
        var stack = [root]
        while let element = stack.popLast() {
            stack.append(contentsOf: element.children)
            guard element.elementType == .staticText || element.elementType == .button,
                !element.label.trimmingCharacters(in: .whitespaces).isEmpty
            else { continue }
            elements.append((element.label, element.frame))
        }
        let found = halfCovered(elements, covers: covers, screen: root.frame, edges: edges)
        log(screen: screen, covers: covers, what: found.map { ($0.label, $0.frame) })
        return found.map {
            "\(screen): “\($0.label)” rests part way under a \($0.cover.isFade ? "fade" : "bar"), at "
                + "\(describe($0.frame)) (it covers \(describe($0.cover.frame)))"
        }
    }

    /// The covers and the rows looked at, in the test's log, for a look at why a check failed.
    static func log(screen: String, covers: [Cover], what: [(String, CGRect)]) {
        let listed = covers.map { ($0.isFade ? "fade " : "bar ") + describe($0.frame) }
        print("QA-FADES: \(screen): covers \(listed.joined(separator: " | "))")
        for (label, frame) in what { print("QA-FADES: \(screen): “\(label)” at \(describe(frame))") }
    }

    static func describe(_ rect: CGRect) -> String {
        "y \(Int(rect.minY.rounded()))–\(Int(rect.maxY.rounded())), x \(Int(rect.minX.rounded()))–\(Int(rect.maxX.rounded()))"
    }
}
