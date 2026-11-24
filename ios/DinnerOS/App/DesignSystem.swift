import SwiftUI
import UIKit

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
        /// Orange text and icons on a plain or orange-washed background: a darker orange
        /// (WarningText in the asset catalog) in light mode, where system orange was about 2.2:1
        /// on white, and system orange in dark mode. 4.5:1 or better in both (build 122).
        static let warningText = Color("WarningText")
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
        /// Disabled button fill and text: one look for every disabled action. Secondary, not
        /// tertiary: on a white bar in light mode the tertiary pair all but vanished (build 122).
        static let disabledFill = Color(.secondarySystemFill)
        /// An opaque gray (DisabledText in the asset catalog) with 4.5:1 or better on the
        /// disabled fill in light and dark: still gray, so it reads as off, but readable
        /// (secondaryLabel was about 3:1 in light mode, build 122 review 2).
        static let disabledText = Color("DisabledText")
        /// Gray text for a quiet line on a card, like an empty day's "Nothing planned": the same
        /// opaque gray, 4.5:1 or better on a card in light and dark, where the system's tertiary
        /// gray was about 1.8:1 (build 122 review 4).
        static let quietText = Color("DisabledText")
        /// An empty star in a rating: the same opaque gray, so an unrated star stays clear on a
        /// card in dark mode, where the system's secondary gray all but vanished (build 123).
        static let emptyStar = Color("DisabledText")
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
        /// The fallback glyph's symbol: a plain fork and knife, which reads as "food" for any
        /// ingredient. A carrot sat beside chicken thighs, pork chops and tofu (build 122).
        static let thumbnailGlyphSymbol = "fork.knife"
        /// The wash behind the bar's buttons, tab labels and status bar where they sit on a photo:
        /// the screen's own color, strong enough that their glyphs reach 4.5:1 on any photo, and
        /// one status bar style over the whole hero (build 122).
        static let heroScrim = Color(.systemBackground).opacity(0.86)
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
        /// How far the scrim under the floating tab bar fades in above the bar (`TabBarScrim`).
        static let tabBarScrimFade: CGFloat = 24
        /// A cuisine tile on "What do you like?": its usual width, its width at accessibility
        /// text sizes (one column), the narrowest that still fits a name like "Mediterranean",
        /// and how short it may get (a share of its width) so every tile fits (build 122).
        static let cuisineTileWidth: CGFloat = 150
        static let cuisineTileWidthLarge: CGFloat = 260
        static let cuisineTileMinWidth: CGFloat = 116
        static let cuisineTileMinHeightRatio: CGFloat = 0.6
        /// A search result's photo while the keyboard is up: small enough that the result, its
        /// name, time and Add all fit above the keyboard on the Duo's cover screen.
        static let searchThumbnail: CGFloat = 56
        /// The strip kept clear above the keyboard for its floating Done button, so Done never
        /// sits on a result.
        static let keyboardBar: CGFloat = 48
        /// How far content fades out at a pinned bar's edge (`DSEdgeFade`), so a row passing under
        /// the bar fades away instead of being cut in half by a hard line (build 122).
        static let edgeFade: CGFloat = 32
        /// A fade a list row tall, so the row passing under a pinned bar fades away whole rather
        /// than being sliced: above Open in Walmart, and the most the fade
        /// under the recipe page's picker and the cooking screen's bar grows to
        /// (`DSEdgeFade.height(scrolledUnder:)`) (build 122, Hark round 4 item 1).
        static let rowEdgeFade: CGFloat = 56
        /// The fade under a form's solid bar: shorter than a section header's space above its
        /// title, so a header lined up with the bar's edge still reads whole (build 122).
        static let formEdgeFade: CGFloat = 16
        /// Room left under a form's last section, so the field being typed in there and its help
        /// text can scroll clear of the fade above the keyboard (build 122 round 4).
        static let formEndRoom: CGFloat = edgeFade + DS.Space.s
        /// Room under the end of a list whose rows fade out whole at the bar above it (the cooking
        /// screen): when it stops at its end and moves back to show its top row or heading whole,
        /// a fade and a row's height at most, its last row still rests whole on screen with a gap
        /// under it (Hark's review, build 121 round 2: Lemon Wedges cut off at the bottom of an SE).
        static let listEndRoom: CGFloat = edgeFade + rowEdgeFade + DS.Space.l
        /// The narrowest a card in a sideways row gets so a short row fits the screen whole on
        /// an iPad instead of running a card off the edge (build 122 review 4).
        static let carouselCardMinWidth: CGFloat = 240
        /// How far the wash at the top of a recipe photo eases out below the bar's buttons
        /// (`RecipeHeroScrim`): long and eased, so it never ends in a visible band (build 122).
        static let heroScrimFade: CGFloat = 96
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
///
/// `solid` fills the bar with `background` on iOS 26 too, for a footer over photos that the
/// soft edge can't make readable (Autopilot setup's "Set Up Later" over the cuisine tiles,
/// build 122). Pair it with the same color as the sheet's `presentationBackground`.
private struct DSBottomBarModifier<Bar: View>: ViewModifier {
    let isShown: Bool
    let background: Color
    var solid = false
    let bar: Bar

