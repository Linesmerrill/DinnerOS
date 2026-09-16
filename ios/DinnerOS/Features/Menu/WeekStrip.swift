import SwiftUI
import UIKit

/// How tall the week strip is, and how large its text is allowed to get.
///
/// The strip is chrome pinned as a top safe-area inset, and a horizontal `ScrollView` takes every
/// point of height it is offered — which as a safe-area inset is the whole screen — so the height
/// has to be set explicitly. Scaling that height freely is the other half of the same trap: at
/// `.accessibility5` a `@ScaledMetric` of 62 points became about 210, and a strip that tall sits
/// above every screen in the Menu tab and leaves a phone showing little else.
///
/// So the pills stop growing at `largestTypeSize`, and the reserved height stops with them — the
/// two are computed from the same clamp, so the pills can't outgrow the box that holds them.
/// Nothing lives only here: the shown week's dates are in the bottom bar at full size, the pill
/// names repeat in Past Weeks, and VoiceOver reads each pill's name, dates, and counts whatever
/// the text size is.
nonisolated enum WeekStripMetrics {
    /// The largest text size the strip renders at. One step into the accessibility range.
    static let largestTypeSize = DynamicTypeSize.accessibility1

    /// `WeekPill`'s own `VStack` spacing and vertical padding. They belong to the pill; the
    /// reservation has to include them, so changing one means changing the other.
    private static let lineSpacing: CGFloat = 2
    private static let verticalPadding: CGFloat = 8

    /// The text styles `WeekPill` stacks, top to bottom.
    private static let lineStyles: [UIFont.TextStyle] = [.caption2, .subheadline, .caption2]

    /// The height to reserve for the pills at `size`: what the three lines actually measure at
    /// the clamped text size, plus the pill's own spacing and padding.
    ///
    /// Scaling one magic number was what made the first version wrong in both directions — 62
    /// points scaled by `.footnote` grew to about 210 at `.accessibility5`, and clamping that
    /// same number still under-reserved what the pill draws, so pills clipped instead. Adding up
    /// the real line heights can't drift from the pill: whatever the text size, the box is as
    /// tall as its contents.
    static func height(for size: DynamicTypeSize) -> CGFloat {
        let traits = UITraitCollection(
            preferredContentSizeCategory: contentSizeCategory(for: min(size, largestTypeSize)))
        let lines =
            lineStyles
            .map { UIFont.preferredFont(forTextStyle: $0, compatibleWith: traits).lineHeight }
            .reduce(0, +)
        return (lines + lineSpacing * 2 + verticalPadding * 2).rounded(.up)
    }

    private static func contentSizeCategory(for size: DynamicTypeSize) -> UIContentSizeCategory {
        switch size {
        case .xSmall: .extraSmall
        case .small: .small
        case .medium: .medium
        case .large: .large
        case .xLarge: .extraLarge
        case .xxLarge: .extraExtraLarge
        case .xxxLarge: .extraExtraExtraLarge
        case .accessibility1: .accessibilityMedium
        case .accessibility2: .accessibilityLarge
        case .accessibility3: .accessibilityExtraLarge
        case .accessibility4: .accessibilityExtraExtraLarge
        case .accessibility5: .accessibilityExtraExtraExtraLarge
        @unknown default: .large
        }
    }
}

/// The week pills across the top of the Menu screen: a **Past** link, then one pill per week
/// with its dates and counts. It scrolls to the selected week and loads earlier weeks when
/// scrolled back, down to the household's first week.
struct WeekStrip: View {
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var scrolledWeek: String?

    /// What the pills need at this text size, capped so the strip can't take the screen.
    private var pillHeight: CGFloat {
        WeekStripMetrics.height(for: dynamicTypeSize)
    }

