import UIKit
import XCTest

/// The duplicate-screen rule the screenshot tests use (QAShotCompare in QAHelpers.swift), on drawn
/// images: no app launch. scripts/test_qa_shot_compare.py checks the publisher's copy of the rule.
final class QAShotCompareTests: XCTestCase {
    /// iPhone 17 Pro Max, in points, drawn at 3 pixels per point.
    private let size = CGSize(width: 440, height: 956)

    private func png(_ size: CGSize? = nil, scale: CGFloat = 3, _ draw: (CGContext) -> Void) -> Data {
        let format = UIGraphicsImageRendererFormat()
        format.scale = scale
        format.opaque = true
        return UIGraphicsImageRenderer(size: size ?? self.size, format: format).pngData { context in
            UIColor.white.setFill()
            context.fill(CGRect(origin: .zero, size: size ?? self.size))
            draw(context.cgContext)
        }
    }

    private func image(_ data: Data, pad: Bool = false) throws -> QAShotImage {
        try XCTUnwrap(QAShotCompare.image(png: data, scale: 3, pad: pad))
    }

    private func page(_ extra: (CGContext) -> Void = { _ in }) -> Data {
        png { context in
            context.setFillColor(UIColor.orange.cgColor)
            context.fill(CGRect(x: 20, y: 120, width: 400, height: 300))
            extra(context)
        }
    }

    func testOneShotIsOnePixelPerPoint() throws {
        let shot = try image(page())
        XCTAssertEqual(shot.width, 440)
        XCTAssertEqual(shot.height, 956)
        // Top-left first: the orange block starts 120 points down.
        let row = 200 * shot.width * 4 + 100 * 4
        XCTAssertGreaterThan(Int(shot.pixels[row]), 200)
        XCTAssertLessThan(Int(shot.pixels[row + 2]), 100)
        XCTAssertEqual(Int(shot.pixels[10 * shot.width * 4 + 10 * 4 + 2]), 255)
    }

    func testAnotherClockIsTheSameScreen() throws {
        let clock = page { context in
            context.setFillColor(UIColor.black.cgColor)
            context.fill(CGRect(x: 30, y: 15, width: 50, height: 20))
        }
        XCTAssertTrue(QAShotCompare.isSameScreen(try image(page()), try image(clock)))
        XCTAssertEqual(QAShotCompare.changedShare(try image(page()), try image(clock)), 0)
    }

    func testTheHomeIndicatorIsLeftOut() throws {
        let indicator = page { context in
            context.setFillColor(UIColor.black.cgColor)
            context.fill(CGRect(x: 150, y: 946, width: 140, height: 5))
        }
        XCTAssertTrue(QAShotCompare.isSameScreen(try image(page()), try image(indicator)))
    }

    func testAMenuOfThinTextIsADifferentScreen() throws {
        // Ten one-point lines: under 1% of the pixels, but more than 1% of the cells.
        let menu = page { context in
            context.setFillColor(UIColor.black.cgColor)
            for line in 0..<10 { context.fill(CGRect(x: 300, y: 500 + line * 20, width: 120, height: 1)) }
        }
        XCTAssertFalse(QAShotCompare.isSameScreen(try image(page()), try image(menu)))
    }

    func testAFaintTintIsTheSameScreen() throws {
        let tint = page { context in
            context.setFillColor(UIColor(white: 0.95, alpha: 1).cgColor)
            context.fill(CGRect(x: 0, y: 500, width: 440, height: 300))
        }
        XCTAssertTrue(QAShotCompare.isSameScreen(try image(page()), try image(tint)))
    }

    func testTheDuoRailStatusBarIsLeftOut() throws {
        let duo = CGSize(width: 466, height: 678)
        let blank = png(duo) { _ in }
        let clock = png(duo) { context in
            context.setFillColor(UIColor.black.cgColor)
            context.fill(CGRect(x: 395, y: 80, width: 60, height: 20))
        }
        let button = png(duo) { context in
            context.setFillColor(UIColor.black.cgColor)
            context.fill(CGRect(x: 395, y: 200, width: 60, height: 60))
        }
        XCTAssertTrue(QAShotCompare.isSameScreen(try image(blank), try image(clock)))
        XCTAssertFalse(QAShotCompare.isSameScreen(try image(blank), try image(button)))
    }

    func testTheQuickLookNeverDropsANearCopy() throws {
        let near = page { context in
            context.setFillColor(UIColor.black.cgColor)
            context.fill(CGRect(x: 24, y: 504, width: 8, height: 8))
        }
        let a = try image(page())
        let b = try image(near)
        XCTAssertFalse(QAShotCompare.surelyDifferent(a, b, share: QAShotCompare.sameScreen))
        XCTAssertTrue(QAShotCompare.isSameScreen(a, b))
        XCTAssertGreaterThan(QAShotCompare.changedShare(a, b), 0)
        // A whole different page is ruled out by the averages alone.
        let other = png { context in
            context.setFillColor(UIColor.systemGreen.cgColor)
            context.fill(CGRect(x: 0, y: 300, width: 440, height: 500))
        }
        XCTAssertTrue(QAShotCompare.surelyDifferent(a, try image(other), share: QAShotCompare.sameScreen))
    }

    func testDifferentSizesAreDifferentScreens() throws {
        let smaller = png(CGSize(width: 430, height: 932)) { _ in }
        XCTAssertEqual(QAShotCompare.changedShare(try image(page()), try image(smaller)), 1)
    }

    func testMasksByDevice() {
        XCTAssertEqual(QAShotCompare.mask(width: 1210, height: 834, pad: true).top, 24)
        XCTAssertEqual(QAShotCompare.mask(width: 375, height: 667, pad: false).top, 20)
        XCTAssertEqual(QAShotCompare.mask(width: 466, height: 678, pad: false).rail?.x, 382)
        XCTAssertEqual(QAShotCompare.mask(width: 440, height: 956, pad: false).bottom, 34)
    }

    func testNamesBecomeThePublishedFileNames() {
        XCTAssertEqual(QAShotCompare.slug("Menu › search typed, imported (light)"), "menu-search-typed-imported")
        XCTAssertEqual(QAShotCompare.slug("Household › new household"), "household-new-household")
        XCTAssertEqual(QAShotCompare.slug("Start Cooking › Steps"), "start-cooking-steps")
    }

    /// Two searches on an iPad, one result card each, differ in under 1% of cells but are
    /// different screens (Hark's review, build 122 round 4 item 15c): the pair is allowed, both
    /// ways round, and only that pair.
    func testAnAllowedPairIsNotADuplicate() {
        let imported = "Menu › search typed, imported (dark)"
        let byHand = "Menu › search typed, typed by hand (dark)"
        XCTAssertTrue(QAShotCompare.isAllowedPair(imported, byHand))
        XCTAssertTrue(QAShotCompare.isAllowedPair(byHand, imported))
        XCTAssertTrue(QAShotCompare.isAllowedPair("Household", "Household › new household"))
        XCTAssertTrue(QAShotCompare.isAllowedPair("Start Cooking › Steps", "Cooking › imported"))
        XCTAssertFalse(QAShotCompare.isAllowedPair("Start Cooking › Steps scrolled", "Cooking › imported"))
        XCTAssertFalse(QAShotCompare.isAllowedPair(imported, "Menu"))
        XCTAssertFalse(QAShotCompare.isAllowedPair(imported, imported))
    }
}