    func body(content: Content) -> some View {
        if !isShown {
            content
        } else if #available(iOS 26.0, *) {
            content.safeAreaBar(edge: .bottom, spacing: 0) {
                DSBottomBar(background: solid ? background : nil, showsDivider: solid) { bar }
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
        isShown: Bool = true, background: Color = DS.Colors.barBackground, solid: Bool = false,
        @ViewBuilder _ bar: () -> Bar
    ) -> some View {
        modifier(DSBottomBarModifier(isShown: isShown, background: background, solid: solid, bar: bar()))
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
            // Not `.plain`: it fades a disabled label a second time, which left the disabled
            // colors far lighter than their tokens (build 122).
            .buttonStyle(DSDisabledLookStyle())
        }
    }
}

/// Shows a disabled button's label exactly as styled, with no extra fading of its own.
struct DSDisabledLookStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
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

// MARK: Edge fades

/// A fade from a pinned bar's color to clear, `DS.Size.edgeFade` tall, drawn just outside the
/// bar's edge so content scrolling under it fades out rather than being cut mid-line. Use it
/// through `.dsEdgeFade(_:)` on the pinned bar itself.
struct DSEdgeFade: View {
    /// The bar's edge the fade hangs from: `.bottom` for a bar pinned at the top (the fade runs
    /// down from it), `.top` for a bar pinned at the bottom (the fade runs up from it).
    let edge: VerticalEdge
    var color: Color = DS.Colors.barBackground
    var height: CGFloat = DS.Size.edgeFade

