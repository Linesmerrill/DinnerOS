import SwiftUI

/// How the app shows a sheet on this screen (build 122, review item 22).
///
/// Usually it's the system sheet. On iPhone Duo's cover screen the system sheet sits off-center:
/// it runs under the button column on the trailing side, so its top corners differ, the screen
/// underneath peeks out on the leading side, and its pinned buttons come out wider than the
/// content above them. There every sheet fills the screen instead, inside the same safe area as
/// the screen under it, so nothing peeks and everything lines up.
enum DSSheetStyle: Equatable {
    case sheet
    /// Full screen, on the Duo's cover screen.
    case cover

    static func resolve(
        horizontal: UserInterfaceSizeClass?, vertical: UserInterfaceSizeClass?, safeArea: EdgeInsets
    ) -> DSSheetStyle {
        if horizontal == .regular { return .sheet }
        return vertical == .regular && hasSideColumn(safeArea) ? .cover : .sheet
    }

    /// One side inset and not the other, as the cover screen's button column makes while the phone
    /// is upright. The same test as `StartCookingPlacement.hasSideColumn`.
    static func hasSideColumn(_ safeArea: EdgeInsets) -> Bool {
        abs(safeArea.leading - safeArea.trailing) > 1
    }
}

extension EnvironmentValues {
    /// How sheets show on this screen. Set once at the root by `.dsSheetStyleReader()`.
    @Entry var dsSheetStyle: DSSheetStyle = .sheet
}

/// Reads the screen's safe area and size classes at the root and passes the sheet style down.
/// Presented sheets and covers inherit it.
private struct DSSheetStyleReader: ViewModifier {
    @Environment(\.horizontalSizeClass) private var horizontal
    @Environment(\.verticalSizeClass) private var vertical
    @State private var safeArea = EdgeInsets()

    func body(content: Content) -> some View {
        content
            .background {
                GeometryReader { proxy in
                    Color.clear
                        .onChange(of: proxy.safeAreaInsets, initial: true) { _, insets in
                            safeArea = insets
                        }
                }
                .ignoresSafeArea(.keyboard)
                .accessibilityHidden(true)
            }
            .environment(
                \.dsSheetStyle, DSSheetStyle.resolve(horizontal: horizontal, vertical: vertical, safeArea: safeArea))
    }
}

/// A sheet that is the system sheet, or a full-screen cover where the sheet style says so.
private struct DSSheetModifier<Item: Identifiable, Sheet: View>: ViewModifier {
    @Binding var item: Item?
    let onDismiss: (() -> Void)?
    let sheet: (Item) -> Sheet
    @Environment(\.dsSheetStyle) private var style

    func body(content: Content) -> some View {
        content
            .sheet(item: binding(for: .sheet), onDismiss: onDismiss, content: sheet)
            .fullScreenCover(item: binding(for: .cover), onDismiss: onDismiss) { item in
                sheet(item).environment(\.dsSheetStyle, .cover)
            }
    }

    /// The item, only for the presentation this screen uses.
    private func binding(for presentation: DSSheetStyle) -> Binding<Item?> {
        Binding(
            get: { style == presentation ? item : nil },
            set: { item = $0 }
        )
    }
}

/// Stands in for the item of a sheet shown by a Bool.
private struct DSSheetShown: Identifiable {
    var id: Int { 0 }
}

extension View {
    /// At the app's root: lets `.dsSheet` know which screen it's on.
    func dsSheetStyleReader() -> some View {
        modifier(DSSheetStyleReader())
    }

    /// Use instead of `.sheet(isPresented:)`: the system sheet, or full screen on the Duo's cover
    /// screen, where the system sheet sits off-center.
    func dsSheet<Sheet: View>(
        isPresented: Binding<Bool>, onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping () -> Sheet
    ) -> some View {
        modifier(
            DSSheetModifier(
                item: Binding(
                    get: { isPresented.wrappedValue ? DSSheetShown() : nil },
                    set: { isPresented.wrappedValue = $0 != nil }
                ),
                onDismiss: onDismiss,
                sheet: { _ in content() }
            ))
    }

    /// Use instead of `.sheet(item:)`: the system sheet, or full screen on the Duo's cover screen.
    func dsSheet<Item: Identifiable, Sheet: View>(
        item: Binding<Item?>, onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping (Item) -> Sheet
    ) -> some View {
        modifier(DSSheetModifier(item: item, onDismiss: onDismiss, sheet: content))
    }
}

// MARK: Sheets that fit their content

/// The height a sheet needs so its content leaves no empty space under it (build 122, Hark's
/// review item 16: the paywall and Notifications sat at the top of a tall, mostly empty sheet).
enum DSSheetFit {
    /// The sheet's height for content of `content` points under bars and insets of `chrome`
    /// points, rounded up so the last line is never cut by a fraction of a point. `nil` while
    /// nothing has been measured, so the sheet keeps its usual size.
    static func height(content: CGFloat?, chrome: CGFloat) -> CGFloat? {
        guard let content, content > 0 else { return nil }
        return (content + max(chrome, 0)).rounded(.up)
    }

    /// Whether a new height is worth moving the sheet for: changes under a hairline are rounding.
    static func isNewHeight(_ new: CGFloat?, from old: CGFloat?) -> Bool {
        switch (new, old) {
        case (nil, nil): false
        case (let new?, let old?): abs(new - old) >= DS.Space.hairline
        default: true
        }
    }

    /// A scrolling list's or form's height for all its rows: its rows and the bar above them.
    /// The system adds the home indicator's room to a sheet's height itself.
    static func height(_ geometry: ScrollGeometry) -> CGFloat? {
        height(content: geometry.contentSize.height, chrome: geometry.contentInsets.top)
    }

