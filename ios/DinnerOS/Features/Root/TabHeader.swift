import SwiftUI
import UIKit

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
    /// How opaque the scrim is where the bar begins: solid, so the bar's glass reads one tone,
    /// the screen's own background, and never a card or photo under it. At 0.94 (build 122) a
    /// photo still tinted the glass and its labels fell to about 3:1 (Hark's review item 10).
    static let opacity = 1.0

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

    /// A solid band of the screen's background under the tab bar, on every screen of a tab
    /// (`TabBarBacking`).
    func tabBarBacking(_ edge: VerticalEdge) -> some View {
        modifier(TabBarBacking(edge: edge))
    }
}

/// Where the tab bar sits, so `TabBarBacking` knows which edge to back.
nonisolated enum TabBarPlacement {
    /// The tab bar runs along the top on an iPad with room for it, and along the bottom on an
    /// iPhone. The iPhone Duo's cover screen keeps it in a rail beside the screen, where neither
    /// band reaches it and the bottom one only covers the home indicator's strip.
    static func edge(isPad: Bool, horizontalSizeClass: UserInterfaceSizeClass?) -> VerticalEdge {
        isPad && horizontalSizeClass == .regular ? .top : .bottom
    }
}

/// A solid band of the screen's background where the tab bar floats (iOS 26 and later), so the
/// bar's glass always reads one tone and its labels keep 4.5:1 or better: a hero photo, a yellow
/// placeholder or a card under the bar tinted it, with the selected tab green on yellow on an
/// iPad and about 3:1 on Shared Recipes on a large iPhone, with a seam between the selected
/// tab's pill and the rest (Hark's review item 10, build 122).
///
/// It fills only the bar's own inset (from the safe area's edge to the screen's), never the
/// page, so a page's own bottom bar or top content is untouched; the tab roots add a short fade
/// above it with `tabBarEdge()`. Put the bottom band on a tab's navigation stack, which reaches
/// every page pushed in it, under the tab bar but not under the navigation bar's buttons. The
/// top band (an iPad's tab bar) goes on each page inside the stack, so the back and more
/// buttons draw over it. No state and no measuring, so it can't feed a layout back into itself
/// (a hard scroll edge froze the iPad Menu in landscape, build 122). It ignores the keyboard, so
/// with the keyboard up it stays at the screen's edge behind it instead of riding above it.
struct TabBarBacking: ViewModifier {
    let edge: VerticalEdge
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    private var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    func body(content: Content) -> some View {
        if #available(iOS 26.0, *),
            TabBarPlacement.edge(isPad: isPad, horizontalSizeClass: horizontalSizeClass) == edge
        {
            content.overlay {
                VStack(spacing: 0) {
                    if edge == .bottom { Spacer(minLength: 0) }
                    // Zero high on the safe area's edge; its background reaches over the bar's
                    // whole inset, however tall that is.
                    Color.clear
                        .frame(height: 0)
                        .background {
                            DS.Colors.barBackground
                                .ignoresSafeArea(.container, edges: edge == .top ? .top : .bottom)
                        }
                    if edge == .top { Spacer(minLength: 0) }
                }
                .ignoresSafeArea(.keyboard)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        } else {
            content
        }
    }
}