    var body: some View {
        LinearGradient(
            colors: edge == .bottom ? [color, color.opacity(0)] : [color.opacity(0), color],
            startPoint: .top, endPoint: .bottom
        )
        .frame(height: max(height, 0))
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    /// The fade under a pinned bar for content scrolled `scrolledUnder` points past where it
    /// rests: `DS.Size.edgeFade` at rest (the content's top inset, so its first row starts
    /// whole), growing with the scroll to `DS.Size.rowEdgeFade`, so whatever row is passing
    /// under the bar fades out over its whole height.
    nonisolated static func height(scrolledUnder: CGFloat) -> CGFloat {
        min(DS.Size.edgeFade + max(scrolledUnder, 0), DS.Size.rowEdgeFade)
    }
}

extension View {
    /// Hangs a `DSEdgeFade` off this pinned bar's `edge`, outside its frame, so it never changes
    /// the bar's layout. The screen's content needs an inset of the same height at that edge
    /// (`DS.Size.edgeFade`) so its last row can come fully clear of the fade.
    func dsEdgeFade(
        _ edge: VerticalEdge, color: Color = DS.Colors.barBackground, height: CGFloat = DS.Size.edgeFade
    ) -> some View {
        overlay(alignment: edge == .bottom ? .bottom : .top) {
            DSEdgeFade(edge: edge, color: color, height: height)
                .offset(y: edge == .bottom ? max(height, 0) : -max(height, 0))
        }
    }
}

// MARK: Row fades

extension EnvironmentValues {
    /// Where the bar pinned over a scrolling list ends, in the scroll view's own coordinates
    /// (`.scrollView`): rows marked `dsFadesAtBarEdge()` fade out whole before they reach it.
    /// `nil` when nothing is pinned over the list.
    @Entry var dsBarEdge: CGFloat? = nil
}

/// A list row that fades out as a whole on its way under a pinned bar: fully there until its top
/// comes within `DS.Size.edgeFade` of the bar's edge (where a list's first row rests, so nothing
/// fades at rest), gone by the time it reaches it. A
/// gradient at the bar's edge alone left the row it crossed with its top half missing and its
/// bottom half sharp on the Duo's short screen (Hark's review, build 122 round 5).
nonisolated enum DSRowEdgeFade {
    /// Rows taller than this (a step card with its photo) keep the bar's gradient instead: fading
    /// one whole would blank most of a screen.
    static let maxRowHeight: CGFloat = 2 * DS.Size.rowEdgeFade

    /// The row's opacity, from its top and height and the bar's edge (all in the scroll view's
    /// coordinates). 1 with no bar, or for a row too tall to fade whole.
    static func opacity(top: CGFloat, height: CGFloat, barEdge: CGFloat?) -> Double {
        guard let barEdge, height <= maxRowHeight else { return 1 }
        return Double(min(max((top - barEdge) / DS.Size.edgeFade, 0), 1))
    }

    /// Where a scroll view's rows meet the bar over it, in the scroll view's own coordinates
    /// (`.scrollView`), from its scroll geometry: `nil` (no fade) while its content rests at the
    /// top. That space starts below the scroll view's top inset (the navigation bar on the iPad
    /// cooking screen), so the edge is 0 there: using the inset itself as the edge faded the
    /// first rows out at rest on iPad (build 122 round 5).
    static func barEdge(contentOffset: CGFloat, topInset: CGFloat) -> CGFloat? {
        contentOffset + topInset > 0.5 ? 0 : nil
    }

    /// How far to move a list that came to rest with a row part way into the fade at the bar's
    /// edge (`rows` are the fading rows' tops and heights, in the scroll view's coordinates), so
    /// every row on screen is whole and fully drawn at rest (Hark's review, build 122 round 5: the
    /// Allergens line rested under the fade at 1.5:1, and an SE's ingredients started under a
    /// blank band). Negative moves the content back down, bringing the row whole into view;
    /// positive moves on to the next row, when the row is mostly gone and that's the shorter
    /// move, never past `maxForward`. `nil` when nothing rests in the fade.
    ///
    /// A heading (its top in `headings`) always comes back whole instead: moving on past it
    /// left the rows under it with no heading and a blank band above them, since a heading above
    /// the bar's edge is drawn fully faded (Hark's review, build 121 round 2: "Step 1" gone above
    /// ¼ tsp Turmeric on an SE).
    static func restingShift(
        rows: [(top: CGFloat, height: CGFloat)], barEdge: CGFloat?, maxForward: CGFloat = .infinity,
        headings: [CGFloat] = []
    ) -> CGFloat? {
        guard let barEdge else { return nil }
        let rest = barEdge + DS.Size.edgeFade
        let showing = rows.filter { $0.height <= maxRowHeight && $0.top + $0.height > barEdge + 0.5 }
        guard let first = showing.min(by: { $0.top < $1.top }), first.top < rest - 0.5 else { return nil }
        let back = first.top - rest
        // The next row down, brought to where a list's first row rests.
        let next = rows.filter { $0.top >= first.top + first.height - 0.5 }.map(\.top).min()
        let isHeading = headings.contains { abs($0 - first.top) < 0.5 }
        // Mostly under the bar already: the next row comes up instead, when that's the shorter move.
        var shift = back
        if !isHeading, let next, first.top + first.height / 2 < barEdge, next - rest < -back, next - rest <= maxForward
        {
            shift = next - rest
        }
        // Where the list then rests, a heading partly below the bar's edge would be drawn faded
        // out over its first row ("Step 4" over 1 cup Grape Tomatoes at the end of an SE's list):
        // the list comes back to show that heading whole instead.
        let hidden = rows.filter { row in
            headings.contains { abs($0 - row.top) < 0.5 } && row.top - shift < rest - 0.5
                && row.top - shift + row.height > barEdge + 0.5
        }
        if let heading = hidden.min(by: { $0.top < $1.top }) {
            shift = heading.top - rest
        }
        return shift
    }
}

/// Where a list's fading rows sit in its content (the coordinate space `space`, on the scroll
/// view's content), for `restingShift`. Places in the content don't move as the list scrolls, so
/// they're measured once; a plain class, so measuring never redraws anything.
@MainActor final class DSRowFrames {
    /// The coordinate space on a list's content that `dsRowsContent()` names.
    nonisolated static let contentSpace = "DSRowsContent"

