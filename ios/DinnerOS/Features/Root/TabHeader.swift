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

/// A scrim along the bottom of a tab's scrolling content (iOS 26 and later): it fades the content
/// into the screen's background under the floating tab bar, so a bright card scrolling under the
/// bar can't tint its glass and wash out its labels (Hark's review item 11: orange from the
/// Suggested card on a large iPhone in dark mode). Where there's no tab bar along the bottom (an
/// iPad, the iPhone Duo's cover screen), the same short fade keeps a sliver of the next card from
/// showing at the screen's edge like a floating control (Hark's review item 4: the Suggested
/// card's Add button, 2 points of it, on the Duo).
///
/// Build 122's scrim was a fixed 64 points that only reached full strength at the screen's edge,
/// so under an 83-point tab bar the labels sat where it was still mostly clear. This one covers
/// the whole bottom inset (the tab bar, the home indicator and any bottom bar of the page) at
/// nearly full strength, plus a short fade above it.
///
/// Put it on the scroll view, inside the page's own bottom bars, so they draw over it. An
/// overlay with no state and no measuring, so it can't change the layout or feed one back into
/// it: a hard scroll edge here (`scrollEdgeEffectStyle(.hard, for: .bottom)`) froze the iPad
/// Menu in landscape. The fade sits on the safe area's bottom edge (the top of the tab bar), and
/// the solid part reaches from there to the screen's edge by ignoring the safe area.
struct TabBarScrim: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content
                .overlay(alignment: .bottom) {
                    VStack(spacing: 0) {
                        gradient(TabBarScrimLayout.fadeStops)
                            .frame(height: DS.Size.tabBarScrimFade)
                        // Zero high on the safe area's edge; its background reaches down over
                        // the bar's whole inset, however tall that is.
                        Color.clear
                            .frame(height: 0)
                            .background {
                                gradient(TabBarScrimLayout.barStops)
                                    .ignoresSafeArea(.container, edges: .bottom)
                            }
                    }
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                }
        } else {
            content
        }
    }

    private func gradient(_ stops: [(location: CGFloat, opacity: Double)]) -> LinearGradient {
        LinearGradient(
            stops: stops.map {
                Gradient.Stop(color: DS.Colors.barBackground.opacity($0.opacity), location: $0.location)
            },
            startPoint: .top, endPoint: .bottom)
    }
}

/// How the tab bar scrim fades (`TabBarScrim`).
nonisolated enum TabBarScrimLayout {
    /// How opaque the scrim is where the bar begins: enough that the bar's glass reads the
    /// screen's own background, not the card under it.
    static let opacity = 0.94

    /// Above the bar: clear to nearly full strength.
    static let fadeStops: [(location: CGFloat, opacity: Double)] = [(0, 0), (1, opacity)]

    /// Under the bar: from there to solid at the screen's edge, so not even a sliver of a card
    /// shows there.
    static let barStops: [(location: CGFloat, opacity: Double)] = [(0, opacity), (1, 1)]
}

extension View {
    /// The scrim along the bottom of a tab's scrolling content (`TabBarScrim`).
    func tabBarEdge() -> some View {
        modifier(TabBarScrim())
    }
}
