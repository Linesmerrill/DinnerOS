import SwiftUI
import Testing

@testable import DinnerOS

/// The week strip on the three screens the household uses: a phone, an iPad upright, and the
/// kitchen iPad on its side, where three-line pills two-fifths of the screen wide made the Menu's
/// chrome take a third of it.
@MainActor
struct WeekStripLayoutTests {
    private let phone: CGFloat = 402
    private let iPadPortrait: CGFloat = 1032
    private let iPadLandscape: CGFloat = 1376
    private let iPad11Landscape: CGFloat = 1194

    @Test func aPhoneKeepsTwoAndAHalfWeeksInView() {
        #expect(!WeekStripMetrics.isWide(phone))
        #expect(WeekStripMetrics.pillWidth(stripWidth: phone, typeSize: .large) == 148)
    }

    @Test func anIPadShowsMoreWeeksNotBiggerOnes() {
        for width in [iPadPortrait, iPadLandscape, iPad11Landscape] {
            #expect(WeekStripMetrics.isWide(width))
            let pill = WeekStripMetrics.pillWidth(stripWidth: width, typeSize: .large)
            #expect(pill == WeekStripMetrics.widePillWidth)
            // At least four weeks across, where there were two and a half.
            #expect((width - 32) / (pill + 8) >= 4)
        }
    }

    /// Two lines on an iPad: shorter than the phone's three at every text size.
    @Test func anIPadStripIsShorter() {
        for size in DynamicTypeSize.allCases {
            #expect(WeekStripMetrics.height(for: size, wide: true) < WeekStripMetrics.height(for: size))
        }
        // The strip, its padding and the divider stay under a tenth of a landscape iPad's height.
        let strip = WeekStripMetrics.height(for: .large, wide: true) + 12
        #expect(strip <= 834 * 0.1)
    }

    /// Larger text still gets room for its dates on a wide screen.
    @Test func largeTextWidensTheIPadPill() {
        let pill = WeekStripMetrics.pillWidth(stripWidth: iPadLandscape, typeSize: .accessibility1)
        #expect(pill > WeekStripMetrics.widePillWidth)
    }
}
