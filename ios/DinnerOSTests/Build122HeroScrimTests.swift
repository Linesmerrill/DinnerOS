import CoreGraphics
import Testing

@testable import DinnerOS

/// The wash at the top of a recipe's photo (Hark's review, build 122 round 4): full behind the
/// bar's buttons, then easing out with no step that reads as a band.
struct Build122HeroScrimTests {
    @Test func fullDownToTheBarsEdge() {
        let barBottom: CGFloat = 116
        let stops = RecipeHeroScrim.stops(barBottom: barBottom)
        let edge = barBottom / (barBottom + DS.Size.heroScrimFade)
        #expect(stops.first?.location == 0)
        #expect(stops.first?.opacity == 1)
        for stop in stops where stop.location <= edge {
            #expect(stop.opacity == 1)
        }
    }

    @Test func easesOutToClearWithoutAJump() throws {
        let stops = RecipeHeroScrim.stops(barBottom: 70)
        let last = try #require(stops.last)
        #expect(last.location == 1)
        #expect(abs(last.opacity) < 0.000_1)
        for (earlier, later) in zip(stops, stops.dropFirst()) {
            #expect(later.location >= earlier.location)
            #expect(later.opacity <= earlier.opacity)
            // No step bigger than a smooth curve takes: a jump is what showed as a hard band.
            #expect(earlier.opacity - later.opacity < 0.15)
        }
    }

    @Test func noBarStillEasesOut() {
        let stops = RecipeHeroScrim.stops(barBottom: 0)
        #expect(stops.first?.opacity == 1)
        #expect(stops.last?.location == 1)
    }

    @Test func listBarFadeIsARowTall() {
        // A list row is at least 44 pt; the fade above Open in Walmart and Add to Library covers one.
        #expect(DS.Size.rowEdgeFade >= 44)
    }
}

extension Build122HeroScrimTests {
    @Test func easesFromTheButtonsMiddleAndStillCoversThem() {
        let barBottom: CGFloat = 116
        let easeFrom = barBottom - DS.Space.xxl
        let stops = RecipeHeroScrim.stops(barBottom: barBottom, easeFrom: easeFrom)
        let height = barBottom + DS.Size.heroScrimFade
        // Full down to the buttons' middle.
        for stop in stops where stop.location <= easeFrom / height {
            #expect(stop.opacity == 1)
        }
        // Still nearly full at the bar's edge, where the buttons end.
        let atEdge = stops.last { $0.location <= barBottom / height }
        #expect((atEdge?.opacity ?? 0) > 0.8)
        // An ease start below the bar is held at the bar's edge.
        let late = RecipeHeroScrim.stops(barBottom: barBottom, easeFrom: barBottom + 50)
        let atBar = RecipeHeroScrim.stops(barBottom: barBottom)
        #expect(late.map(\.location) == atBar.map(\.location))
        #expect(late.map(\.opacity) == atBar.map(\.opacity))
    }
}