    var body: some View {
        HStack(spacing: 8) {
            NavigationLink(value: PastWeeksRoute()) {
                Label("Past", systemImage: "clock.arrow.circlepath")
                    .font(.footnote.weight(.semibold))
                    .labelStyle(.titleAndIcon)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 8)
                    .background(.quaternary, in: .capsule)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.tint)
            .accessibilityHint("Shows earlier weeks")
            .padding(.leading, 16)

            ScrollView(.horizontal) {
                LazyHStack(spacing: 8) {
                    if menu.canLoadEarlierWeeks {
                        ProgressView()
                            .frame(width: 44)
                            .task(id: menu.oldestWeek) {
                                await menu.loadEarlierWeeks()
                            }
                            .accessibilityLabel("Loading earlier weeks")
                    }
                    ForEach(menu.stripItems) { item in
                        Button {
                            select(item.week)
                        } label: {
                            WeekPill(
                                item: item, isSelected: item.week == plans.week,
                                currentWeek: menu.currentWeek)
                        }
                        .buttonStyle(.plain)
                        .id(item.id)
                    }
                }
                .scrollTargetLayout()
                .padding(.trailing, 16)
            }
            .scrollIndicators(.hidden)
            .scrollPosition(id: $scrolledWeek, anchor: .center)
            // A horizontal ScrollView takes all the height it is offered, and as a top
            // safe-area inset that is the whole screen, so pin it to the pills' height.
            .frame(height: pillHeight)
        }
        // The pills stop growing where `pillHeight` stops, so they always fit what it reserves.
        .dynamicTypeSize(...WeekStripMetrics.largestTypeSize)
        .padding(.vertical, 8)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
        .onChange(of: plans.week, initial: true) { _, week in
            scrollTo(week)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Weeks")
    }

    private func select(_ week: ISOWeek) {
        guard week != plans.week else { return }
        // `PlanStore` is the week everything follows; the Menu screen loads the menu from it.
        Task { await plans.show(week: week) }
    }

    private func scrollTo(_ week: ISOWeek) {
        guard scrolledWeek != week.description else { return }
        if reduceMotion {
            scrolledWeek = week.description
        } else {
            withAnimation(.easeInOut(duration: 0.25)) {
                scrolledWeek = week.description
            }
        }
    }
}

/// One week in the strip: "This Week", its dates, and a small count.
struct WeekPill: View {
    let item: WeekStripItem
    let isSelected: Bool
    let currentWeek: ISOWeek

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(relativeName ?? item.week.monthAndYear())
                .font(.caption2.weight(.semibold))
                .foregroundStyle(
                    isSelected ? AnyShapeStyle(Color.onAccent.opacity(0.9)) : AnyShapeStyle(Color.secondary))
            Text(item.week.rangeLabel())
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(isSelected ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
            detail
        }
        .lineLimit(1)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.secondarySystemBackground)),
            in: .rect(cornerRadius: 14)
        )
        .overlay {
            if item.timing == .current, !isSelected {
                RoundedRectangle(cornerRadius: 14).strokeBorder(.tint, lineWidth: 1.5)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    private var relativeName: String? {
        MenuFormat.relativeWeekName(item.week, current: currentWeek)
    }

    @ViewBuilder
    private var detail: some View {
        HStack(spacing: 4) {
            if item.timing == .past, (item.summary?.cookedCount ?? 0) > 0 {
                Image(systemName: "checkmark.circle.fill")
                    .font(.caption2)
            }
            Text(MenuFormat.weekPillDetail(item.summary, timing: item.timing) ?? " ")
                .font(.caption2)
        }
        .foregroundStyle(isSelected ? AnyShapeStyle(Color.onAccent.opacity(0.9)) : AnyShapeStyle(Color.secondary))
    }

    private var accessibilityLabel: String {
        var parts = [relativeName ?? item.week.rangeLabel()]
        if relativeName != nil {
            parts.append(item.week.rangeLabel())
        }
        if let detail = MenuFormat.weekPillDetail(item.summary, timing: item.timing) {
            parts.append(detail)
        }
        return parts.joined(separator: ", ")
    }
}

#Preview("Week strip") {
    NavigationStack {
        Color(.systemGroupedBackground)
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
}

#Preview("Week strip, accessibility size") {
    NavigationStack {
        Color(.systemGroupedBackground)
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}

#Preview("Week strip, dark") {
    NavigationStack {
        Color(.systemGroupedBackground)
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}