    let space: String
    private var frames: [UUID: (top: CGFloat, height: CGFloat)] = [:]
    private var headingIDs: Set<UUID> = []

    init(space: String = DSRowFrames.contentSpace) {
        self.space = space
    }

    /// The rows as they sit on screen, in the scroll view's own coordinates (`.scrollView`), for
    /// content scrolled `scrolled` points up from its resting place (its offset and top inset).
    func rows(scrolled: CGFloat) -> [(top: CGFloat, height: CGFloat)] {
        frames.values.map { ($0.top - scrolled, $0.height) }
    }

    /// The headings' tops, as `rows(scrolled:)` gives them, for `restingShift(headings:)`.
    func headingTops(scrolled: CGFloat) -> [CGFloat] {
        headingIDs.compactMap { frames[$0].map { $0.top - scrolled } }
    }

    func set(_ id: UUID, _ frame: (top: CGFloat, height: CGFloat)?, isHeading: Bool = false) {
        frames[id] = frame
        if isHeading, frame != nil { headingIDs.insert(id) } else { headingIDs.remove(id) }
    }
}

extension EnvironmentValues {
    /// Where a list's `dsFadesAtBarEdge()` rows report their places (`DSRowFrames`).
    @Entry var dsRowFrames: DSRowFrames? = nil
}

private struct DSRowEdgeFadeModifier: ViewModifier {
    var isHeading = false
    @Environment(\.dsBarEdge) private var barEdge
    @Environment(\.dsRowFrames) private var rowFrames
    @State private var id = UUID()

    func body(content: Content) -> some View {
        content.visualEffect { [barEdge] content, proxy in
            let frame = proxy.frame(in: .scrollView)
            return content.opacity(DSRowEdgeFade.opacity(top: frame.minY, height: frame.height, barEdge: barEdge))
        }
        .onGeometryChange(for: CGRect.self) { [space = rowFrames?.space ?? DSRowFrames.contentSpace] proxy in
            proxy.frame(in: .named(space))
        } action: { [isHeading] frame in
            rowFrames?.set(id, (frame.minY, frame.height), isHeading: isHeading)
        }
        .onDisappear { rowFrames?.set(id, nil) }
    }
}

/// Hands a scroll view's rows its top edge as `dsBarEdge` while its content is scrolled up under
/// that edge, and nothing at rest, so nothing fades when the screen opens (Hark's review, build 122
/// round 5: the iPad cooking screen opened with Step 1 and the photo faded out). When the list
/// comes to rest with a row part way into the fade, it moves to show that row whole.
private struct DSScrollBarEdge: ViewModifier {
    @State private var barEdge: CGFloat?
    @State private var rowFrames = DSRowFrames()
    @State private var position = ScrollPosition()

    func body(content: Content) -> some View {
        content
            .onScrollGeometryChange(for: CGFloat?.self) {
                DSRowEdgeFade.barEdge(contentOffset: $0.contentOffset.y, topInset: $0.contentInsets.top)
            } action: { _, edge in
                if edge != barEdge { barEdge = edge }
            }
            .scrollPosition($position)
            .onScrollPhaseChange { oldPhase, phase, context in
                // Once, after a person's scroll: never again after its own move.
                guard phase == .idle, oldPhase != .animating else { return }
                let geometry = context.geometry
                let scrolled = geometry.contentOffset.y + geometry.contentInsets.top
                let farthest =
                    geometry.contentSize.height + geometry.contentInsets.bottom - geometry.containerSize.height
                guard
                    let shift = DSRowEdgeFade.restingShift(
                        rows: rowFrames.rows(scrolled: scrolled), barEdge: barEdge,
                        maxForward: farthest - geometry.contentOffset.y,
                        headings: rowFrames.headingTops(scrolled: scrolled))
                else { return }
                // A scroll position counts from the content's resting place, below the top inset.
                withAnimation(.snappy) { position.scrollTo(y: scrolled + shift) }
            }
            .environment(\.dsBarEdge, barEdge)
            .environment(\.dsRowFrames, rowFrames)
    }
}

extension View {
    /// Fades this row out whole before it reaches the bar pinned over its list (`dsBarEdge`), so
    /// it's never cut in half by the bar's edge. Only moves its drawing; the layout is unchanged.
    /// A `heading` is never left faded out above the rows under it when the list stops: the list
    /// moves back to show it whole (`DSRowEdgeFade.restingShift(headings:)`).
    func dsFadesAtBarEdge(heading: Bool = false) -> some View {
        modifier(DSRowEdgeFadeModifier(isHeading: heading))
    }

