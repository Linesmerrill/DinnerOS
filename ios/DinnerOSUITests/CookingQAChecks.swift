import XCTest

/// Checks on the cooking screen that a green test missed on TestFlight 120 (Merrill, Nov 23, iPad
/// in landscape): the recipe's photo and the rows just under the top bar were drawn fully
/// transparent, so the photo was a blank band and rows went missing as the list scrolled, while
/// every element was still there for XCTest. So these look at the pixels too.
enum CookingQAChecks {
    /// How far a row's darkest and lightest pixels must differ for it to count as drawn: text
    /// on the page differs by 150 or more, a blank row by nothing.
    static let drawnContrast = 80
    /// How much a photo's brightness must vary: a photo varies by 30 or more, a blank square
    /// or a flat fill by under 5.
    static let photoSpread = 15.0
    /// Room under the top bar where rows may still be fading as a list comes to rest.
    static let fadeRoom: CGFloat = 40
    /// The most space allowed between the photo and the Ingredients heading under it.
    static let maxGapUnderPhoto: CGFloat = 64

    /// The brightness (0 to 255) of each sampled point in `rect` (points, one pixel each).
    static func brightness(in rect: CGRect, of image: QAShotImage) -> [Int] {
        let box = rect.integral.intersection(CGRect(x: 0, y: 0, width: image.width, height: image.height))
        guard !box.isNull, box.width >= 1, box.height >= 1 else { return [] }
        var values: [Int] = []
        for y in stride(from: Int(box.minY), to: Int(box.maxY), by: 2) {
            for x in stride(from: Int(box.minX), to: Int(box.maxX), by: 2) {
                let i = (y * image.width + x) * 4
                let r = Int(image.pixels[i]), g = Int(image.pixels[i + 1]), b = Int(image.pixels[i + 2])
                values.append((r * 299 + g * 587 + b * 114) / 1000)
            }
        }
        return values
    }

    /// Whether anything is drawn in `rect`: text or a check circle against the page.
    static func isDrawn(_ rect: CGRect, in image: QAShotImage) -> Bool {
        let values = brightness(in: rect, of: image)
        guard let low = values.min(), let high = values.max() else { return false }
        return high - low >= drawnContrast
    }

    /// Whether `rect` shows a photo: its brightness varies, as a picture's does.
    static func showsPhoto(_ rect: CGRect, in image: QAShotImage) -> Bool {
        let values = brightness(in: rect.insetBy(dx: 4, dy: 4), of: image).map(Double.init)
        guard values.count > 20 else { return false }
        let mean = values.reduce(0, +) / Double(values.count)
        let spread = (values.map { ($0 - mean) * ($0 - mean) }.reduce(0, +) / Double(values.count)).squareRoot()
        return spread >= photoSpread
    }

    /// The recipe's ingredients no row names: a row names an ingredient when its words include
    /// the ingredient's ("1 Lemon zested and quartered" names Lemon, "Lemon Wedges" too).
    static func missing(_ expected: [String], from rows: Set<String>) -> [String] {
        let rowWords = rows.map { words($0) }
        return expected.filter { name in
            let wanted = words(name)
            return !rowWords.contains { row in
                row.indices.contains { start in
                    start + wanted.count <= row.count && Array(row[start..<start + wanted.count]) == wanted
                }
            }
        }
    }

    static func words(_ text: String) -> [String] {
        text.lowercased().split { !$0.isLetter }.map(String.init)
    }

    /// The cooking screen's top bar and, on a phone, its tab picker: rows scroll under them.
    @MainActor
    static func barBottom(_ app: XCUIApplication) -> CGFloat {
        // The help button is in the top bar, except on iPhone Duo's cover screen, where it's in
        // the rail at the side, halfway down: it says nothing about the top there.
        let help = app.buttons["How the Cooking Screen Works"].firstMatch
        let inTopBar = help.exists && help.frame.minY < app.frame.minY + app.frame.height * 0.15
        var bottom = inTopBar ? help.frame.maxY : app.frame.minY + 60
        let picker = app.segmentedControls.firstMatch
        if picker.exists, picker.frame.minY < app.frame.midY { bottom = max(bottom, picker.frame.maxY) }
        return bottom
    }

