import SwiftUI
import Testing

@testable import DinnerOS

/// Sheets fill the screen only on iPhone Duo's cover screen, where the system sheet sits
/// off-center under the button column (build 122, review item 22).
struct DSSheetStyleTests {
    private func style(_ h: UserInterfaceSizeClass?, _ v: UserInterfaceSizeClass?, _ insets: EdgeInsets)
        -> DSSheetStyle
    {
        DSSheetStyle.resolve(horizontal: h, vertical: v, safeArea: insets)
    }

    @Test func theDuoCoverScreenFillsTheScreen() {
        // 382 x 644, the button column on the trailing side.
        #expect(style(.compact, .regular, EdgeInsets(top: 20, leading: 0, bottom: 20, trailing: 84)) == .cover)
        // The column on the other side, as it would be turned the other way.
        #expect(style(.compact, .regular, EdgeInsets(top: 20, leading: 84, bottom: 20, trailing: 0)) == .cover)
    }

    @Test func anIPhoneHeldUprightKeepsTheSystemSheet() {
        #expect(style(.compact, .regular, EdgeInsets(top: 62, leading: 0, bottom: 34, trailing: 0)) == .sheet)
        // iPhone SE: no insets at all.
        #expect(style(.compact, .regular, EdgeInsets()) == .sheet)
    }

    @Test func anIPhoneHeldSidewaysKeepsTheSystemSheet() {
        #expect(style(.compact, .compact, EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 0)) == .sheet)
        #expect(style(.compact, .compact, EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 62)) == .sheet)
    }

    @Test func iPadKeepsTheSystemSheet() {
        #expect(style(.regular, .regular, EdgeInsets(top: 24, leading: 0, bottom: 20, trailing: 0)) == .sheet)
        #expect(style(.regular, .compact, EdgeInsets(top: 24, leading: 0, bottom: 20, trailing: 84)) == .sheet)
    }

    @Test func aRoundingDifferenceIsNotAColumn() {
        #expect(!DSSheetStyle.hasSideColumn(EdgeInsets(top: 0, leading: 0.5, bottom: 0, trailing: 0)))
        #expect(DSSheetStyle.hasSideColumn(EdgeInsets(top: 0, leading: 0, bottom: 0, trailing: 84)))
    }

    @Test func theDefaultIsTheSystemSheet() {
        #expect(EnvironmentValues().dsSheetStyle == .sheet)
    }

    /// The same test as the Duo's Start Cooking row, so the two never disagree about the screen.
    @Test func agreesWithStartCookingPlacement() {
        for insets in [
            EdgeInsets(), EdgeInsets(top: 20, leading: 0, bottom: 20, trailing: 84),
            EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 62),
        ] {
            #expect(DSSheetStyle.hasSideColumn(insets) == StartCookingPlacement.hasSideColumn(insets))
            let isRow =
                StartCookingPlacement.resolve(horizontal: .compact, vertical: .regular, safeArea: insets) == .bottomRow
            #expect((style(.compact, .regular, insets) == .cover) == isRow)
        }
    }
}
