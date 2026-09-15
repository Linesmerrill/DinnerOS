import SwiftUI

/// The week pills across the top of the Menu screen: a **Past** link, then one pill per week
/// with its dates and counts. It scrolls to the selected week and loads earlier weeks when
/// scrolled back, down to the household's first week.
struct WeekStrip: View {
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var scrolledWeek: String?

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
        }
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
                .foregroundStyle(isSelected ? AnyShapeStyle(Color.white.opacity(0.9)) : AnyShapeStyle(Color.secondary))
            Text(item.week.rangeLabel())
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(isSelected ? AnyShapeStyle(Color.white) : AnyShapeStyle(Color.primary))
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
        .foregroundStyle(isSelected ? AnyShapeStyle(Color.white.opacity(0.9)) : AnyShapeStyle(Color.secondary))
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

#Preview("Week strip, dark") {
    NavigationStack {
        Color(.systemGroupedBackground)
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}
