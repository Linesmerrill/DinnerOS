import SwiftUI
import UIKit

/// A More menu drawn by the screen itself: an opaque card under its button, with a red row for
/// a destructive item (Hark's review item 8, build 122).
///
/// The system `Menu` is glass on iOS 26, so whatever is under it (a photo, Start Cooking) shows
/// through, and on iPhone Duo's cover screen it spread across the button rail. A `.popover`
/// (build 121) broke instead: it was attached to a toolbar item, and the menu's measured height
/// changed the screen's state while it was up, which rebuilt the toolbar item and left the
/// popover orphaned, so a row's tap couldn't close it or run anything. This is an overlay, not a
/// presentation: nothing for the toolbar to rebuild, a row's action runs at once, and the card is
/// kept inside the screen's safe area.
///
/// Use it in two parts: `DSMenuButton` in the toolbar, which reports where it is, and
/// `.dsMenuPopover(isPresented:anchor:items:)` on the screen's root, which draws the card.
struct DSMenuItem: Identifiable {
    let title: String
    let systemImage: String
    var role: ButtonRole?
    let action: () -> Void

    var id: String { title }

    var isDestructive: Bool { role == .destructive }
}

/// Where the menu's card goes, in the coordinates of the screen it covers.
nonisolated enum DSMenuPlacement {
    /// The card's trailing edge: under the button's trailing edge, but never past the screen's
    /// own edge less a margin. On iPhone Duo's cover screen the button sits in the rail outside
    /// the screen's frame (which still reports the rail as a trailing inset), so the card stops
    /// at the content's edge. With no button measured yet, at the safe area's margin.
    static func trailingEdge(anchor: CGRect, containerWidth: CGFloat, trailingInset: CGFloat) -> CGFloat {
        guard anchor.width > 0 else { return containerWidth - trailingInset - DS.Space.s }
        return min(anchor.maxX, containerWidth - DS.Space.s)
    }

    /// The card's top edge: just under the button, or at the top of the safe area when there's
    /// no button measured yet.
    static func top(anchor: CGRect, topInset: CGFloat) -> CGFloat {
        guard anchor.height > 0 else { return topInset + DS.Space.s }
        return anchor.maxY + DS.Space.s
    }

    /// The card's widest: the space left of its trailing edge, less the leading margin.
    static func maxWidth(trailingEdge: CGFloat, leadingInset: CGFloat) -> CGFloat {
        max(0, trailingEdge - leadingInset - DS.Space.s)
    }
}

extension DS.Size {
    /// A More menu's narrowest: "Delete Recipe" on one line with room to spare.
    static let menuMinWidth: CGFloat = 250
}

/// The toolbar button that opens a `.dsMenuPopover`, with the tab toolbars' More icon. It reports
/// its frame to `anchor`, so the card opens under it.
struct DSMenuButton<Label: View>: View {
    @Binding var isPresented: Bool
    @Binding var anchor: CGRect
    var accessibilityLabel: LocalizedStringKey = "More"
    @ViewBuilder var label: () -> Label

    var body: some View {
        Button {
            isPresented.toggle()
        } label: {
            label()
                .onGeometryChange(for: CGRect.self) {
                    $0.frame(in: .global)
                } action: { frame in
                    if frame != anchor { anchor = frame }
                }
        }
        .accessibilityLabel(accessibilityLabel)
    }
}

extension DSMenuButton where Label == Image {
    init(isPresented: Binding<Bool>, anchor: Binding<CGRect>) {
        self.init(isPresented: isPresented, anchor: anchor) {
            Image(systemName: TabHeader.moreSystemImage)
        }
    }
}

extension View {
    /// The card for a `DSMenuButton`, drawn over this view (put it on the screen's root, after
    /// any pinned bars, so a tap anywhere outside the card closes it).
    func dsMenuPopover(isPresented: Binding<Bool>, anchor: CGRect, items: [DSMenuItem]) -> some View {
        overlay {
            DSMenuLayer(isPresented: isPresented, anchor: anchor, items: items)
        }
    }
}

private struct DSMenuLayer: View {
    @Binding var isPresented: Bool
    let anchor: CGRect
    let items: [DSMenuItem]

