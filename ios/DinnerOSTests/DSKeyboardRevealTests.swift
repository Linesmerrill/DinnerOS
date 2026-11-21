import CoreGraphics
import Testing

@testable import DinnerOS

/// The field being typed in comes up whole above the keyboard's Done strip (build 122).
struct DSKeyboardRevealTests {
    @Test func fieldAlreadyClearDoesNotMove() {
        #expect(DSKeyboardReveal.shift(field: 200...240, visible: 100...400, margin: 8) == nil)
    }

    @Test func fieldUnderTheStripScrollsUpAboveIt() {
        // The Amount field on iPhone SE: its bottom at 410, the strip's top at 387.
        #expect(DSKeyboardReveal.shift(field: 360...410, visible: 90...387, margin: 8) == 31)
    }

    @Test func fieldUnderTheBarScrollsDown() {
        #expect(DSKeyboardReveal.shift(field: 80...120, visible: 100...400, margin: 8) == -28)
    }

    @Test func tallFieldKeepsItsTopInView() {
        // Too tall to fit: it moves only until its top reaches the top of the space.
        #expect(DSKeyboardReveal.shift(field: 150...600, visible: 100...400, margin: 8) == 42)
    }

    @Test func fieldTopAlreadyAtTheTopStays() {
        #expect(DSKeyboardReveal.shift(field: 108...600, visible: 100...400, margin: 8) == nil)
    }
}
