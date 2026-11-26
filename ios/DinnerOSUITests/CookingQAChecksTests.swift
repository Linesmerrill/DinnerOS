import XCTest

/// The cooking screen checks' own logic, with no app: what counts as a drawn row, a photo, and an
/// ingredient named by a row.
final class CookingQAChecksTests: XCTestCase {
    /// A 40 × 40 point screen of one gray, with `draw` setting pixels' brightness.
    private func screen(_ fill: UInt8 = 30, draw: (inout [UInt8], Int) -> Void = { _, _ in }) -> QAShotImage {
        let width = 40
        var pixels = [UInt8](repeating: fill, count: width * width * 4)
        draw(&pixels, width)
        return QAShotImage(width: width, height: width, pixels: pixels, cellKept: [], columns: 5, cellMeans: [])
    }

    private func set(_ pixels: inout [UInt8], _ width: Int, x: Int, y: Int, _ value: UInt8) {
        let i = (y * width + x) * 4
        pixels[i] = value
        pixels[i + 1] = value
        pixels[i + 2] = value
    }

    func testABlankRowIsNotDrawnAndARowWithTextIs() {
        let rect = CGRect(x: 0, y: 0, width: 40, height: 20)
        XCTAssertFalse(CookingQAChecks.isDrawn(rect, in: screen()))
        let text = screen { pixels, width in
            for x in stride(from: 4, to: 36, by: 2) { self.set(&pixels, width, x: x, y: 10, 240) }
        }
        XCTAssertTrue(CookingQAChecks.isDrawn(rect, in: text))
    }

    func testAFlatSquareIsNoPhotoAndAVariedOneIs() {
        let rect = CGRect(x: 0, y: 0, width: 40, height: 40)
        XCTAssertFalse(CookingQAChecks.showsPhoto(rect, in: screen()))
        let photo = screen { pixels, width in
            for y in 0..<40 {
                for x in 0..<40 { self.set(&pixels, width, x: x, y: y, UInt8((x * 6 + y * 3) % 256)) }
            }
        }
        XCTAssertTrue(CookingQAChecks.showsPhoto(rect, in: photo))
    }

    func testARowNamesAnIngredientByItsWholeWords() {
        let rows: Set = [
            "1 Lemon zested and quartered", "6 oz Penne Pasta", "Salt", "3 Lemon Wedges from step 1",
            "2 tsp Cooking Oil",
        ]
        XCTAssertEqual(
            CookingQAChecks.missing(
                ["Lemon", "Penne Pasta", "Salt", "Pepper", "Olive Oil", "Cooking Oil"], from: rows),
            ["Pepper", "Olive Oil"])
        // A word inside another word isn't the ingredient: "Peppercorns" isn't Pepper.
        XCTAssertEqual(CookingQAChecks.missing(["Pepper"], from: ["Peppercorns"]), ["Pepper"])
    }

    /// "Step 1" faded out in the band under the bar over ¼ tsp Turmeric on an SE (Hark's review,
    /// build 121 round 2, item 4): a heading there must be drawn. One drawn there, or gone above
    /// the bar, is fine.
    func testAHeadingFadedOutUnderTheBarIsReported() {
        let bar: CGFloat = 4
        let heading = (label: "Step 1", frame: CGRect(x: 0, y: 10, width: 40, height: 12))
        let screenFrame = CGRect(x: 0, y: 0, width: 40, height: 40)
        XCTAssertEqual(
            CookingQAChecks.fadedHeadings([heading], bar: bar, in: screen(), screen: screenFrame), ["Step 1"])
        let drawn = screen { pixels, width in
            for x in stride(from: 4, to: 36, by: 2) { self.set(&pixels, width, x: x, y: 16, 240) }
        }
        XCTAssertEqual(CookingQAChecks.fadedHeadings([heading], bar: bar, in: drawn, screen: screenFrame), [])
        let above = (label: "Step 1", frame: CGRect(x: 0, y: -10, width: 40, height: 12))
        XCTAssertEqual(CookingQAChecks.fadedHeadings([above], bar: bar, in: screen(), screen: screenFrame), [])
    }