    var body: some View {
        GeometryReader { proxy in
            let origin = proxy.frame(in: .global).origin
            let local = anchor.offsetBy(dx: -origin.x, dy: -origin.y)
            let insets = proxy.safeAreaInsets
            let trailing = DSMenuPlacement.trailingEdge(
                anchor: local, containerWidth: proxy.size.width, trailingInset: insets.trailing)
            ZStack(alignment: .topLeading) {
                if isPresented {
                    // A tap anywhere else closes it, as with the system menu.
                    Color.clear
                        .contentShape(.rect)
                        .onTapGesture { isPresented = false }
                        .ignoresSafeArea()
                        .accessibilityHidden(true)
                    DSMenuCard(items: items) { isPresented = false }
                        .frame(
                            maxWidth: DSMenuPlacement.maxWidth(trailingEdge: trailing, leadingInset: insets.leading),
                            alignment: .trailing
                        )
                        .frame(width: trailing, alignment: .trailing)
                        .offset(y: DSMenuPlacement.top(anchor: local, topInset: 0))
                        // Gone at once on closing, so the row tapped can't be found again
                        // (or tapped) while whatever it opened comes up.
                        .transition(
                            .asymmetric(
                                insertion: .scale(scale: 0.9, anchor: .topTrailing).combined(with: .opacity),
                                removal: .identity))
                }
            }
            .frame(width: proxy.size.width, height: proxy.size.height, alignment: .topLeading)
        }
        .animation(.snappy(duration: 0.2), value: isPresented)
        // The card is modal to VoiceOver: move its focus into the card on opening, and back to
        // the page on closing.
        .onChange(of: isPresented) {
            UIAccessibility.post(notification: .screenChanged, argument: nil)
        }
    }
}

/// The card: one row per item on an opaque surface, the destructive one in red.
private struct DSMenuCard: View {
    let items: [DSMenuItem]
    let close: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                if index > 0 { Divider() }
                Button(role: item.role) {
                    // Closed first, so whatever the row opens (a sheet, a confirmation) has
                    // nothing over it.
                    close()
                    item.action()
                } label: {
                    DSIconRow(systemImage: item.systemImage, tint: tint(item)) {
                        Text(item.title).foregroundStyle(tint(item))
                    }
                    .font(DS.Typography.body)
                    .padding(.horizontal, DS.Space.l)
                    .frame(minHeight: DS.Size.tapTarget)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, DS.Space.xxs)
        .frame(minWidth: DS.Size.menuMinWidth)
        .fixedSize()
        .background(DS.Colors.menuSurface, in: .rect(cornerRadius: DS.Radius.large))
        .overlay {
            RoundedRectangle(cornerRadius: DS.Radius.large).strokeBorder(DS.Colors.cardStroke)
        }
        .shadow(color: DS.Colors.menuShadow, radius: DS.Space.l, y: DS.Space.xxs)
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isModal)
        .accessibilityAction(.escape) { close() }
    }

    private func tint(_ item: DSMenuItem) -> Color {
        item.isDestructive ? DS.Colors.destructive : Color.primary
    }
}

extension DS.Colors {
    /// A menu card's fill: opaque, a step up from the screen in dark mode.
    static let menuSurface = Color(.secondarySystemGroupedBackground)
    /// A menu card's shadow, so it lifts off a photo or a white page.
    static let menuShadow = Color.black.opacity(0.22)
}

/// The system share sheet, shown from a menu row: a `ShareLink` can't live in a menu that closes
/// as its row is tapped. On iPad it points at `anchor` (the More button), in window coordinates.
@MainActor
enum DSShareSheet {
    static func present(_ url: URL, subject: String, anchor: CGRect) {
        guard
            let window = UIApplication.shared.connectedScenes
                .compactMap({ $0 as? UIWindowScene })
                .flatMap(\.windows)
                .first(where: \.isKeyWindow),
            var top = window.rootViewController
        else { return }
        while let presented = top.presentedViewController { top = presented }
        let sheet = UIActivityViewController(
            activityItems: [DSShareItem(url: url, subject: subject)], applicationActivities: nil)
        if let popover = sheet.popoverPresentationController {
            popover.sourceView = window
            popover.sourceRect =
                anchor.isEmpty
                ? CGRect(origin: CGPoint(x: window.bounds.midX, y: window.bounds.midY), size: .zero) : anchor
        }
        top.present(sheet, animated: true)
    }
}

/// A link to share, with the subject Mail uses for it.
private final class DSShareItem: NSObject, UIActivityItemSource {
    let url: URL
    let subject: String

    init(url: URL, subject: String) {
        self.url = url
        self.subject = subject
    }

    func activityViewControllerPlaceholderItem(_ controller: UIActivityViewController) -> Any { url }

    func activityViewController(
        _ controller: UIActivityViewController, itemForActivityType activityType: UIActivity.ActivityType?
    ) -> Any? { url }

    func activityViewController(
        _ controller: UIActivityViewController, subjectForActivityType activityType: UIActivity.ActivityType?
    ) -> String { subject }
}