    /// The photo at the top of the cooking screen is drawn, with the Ingredients heading right
    /// under it: no blank band (TestFlight 120 drew it invisible). Waits up to `timeout` for it
    /// to load.
    @MainActor
    static func checkPhoto(
        _ app: XCUIApplication, recipe: String, screen: String, timeout: TimeInterval = 15,
        capture: () -> QAShot
    ) -> [String] {
        let marker = app.descendants(matching: .any)[QAMarkers.cookPhoto].firstMatch
        guard marker.waitForExistence(timeout: 10) else {
            return ["\(screen): \(recipe) has a photo, but the cooking screen has no photo"]
        }
        var problems: [String] = []
        let photo = marker.frame
        let heading = app.staticTexts.matching(NSPredicate(format: "label == 'Ingredients'")).allElementsBoundByIndex
            .map(\.frame).filter { $0.minY >= photo.maxY - 1 }.min { $0.minY < $1.minY }
        if let heading, heading.minY - photo.maxY > maxGapUnderPhoto {
            problems.append(
                "\(screen): \(Int(heading.minY - photo.maxY)) points of blank space between the photo and Ingredients")
        }
        let deadline = Date().addingTimeInterval(timeout)
        var shown = false
        repeat {
            if let image = capture().image {
                guard image.width == Int(app.frame.width.rounded()) else {
                    return problems + [
                        "\(screen): the screenshot is \(image.width) wide, the screen \(app.frame.width)"
                    ]
                }
                shown = showsPhoto(photo, in: image)
            }
            if !shown { sleep(1) }
        } while !shown && Date() < deadline
        if !shown {
            problems.append("\(screen): the photo’s place at \(photo.integral) is blank: the photo isn’t showing")
        }
        let placeholder = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == %@ AND label == %@", MenuQAChecks.photoPlaceholder, recipe))
        if placeholder.count > 0 {
            problems.append("\(screen): \(recipe)’s photo shows the no-photo picture")
        }
        return problems
    }

