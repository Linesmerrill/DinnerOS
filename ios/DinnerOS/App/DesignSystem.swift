import SwiftUI

/// DinnerOS's one design system (build 120): colors, spacing, corner radii, type, and the shared
/// button styles. Screens use these tokens instead of their own numbers, so the same kind of
/// thing looks the same everywhere.
///
/// Buttons: `.buttonStyle(.dsPrimary)`, `.dsSecondary`, `.dsOutline` and `.dsTextLink`. Each has
/// one fixed height per size (`DS.ButtonSize`, 50 pt by default) that grows with Dynamic Type.
/// Primary and secondary wrap the system's prominent and bordered styles, so they keep the
/// system look (Liquid Glass on iOS 26 and later) and only their height, type and shape are set
/// here. The system adds 15 pt above and below a large button's label (10 pt for small); the
/// styles size the label so the whole button lands on the token height.
nonisolated enum DS {
    // MARK: Colors

    enum Colors {
        /// The app's green (AccentColor), lighter in dark mode.
        static let accent = Color.accentColor
        static let background = Color(.systemBackground)
        static let groupedBackground = Color(.systemGroupedBackground)
        /// Cards, banners and fields on a plain background.
        static let surface = Color(.secondarySystemBackground)
        /// Cards on a grouped background.
        static let groupedSurface = Color(.secondarySystemGroupedBackground)
        static let raisedSurface = Color(.tertiarySystemBackground)
        static let fill = Color(.secondarySystemFill)
        static let subtleFill = Color(.tertiarySystemFill)
        static let secondaryText = Color.secondary
        static let destructive = Color(.systemRed)
        static let warning = Color(.systemOrange)
        static let link = Color(.systemBlue)
        /// The tint behind a light message, like the sign-in screen's notes.
        static let noticeFill = Color.secondary.opacity(0.12)
        static let errorFill = Color.red.opacity(0.12)
        /// Text and icons on an accent fill (filled buttons, avatars, selected chips): white in
        /// light mode, near-black on the light dark-mode green, so both pass 4.5:1 (build 121).
        static let onAccent = Color("OnAccent")
        /// Done, included, checked: the accent, never system green.
        static let success = Color.accentColor
        /// A small label's tint, like a difficulty badge: the accent, never system blue.
        static let badge = Color.accentColor
        /// Disabled button fill and text: one look for every disabled action.
        static let disabledFill = Color(.tertiarySystemFill)
        static let disabledText = Color(.tertiaryLabel)
        /// A card's outline, for a card on a sheet where its fill alone is too faint to see.
        static let cardStroke = Color(.separator)
        /// The background under a pinned footer or bottom bar: the same as the sheet or screen.
        static let barBackground = Color(.systemBackground)
        /// The light accent wash behind a tinted control that isn't a system button, like the
        /// recipe bar's servings stepper, matching `.dsSecondary` (build 121).
        static let accentFill = Color.accentColor.opacity(0.15)
        /// An ingredient thumbnail's plate: white in light and dark, like the product photos
        /// shot on white that sit beside it (build 121).
        static let thumbnailFill = Color.white
        /// The fallback glyph on that plate: one mid gray in light and dark.
        static let thumbnailGlyph = Color(white: 0.62)
    }

    // MARK: Spacing

    /// Padding and stack spacing, in points.
    enum Space {
        static let hairline: CGFloat = 2
        static let xxs: CGFloat = 4
        static let xs: CGFloat = 6
        static let s: CGFloat = 8
        static let sm: CGFloat = 10
        static let m: CGFloat = 12
        static let ml: CGFloat = 14
        /// The usual screen margin and card padding.
        static let l: CGFloat = 16
        static let xl: CGFloat = 20
        static let xxl: CGFloat = 24
        static let xxxl: CGFloat = 32
        static let huge: CGFloat = 40
    }

    // MARK: Corner radii

    enum Radius {
        /// Chips, thumbnails and small badges.
        static let small: CGFloat = 8
        /// Fields, banners and buttons that aren't capsules.
        static let medium: CGFloat = 12
        /// Cards and photos.
        static let large: CGFloat = 16
        /// Sheets and big hero cards.
        static let xLarge: CGFloat = 22
    }

    // MARK: Type

    /// Text styles, all Dynamic Type.
    enum Typography {
        static let largeTitle = Font.largeTitle.bold()
        static let title = Font.title2.bold()
        static let sectionTitle = Font.title3.weight(.semibold)
        static let subtitle = Font.title3
        static let headline = Font.headline
        static let body = Font.body
        static let callout = Font.callout
        static let subheadline = Font.subheadline
        static let footnote = Font.footnote
        static let caption = Font.caption
        /// Large and small button labels.
        static let button = Font.headline
        static let smallButton = Font.subheadline.weight(.semibold)
        static let link = Font.subheadline.weight(.medium)

        /// The sign-in buttons' title, matched to Sign in with Apple's own button, whose title is
        /// 43% of its height in a medium weight.
        static func signInButton(height: CGFloat) -> Font {
            .system(size: (height * 0.43).rounded(), weight: .medium)
        }
    }

    // MARK: Sizes

    enum Size {
        /// The smallest comfortable tap target.
        static let tapTarget: CGFloat = 44
        /// The fixed column a row's leading icon sits in, so every row's text lines up.
        static let iconColumn: CGFloat = 28
        /// Forms and single-column content on a wide screen (iPad) stay this wide, centered.
        static let readableWidth: CGFloat = 640
        /// A recipe's photo is never taller than this on a wide screen.
        static let heroMaxHeight: CGFloat = 360
        /// The height of `DSSegmentedControl`: short enough that its capsule thumb stays round inside
        /// a toolbar item.
        static let segmentedHeight: CGFloat = 32
    }

    /// Button heights. Each style has one fixed height per size; it scales with Dynamic Type.
    enum ButtonSize {
        /// 50 pt: full-width actions and the sign-in buttons.
        case large
        /// 34 pt: actions inside rows and cards.
        case small

        var height: CGFloat {
            switch self {
            case .large: 50
            case .small: 34
            }
        }

        /// What the system adds above and below a label at this size.
        var systemPadding: CGFloat {
            switch self {
            case .large: 30
            case .small: 14
            }
        }

        var controlSize: ControlSize {
            switch self {
            case .large: .large
            case .small: .regular
            }
        }

        var font: Font {
            switch self {
            case .large: Typography.button
            case .small: Typography.smallButton
            }
        }
    }
}

