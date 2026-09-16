import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// When the Menu's week strip collapses and when it comes back.
///
/// The rule is a pure type precisely so this can be written as arithmetic: each test is a
/// sequence of content offsets, the way a scroll view would report them, and an assertion about
/// the form the strip should be in afterwards.
struct MenuChromeCollapseTests {
    /// Feeds `offsets` to a fresh collapse, starting from the top, and answers whether the strip
    /// is expanded at the end.
    private func isExpanded(after offsets: [CGFloat]) -> Bool {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        for offset in offsets {
            collapse.update(offset: offset)
        }
        return collapse.isExpanded
    }

    // MARK: - Showing it

    /// On load, before any scrolling, the strip is showing: the screen opens with its chrome.
    @Test func aFreshMenuShowsTheStrip() {
        #expect(MenuChromeCollapse().isExpanded)
    }

    /// The owner's hard case: at the top of the list the strip is always shown, whatever the
    /// gesture that arrived there was doing.
    @Test func theTopOfTheListAlwaysShowsTheStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        #expect(!collapse.isExpanded)

        // Straight back to the top, in one jump rather than a scroll.
        collapse.update(offset: 0)
        #expect(collapse.isExpanded)
    }

    /// Rubber-banding past the top is still the top.
    @Test func bouncingPastTheTopShowsTheStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        collapse.update(offset: -20)
        #expect(collapse.isExpanded)
    }

    // MARK: - Hiding it

    /// Enough downward travel collapses it.
    @Test func scrollingDownCollapsesTheStrip() {
        #expect(!isExpanded(after: [20, 60]))
    }

    /// Less than `collapseTravel` does not. This is the tap that nudges the list.
    @Test func aSmallNudgeDownLeavesTheStripShowing() {
        #expect(isExpanded(after: [MenuChromeCollapse.collapseTravel - 1]))
    }

    /// Stopping keeps whatever you had: no offsets arrive, so nothing changes.
    @Test func stoppingChangesNothing() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 300)
        #expect(!collapse.isExpanded)

        // The same offset again — a scroll view at rest still reports.
        collapse.update(offset: 300)
        #expect(!collapse.isExpanded)
    }

    // MARK: - Bringing it back

    /// Scrolling back up restores it, and on the first sample of the new direction: travel is
    /// measured from where the scroll turned, not from the sample that noticed the turn.
    @Test func scrollingBackUpRestoresTheStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        #expect(!collapse.isExpanded)

        collapse.update(offset: 400 - MenuChromeCollapse.expandTravel)
        #expect(collapse.isExpanded)
    }

    /// Coming back is easier than going away: the strip returns after less travel than it took
    /// to hide it, which is the owner's "if in doubt, show it" as a number.
    @Test func theStripReturnsMoreEasilyThanItHides() {
        #expect(MenuChromeCollapse.expandTravel < MenuChromeCollapse.collapseTravel)
    }

    // MARK: - Not fighting the user

    /// The flicker case. A hand resting on a moving screen sends a stream of small reversals;
    /// none of them should reach either threshold, so the strip never toggles.
    @Test func jitterNeverTogglesTheStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        #expect(!collapse.isExpanded)

        for step in 0..<40 {
            collapse.update(offset: 400 + (step.isMultiple(of: 2) ? -5 : 5))
            #expect(!collapse.isExpanded, "jitter collapsed or restored the strip at step \(step)")
        }
    }

    /// The same while it is showing: small reversals part way down do not hide it.
    @Test func jitterDoesNotHideAShowingStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 20)

        for step in 0..<40 {
            collapse.update(offset: 20 + (step.isMultiple(of: 2) ? -6 : 6))
            #expect(collapse.isExpanded, "jitter hid the strip at step \(step)")
        }
    }

    /// A reversal restarts the measurement, so travel down and travel back up do not add up
    /// into a collapse that neither gesture earned.
    @Test func aReversalRestartsTheTravel() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 30)  // Not far enough to collapse.
        collapse.update(offset: 20)  // Turned around.
        collapse.update(offset: 50)  // 30 points from the turn — still not far enough.
        #expect(collapse.isExpanded)
    }

    // MARK: - The jump to All Meals

    /// A filter change jumps the menu to All Meals. That is not the reader scrolling, so it must
    /// decide nothing — and it shows the strip, because a fresh set of meals is a moment to know
    /// which week you are looking at.
    @Test func theJumpToAllMealsDoesNotCollapseTheStrip() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        #expect(!collapse.isExpanded)

        collapse.hold()
        #expect(collapse.isExpanded)

        // The offsets the animated jump produces, which would be a long downward gesture.
        for offset in stride(from: 400.0, through: 1200.0, by: 100.0) {
            collapse.update(offset: offset)
        }
        #expect(collapse.isExpanded, "the jump was read as a gesture")
    }

    /// After the jump settles, scrolling decides again — measured from where the jump landed,
    /// so the distance it covered is not charged to the reader's next gesture.
    @Test func scrollingDecidesAgainAfterTheJumpSettles() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.hold()
        collapse.update(offset: 1200)
        collapse.release(offset: 1200)
        #expect(collapse.isExpanded)

        collapse.update(offset: 1200 + MenuChromeCollapse.collapseTravel)
        #expect(!collapse.isExpanded)
    }

    // MARK: - Tapping the collapsed bar

    /// Tapping the one-line bar shows the pills, and does not leave travel behind that would
    /// hide them again on the next small scroll.
    @Test func tappingTheCollapsedBarShowsThePills() {
        var collapse = MenuChromeCollapse()
        collapse.update(offset: 0)
        collapse.update(offset: 400)
        #expect(!collapse.isExpanded)

        collapse.expand(offset: 400)
        #expect(collapse.isExpanded)

        collapse.update(offset: 400 + MenuChromeCollapse.collapseTravel - 1)
        #expect(collapse.isExpanded)
    }

    // MARK: - Reduce Motion

    /// The strip still changes form under Reduce Motion — the space is the point — but the
    /// change is not animated, the same answer the jump to All Meals gives (#458).
    @Test func theCollapseIsNotAnimatedUnderReduceMotion() {
        #expect(MenuScroll.collapseAnimation(reduceMotion: true) == nil)
        #expect(MenuScroll.collapseAnimation(reduceMotion: false) != nil)
    }

    // MARK: - Dynamic Type

    /// The collapsed bar's height is measured from the line it draws, like the pills' height
    /// (#455), so it grows with the text and never reserves less than the text needs.
    @Test func theCollapsedHeightGrowsWithTheTextAndIsAlwaysShorter() {
        var previous: CGFloat = 0
        for size in [DynamicTypeSize.xSmall, .large, .xxxLarge, .accessibility1, .accessibility5] {
            let collapsed = WeekStripMetrics.collapsedHeight(for: size)
            let full = WeekStripMetrics.height(for: size)

            #expect(collapsed > 0)
            #expect(collapsed < full, "the collapsed strip is not shorter at \(size)")
            #expect(collapsed >= previous, "the collapsed height shrank as the text grew at \(size)")
            previous = collapsed
        }
    }

    /// The clamp the pills use applies to the collapsed bar too: past `largestTypeSize` the
    /// reserved height stops growing, so the strip can never take the screen.
    @Test func theCollapsedHeightStopsGrowingWithThePills() {
        let clamped = WeekStripMetrics.collapsedHeight(for: WeekStripMetrics.largestTypeSize)
        #expect(WeekStripMetrics.collapsedHeight(for: .accessibility5) == clamped)
    }

    /// Even at the largest size it renders, the collapsed bar is a small fraction of a phone.
    @Test func theCollapsedBarIsSmallEvenAtAccessibilitySizes() {
        #expect(WeekStripMetrics.collapsedHeight(for: .accessibility5) < 80)
    }
}
