import CoreGraphics
import Testing

@testable import DinnerOS

/// A ringing timer is stopped from across the counter, often at a landscape iPad: its buttons
/// must be big, with Stop the biggest.
@MainActor
struct CookTimerRingingTests {
    @Test func stopIsTheBigTarget() {
        #expect(CookTimerStyle.ringingButtonHeight >= 56)
        #expect(CookTimerStyle.ringingAddWidth >= 88)
        #expect(CookTimerStyle.ringingStopWidth >= CookTimerStyle.ringingAddWidth * 1.8)
    }

    /// Both buttons, their gap and the chip's padding fit the narrowest phone in the timer row.
    @Test func theButtonsFitAPhone() {
        let chipPadding: CGFloat = 8 + 8 + 16
        let row = CookTimerStyle.ringingAddWidth + 8 + CookTimerStyle.ringingStopWidth + chipPadding
        #expect(row <= 375 - 32)
    }
}