// MARK: Layout helpers

extension View {
    /// Keeps a form or single column at `DS.Size.readableWidth`, centered, on a wide screen.
    func dsReadableWidth() -> some View {
        frame(maxWidth: DS.Size.readableWidth).frame(maxWidth: .infinity)
    }

    /// A grouped List or Form held to the readable column on a wide screen, with the grouped
    /// gray continuing out to the screen's edges (build 121).
    func dsReadableForm() -> some View {
        dsReadableWidth().background(DS.Colors.groupedBackground)
    }

    /// A row's leading icon in the shared fixed-width column.
    func dsIconColumn() -> some View {
        frame(width: DS.Size.iconColumn, alignment: .center)
    }
}

/// A row with its icon in the fixed icon column, so text lines up from row to row.
struct DSIconRow<Title: View>: View {
    let systemImage: String
    var tint: Color = DS.Colors.accent
    @ViewBuilder let title: () -> Title

    var body: some View {
        HStack(spacing: DS.Space.m) {
            Image(systemName: systemImage)
                .foregroundStyle(tint)
                .dsIconColumn()
                .accessibilityHidden(true)
            title()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// A pinned footer's content: in the readable column, from edge to edge. Use it through
/// `.dsBottomBar { }`, which picks the right way to pin it (build 121).
struct DSBottomBar<Content: View>: View {
    /// `nil` leaves the bar clear, so it shows whatever the sheet or screen is drawn on.
    var background: Color? = DS.Colors.barBackground
    var showsDivider = true
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(spacing: DS.Space.sm) {
            content()
        }
        .padding(DS.Space.l)
        .dsReadableWidth()
        .background(background ?? .clear)
        .overlay(alignment: .top) {
            if showsDivider { Divider() }
        }
    }
}

/// Pins a `DSBottomBar`. On iOS 26 it's a safe-area bar: no fill of its own, so it's exactly
/// the sheet's or screen's color, and content passing under it fades behind the system's soft
/// scroll edge instead of being cut by a hard line. Earlier, an inset on `background`.
private struct DSBottomBarModifier<Bar: View>: ViewModifier {
    let isShown: Bool
    let background: Color
    let bar: Bar

    func body(content: Content) -> some View {
        if !isShown {
            content
        } else if #available(iOS 26.0, *) {
            content.safeAreaBar(edge: .bottom, spacing: 0) {
                DSBottomBar(background: nil, showsDivider: false) { bar }
            }
        } else {
            content.safeAreaInset(edge: .bottom, spacing: 0) {
                DSBottomBar(background: background) { bar }
            }
        }
    }
}

extension View {
    /// A footer pinned to the bottom, with a bottom inset equal to its height so the last
    /// content scrolls clear of it. `background` is the fill before iOS 26: the screen's own
    /// color (`groupedBackground` under a grouped list).
    func dsBottomBar<Bar: View>(
        isShown: Bool = true, background: Color = DS.Colors.barBackground, @ViewBuilder _ bar: () -> Bar
    ) -> some View {
        modifier(DSBottomBarModifier(isShown: isShown, background: background, bar: bar()))
    }
}

// MARK: Button styles

/// The label inside a design-system button: one line at the token height, centered, with an
/// optional icon in a fixed slot.
private struct DSButtonLabel<Label: View>: View {
    let label: Label
    let size: DS.ButtonSize
    let fullWidth: Bool
    let font: Font?
    /// Height of the label's own box: the button height minus what the system adds.
    let contentHeight: CGFloat
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let large = dynamicTypeSize.isAccessibilitySize
        label
            .font(font ?? size.font)
            .lineLimit(large ? 3 : 1)
            .minimumScaleFactor(large ? 1 : 0.85)
            .multilineTextAlignment(.center)
            .frame(maxWidth: fullWidth ? .infinity : nil)
            // One fixed height; at accessibility text sizes it may grow to fit two lines.
            .frame(minHeight: contentHeight, maxHeight: large ? nil : contentHeight)
    }
}

