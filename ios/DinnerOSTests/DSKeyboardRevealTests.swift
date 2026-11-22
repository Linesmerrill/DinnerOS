import CoreGraphics
import Testing

@testable import DinnerOS

/// The card being typed in comes up whole above the keyboard's Done bar, and nothing is left half
/// under the top bar (build 122). Screen points: the bar's edge at 100, the Done bar's fade at 400.
struct DSKeyboardRevealTests {
    private let visible: ClosedRange<CGFloat> = 100...400
    private let free: ClosedRange<CGFloat> = -1000...1000

    private func shift(
        card: ClosedRange<CGFloat>, pieces: [ClosedRange<CGFloat>], allowed: ClosedRange<CGFloat>? = nil
    ) -> CGFloat? {
        DSKeyboardReveal.shift(
            card: card, pieces: pieces, visible: visible, topFade: 16, margin: 8, allowed: allowed ?? free)
    }

    @Test func cardAlreadyClearDoesNotMove() {
        #expect(shift(card: 200...300, pieces: [120...150, 200...300]) == nil)
    }

    @Test func cardUnderTheDoneBarComesUpWhole() {
        // The Amount card (field and Unit row) ends at 450; the next stop that leaves no header
        // half under the bar lines the Status header up with the bar's edge.
        let pieces: [ClosedRange<CGFloat>] = [110...140, 140...190, 190...230, 230...280, 280...330, 330...450]
        #expect(shift(card: 330...450, pieces: pieces) == 90)
    }

    @Test func movesPastAHalfHiddenPiece() {
        // Moving the least (58) would leave the 120...200 card half under the bar: hide it.
        let pieces: [ClosedRange<CGFloat>] = [120...200, 200...240, 240...300, 300...450]
        #expect(shift(card: 300...450, pieces: pieces) == 100)
    }

    @Test func fixesAPieceHalfUnderTheBarWithoutNeedingRoom() {
        // The form was scrolled so a card sits half under the bar: bring it back down whole.
        #expect(shift(card: 250...300, pieces: [80...130, 130...170, 170...250, 250...300]) == -20)
    }

    @Test func cardTooTallKeepsItsTopInView() {
        #expect(shift(card: 150...600, pieces: [150...600]) == 34)
    }

    @Test func staysInsideTheScrollableRange() {
        #expect(shift(card: 330...450, pieces: [330...450], allowed: -10...20) == 20)
    }
}
