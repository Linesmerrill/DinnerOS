import SwiftUI

// MARK: - Photos

/// A recipe photo, asked for at the size it's shown (`RecipeImageURL`) and kept in memory once
/// decoded (`ImageLoader`), with a shimmering placeholder for a slow load and a fallback glyph
/// when there's no image.
struct RecipePhoto: View {
    let url: URL?
    /// `nil` fills whatever frame the caller sets.
    var aspectRatio: CGFloat? = 4.0 / 3.0
    /// The width the photo is shown at, in points. Callers that can't know it — a hero sized by
    /// its container, a card sized by the layout — pass their best estimate and the view
    /// refines it once the frame is measured.
    var pointWidth: CGFloat = 360
    var cornerRadius: CGFloat = 18
    /// The widest bucket to ask the CDN for. Cards keep the default cap; the hero, the one
    /// photo shown full-screen, passes `nil` for the original.
    var maxBucket: Int? = ImageKey.cardBucketCap

    @Environment(\.displayScale) private var displayScale

    /// The measured width, once there is one. Until then `pointWidth` stands in, so the first
    /// frame asks the CDN for a card-sized photo rather than the 1200-pixel original.
    @State private var measuredWidth: CGFloat?

    var body: some View {
        sized
            .clipShape(.rect(cornerRadius: cornerRadius))
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: { width in
                guard width > 0 else { return }
                measuredWidth = width
            }
            .accessibilityHidden(true)
    }

    /// Only a measured width that lands in a different bucket changes the key, so refining the
    /// width usually costs nothing.
    private var key: ImageKey? {
        ImageKey(url: url, pointWidth: measuredWidth ?? pointWidth, scale: displayScale, maxBucket: maxBucket)
    }

    /// The photo's box, with the image filling it. The box's size never depends on the image:
    /// a loaded photo, the shimmer, and the fallback glyph are all drawn inside the same frame.
    @ViewBuilder
    private var sized: some View {
        if let aspectRatio {
            RecipePhotoFrame(aspectRatio: aspectRatio, idealWidth: pointWidth) {
                filled
            }
        } else {
            filled
        }
    }

    private var filled: some View {
        Rectangle()
            .fill(Color(.secondarySystemBackground))
            .overlay {
                CachedImage(key: key) {
                    ShimmerView()
                } fallback: {
                    fallbackGlyph
                }
            }
    }

    private var fallbackGlyph: some View {
        Image(systemName: "fork.knife")
            .font(.title2)
            .foregroundStyle(.tertiary)
    }
}

/// Sizes a photo from the width it's offered alone: the height is always `width / aspectRatio`,
/// whatever height the parent proposes.
///
/// `.aspectRatio(_, contentMode: .fit)` also honours a proposed height. A carousel card is a
/// `VStack` of the photo and rows under it, and a lazy horizontal stack proposes every card the
/// height it measured for the first few; a card with more rows under its photo (a past meal's
/// outcome and rating) left the photo less height, and `.fit` shrank it in both directions. So
/// neighbouring cards showed photos of different sizes. Ignoring the height makes every photo
/// in a row the same size, and the rows below take the space they need instead.
struct RecipePhotoFrame: Layout {
    let aspectRatio: CGFloat
    /// The width used when the parent proposes none, such as inside a horizontal scroll view.
    var idealWidth: CGFloat

    static func size(proposedWidth: CGFloat?, aspectRatio: CGFloat, idealWidth: CGFloat) -> CGSize {
        let width = proposedWidth.flatMap { $0.isFinite ? max($0, 0) : nil } ?? idealWidth
        return CGSize(width: width, height: aspectRatio > 0 ? width / aspectRatio : width)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        Self.size(proposedWidth: proposal.width, aspectRatio: aspectRatio, idealWidth: idealWidth)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for subview in subviews {
            subview.place(
                at: CGPoint(x: bounds.midX, y: bounds.midY), anchor: .center, proposal: ProposedViewSize(bounds.size))
        }
    }
}

/// A small round ingredient photo with a fallback glyph.
struct IngredientPhoto: View {
    let url: URL?
    var size: CGFloat = 48

    @Environment(\.displayScale) private var displayScale

    var body: some View {
        Circle()
            .fill(Color(.secondarySystemBackground))
            .frame(width: size, height: size)
            .overlay {
                CachedImage(key: ImageKey(url: url, pointWidth: size, scale: displayScale)) {
                    carrotGlyph
                } fallback: {
                    carrotGlyph
                }
            }
            .clipShape(.circle)
            .accessibilityHidden(true)
    }

    /// A thumbnail is too small for a shimmer to read as anything, so it waits behind the glyph.
    private var carrotGlyph: some View {
        Image(systemName: "carrot")
            .font(.system(size: size * 0.4))
            .foregroundStyle(.tertiary)
    }
}

