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
}
