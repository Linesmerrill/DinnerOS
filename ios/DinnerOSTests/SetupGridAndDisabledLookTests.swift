import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// Build 122, Hark's review: every cuisine tile fits on Autopilot setup where there's room
/// (item 4), and a disabled button's label is readable while still looking off (item 16).
struct SetupGridAndDisabledLookTests {
    private func layout(count: Int = 9, width: Double, height: Double) -> CuisineTiles.GridLayout {
        CuisineTiles.gridLayout(
            count: count, width: width, height: height, spacing: DS.Space.m, preferredWidth: DS.Size.cuisineTileWidth,
            minWidth: DS.Size.cuisineTileMinWidth, minHeightRatio: DS.Size.cuisineTileMinHeightRatio)
    }

    /// Rows times tile height plus the gaps between them.
    private func height(of layout: CuisineTiles.GridLayout, count: Int = 9) -> Double {
        let rows = Double((count + layout.columns - 1) / layout.columns)
        return rows * layout.tileHeight + (rows - 1) * DS.Space.m
    }

    @Test func squareTilesStayWhenTheyFit() {
        let grid = layout(width: 540, height: 600)
        #expect(grid.fits && grid.columns == 3 && grid.tileHeight == grid.tileWidth)
        #expect(height(of: grid) <= 600)
    }

    /// A large iPhone: two square columns would be five rows; three columns fit all nine.
    @Test func aLargeIPhoneTakesAThirdColumn() {
        let grid = layout(width: 408, height: 560)
        #expect(grid.fits && grid.columns == 3)
        #expect(grid.tileWidth >= DS.Size.cuisineTileMinWidth)
        #expect(height(of: grid) <= 560)
    }

    /// An iPad sheet on its side: three rows of shorter tiles rather than a cut-off third row.
    @Test func aShortSheetShortensTheTiles() {
        let grid = layout(width: 540, height: 400)
        #expect(grid.fits)
        #expect(grid.tileHeight < grid.tileWidth)
        #expect(grid.tileHeight >= grid.tileWidth * DS.Size.cuisineTileMinHeightRatio)
        #expect(height(of: grid) <= 400)
    }

    @Test func whenNothingFitsTheGridScrolls() {
        let grid = layout(width: 300, height: 200)
        #expect(!grid.fits)
        #expect(grid.tileHeight == grid.tileWidth)
        #expect(grid.tileWidth >= DS.Size.cuisineTileMinWidth)
    }

    // MARK: Disabled look

    private func luminance(_ color: UIColor) -> Double {
        var (r, g, b, a): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
        color.getRed(&r, green: &g, blue: &b, alpha: &a)
        func channel(_ c: CGFloat) -> Double {
            let c = Double(c)
            return c <= 0.03928 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
    }

    /// `top` over `bottom`, by its alpha.
    private func composite(_ top: UIColor, over bottom: UIColor) -> UIColor {
        var (tr, tg, tb, ta): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
        var (br, bg, bb, ba): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
        top.getRed(&tr, green: &tg, blue: &tb, alpha: &ta)
        bottom.getRed(&br, green: &bg, blue: &bb, alpha: &ba)
        return UIColor(
            red: tr * ta + br * (1 - ta), green: tg * ta + bg * (1 - ta), blue: tb * ta + bb * (1 - ta), alpha: 1)
    }

    private func contrast(_ a: UIColor, _ b: UIColor) -> Double {
        let (la, lb) = (luminance(a), luminance(b))
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    @Test(arguments: [UIUserInterfaceStyle.light, .dark])
    func disabledTextIsReadableOnTheDisabledFill(_ style: UIUserInterfaceStyle) throws {
        let traits = UITraitCollection(userInterfaceStyle: style)
        let text = try #require(UIColor(named: "DisabledText")).resolvedColor(with: traits)
        let fill = UIColor.secondarySystemFill.resolvedColor(with: traits)
        // On a plain screen and on a sheet (the elevated background in dark mode).
        let backgrounds = [
            UIColor.systemBackground.resolvedColor(with: traits),
            UIColor.systemBackground.resolvedColor(
                with: UITraitCollection(traitsFrom: [traits, UITraitCollection(userInterfaceLevel: .elevated)])),
        ]
        for background in backgrounds {
            let button = composite(fill, over: background)
            #expect(contrast(text, button) >= 4.5)
            // Still gray, a step down from full-strength text, so it reads as off.
            let label = UIColor.label.resolvedColor(with: traits)
            #expect(contrast(text, button) < contrast(composite(label, over: button), button))
        }
    }
}