/// A soft sweep over a placeholder while an image loads. Still under Reduce Motion.
struct ShimmerView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isAnimating = false

    var body: some View {
        LinearGradient(
            colors: [.clear, Color.primary.opacity(0.08), .clear], startPoint: .topLeading, endPoint: .bottomTrailing
        )
        .opacity(reduceMotion ? 0.5 : (isAnimating ? 1 : 0.2))
        .animation(
            reduceMotion ? nil : .easeInOut(duration: 1.1).repeatForever(autoreverses: true), value: isAnimating
        )
        .onAppear { isAnimating = true }
    }
}

// MARK: - Photo badges

/// A capsule over a photo.
///
/// A plain badge sits on a thick material, which is more opaque than the regular one and so keeps
/// its contrast over a bright photo rather than borrowing the photo's brightness. A prominent one
/// fills with the accent instead: accent-colored text on a material was about 4.3:1 in light and
/// 3.1:1 in dark at 12 points, both under AA, and the filled version is 5.1:1 and 8.1:1.
struct PhotoBadge: View {
    let text: String
    var systemImage: String?
    var isProminent = false

    var body: some View {
        Label {
            Text(text)
        } icon: {
            if let systemImage {
                Image(systemName: systemImage)
            }
        }
        .labelStyle(.titleAndIcon)
        .font(.caption.weight(.semibold))
        .foregroundStyle(isProminent ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(
            isProminent ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.thickMaterial), in: .capsule)
    }
}

/// "20 min" over a card's photo, with a bolt and the accent colour when the recipe is quick.
/// Medium and long times stay neutral, so the bolt means something.
struct TimeBadge: View {
    let minutes: Int
    var isQuick = false

    var body: some View {
        PhotoBadge(
            text: RecipeFormat.minutes(minutes), systemImage: isQuick ? "bolt.fill" : nil, isProminent: isQuick)
    }
}

/// Marks a planned add-on, so a week that reads "5 meals · 1 add-on" and the cards agree.
struct AddOnLabel: View {
    var body: some View {
        Text("Add-on")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(Color.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(.quaternary, in: .capsule)
    }
}

/// Which single badge a card shows over its photo.
nonisolated enum MenuCardBadge {
    /// Most worth knowing first. Only the first match is shown, so a card never stacks badges
    /// and every card in a row is the same shape.
    static let priority: [MenuBadgeCode] = [.autopilotPick, .makeAgain, .oftenOrdered]

    /// The one badge to show, or `nil` when none of the card's badges are worth the space.
    static func context(in badges: [MenuBadge]) -> MenuBadge? {
        for code in priority {
            if let badge = badges.first(where: { $0.code == code }) {
                return badge
            }
        }
        return nil
    }
}

// MARK: - Card shape

/// Keeps every card in a row the same height.
///
/// Cards used to grow with their text, so a long name or a missing subtitle made neighbours in
/// a carousel different heights. The photo has a fixed aspect ratio and the name a reserved
/// height, which leaves nothing that varies per card.
nonisolated enum MenuCardMetrics {
    /// Lines reserved for a card's name, or `nil` at accessibility sizes, where the name takes
    /// as many lines as it needs.
    ///
    /// Equal heights are a layout nicety; reading the name is the point of the card. At
    /// `.accessibility3` a carousel card is 300 points wide and a line holds three or four
    /// words, so clamping to two or three lines truncated most real names to "One-Pan Santa
    /// Fe…" — the failure #447 fixed at the default size, back again where the text is largest
    /// and the reader least able to guess the rest. The carousels lay their cards out
    /// `.top`-aligned and Your Meals stacks vertically at these sizes, so cards of different
    /// heights cost nothing there.
    static func titleLines(for size: DynamicTypeSize) -> Int? {
        size.isAccessibilitySize ? nil : 2
    }
}

/// A card's name, clamped and given a fixed height so cards line up — until accessibility sizes,
/// where it wraps freely instead.
struct CardTitle: View {
    let name: String

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    /// One line of `.headline`, with room to spare.
    ///
    /// This has to be at least what a line of `.headline` actually occupies — about 20.9 points
    /// at the default text size, once line spacing is counted. At 21 it wasn't: a two-line name
    /// was offered 42 points, needed a fraction more, and SwiftUI dropped the second line, so
    /// every long name read as "One-Pan Santa Fe Pork T…" in a box with an empty line under it.
    @ScaledMetric(relativeTo: .headline) private var lineHeight = 22

