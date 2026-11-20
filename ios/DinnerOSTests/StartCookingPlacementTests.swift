import SwiftUI
import Testing

@testable import DinnerOS

/// Start Cooking moves to the bottom bar only on iPad and on iPhone Duo's cover screen (build 119).
struct StartCookingPlacementTests {
    private func place(_ h: UserInterfaceSizeClass?, _ v: UserInterfaceSizeClass?, _ insets: EdgeInsets)
        -> StartCookingPlacement
    {
        StartCookingPlacement.resolve(horizontal: h, vertical: v, safeArea: insets)
    }

    @Test func anIPhoneHeldUprightKeepsTheTopBar() {
        #expect(place(.compact, .regular, EdgeInsets(top: 62, leading: 0, bottom: 34, trailing: 0)) == .topBar)
        // iPhone SE: no insets at all.
        #expect(place(.compact, .regular, EdgeInsets()) == .topBar)
    }

    @Test func anIPhoneHeldSidewaysKeepsTheTopBar() {
        #expect(place(.compact, .compact, EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 62)) == .topBar)
        // The camera on one side only, still sideways.
        #expect(place(.compact, .compact, EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 0)) == .topBar)
        #expect(place(.regular, .compact, EdgeInsets(top: 0, leading: 62, bottom: 21, trailing: 62)) == .bottomBar)
    }

    @Test func iPadUsesTheBottomBar() {
        #expect(place(.regular, .regular, EdgeInsets(top: 24, leading: 0, bottom: 20, trailing: 0)) == .bottomBar)
    }

    @Test func theDuoCoverScreenUsesTheBottomBar() {
        // As the iOS 27.2 simulator reports it: the button column on the right.
        #expect(place(.compact, .regular, EdgeInsets(top: 0, leading: 0, bottom: 34, trailing: 84)) == .bottomBar)
    }

    @Test func unknownSizeClassesKeepTheTopBar() {
        #expect(place(nil, nil, EdgeInsets()) == .topBar)
    }
}
