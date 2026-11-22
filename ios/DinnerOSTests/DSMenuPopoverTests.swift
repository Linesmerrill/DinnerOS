import CoreGraphics
import SwiftUI
import Testing

@testable import DinnerOS

/// The More menu's card opens under its button and stays inside the screen's safe area, the
/// Duo's cover-screen button rail included (build 122, review item 8).
@MainActor
struct DSMenuPopoverTests {
    @Test func theCardLinesUpWithTheButtonsTrailingEdge() {
        // iPhone 17 Pro Max: the button near the top trailing corner.
        let button = CGRect(x: 370, y: 62, width: 36, height: 36)
        #expect(DSMenuPlacement.trailingEdge(anchor: button, containerWidth: 440, trailingInset: 0) == 406)
        #expect(DSMenuPlacement.top(anchor: button, topInset: 0) == 98 + DS.Space.s)
    }

    @Test func theCardNeverCrossesTheDuoButtonRail() {
        // The cover screen's frame is 382 wide, and the More button sits in the 84-point rail
        // beyond it (the frame still reports the rail as a trailing inset). The card stops at
        // the content's edge.
        let button = CGRect(x: 405, y: 245, width: 26, height: 26)
        let edge = DSMenuPlacement.trailingEdge(anchor: button, containerWidth: 382, trailingInset: 84)
        #expect(edge == 382 - DS.Space.s)
        // Nothing under a rail button for the card to hang from: it starts at the content's top.
        #expect(DSMenuPlacement.top(anchor: button, topInset: 0, containerWidth: 382) == DS.Space.s)
    }

    @Test func beforeTheButtonIsMeasuredTheCardSitsAtTheTopTrailingMargin() {
        #expect(DSMenuPlacement.trailingEdge(anchor: .zero, containerWidth: 402, trailingInset: 0) == 402 - DS.Space.s)
        #expect(DSMenuPlacement.top(anchor: .zero, topInset: 59) == 59 + DS.Space.s)
    }

    @Test func theCardsWidthLeavesTheLeadingMargin() {
        #expect(DSMenuPlacement.maxWidth(trailingEdge: 300, leadingInset: 0) == 300 - DS.Space.s)
        #expect(DSMenuPlacement.maxWidth(trailingEdge: 300, leadingInset: 84) == 300 - 84 - DS.Space.s)
        #expect(DSMenuPlacement.maxWidth(trailingEdge: 4, leadingInset: 0) == 0)
    }

    @Test func onlyTheDestructiveItemIsRed() {
        let delete = DSMenuItem(title: "Delete Recipe", systemImage: "trash", role: .destructive) {}
        let edit = DSMenuItem(title: "Edit Recipe", systemImage: "pencil") {}
        #expect(delete.isDestructive)
        #expect(!edit.isDestructive)
        #expect(edit.id == "Edit Recipe")
    }

    @Test func theCardStaysBelowTheNavigationBar() {
        // The button in the bar ends 31 points above the content's top: the card starts at it.
        let button = CGRect(x: 370, y: -67, width: 36, height: 36)
        #expect(DSMenuPlacement.top(anchor: button, topInset: 0) == DS.Space.s)
    }

    @Test func aLongMenuScrollsInsideTheSafeArea() {
        // The Duo's cover screen: a card that starts 120 points down on a 678-point screen.
        #expect(
            DSMenuPlacement.maxHeight(top: 120, containerHeight: 678, bottomInset: 20) == 678 - 20 - 120 - DS.Space.s)
        // Never shorter than one row.
        #expect(DSMenuPlacement.maxHeight(top: 670, containerHeight: 678, bottomInset: 20) == DS.Size.tapTarget)
    }

    @Test func aRowCanBeDisabledOrStartAGroup() {
        let plan = DSMenuItem(title: "Plan with Autopilot", systemImage: "sparkles", isDisabled: true) {}
        var past = DSMenuItem(title: "Past Weeks", systemImage: "clock") {}
        #expect(plan.isDisabled)
        #expect(!past.isDisabled)
        #expect(past.group == nil)
        past.group = ""
        #expect(past.group == "")
    }
}

/// The solid band under the tab bar goes where the bar is (Hark's review item 10, build 122).
struct TabBarPlacementTests {
    @Test func theBarIsAlongTheTopOnAnIPadWithRoom() {
        #expect(TabBarPlacement.edge(isPad: true, horizontalSizeClass: .regular) == .top)
    }

    @Test func theBarIsAlongTheBottomOnAnIPhoneOrANarrowIPad() {
        #expect(TabBarPlacement.edge(isPad: false, horizontalSizeClass: .compact) == .bottom)
        #expect(TabBarPlacement.edge(isPad: false, horizontalSizeClass: .regular) == .bottom)
        #expect(TabBarPlacement.edge(isPad: true, horizontalSizeClass: .compact) == .bottom)
    }

    /// One band per size class, at the bar's edge; the page itself never moves between branches,
    /// so a size class flip (an iPad going to the background) keeps the tab where it was.
    @Test func theBandShowsOnlyAtTheBarsEdge() {
        #expect(TabBarBacking.shows(edge: .top, isPad: true, horizontalSizeClass: .regular))
        #expect(!TabBarBacking.shows(edge: .bottom, isPad: true, horizontalSizeClass: .regular))
        #expect(TabBarBacking.shows(edge: .bottom, isPad: true, horizontalSizeClass: .compact))
        #expect(!TabBarBacking.shows(edge: .top, isPad: true, horizontalSizeClass: .compact))
        #expect(TabBarBacking.shows(edge: .bottom, isPad: false, horizontalSizeClass: .compact))
    }

    @Test func theScrimIsSolidWhereTheBarBegins() {
        #expect(TabBarScrimLayout.opacity == 1)
        #expect(TabBarScrimLayout.barStops.allSatisfy { $0.opacity == 1 })
    }
}
