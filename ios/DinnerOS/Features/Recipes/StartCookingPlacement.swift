import SwiftUI

/// Where a recipe page puts Start Cooking.
///
/// Usually it sits in the top bar. On iPad it sits in the bottom bar, because the tab bar shares
/// the top bar there. On iPhone Duo's cover screen (iOS 27) the top bar's buttons move to a column
/// beside the page, and Start Cooking only shows at the top, so it scrolls away: it goes in the
/// bottom bar there too (build 119).
///
/// The cover screen reports the same size classes as any iPhone held upright. What sets it apart
/// is that column: a safe area on one side only while the phone is upright. An iPhone held
/// sideways has a compact height and keeps the top bar.
enum StartCookingPlacement: Equatable {
    case topBar
    case bottomBar

    static func resolve(
        horizontal: UserInterfaceSizeClass?, vertical: UserInterfaceSizeClass?, safeArea: EdgeInsets
    ) -> StartCookingPlacement {
        if horizontal == .regular { return .bottomBar }
        if vertical == .regular, hasSideColumn(safeArea) { return .bottomBar }
        return .topBar
    }

    /// One side inset and not the other, as the cover screen's button column makes.
    static func hasSideColumn(_ safeArea: EdgeInsets) -> Bool {
        abs(safeArea.leading - safeArea.trailing) > 1
    }
}