    var body: some View {
        let lines = MenuCardMetrics.titleLines(for: dynamicTypeSize)
        Text(name)
            .font(.headline)
            .foregroundStyle(Color.primary)
            .lineLimit(lines)
            .multilineTextAlignment(.leading)
            // Wrap to the width the card gives, and take the height those lines need rather than
            // the height the reserved box offers — `lineLimit` is what bounds it, not the frame.
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .frame(height: lines.map { lineHeight * CGFloat($0) }, alignment: .topLeading)
    }
}

// MARK: - Text and chips

/// A small capsule, for badges and tags.
///
/// A prominent chip fills with the accent rather than tinting text on a 14%-accent wash: at 11
/// points that wash was 4.4:1 in light and 3.6:1 in dark, and these chips sit over photography
/// where the backdrop is not something the app controls.
struct MenuChip: View {
    let text: String
    var systemImage: String?
    var isProminent = false

    var body: some View {
        Label {
            Text(text)
        } icon: {
            if let systemImage {
                Image(systemName: systemImage)
            }
        }
        .labelStyle(.titleAndIcon)
        .font(.caption2.weight(.semibold))
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .foregroundStyle(isProminent ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.secondary))
        .background(
            isProminent ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.quaternary), in: .capsule)
    }
}

// MARK: - Controls

/// "− 4 servings +" on a card, or the recipe screen's wider bar. Minus at the smallest size
/// removes the meal, which the caller confirms with an undo toast.
struct ServingsStepper: View {
    enum Style {
        /// A compact capsule on a card.
        case card
        /// The recipe screen's full-width bar.
        case bar
    }

    let label: String
    var style: Style = .card
    var isBusy = false
    var canIncrease = true
    let decrease: () -> Void
    let increase: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The smallest a control may be and still be reliably tappable. The card stepper's buttons
    /// were 24 points square, which is not something someone with a tremor can hit between a
    /// minus and a plus eight points apart.
    static let minimumTapTarget: CGFloat = 44

    var body: some View {
        HStack(spacing: style == .bar ? 12 : 8) {
            button(systemImage: "minus", label: String(localized: "Fewer servings"), action: decrease)
            Group {
                if isBusy {
                    ProgressView()
                        .tint(foreground)
                } else {
                    Text(label)
                        .font(style == .bar ? .headline : .footnote.weight(.semibold))
                        .monospacedDigit()
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                }
            }
            .frame(maxWidth: style == .bar ? .infinity : nil)
            button(systemImage: "plus", label: String(localized: "More servings"), action: increase)
                .disabled(!canIncrease)
                .opacity(canIncrease ? 1 : 0.4)
        }
        .foregroundStyle(foreground)
        .padding(.horizontal, style == .bar ? 16 : 10)
        // The buttons carry the height now, so the capsule adds almost none of its own.
        .padding(.vertical, style == .bar ? 4 : 0)
        .background(.tint, in: .capsule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("Servings"))
        .accessibilityValue(Text(label))
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: increase()
            case .decrement: decrease()
            @unknown default: break
            }
        }
    }

    private var foreground: Color { .onAccent }

    private func button(systemImage: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.footnote.weight(.bold))
                .frame(minWidth: Self.minimumTapTarget, minHeight: Self.minimumTapTarget)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .disabled(isBusy)
    }
}

/// The floating add toggle on a card's photo: `+` to add the recipe, `✓` once it's in the week.
struct AddToWeekButton: View {
    let isInPlan: Bool
    let isBusy: Bool
    let recipeName: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Group {
                if isBusy {
                    ProgressView()
                } else {
                    Image(systemName: isInPlan ? "checkmark" : "plus")
                        .font(.subheadline.weight(.bold))
                }
            }
            .foregroundStyle(isInPlan ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(.tint))
            .frame(width: 32, height: 32)
            .background {
                Circle()
                    .fill(isInPlan ? AnyShapeStyle(.tint) : AnyShapeStyle(.regularMaterial))
            }
            .frame(width: 44, height: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(isBusy)
        .accessibilityLabel(
            isInPlan
                ? Text("Remove \(recipeName) from your week") : Text("Add \(recipeName) to your week"))
    }
}

// MARK: - Toast

/// "Added Skillet Tacos · Undo" above the bottom bar. It fades out on its own.
struct PlannerToastView: View {
    let toast: MealPlanner.Toast
    let undo: () -> Void
    let dismiss: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Long enough to reach for Undo, short enough to stay out of the way.
    private static let duration = Duration.seconds(5)

    var body: some View {
        HStack(spacing: 12) {
            Text(toast.message)
                .font(.subheadline)
                .foregroundStyle(Color.primary)
            if toast.undo != nil {
                Button("Undo", action: undo)
                    .font(.subheadline.weight(.semibold))
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.regularMaterial, in: .capsule)
        .overlay(Capsule().strokeBorder(.quaternary))
        .shadow(color: .black.opacity(0.12), radius: 8, y: 2)
        .padding(.bottom, 8)
        .transition(reduceMotion ? .opacity : .move(edge: .bottom).combined(with: .opacity))
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isSummaryElement)
        .task(id: toast.id) {
            AccessibilityNotification.Announcement(toast.message).post()
            try? await Task.sleep(for: Self.duration)
            dismiss()
        }
    }
}

