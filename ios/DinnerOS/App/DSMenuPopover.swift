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
    /// Shown dimmed and not tappable, like a system menu's disabled row.
    var isDisabled = false
    /// Starts a new group under a line, with this heading over it ("Autopilot"), or with no
    /// heading when it's empty.
    var group: String?
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
    /// no button measured yet. Never above the top of the screen's content (0 here): a button in
    /// the navigation bar ends inside the bar, and a row drawn there sat under the bar, which
    /// took its taps, so "Low-Stock Alerts…" and "Edit Recipe" did nothing on a large iPhone
    /// (build 122).
    ///
    /// A button beside the content, in the iPhone Duo's cover-screen rail, has nothing under it
    /// for the card to hang from, so the card starts at the content's top instead: from under
    /// the button it had room for half the Menu's week menu (build 122).
    static func top(anchor: CGRect, topInset: CGFloat, containerWidth: CGFloat = .infinity) -> CGFloat {
        guard anchor.height > 0 else { return topInset + DS.Space.s }
        if anchor.minX >= containerWidth { return DS.Space.s }
        return max(anchor.maxY, 0) + DS.Space.s
    }

    /// The card's widest: the space left of its trailing edge, less the leading margin.
    static func maxWidth(trailingEdge: CGFloat, leadingInset: CGFloat) -> CGFloat {
        max(0, trailingEdge - leadingInset - DS.Space.s)
    }

    /// The card's tallest: from its top to the bottom of the safe area, less a margin. A longer
    /// menu (the Menu's week menu on the iPhone Duo's cover screen) scrolls inside the card.
    static func maxHeight(top: CGFloat, containerHeight: CGFloat, bottomInset: CGFloat) -> CGFloat {
        max(DS.Size.tapTarget, containerHeight - bottomInset - top - DS.Space.s)
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
            let top = DSMenuPlacement.top(anchor: local, topInset: 0, containerWidth: proxy.size.width)
            ZStack(alignment: .topLeading) {
                if isPresented {
                    // A tap anywhere else closes it, as with the system menu.
                    Color.clear
                        .contentShape(.rect)
                        .onTapGesture { isPresented = false }
                        .ignoresSafeArea()
                        .accessibilityHidden(true)
                    DSMenuCard(
                        items: items,
                        maxHeight: DSMenuPlacement.maxHeight(
                            top: top, containerHeight: proxy.size.height, bottomInset: insets.bottom)
                    ) { isPresented = false }
                    .frame(
                        maxWidth: DSMenuPlacement.maxWidth(trailingEdge: trailing, leadingInset: insets.leading),
                        alignment: .trailing
                    )
                    .frame(width: trailing, alignment: .trailing)
                    .offset(y: top)
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
    let maxHeight: CGFloat
    let close: () -> Void

    var body: some View {
        // As tall as its rows when they fit, and scrolling inside the tallest it may be when
        // they don't: a scroll view alone always took the whole height.
        ViewThatFits(in: .vertical) {
            card(rows.fixedSize())
            card(
                ScrollView {
                    rows.fixedSize()
                }
                .scrollBounceBehavior(.basedOnSize))
        }
        // At the top of the room it's given, not centered in it: the card hugs its rows.
        .frame(maxHeight: maxHeight, alignment: .top)
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isModal)
        .accessibilityAction(.escape) { close() }
    }

    /// The opaque card around the rows, as wide as its widest row.
    private func card(_ content: some View) -> some View {
        content
            .fixedSize(horizontal: true, vertical: false)
            .background(DS.Colors.menuSurface, in: .rect(cornerRadius: DS.Radius.large))
            .clipShape(.rect(cornerRadius: DS.Radius.large))
            .overlay {
                RoundedRectangle(cornerRadius: DS.Radius.large).strokeBorder(DS.Colors.cardStroke)
            }
            .shadow(color: DS.Colors.menuShadow, radius: DS.Space.l, y: DS.Space.xxs)
    }

    private var rows: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                if let group = item.group, index > 0 {
                    // A wider gap than a row's line, like a system menu's groups.
                    DS.Colors.menuGroupGap.frame(height: DS.Space.s)
                    if !group.isEmpty { groupHeading(group) }
                } else if let group = item.group, !group.isEmpty {
                    groupHeading(group)
                } else if index > 0 {
                    Divider()
                }
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
                .disabled(item.isDisabled)
            }
        }
        .padding(.vertical, DS.Space.xxs)
        .frame(minWidth: DS.Size.menuMinWidth, alignment: .leading)
    }

    private func groupHeading(_ title: String) -> some View {
        Text(title)
            .font(DS.Typography.footnote.weight(.semibold))
            .foregroundStyle(DS.Colors.secondaryText)
            .padding(.horizontal, DS.Space.l)
            .padding(.top, DS.Space.s)
            .padding(.bottom, DS.Space.xxs)
            .accessibilityAddTraits(.isHeader)
    }

    private func tint(_ item: DSMenuItem) -> Color {
        if item.isDisabled { return DS.Colors.disabledText }
        return item.isDestructive ? DS.Colors.destructive : Color.primary
    }
}

extension DS.Colors {
    /// A menu card's fill: opaque, a step up from the screen in dark mode.
    static let menuSurface = Color(.secondarySystemGroupedBackground)
    /// A menu card's shadow, so it lifts off a photo or a white page.
    static let menuShadow = Color.black.opacity(0.22)
    /// The band between a menu card's groups, a step darker than its rows.
    static let menuGroupGap = Color(.tertiarySystemFill)
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
