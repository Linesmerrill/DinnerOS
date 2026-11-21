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
