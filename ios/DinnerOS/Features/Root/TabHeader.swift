import SwiftUI

/// The one header every tab uses (build 121): a small title in the bar, never a large title, and
/// the trailing buttons in one order.
///
/// - Title: `.tabHeader("Pantry")` on the tab's root view.
/// - Trailing toolbar, left to right: the bell, then +, then more. The bell comes from
///   `.notificationsToolbar()`, applied outside the tab's own `.toolbar`, which puts it first;
///   inside the tab's own `.toolbar`, put the + item before the more item.
/// - Icons: `TabHeader.addSystemImage` for +, `TabHeader.moreSystemImage` for more, so every tab
///   draws the same symbols.
nonisolated enum TabHeader {
    /// The + button.
    static let addSystemImage = "plus"
    /// The more menu: always the circled dots.
    static let moreSystemImage = "ellipsis.circle"
}

extension View {
    /// A tab's title in the shared header style: inline, in the bar.
    func tabHeader(_ title: String) -> some View {
        navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
    }
}

extension View {
    /// Keeps a sideways-scrolling row inside the safe area. A horizontal scroll view that reaches
    /// the screen's edge draws into the safe area beside it, so on the iPhone Duo's cover screen
    /// the week strip, carousels and chips ran under the status icons and side buttons (build
    /// 121). Clips only sideways: shadows and badges above and below still show.
    func carouselInSafeArea() -> some View {
        mask {
            Rectangle().padding(.vertical, -DS.Space.huge)
        }
    }
}