/// Filled with the accent color: the main action on a screen.
struct DSPrimaryButtonStyle: PrimitiveButtonStyle {
    var size: DS.ButtonSize = .large
    var fullWidth = false
    var font: Font?
    @ScaledMetric(relativeTo: .body) private var scale: CGFloat = 1

    @Environment(\.isEnabled) private var isEnabled

    @ViewBuilder
    func makeBody(configuration: Configuration) -> some View {
        if isEnabled {
            Button(role: configuration.role, action: configuration.trigger) {
                DSButtonLabel(
                    label: configuration.label, size: size, fullWidth: fullWidth, font: font,
                    contentHeight: (size.height * scale - size.systemPadding).rounded()
                )
                // White in light mode, near-black on dark mode's lighter green (4.5:1 or better).
                .foregroundStyle(DS.Colors.onAccent)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(size.controlSize)
        } else {
            // The shared disabled look: the disabled fill with an outline, so it stays visible on
            // a white bar in light mode instead of fading into it (build 121, review 2).
            // Still a button, so VoiceOver and UI tests see a disabled button.
            Button(role: configuration.role, action: configuration.trigger) {
                DSButtonLabel(
                    label: configuration.label, size: size, fullWidth: fullWidth, font: font,
                    contentHeight: (size.height * scale).rounded() - 2
                )
                .padding(.horizontal, size == .large ? DS.Space.xl : DS.Space.ml)
                .padding(.vertical, 1)
                .foregroundStyle(DS.Colors.disabledText)
                .background(DS.Colors.disabledFill, in: Capsule())
                .overlay(Capsule().strokeBorder(DS.Colors.cardStroke, lineWidth: 1))
            }
            .buttonStyle(.plain)
        }
    }
}

/// Tinted with the accent color: actions beside the main one.
struct DSSecondaryButtonStyle: PrimitiveButtonStyle {
    var size: DS.ButtonSize = .large
    var fullWidth = false
    var font: Font?
    @ScaledMetric(relativeTo: .body) private var scale: CGFloat = 1

