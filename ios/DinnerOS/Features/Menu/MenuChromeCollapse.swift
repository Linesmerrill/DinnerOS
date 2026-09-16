import Foundation

/// Whether the Menu's week strip is showing in full or in its compact form, and the rule that
/// decides which as the screen scrolls.
///
/// The rule lives here, apart from any scroll view, because "does this scroll collapse the
/// strip?" is the whole feature and it is a question about numbers. A test can feed this type a
/// sequence of offsets; a scroll view can only be watched.
///
/// The shape of the rule:
///
/// * **At the top, always expanded.** Load, a pull-to-refresh, a scroll back to the first card:
///   whenever the content is within `topThreshold` of the top, the strip is shown. That is the
///   owner's rule — when in doubt, show it — written as a hard case rather than a tendency.
/// * **Direction, not position, decides the rest.** Travel is measured from an anchor that moves
///   to the turning point every time the scroll reverses, so the question is "how far has this
///   gesture gone", not "how far down the page are we". Someone who stops half way down keeps
///   whatever they had.
/// * **Hysteresis, asymmetric on purpose.** Collapsing needs `collapseTravel` points of downward
///   travel; restoring needs only `expandTravel`, which is smaller. A few points of jitter moves
///   nothing either way, and where the two could disagree the strip comes back.
///
/// Nothing here animates or measures: `MenuScroll.collapseAnimation(reduceMotion:)` decides
/// whether the change is animated, and `WeekStripMetrics` decides how tall each form is.
nonisolated struct MenuChromeCollapse: Equatable {
    /// Content within this many points of the top counts as "at the top", where the strip is
    /// always shown. A few points of slack absorbs the end of a rubber-band scroll.
    static let topThreshold: CGFloat = 8

    /// Downward travel, in points, before the strip collapses. About a thumb-flick — far enough
    /// that a tap which nudges the list, or a hand resting on a moving screen, never reaches it.
    static let collapseTravel: CGFloat = 40

    /// Upward travel, in points, before the strip comes back. Deliberately less than
    /// `collapseTravel`: scrolling up is how someone looks for the week they are on, and the
    /// owner's rule where the two could disagree is to show it.
    static let expandTravel: CGFloat = 24

    /// Whether the strip is currently showing in full.
    private(set) var isExpanded = true

    /// True while the rule is suspended — see `hold()`.
    private(set) var isHeld = false

    /// The offset the current run of travel is measured from: the last turning point, or the
    /// last place the strip changed form.
    private var anchor: CGFloat = 0

    /// The offset of the previous sample, so a reversal can measure from where the scroll
    /// actually turned rather than from the sample that noticed it.
    private var lastOffset: CGFloat = 0

    /// Which way the scroll was last going, or `nil` before it has moved.
    private var isDescending: Bool?

    /// A collapse that has seen no scrolling: expanded, because a screen opens showing its
    /// chrome.
    init() {}

    /// Takes the scroll's new content offset and returns whether the strip should now show in
    /// full.
    ///
    /// `offset` is the distance scrolled down from the top, so it grows as the reader moves away
    /// from the first card and can go slightly negative while rubber-banding.
    @discardableResult
    mutating func update(offset: CGFloat) -> Bool {
        defer { lastOffset = offset }

        guard !isHeld else {
            // Something other than the reader is moving the scroll view; see `hold()`. Follow
            // the offset, so travel measured after the release starts from where it lands.
            anchor = offset
            isDescending = nil
            return isExpanded
        }

        if offset <= Self.topThreshold {
            // The top is not a matter of degree: the strip belongs on screen here.
            anchor = offset
            isDescending = nil
            return setExpanded(true)
        }

        let step = offset - lastOffset
        guard step != 0 else { return isExpanded }
        let descending = step > 0

        if descending != isDescending {
            // The reader changed direction. Measure the new run from the turning point, so
            // reversing costs no travel and a scroll back up answers on its first sample.
            isDescending = descending
            anchor = lastOffset
        }

        let travel = offset - anchor
        if descending {
            guard travel >= Self.collapseTravel else { return isExpanded }
            anchor = offset
            return setExpanded(false)
        }
        guard -travel >= Self.expandTravel else { return isExpanded }
        anchor = offset
        return setExpanded(true)
    }

    /// Suspends the rule and shows the strip, for a scroll the reader did not perform.
    ///
    /// The Menu jumps itself to All Meals when a filter changes (#458). That jump is a large,
    /// animated change of offset, and read as a gesture it would collapse the strip in the same
    /// instant the reader asked to see a different set of meals. While held, offsets only move
    /// the anchor, so the jump decides nothing.
    mutating func hold() {
        isHeld = true
        setExpanded(true)
    }

    /// Re-arms the rule after a `hold()`, measuring from `offset`.
    mutating func release(offset: CGFloat) {
        isHeld = false
        anchor = offset
        lastOffset = offset
        isDescending = nil
    }

    /// Shows the strip and forgets any travel — what a tap on the collapsed bar does.
    mutating func expand(offset: CGFloat) {
        anchor = offset
        lastOffset = offset
        isDescending = nil
        setExpanded(true)
    }

    @discardableResult
    private mutating func setExpanded(_ expanded: Bool) -> Bool {
        isExpanded = expanded
        return isExpanded
    }
}