    /// On a scroll view whose rows pass under a bar at its top edge (a pinned picker above it, or
    /// the navigation bar): its `dsFadesAtBarEdge()` rows fade out at that edge.
    func dsRowsFadeAtTopEdge() -> some View {
        modifier(DSScrollBarEdge())
    }

    /// On the content inside a scroll view marked `dsRowsFadeAtTopEdge()`: where its rows are
    /// measured from, so a list that stops with a row half faded can move to show it whole.
    func dsRowsContent() -> some View {
        coordinateSpace(.named(DSRowFrames.contentSpace))
    }
}

extension View {
    /// For a scrolling pane under a system navigation bar: fills the bar's area with `color` and
    /// hangs a `DSEdgeFade` below it, so rows fade out under the bar instead of showing through
    /// its buttons or being cut by its edge. Pad the pane's content `height` at the top so its
    /// first row starts clear of the fade (build 122). For two panes of different colors under
    /// one bar (the iPad cooking screen); a single form uses `dsSolidBarFade`.
    func dsTopBarEdge(color: Color = DS.Colors.barBackground, height: CGFloat = DS.Size.edgeFade) -> some View {
        safeAreaInset(edge: .top, spacing: 0) {
            Color.clear
                .frame(height: 0)
                .background { color.ignoresSafeArea(edges: .top) }
                .dsEdgeFade(.bottom, color: color, height: height)
        }
    }

    /// A form under a navigation bar: the bar itself turns solid `color` (drawn by the system,
    /// behind its buttons) and a `DSEdgeFade` hangs below it, so rows fade out under the bar and
    /// nothing draws over Cancel or the title. The bar stays put as the form scrolls: on iPhone
    /// Duo's cover screen it otherwise slid away, taking Cancel with it (build 122).
    func dsSolidBarFade(color: Color, height: CGFloat = DS.Size.formEdgeFade) -> some View {
        modifier(DSPinnedNavigationBar())
            // Up to the screen's top edge too: on iPhone Duo's cover screen a strip above the bar
            // showed the screen's lighter top (Hark's review, build 122 round 4).
            .background { color.ignoresSafeArea() }
            .toolbarBackground(color, for: .navigationBar)
            .toolbarBackgroundVisibility(.visible, for: .navigationBar)
            .safeAreaInset(edge: .top, spacing: 0) {
                Color.clear
                    .frame(height: 0)
                    .dsEdgeFade(.bottom, color: color, height: height)
            }
            .environment(\.dsTopFade, height)
    }

    /// While the keyboard is up, fills the row the keyboard's floating Done sits in with `color`
    /// (with a fade above it), so Done sits on a solid bar and never on a field, and brings the
    /// field being typed in up whole, with its help text, above the fade (build 122). The bar is
    /// placed from the keyboard's own frame, whose top is the top of Done's row: the safe area
    /// put it below Done on a large iPhone (round 4). Without a Done row (an iPad sheet) the bar
    /// sits behind the keyboard and only the fade shows.
    func dsKeyboardBar(color: Color = DS.Colors.barBackground) -> some View {
        modifier(DSKeyboardBar(color: color))
    }
}

private struct DSKeyboardBar: ViewModifier {
    let color: Color
    /// The top of the keyboard, Done's row included, in screen points, while it's up.
    @State private var keyboardTop: CGFloat?
    @Environment(\.dsTopFade) private var topFade