    /// Whether a sheet keeps its size whatever its content, so its content fills it: on an iPad,
    /// and full screen on the Duo's cover screen. Only a phone's sheet follows its content.
    static func fills(style: DSSheetStyle, horizontal: UserInterfaceSizeClass?) -> Bool {
        style == .cover || horizontal == .regular
    }

    /// How long a sheet takes to come up: what its content measures before then is mid-animation.
    static let settleDelay: Duration = .milliseconds(500)
}

/// Tells `.dsFittedSheet()` how tall the sheet's content needs it to be. Passed down in the
/// environment, not up as a preference: a preference set inside a NavigationStack doesn't reach a
/// modifier outside it.
struct DSSheetHeightReport {
    let report: @MainActor (CGFloat?) -> Void
}

extension EnvironmentValues {
    @Entry var dsSheetHeight: DSSheetHeightReport?
}

/// Sizes the sheet to its content on a phone: a detent of the content's height. Heights measured
/// while the sheet is still coming up are held until it's up.
private struct DSFittedSheet: ViewModifier {
    /// The sheet's own color: a sheet shorter than the screen is otherwise see-through glass,
    /// with the tab bar showing under its links.
    let background: Color
    @Environment(\.dsSheetStyle) private var style
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @State private var needed: CGFloat?
    @State private var settled = false
    @State private var fitted: CGFloat?

    func body(content: Content) -> some View {
        if DSSheetFit.fills(style: style, horizontal: horizontalSizeClass) {
            // Full screen on the Duo's cover screen, and the system's size on an iPad, where a
            // sheet can't follow its content's height once it's up: the content fills it instead
            // (`dsSheetFills`, `DSSheetGap`).
            content.environment(\.dsSheetFills, true)
        } else {
            measured(content)
                .presentationDetents(fitted.map { [.height($0)] } ?? [.large])
                .presentationBackground(background)
        }
    }

    private func measured(_ content: Content) -> some View {
        content
            .environment(
                \.dsSheetHeight,
                DSSheetHeightReport { value in
                    needed = value
                    refit()
                }
            )
            .task {
                try? await Task.sleep(for: DSSheetFit.settleDelay)
                settled = true
                refit()
            }
    }

    private func refit() {
        guard settled else { return }
        if DSSheetFit.isNewHeight(needed, from: fitted) { fitted = needed }
    }
}

extension EnvironmentValues {
    /// True in a sheet that keeps its size whatever its content (an iPad's, or a full-screen one on
    /// the Duo's cover screen): its content spreads out to fill it.
    @Entry var dsSheetFills = false
}

/// The space between a sheet's sections: `min` where the sheet is as tall as its content, and
/// growing evenly with the others to fill a sheet that keeps its size (`dsSheetFills`), so the
/// content fills it instead of leaving empty space under it.
struct DSSheetGap: View {
    let min: CGFloat
    @Environment(\.dsSheetFills) private var fills

    var body: some View {
        if fills {
            Spacer(minLength: min)
        } else {
            Color.clear.frame(height: min)
        }
    }
}

extension View {
    /// On a sheet's root view: on a phone the sheet is as tall as its content, which reports its
    /// height with `.dsSheetContent(height:)` or `.dsSheetScrollContent()`. Elsewhere the content
    /// fills the sheet, spread out with `DSSheetGap`. `background` is the color under
    /// the content (the grouped gray under a List).
    func dsFittedSheet(background: Color = DS.Colors.background) -> some View {
        modifier(DSFittedSheet(background: background))
    }

    /// On content that fills the sheet under its bar: `height` is how tall what it shows is (`nil`
    /// keeps the sheet its usual size); the bar and insets around it are measured here.
    func dsSheetContent(height: CGFloat?) -> some View {
        modifier(DSSheetContentReporter(height: height))
    }

    /// On a List, Form or ScrollView that fills the sheet: the sheet is as tall as its rows.
    /// `isOn: false` (an empty state drawn over an empty list) keeps the sheet its usual size.
    func dsSheetScrollContent(isOn: Bool = true) -> some View {
        modifier(DSSheetScrollReporter(isOn: isOn))
    }
}

private struct DSSheetContentReporter: ViewModifier {
    let height: CGFloat?
    @Environment(\.dsSheetHeight) private var sheet
    /// The top of the content's room, under the bar.
    @State private var innerTop: CGFloat?
    /// The top of the sheet.
    @State private var outerTop: CGFloat?

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGFloat.self) {
                $0.frame(in: .global).minY
            } action: { new in
                innerTop = new
                report()
            }
            .background {
                GeometryReader { proxy in
                    Color.clear
                        .onChange(of: proxy.frame(in: .global).minY, initial: true) { _, new in
                            outerTop = new
                            report()
                        }
                }
                .ignoresSafeArea()
                .accessibilityHidden(true)
            }
            .onChange(of: height, initial: true) { report() }
            .onDisappear { sheet?.report(nil) }
    }

    /// The content and the bar above it. The system adds the home indicator's room to a sheet's
    /// height itself.
    private func report() {
        guard let innerTop, let outerTop else { return }
        sheet?.report(DSSheetFit.height(content: height, chrome: innerTop - outerTop))
    }
}

private struct DSSheetScrollReporter: ViewModifier {
    let isOn: Bool
    @Environment(\.dsSheetHeight) private var sheet
    @State private var height: CGFloat?

    func body(content: Content) -> some View {
        content
            .onScrollGeometryChange(for: CGFloat?.self) {
                DSSheetFit.height($0)
            } action: { _, new in
                height = new
                sheet?.report(isOn ? new : nil)
            }
            .onChange(of: isOn) { sheet?.report(isOn ? height : nil) }
            .onDisappear { sheet?.report(nil) }
    }
}
