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

extension View {
    /// A scrim under the floating tab bar (iOS 26 and later), meant to keep a bright card from
    /// tinting the bar's glass (Hark's review item 11). An overlay, so it never changes the
    /// layout: a hard scroll edge here (`scrollEdgeEffectStyle(.hard, for: .bottom)`) froze the
    /// iPad Menu in landscape. Build 122's screenshots show it doesn't reach under the bar yet.
    @ViewBuilder
    func tabBarEdge() -> some View {
        if #available(iOS 26.0, *) {
            overlay(alignment: .bottom) {
                LinearGradient(
                    colors: [DS.Colors.barBackground.opacity(0), DS.Colors.barBackground.opacity(0.9)],
                    startPoint: .top, endPoint: .bottom
                )
                .frame(height: DS.Size.tabBarScrimHeight)
                .ignoresSafeArea(edges: .bottom)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        } else {
            self
        }
    }
}