    func body(content: Content) -> some View {
        content
            .overlay {
                if let keyboardTop {
                    GeometryReader { proxy in
                        let frame = proxy.frame(in: .global)
                        // The keyboard's top, in this view's space.
                        let top = keyboardTop - frame.minY
                        if top > 0 && top < frame.height {
                            Rectangle()
                                .fill(color)
                                .frame(width: frame.width, height: DS.Size.keyboardBar)
                                .dsEdgeFade(.top, color: color)
                                .offset(y: top)
                        }
                    }
                    .ignoresSafeArea()
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) {
                keyboardTop = Self.top(of: $0)
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillChangeFrameNotification)) {
                if keyboardTop != nil { keyboardTop = Self.top(of: $0) }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
                keyboardTop = nil
            }
            // Once the keyboard is in place, the field being typed in comes up whole above the
            // bar's fade (a form leaves it partly under Done).
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidShowNotification)) {
                keyboardTop = Self.top(of: $0)
                reveal()
            }
            // Moving to a field with another keyboard (numbers, then text with suggestions)
            // changes its height: bring the field up again once it has.
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidChangeFrameNotification)) {
                guard keyboardTop != nil, let top = Self.top(of: $0) else { return }
                keyboardTop = top
                reveal()
            }
            .onReceive(NotificationCenter.default.publisher(for: UITextField.textDidBeginEditingNotification)) { _ in
                if keyboardTop != nil { reveal() }
            }
            .onReceive(NotificationCenter.default.publisher(for: UITextView.textDidBeginEditingNotification)) { _ in
                if keyboardTop != nil { reveal() }
            }
    }

    private func reveal() {
        DSKeyboardReveal.revealFocusedField(topFade: topFade, clearBottom: keyboardTop.map { $0 - DS.Size.edgeFade })
    }

    private static func top(of note: Notification) -> CGFloat? {
        (note.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? CGRect)?.minY
    }
}

/// Finds the field being typed in and works out how far to scroll so it shows whole between a
/// form's top and the keyboard's Done strip.
enum DSKeyboardReveal {
    /// Scrolls so the card holding the field being typed in (every row of its section) shows
    /// whole between the bar's fade and the fade above the keyboard's Done bar, and so no header
    /// or card is left half under the top bar.
    /// `clearBottom`, in screen points, is the lowest the card may reach (above the keyboard's
    /// Done bar and its fade) when the caller knows it.
    @MainActor static func revealFocusedField(topFade: CGFloat, clearBottom: CGFloat? = nil) {
        guard let field = focusedField(), let scrollView = scrollView(containing: field),
            let window = field.window
        else { return }
        let frame = scrollView.convert(scrollView.bounds, to: window)
        let top = frame.minY + scrollView.adjustedContentInset.top
        var bottom = frame.maxY - scrollView.adjustedContentInset.bottom - DS.Size.edgeFade
        if let clearBottom {
            // The keyboard's own frame says where the clear space ends: the scroll view's inset
            // also counts room left under the last row (`DS.Size.formEndRoom`), which must not
            // shrink it. The app's window fills the screen, so screen and window points agree.
            bottom = clearBottom
        }
        var card = field.convert(field.bounds, to: window)
        var pieces: [ClosedRange<CGFloat>] = []
        if let list = scrollView as? UICollectionView, let section = section(of: field, in: list) {
            let everything = CGRect(origin: .zero, size: list.contentSize)
            let all = list.collectionViewLayout.layoutAttributesForElements(in: everything) ?? []
            var cards: [Int: CGRect] = [:]
            var footer: CGRect?
            for item in all where item.frame.height > 0 {
                let onScreen = list.convert(item.frame, to: window)
                if item.representedElementCategory == .cell {
                    cards[item.indexPath.section] = cards[item.indexPath.section]?.union(onScreen) ?? onScreen
                } else if item.indexPath.section == section,
                    item.representedElementKind == UICollectionView.elementKindSectionFooter
                {
                    // The field's help text comes up with it (round 4).
                    footer = onScreen
                } else {
                    pieces.append(onScreen.minY...onScreen.maxY)
                }
            }
            pieces += cards.values.map { $0.minY...$0.maxY }
            if let whole = cards[section] { card = footer.map(whole.union) ?? whole }
        }
        let lowest = -scrollView.adjustedContentInset.top
        let highest = max(
            scrollView.contentSize.height + scrollView.adjustedContentInset.bottom - scrollView.bounds.height, lowest)
        let offset = scrollView.contentOffset.y
        guard
            let move = shift(
                card: card.minY...card.maxY, pieces: pieces, visible: top...bottom, topFade: topFade,
                margin: DS.Space.s, allowed: (lowest - offset)...(highest - offset))
        else { return }
        scrollView.setContentOffset(CGPoint(x: scrollView.contentOffset.x, y: offset + move), animated: true)
    }

