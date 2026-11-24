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
                    return problems + ["\(screen): the screenshot is \(image.width) wide, the screen \(app.frame.width)"]
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
    /// clear of the bar, and on any of
    /// `expected` that no drawn row names. Leaves the list scrolled to its end.
    @MainActor
    static func checkEveryIngredient(
        _ app: XCUIApplication, expected: [String], screen: String, column: CGFloat,
        capture: () -> QAShot
    ) -> [String] {
        var problems: [String] = []
        var seen = Set<String>()
        var blank = Set<String>()
        var previous: QAShotImage?
        for _ in 0..<16 {
            let shot = capture()
            guard let image = shot.image else { break }
            if let previous, QAShotCompare.isSameScreen(image, previous) { break }
            previous = image
            let top = barBottom(app) + fadeRoom
            let bottom = app.frame.maxY - fadeRoom - 60
            // One snapshot of the screen, walked here: asking each row for its frame took
            // minutes on a long list.
            var stack = (try? app.snapshot()).map { [$0] } ?? []
            while let element = stack.popLast() {
                stack.append(contentsOf: element.children)
                guard element.identifier == QAMarkers.cookIngredient else { continue }
                let frame = element.frame
                guard frame.minY >= top, frame.maxY <= bottom, !frame.isEmpty else { continue }
                // A row clear of the bar is drawn at rest, every time: on TestFlight 120 the rows just
                // under the bar were blank after each scroll, and came back only further down.
                if isDrawn(frame, in: image) {
                    seen.insert(element.label)
                } else {
                    blank.insert(element.label)
                }
            }
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
        let gone = missing(expected, from: seen)
        if !gone.isEmpty {
            problems.append(
                "\(screen): the list shows \(expected.count - gone.count) of the recipe’s \(expected.count) "
                    + "ingredients; missing \(gone.joined(separator: ", "))")
        }
        return problems
    }
}

/// The app's QA marker names (DinnerOS/Features/Root/QAMarker.swift), for the tests.
enum QAMarkers {
    static let cookPhoto = "cook.photo"
    static let cookIngredient = "cook.ingredient"
}
