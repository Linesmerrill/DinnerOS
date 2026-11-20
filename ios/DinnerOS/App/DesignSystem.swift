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
enum DS {
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

    func makeBody(configuration: Configuration) -> some View {
        Button(role: configuration.role, action: configuration.trigger) {
            DSButtonLabel(
                label: configuration.label, size: size, fullWidth: fullWidth, font: font,
                contentHeight: (size.height * scale - size.systemPadding).rounded())
        }
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .controlSize(size.controlSize)
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
            contentHeight: (size.height * scale).rounded() - 2)
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