    /// Scrolls the cooking screen's ingredient list from top to bottom (dragging at `column`
    /// across the screen) and fails on any row that's on the list but not drawn where it rests,
    /// clear of the bar, on any of `expected` that no drawn row names, on a step's heading left
    /// faded out under the bar above its rows, and on a last row that isn't whole at the end.
    /// Leaves the list at its true end, its last row whole on screen. `middle` gets the screen
    /// halfway down the list: the screen on the way closest to it, or, when none was near (a short
    /// list), the list brought back there before it goes on to its end again.
    @MainActor
    static func checkEveryIngredient(
        _ app: XCUIApplication, expected: [String], screen: String, column: CGFloat,
        middle: ((QAShot) -> Void)? = nil, fadedHeading: ((QAShot) -> Void)? = nil, capture: () -> QAShot
    ) -> [String] {
        var problems: [String] = []
        var seen = Set<String>()
        var blank = Set<String>()
        var faded = Set<String>()
        var previous: QAShotImage?
        // Where the list's last row starts out, and where it is on each screen: how far down the
        // list a screen is.
        var startLast: CGFloat?
        var endLast: CGFloat?
        var onTheWay: [(last: CGFloat, shot: QAShot)] = []
        var lastWhole = false
        // Asked once: each ask is a query of the app, and a long list has hundreds of elements.
        let screenFrame = app.frame
        for _ in 0..<24 {
            let shot = capture()
            guard let image = shot.image else { break }
            let bar = barBottom(app)
            let top = bar + fadeRoom
            let bottom = screenFrame.maxY - fadeRoom - 60
            // One snapshot of the screen, walked here: asking each row for its frame took
            // minutes on a long list.
            var rows: [(label: String, frame: CGRect)] = []
            var headings: [(label: String, frame: CGRect)] = []
            var stack = (try? app.snapshot()).map { [$0] } ?? []
            while let element = stack.popLast() {
                stack.append(contentsOf: element.children)
                let frame = element.frame
                guard !frame.isEmpty, inColumn(frame, width: screenFrame.width, column: column) else { continue }
                if element.identifier == QAMarkers.cookIngredient {
                    rows.append((element.label, frame))
                } else if element.elementType == .staticText, isHeading(element.label) {
                    headings.append((element.label, frame))
                }
            }
            for row in rows where row.frame.minY >= top && row.frame.maxY <= bottom {
                // A row clear of the bar is drawn at rest, every time: on TestFlight 120 the rows
                // just under the bar were blank after each scroll, and came back only further down.
                if isDrawn(row.frame, in: image) {
                    seen.insert(row.label)
                } else {
                    blank.insert(row.label)
                }
            }
            // The rows at the end of the list, once it's there, rest lower: drawn there, they're seen.
            for row in rows
            where row.frame.minY >= top && row.frame.maxY > bottom && row.frame.maxY <= screenFrame.maxY - endRoom
                && isDrawn(row.frame, in: image)
            {
                seen.insert(row.label)
            }
            // A heading in the band under the bar is drawn: a heading faded out there left a blank
            // band over its rows with no step named (Hark's review, build 121 round 2, item 4).
            let fadedHere = fadedHeadings(headings, bar: bar, in: image, screen: screenFrame)
            if !fadedHere.isEmpty {
                faded.formUnion(fadedHere)
                fadedHeading?(shot)
                let near = (headings + rows).filter { $0.frame.minY < top + 60 && $0.frame.maxY > bar - 60 }
                    .sorted { $0.frame.minY < $1.frame.minY }
                    .map { "\($0.label.prefix(24)) \(Int($0.frame.minY))-\(Int($0.frame.maxY))" }
                print("QA-NOTE: \(screen): \(fadedHere) faded under the bar at \(Int(bar)): \(near)")
            }
            let last = rows.map(\.frame).max { $0.maxY < $1.maxY }
            if let last {
                if startLast == nil { startLast = last.maxY }
                lastWhole = last.maxY <= screenFrame.maxY - endRoom && isDrawn(last, in: image)
                endLast = last.maxY
                if middle != nil { onTheWay.append((last.maxY, shot)) }
            }
            let stopped = previous.map { QAShotCompare.isSameScreen(image, $0) } ?? false
            previous = image
            // At the end only when the list has stopped moving with its last row whole: a list
            // that looked the same after one drag once left Lemon Wedges cut off at the bottom
            // (Hark's review, build 121 round 2, item 5).
            if stopped && lastWhole { break }
            // A third of the screen at a time, held at the end so the list doesn't coast on: each
            // row then rests clear of the bar on some screen, even on the Duo's short one.
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.75))
            let to = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.42))
            from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.3)
            sleep(1)
        }
        for label in blank.sorted() {
            problems.append("\(screen): the row “\(label)” is on the list but isn’t drawn")
        }
        for label in faded.sorted() {
            problems.append(
                "\(screen): the heading “\(label)” rests faded out under the bar, a blank band over its rows")
        }
        if !lastWhole {
            problems.append("\(screen): at the end of the list its last row isn’t whole on the screen")
        }
        let gone = missing(expected, from: seen)
        if !gone.isEmpty {
            problems.append(
                "\(screen): the list shows \(expected.count - gone.count) of the recipe’s \(expected.count) "
                    + "ingredients; missing \(gone.joined(separator: ", "))")
        }
        if let middle, let startLast, let endLast {
            // Halfway from where the last row started to where it ends.
            let target = (startLast + endLast) / 2
            let near = max((startLast - endLast) / 4, 24)
            if let best = onTheWay.min(by: { abs($0.last - target) < abs($1.last - target) }),
                abs(best.last - target) <= near
            {
                middle(best.shot)
            } else {
                // Back down to halfway, slowly and held so it doesn't coast, then on to the end again.
                let distance = min(target - endLast, screenFrame.height * 0.45)
                let from = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.4))
                let to = from.withOffset(CGVector(dx: 0, dy: distance))
                from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.3)
                sleep(1)
                middle(capture())
                for _ in 0..<4 {
                    let start = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.75))
                    let end = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.3))
                    start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.3)
                    sleep(1)
                }
            }
        }
        return problems
    }

    /// The headings resting in the band under the bar (where the list's first row rests) but not
    /// drawn: a heading faded out there left a blank band over its rows with no step named.
    static func fadedHeadings(
        _ headings: [(label: String, frame: CGRect)], bar: CGFloat, in image: QAShotImage, screen: CGRect
    ) -> [String] {
        headings.filter { heading in
            heading.frame.maxY > bar + 4 && heading.frame.minY < bar + fadeRoom
                && !isDrawn(heading.frame.intersection(screen), in: image)
        }.map(\.label)
    }

    /// The same check on the screen as it is, for a list scrolled some other way.
    @MainActor
    static func checkHeadingsShow(_ app: XCUIApplication, screen: String, column: CGFloat, image: QAShotImage?)
        -> [String]
    {
        guard let image else { return [] }
        let screenFrame = app.frame
        var headings: [(label: String, frame: CGRect)] = []
        var stack = (try? app.snapshot()).map { [$0] } ?? []
        while let element = stack.popLast() {
            stack.append(contentsOf: element.children)
            if element.elementType == .staticText, isHeading(element.label),
                inColumn(element.frame, width: screenFrame.width, column: column)
            {
                headings.append((element.label, element.frame))
            }
        }
        return fadedHeadings(headings, bar: barBottom(app), in: image, screen: screenFrame).map {
            "\(screen): the heading “\($0)” rests faded out under the bar, a blank band over its rows"
        }
    }

    /// Whether `frame` is in the part of the screen a drag at `column` scrolls: the left pane on an
    /// iPad (column under half), the whole width on a phone.
    static func inColumn(_ frame: CGRect, width: CGFloat, column: CGFloat) -> Bool {
        column >= 0.5 || frame.maxX <= width * 0.5
    }

    /// Room under the list's last row at its end for it to count as whole on the screen. The list
    /// keeps its own room above the home indicator; a list that comes back at its end to show a
    /// heading whole can leave less than its full end room (an SE's typed list: 23 points).
    static let endRoom: CGFloat = 8

    /// A heading on the cooking screen's ingredient list: Ingredients, Have Ready or a step's.
    static func isHeading(_ label: String) -> Bool {
        label == "Ingredients" || label == "Have Ready"
            || label.range(of: #"^Step \d+$"#, options: .regularExpression) != nil
    }
}

/// The app's QA marker names (DinnerOS/Features/Root/QAMarker.swift), for the tests.
enum QAMarkers {
    static let cookPhoto = "cook.photo"
    static let cookIngredient = "cook.ingredient"
}