    func makeBody(configuration: Configuration) -> some View {
        Button(role: configuration.role, action: configuration.trigger) {
            DSButtonLabel(
                label: configuration.label, size: size, fullWidth: fullWidth, font: font,
                contentHeight: (size.height * scale - size.systemPadding).rounded())
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .controlSize(size.controlSize)
    }
}

/// A thin outline on the background, for actions that shouldn't draw the eye, and for the
/// sign-in screen's Google button (in Google's own colors, `palette`).
struct DSOutlineButtonStyle: ButtonStyle {
    struct Palette {
        let fill: Color
        let stroke: Color
        let text: Color
    }

    var size: DS.ButtonSize = .large
    var fullWidth = false
    var font: Font?
    var palette: ((ColorScheme) -> Palette)?
    @ScaledMetric(relativeTo: .body) private var scale: CGFloat = 1
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        let colors =
            palette?(colorScheme)
            ?? Palette(fill: .clear, stroke: Color(.separator), text: DS.Colors.accent)
        DSButtonLabel(
            label: configuration.label, size: size, fullWidth: fullWidth, font: font,
            contentHeight: (size.height * scale).rounded() - 2
        )
        .padding(.horizontal, size == .large ? DS.Space.xl : DS.Space.ml)
        .padding(.vertical, 1)
        .foregroundStyle(colors.text)
        .background(colors.fill, in: Capsule())
        .overlay(Capsule().strokeBorder(colors.stroke, lineWidth: 1))
        .contentShape(Capsule())
        .opacity(isEnabled ? (configuration.isPressed ? 0.7 : 1) : 0.4)
    }
}

/// Accent-colored text with no background: "Sign in with email", "Show More". At least a
/// 44 pt tap target.
struct DSTextLinkButtonStyle: ButtonStyle {
    var font: Font = DS.Typography.link
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(font)
            .foregroundStyle(DS.Colors.accent)
            .frame(minHeight: DS.Size.tapTarget)
            .contentShape(Rectangle())
            .opacity(isEnabled ? (configuration.isPressed ? 0.5 : 1) : 0.4)
    }
}

extension PrimitiveButtonStyle where Self == DSPrimaryButtonStyle {
    static var dsPrimary: DSPrimaryButtonStyle { DSPrimaryButtonStyle() }
    static func dsPrimary(_ size: DS.ButtonSize = .large, fullWidth: Bool = false, font: Font? = nil)
        -> DSPrimaryButtonStyle
    {
        DSPrimaryButtonStyle(size: size, fullWidth: fullWidth, font: font)
    }
}

extension PrimitiveButtonStyle where Self == DSSecondaryButtonStyle {
    static var dsSecondary: DSSecondaryButtonStyle { DSSecondaryButtonStyle() }
    static func dsSecondary(_ size: DS.ButtonSize = .large, fullWidth: Bool = false, font: Font? = nil)
        -> DSSecondaryButtonStyle
    {
        DSSecondaryButtonStyle(size: size, fullWidth: fullWidth, font: font)
    }
}

