import SwiftUI
import Testing

@testable import DinnerOS

/// Sheets as tall as their content (build 122 round 4, Hark's item 16: empty space under the
/// paywall and Notifications).
struct DSSheetFitTests {
    @Test func theSheetIsItsContentAndItsBars() {
        // The paywall on an iPhone 17 Pro Max: 514 points of offer under a 160-point bar and
        // home indicator.
        #expect(DSSheetFit.height(content: 514, chrome: 160) == 674)
        // Rounded up, so the last line is never cut by a fraction of a point.
        #expect(DSSheetFit.height(content: 513.81, chrome: 108) == 622)
    }

    @Test func nothingMeasuredKeepsTheUsualSize() {
        #expect(DSSheetFit.height(content: nil, chrome: 100) == nil)
        #expect(DSSheetFit.height(content: 0, chrome: 100) == nil)
        // A bar measured mid-animation as taller than the sheet never takes away from the content.
        #expect(DSSheetFit.height(content: 300, chrome: -20) == 300)
    }

    @Test func roundingNeverMovesTheSheet() {
        #expect(!DSSheetFit.isNewHeight(600, from: 601))
        #expect(DSSheetFit.isNewHeight(600, from: 640))
        #expect(DSSheetFit.isNewHeight(nil, from: 600))
        #expect(DSSheetFit.isNewHeight(600, from: nil))
        #expect(!DSSheetFit.isNewHeight(nil, from: nil))
    }

    @Test func onlyAPhonesSheetFollowsItsContent() {
        #expect(!DSSheetFit.fills(style: .sheet, horizontal: .compact))
        #expect(DSSheetFit.fills(style: .sheet, horizontal: .regular))
        #expect(DSSheetFit.fills(style: .cover, horizontal: .compact))
    }
}