// MARK: - Section chrome

/// A section title with an optional subtitle and a trailing link.
struct MenuSectionHeader<Trailing: View>: View {
    let title: String
    var subtitle: String?
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.title3.bold())
                    .accessibilityAddTraits(.isHeader)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(.subheadline)
                        .foregroundStyle(Color.secondary)
                }
            }
            Spacer(minLength: 8)
            trailing
        }
    }
}

extension MenuSectionHeader where Trailing == EmptyView {
    init(title: String, subtitle: String? = nil) {
        self.init(title: title, subtitle: subtitle) { EmptyView() }
    }
}

/// A short failure with a Try Again button, for one section.
struct MenuSectionError: View {
    let message: String
    let retry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            FormErrorLabel(message: message)
            Button("Try Again", action: retry)
                .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Grey card shapes while a section loads.
struct MenuCardPlaceholders: View {
    var count = 3
    var width: CGFloat = 220

    var body: some View {
        HStack(spacing: 12) {
            ForEach(0..<count, id: \.self) { _ in
                VStack(alignment: .leading, spacing: 8) {
                    RecipePhoto(url: nil, pointWidth: width)
                        .frame(width: width)
                    RoundedRectangle(cornerRadius: 4).fill(.quaternary).frame(width: width * 0.8, height: 12)
                    RoundedRectangle(cornerRadius: 4).fill(.quaternary).frame(width: width * 0.5, height: 10)
                }
            }
        }
        .redacted(reason: .placeholder)
        .accessibilityHidden(true)
    }
}

#Preview("Components, accessibility size") {
    ScrollView {
        VStack(alignment: .leading, spacing: 20) {
            CardTitle(name: "One-Pan Sample Santa Fe Pork Tacos with Charred Corn Salsa")
                .frame(width: 300)
                .border(.red.opacity(0.4))
            PhotoBadge(text: "20 min", systemImage: "bolt.fill", isProminent: true)
            MenuChip(text: "Thursday", systemImage: "calendar", isProminent: true)
            ServingsStepper(label: "4 servings", decrease: {}, increase: {})
            AddToWeekButton(isInPlan: true, isBusy: false, recipeName: "Tacos") {}
        }
        .padding()
    }
    .environment(\.dynamicTypeSize, .accessibility3)
}

#Preview("Components") {
    ScrollView {
        VStack(alignment: .leading, spacing: 20) {
            RecipePhoto(url: nil, pointWidth: 300)
                .frame(width: 300)
            MenuChip(text: "Autopilot Pick", systemImage: "sparkles", isProminent: true)
            MenuChip(text: "Quick", systemImage: "bolt.fill")
            ServingsStepper(label: "4 servings", decrease: {}, increase: {})
            ServingsStepper(label: "1 in your week (2 servings)", style: .bar, decrease: {}, increase: {})
            AddToWeekButton(isInPlan: false, isBusy: false, recipeName: "Tacos") {}
            AddToWeekButton(isInPlan: true, isBusy: false, recipeName: "Tacos") {}
            MenuCardPlaceholders()
        }
        .padding()
    }
}

/// Photos in a paging row, as Your Meals lays them out: loading, failed, and missing photos, with
/// and without badges, and cards with rows under their photo. Every photo is the same size.
#Preview("Carousel photos, mixed") {
    let urls: [URL?] =
        MenuPreviewData.cards.map(\.recipe.imageURL) + [nil, URL(string: "https://invalid.example/x.jpg")]
    ScrollView {
        ScrollView(.horizontal) {
            LazyHStack(alignment: .top, spacing: 12) {
                ForEach(Array(urls.enumerated()), id: \.offset) { index, url in
                    VStack(alignment: .leading, spacing: 8) {
                        RecipePhoto(url: url, pointWidth: 300)
                            .overlay(alignment: .bottomLeading) {
                                if index.isMultiple(of: 2) {
                                    TimeBadge(minutes: 25, isQuick: true)
                                        .padding(8)
                                }
                            }
                            .border(.red.opacity(0.5))
                        CardTitle(
                            name: index.isMultiple(of: 3)
                                ? "Sweet Chili Beef & Green Bean Bowls" : "Turkey Ragù Spaghetti")
                        if index.isMultiple(of: 3) {
                            ServingsStepper(label: "4 servings", decrease: {}, increase: {})
                            ServingsStepper(label: "2 servings", decrease: {}, increase: {})
                        }
                    }
                    .containerRelativeFrame(.horizontal) { width, _ in width * 0.84 }
                }
            }
            .padding(.horizontal, 16)
        }
    }
}