    /// How far to scroll (positive moves content up) so `card` sits whole between `topFade` below
    /// the bar and `margin` above the bottom of `visible`, choosing, of the moves that do, the
    /// smallest that leaves no piece (a header, footer or card) straddling the bar's edge at
    /// `visible`'s top. Nil when nothing needs to move. Moves stay inside `allowed`.
    static func shift(
        card: ClosedRange<CGFloat>, pieces: [ClosedRange<CGFloat>], visible: ClosedRange<CGFloat>,
        topFade: CGFloat, margin: CGFloat, allowed: ClosedRange<CGFloat>
    ) -> CGFloat? {
        let edge = visible.lowerBound
        // At least this far, so the card's bottom clears the bottom; at most this far, so its top
        // stays clear of the fade under the bar.
        let least = card.upperBound - (visible.upperBound - margin)
        let most = card.lowerBound - (edge + topFade)
        func straddles(_ move: CGFloat) -> Bool {
            pieces.contains { $0.lowerBound - move < edge && $0.upperBound - move > edge }
        }
        func fits(_ move: CGFloat) -> Bool {
            move >= least && move <= most && allowed.contains(move)
        }
        if fits(0) && !straddles(0) { return nil }
        let candidates: [CGFloat] = [0] + pieces.flatMap { [$0.lowerBound - edge, $0.upperBound - edge] }
        if let best = candidates.filter({ fits($0) && !straddles($0) }).min(by: { abs($0) < abs($1) }) {
            return best.nonZero
        }
        // No clean stop: just bring the card in, its top first when it can't all fit.
        if fits(0) { return nil }
        let move = least > most ? most : max(least, min(0, most))
        return min(max(move, allowed.lowerBound), allowed.upperBound).nonZero
    }

    @MainActor private static func section(of view: UIView, in list: UICollectionView) -> Int? {
        var current: UIView? = view
        while let candidate = current {
            if let cell = candidate as? UICollectionViewCell { return list.indexPath(for: cell)?.section }
            current = candidate.superview
        }
        return nil
    }

    @MainActor static func focusedField() -> UIView? {
        let windows = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
        for window in windows where window.isKeyWindow {
            if let found = firstResponder(in: window) { return found }
        }
        return nil
    }

    @MainActor private static func firstResponder(in view: UIView) -> UIView? {
        if view.isFirstResponder { return view }
        for subview in view.subviews {
            if let found = firstResponder(in: subview) { return found }
        }
        return nil
    }

    @MainActor static func scrollView(containing view: UIView) -> UIScrollView? {
        var current = view.superview
        while let candidate = current {
            if let scrollView = candidate as? UIScrollView, scrollView.isScrollEnabled,
                !(candidate is UITextView)
            {
                return scrollView
            }
            current = candidate.superview
        }
        return nil
    }
}

extension CGFloat {
    /// Nil for zero, so "no move needed" reads as nil.
    fileprivate var nonZero: CGFloat? { self == 0 ? nil : self }
}

extension EnvironmentValues {
    /// How tall the fade under the bar is (`dsSolidBarFade`), so the keyboard's reveal keeps the
    /// field being typed in clear of it.
    @Entry var dsTopFade: CGFloat = DS.Size.edgeFade
}

/// Keeps the navigation bar from minimizing as the content scrolls (iOS 27).
private struct DSPinnedNavigationBar: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 27.0, *) {
            content.toolbarMinimizationBehavior(.never, for: .navigationBar)
        } else {
            content
        }
    }
}