    /// A time read from any screen, shown or spoken (Hark's build 121 review, round 3).
    func testTimesAreReadFromShownAndSpokenText() {
        XCTAssertEqual(QATimeChecks.minutes(in: "Chicken Penne al Limone, 20 minutes, quick, in your week"), 20)
        XCTAssertEqual(QATimeChecks.minutes(in: "Serves 2, 35 min"), 35)
        XCTAssertEqual(QATimeChecks.minutes(in: "1 hr 5 min"), 65)
        XCTAssertEqual(QATimeChecks.minutes(in: "1 hour, 5 minutes"), 65)
        XCTAssertEqual(QATimeChecks.minutes(in: "2 hr"), 120)
        XCTAssertNil(QATimeChecks.minutes(in: "Serves 2"))
        let shown: [(screen: String, text: String?)] = [
            ("Menu", "Pasta, 20 minutes"), ("Recipe", "30 minutes"), ("Cooking", nil),
        ]
        XCTAssertEqual(QATimeChecks.problems(shown, expected: 20, recipe: "Pasta").count, 2)
        XCTAssertEqual(QATimeChecks.problems([("Menu", "Pasta, 20 minutes")], expected: 20, recipe: "Pasta"), [])
    }

    func testTheListsHeadingsAreKnown() {
        for label in ["Ingredients", "Have Ready", "Step 1", "Step 12"] {
            XCTAssertTrue(CookingQAChecks.isHeading(label), label)
        }
        for label in ["Steps", "Step 3, In a small bowl", "Turmeric", "Notes"] {
            XCTAssertFalse(CookingQAChecks.isHeading(label), label)
        }
    }

    /// Basil Pesto on the typed recipe (the same review, item 6): a list that never shows it fails.
    func testTheTypedRecipeNeedsBasilPesto() {
        XCTAssertTrue(ScreensQATests.typedIngredients.contains("Basil Pesto"))
        let shown: Set<String> = ["8 oz Bow Tie Pasta", "1 cup Grape Tomatoes"]
        XCTAssertTrue(CookingQAChecks.missing(ScreensQATests.typedIngredients, from: shown).contains("Basil Pesto"))
    }

    /// The recipe page's middle Ingredients shot (Hark's qa/124 review, item 5): on the Duo the
    /// top shot ended at Lemon and the end shot began at Fresh Basil.
    func testTheMiddleShotIsThePlaceShowingTheRowsTheTopAndEndLeaveOut() {
        let top: Set = ["Bow Tie Pasta", "Basil Pesto", "Grape Tomatoes", "Baby Spinach", "Parmesan Cheese", "Lemon"]
        let end: Set = ["Fresh Basil", "Red Pepper Flakes", "Salt", "Black Pepper"]
        let onTheWay: [Set<String>] = [
            ["Parmesan Cheese", "Lemon", "Olive Oil"],
            ["Lemon", "Olive Oil", "Garlic", "Pine Nuts"],
            ["Pine Nuts", "Fresh Basil"],
        ]
        let pick = MenuQAChecks.middlePick(
            top: top, end: end, onTheWay: onTheWay, expected: ScreensQATests.typedIngredients)
        XCTAssertEqual(pick.index, 1)
        XCTAssertEqual(pick.missing, [])
        // A row no place shows whole is named.
        let gap = MenuQAChecks.middlePick(
            top: top, end: end, onTheWay: [["Olive Oil", "Garlic"]], expected: ScreensQATests.typedIngredients)
        XCTAssertEqual(gap.index, 0)
        XCTAssertEqual(gap.missing, ["Pine Nuts"])
        // Nothing left out: no pick, nothing missing.
        let all = MenuQAChecks.middlePick(
            top: top.union(["Olive Oil", "Garlic", "Pine Nuts"]), end: end, onTheWay: [["Lemon"]],
            expected: ScreensQATests.typedIngredients)
        XCTAssertNil(all.index)
        XCTAssertEqual(all.missing, [])
    }

    func testHowFarTheListMovedIsTheMiddleOfItsThumbnailsMoves() {
        let a = MenuQAChecks.ListPlace(shown: [], minY: ["Lemon": 500, "Garlic": 600, "Salt": 700])
        let b = MenuQAChecks.ListPlace(shown: [], minY: ["Lemon": 300, "Garlic": 401, "Pine Nuts": 500])
        XCTAssertEqual(MenuQAChecks.moved(from: a, to: b), 200)
        XCTAssertNil(MenuQAChecks.moved(from: a, to: MenuQAChecks.ListPlace(shown: [], minY: ["Basil": 1])))
    }
}
