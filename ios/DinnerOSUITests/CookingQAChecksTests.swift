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
            CookingQAChecks.missing(["Lemon", "Penne Pasta", "Salt", "Pepper", "Olive Oil", "Cooking Oil"], from: rows),
            ["Pepper", "Olive Oil"])
        // A word inside another word isn't the ingredient: "Peppercorns" isn't Pepper.
        XCTAssertEqual(CookingQAChecks.missing(["Pepper"], from: ["Peppercorns"]), ["Pepper"])
    }
}