extension ButtonStyle where Self == DSOutlineButtonStyle {
    static var dsOutline: DSOutlineButtonStyle { DSOutlineButtonStyle() }
    static func dsOutline(_ size: DS.ButtonSize = .large, fullWidth: Bool = false, font: Font? = nil)
        -> DSOutlineButtonStyle
    {
        DSOutlineButtonStyle(size: size, fullWidth: fullWidth, font: font)
    }
}

extension ButtonStyle where Self == DSOutlineButtonStyle {
    /// The sign-in buttons (Face ID, Apple, Google) share this one drawing, so their height,
    /// shape and title size can't drift apart (build 121). The Face ID button is filled with the
    /// accent and its title is `DS.Colors.onAccent`.
    static var dsSignInPrimary: DSOutlineButtonStyle {
        DSOutlineButtonStyle(
            size: .large, fullWidth: true,
            palette: { _ in .init(fill: DS.Colors.accent, stroke: DS.Colors.accent, text: DS.Colors.onAccent) })
    }
}

extension ButtonStyle where Self == DSTextLinkButtonStyle {
    static var dsTextLink: DSTextLinkButtonStyle { DSTextLinkButtonStyle() }
}

#Preview("Buttons") {
    VStack(spacing: DS.Space.m) {
        Button("Add to Week") {}.buttonStyle(.dsPrimary(fullWidth: true))
        Button("Start Cooking", systemImage: "frying.pan") {}.buttonStyle(.dsSecondary(fullWidth: true))
        Button("Not Now") {}.buttonStyle(.dsOutline(fullWidth: true))
        HStack {
            Button("Add") {}.buttonStyle(.dsPrimary(.small))
            Button("Edit") {}.buttonStyle(.dsSecondary(.small))
            Button("Skip") {}.buttonStyle(.dsOutline(.small))
        }
        Button("Sign in with email") {}.buttonStyle(.dsTextLink)
    }
    .padding(DS.Space.l)
}

// MARK: Segmented control

/// A capsule segmented control: the choices in a row, the selected one on a round thumb (build
/// 121). Used instead of `Picker(.segmented)`, which in a toolbar drew its own outline inside the
/// toolbar's glass, offset from it, with a thumb cut flat at the bottom (Hark's review, iPad).
///
/// In a toolbar on iOS 26 and later the toolbar's glass is the track (`inGlassToolbar`), so
/// there is one outline; elsewhere the control draws its own capsule track.
struct DSSegmentedControl<Value: Hashable>: View {
    @Binding var selection: Value
    let options: [(value: Value, title: LocalizedStringKey)]
    var inGlassToolbar = false

    @Namespace private var thumb
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var drawsTrack: Bool {
        if #available(iOS 26.0, *) { return !inGlassToolbar }
        return true
    }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(Array(options.enumerated()), id: \.offset) { _, option in
                segment(option.value, title: option.title)
            }
        }
        .padding(DS.Space.hairline)
        .background {
            if drawsTrack {
                Capsule().fill(DS.Colors.fill)
            }
        }
        .fixedSize()
    }

    private func segment(_ value: Value, title: LocalizedStringKey) -> some View {
        let isSelected = value == selection
        return Button {
            withAnimation(reduceMotion ? nil : .snappy(duration: 0.2)) { selection = value }
        } label: {
            Text(title)
                .font(DS.Typography.subheadline.weight(isSelected ? .semibold : .regular))
                .foregroundStyle(Color.primary)
                .lineLimit(1)
                .padding(.horizontal, DS.Space.ml)
                .frame(minHeight: DS.Size.segmentedHeight - DS.Space.hairline * 2)
                .background {
                    if isSelected {
                        Capsule()
                            .fill(DS.Colors.raisedSurface)
                            .shadow(color: .black.opacity(0.12), radius: 2, y: 1)
                            .matchedGeometryEffect(id: "thumb", in: thumb)
                    }
                }
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }
}
