import SwiftUI

// MARK: - Photos

/// A recipe photo, asked for at the size it's shown (`RecipeImageURL`), with a shimmering
/// placeholder while it loads and a fallback glyph when there's no image.
struct RecipePhoto: View {
    let url: URL?
    /// `nil` fills whatever frame the caller sets.
    var aspectRatio: CGFloat? = 4.0 / 3.0
    /// The width the photo is shown at, in points.
    var pointWidth: CGFloat = 360
    var cornerRadius: CGFloat = 18

    @Environment(\.displayScale) private var displayScale
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        placeholder
            .overlay {
                AsyncImage(
                    url: sizedURL, transaction: Transaction(animation: reduceMotion ? nil : .easeIn(duration: 0.2))
                ) {
                    phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFill()
                    case .failure:
                        fallbackGlyph
                    case .empty:
                        if url == nil {
                            fallbackGlyph
                        } else {
                            ShimmerView()
                        }
                    @unknown default:
                        fallbackGlyph
                    }
                }
            }
            .clipShape(.rect(cornerRadius: cornerRadius))
            .accessibilityHidden(true)
    }

    private var sizedURL: URL? {
        RecipeImageURL.sized(url, pointWidth: pointWidth, scale: displayScale)
    }

    @ViewBuilder
    private var placeholder: some View {
        let base = Rectangle().fill(Color(.secondarySystemBackground))
        if let aspectRatio {
            base.aspectRatio(aspectRatio, contentMode: .fit)
        } else {
            base
        }
    }

    private var fallbackGlyph: some View {
        Image(systemName: "fork.knife")
            .font(.title2)
            .foregroundStyle(.tertiary)
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
                AsyncImage(url: RecipeImageURL.sized(url, pointWidth: size, scale: displayScale)) { phase in
                    if let image = phase.image {
                        image
                            .resizable()
                            .scaledToFill()
                    } else {
                        Image(systemName: "carrot")
                            .font(.system(size: size * 0.4))
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            .clipShape(.circle)
            .accessibilityHidden(true)
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

// MARK: - Text and chips

/// "30 min · 690 cal · 36g protein". Hidden when nothing is known.
struct FactsRow: View {
    let minutes: Int?
    let calories: Int?
    let proteinGrams: Int?

    init(minutes: Int?, calories: Int?, proteinGrams: Int?) {
        self.minutes = minutes
        self.calories = calories
        self.proteinGrams = proteinGrams
    }

    init(summary: RecipeSummary) {
        self.init(minutes: summary.displayMinutes, calories: summary.calories, proteinGrams: summary.proteinGrams)
    }

    var body: some View {
        let text = MenuFormat.factsText(minutes: minutes, calories: calories, proteinGrams: proteinGrams)
        if !text.isEmpty {
            Text(text)
                .font(.footnote)
                .foregroundStyle(Color.secondary)
                .lineLimit(2)
        }
    }
}

/// A small capsule, for badges and tags.
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
        .foregroundStyle(isProminent ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.secondary))
        .background(isProminent ? AnyShapeStyle(.tint.opacity(0.14)) : AnyShapeStyle(.quaternary), in: .capsule)
    }
}

/// A card's badges, wrapping at large text sizes.
struct BadgeRow: View {
    let badges: [MenuBadge]
    var limit = 3

    var body: some View {
        if !badges.isEmpty {
            ChipFlowLayout(spacing: 6) {
                ForEach(badges.prefix(limit)) { badge in
                    MenuChip(
                        text: badge.text, systemImage: badge.code.systemImage,
                        isProminent: badge.code == .autopilotPick)
                }
            }
            .accessibilityElement(children: .combine)
        }
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
        .padding(.vertical, style == .bar ? 10 : 5)
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

    private var foreground: Color { .white }

    private func button(systemImage: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.footnote.weight(.bold))
                .frame(minWidth: style == .bar ? 30 : 24, minHeight: style == .bar ? 30 : 24)
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
            .foregroundStyle(isInPlan ? AnyShapeStyle(Color.white) : AnyShapeStyle(.tint))
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

#Preview("Components") {
    ScrollView {
        VStack(alignment: .leading, spacing: 20) {
            RecipePhoto(url: nil, pointWidth: 300)
                .frame(width: 300)
            BadgeRow(badges: [
                MenuBadge(code: .autopilotPick, text: "Autopilot Pick"), MenuBadge(code: .quick, text: "Quick"),
                MenuBadge(code: MenuBadgeCode(rawValue: "chef"), text: "Chef's Pick"),
            ])
            FactsRow(minutes: 30, calories: 690, proteinGrams: 36)
            ServingsStepper(label: "4 servings", decrease: {}, increase: {})
            ServingsStepper(label: "1 in your week (2 servings)", style: .bar, decrease: {}, increase: {})
            AddToWeekButton(isInPlan: false, isBusy: false, recipeName: "Tacos") {}
            AddToWeekButton(isInPlan: true, isBusy: false, recipeName: "Tacos") {}
            MenuCardPlaceholders()
        }
        .padding()
    }
}
