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
    /// On a wide screen the name sits beside the dates, so the pill is two lines.
    private static let wideLineStyles: [UIFont.TextStyle] = [.subheadline, .caption2]

    /// From this strip width (an iPad, either way up) the pills go to two lines and stop
    /// widening. Two-fifths of a landscape iPad made each week about 540 points of mostly empty
    /// pill, and with the third line the Menu's chrome took a third of the screen.
    static let wideWidth: CGFloat = 700
    /// The widest a week gets on a wide screen: dates and the meal count, with room to spare.
    static let widePillWidth: CGFloat = 240

    static func isWide(_ stripWidth: CGFloat) -> Bool { stripWidth >= wideWidth }

    /// Two-fifths of the row per week on a phone: about two and a half weeks in view, and more
    /// of the row at the larger text sizes so the dates still fit. On a wide screen, a fixed
    /// width, so the extra room shows more weeks rather than bigger pills.
    static func pillWidth(stripWidth: CGFloat, typeSize: DynamicTypeSize) -> CGFloat {
        let share: CGFloat = typeSize >= .xxxLarge ? 0.62 : 0.4
        let phone = max(140, ((stripWidth - 32) * share).rounded())
        return isWide(stripWidth) ? min(widePillWidth + (typeSize >= .xxxLarge ? 80 : 0), phone) : phone
    }

    /// The height to reserve for the pills at `size`: what the three lines actually measure at
    /// the clamped text size, plus the pill's own spacing and padding.
    ///
    /// Scaling one magic number was what made the first version wrong in both directions — 62
    /// points scaled by `.footnote` grew to about 210 at `.accessibility5`, and clamping that
    /// same number still under-reserved what the pill draws, so pills clipped instead. Adding up
    /// the real line heights can't drift from the pill: whatever the text size, the box is as
    /// tall as its contents.
    static func height(for size: DynamicTypeSize, wide: Bool = false) -> CGFloat {
        let traits = UITraitCollection(
            preferredContentSizeCategory: contentSizeCategory(for: min(size, largestTypeSize)))
        let lines =
            (wide ? wideLineStyles : lineStyles)
            .map { UIFont.preferredFont(forTextStyle: $0, compatibleWith: traits).lineHeight }
            .reduce(0, +)
        let spacing = lineSpacing * CGFloat((wide ? wideLineStyles : lineStyles).count - 1)
        return (lines + spacing + verticalPadding * 2).rounded(.up)
    }

    /// The height to reserve for the collapsed strip at `size`: the one line it draws — the
    /// selected week's dates, at the same `.subheadline` the pill uses for them — plus the same
    /// vertical padding.
    ///
    /// Derived the same way as `height(for:)` and for the same reason (#455): a constant here
    /// would be a constant that has to be right at every text size, and the last one was not.
    /// Asking the font what the line measures means the collapsed bar is as tall as what it
    /// draws, whatever the reader's text size.
    static func collapsedHeight(for size: DynamicTypeSize) -> CGFloat {
        let traits = UITraitCollection(
            preferredContentSizeCategory: contentSizeCategory(for: min(size, largestTypeSize)))
        let line = UIFont.preferredFont(forTextStyle: .subheadline, compatibleWith: traits).lineHeight
        return (line + verticalPadding * 2).rounded(.up)
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

/// Brings the selected week pill into view after the strip appears: one scroll, then a few more
/// tries a moment apart until `isDone`, because a lazy row only guesses where pills it hasn't laid
/// out are. Stops at once when its task is cancelled (the strip left the screen), and then returns
/// `false`, so nothing scrolls a strip that is gone.
enum WeekStripSettle {
    @MainActor
    static func run(
        tries: Int = 4, pause: Duration = .milliseconds(250), isDone: () -> Bool, scroll: () -> Void
    ) async -> Bool {
        guard !Task.isCancelled else { return false }
        scroll()
        for _ in 0..<tries {
            try? await Task.sleep(for: pause)
            if Task.isCancelled { return false }
            if isDone() { break }
            scroll()
        }
        return true
    }
}

/// The week pills across the top of the Menu screen: a **Past** link, then one pill per week
/// with its dates and counts. It scrolls to the selected week and loads earlier weeks when
/// scrolled back, down to the household's first week.
///
/// It has two forms. Expanded, it is the pills. Collapsed — while the Menu is scrolled away from
/// the top (`MenuChromeCollapse`) — it is one line naming the week you are on, which expands the
/// pills again when tapped. The week itself never disappears: it is the context the rest of the
/// screen is about, and the navigation stays one tap away rather than nowhere.
struct WeekStrip: View {
    /// Whether the pills are showing. `false` draws the one-line form instead.
    var isExpanded = true

    /// Tapped on the collapsed bar, to ask for the pills back.
    var onExpand: () -> Void = {}

    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var scrolledWeek: String?
    /// Off until a new strip has scrolled to the selected week. A strip starts at its first
    /// pill, beside "loading earlier weeks"; loading then put a page of older weeks in front of
    /// the selected one and the strip opened weeks early.
    @State private var canLoadEarlier = false
    /// Set once the person drags the strip. Only then do earlier weeks load: a strip made again
    /// (the keyboard closing after a search) could land on its first pill by itself and load a
    /// page of older weeks, and it opened on October.
    @State private var hasDragged = false
    /// The loading spinner at the start of the row is on screen, so earlier weeks are wanted.
    @State private var isEarlierVisible = false
    /// Whether this week's pill is on screen, for the way back to it.
    @State private var isCurrentWeekVisible = true
    /// Whether the selected week's pill is on screen. The position binding can name a week the
    /// lazy row never reached, so this is what says a scroll landed.
    @State private var isSelectedVisible = false
    @State private var stripWidth: CGFloat = 402
    /// Scrolls the selected week into view after the strip appears; cancelled when it goes.
    @State private var settleTask: Task<Void, Never>?

    /// What the pills need at this text size, capped so the strip can't take the screen.
    private var pillHeight: CGFloat {
        WeekStripMetrics.height(for: dynamicTypeSize, wide: isWide)
    }

    private var isWide: Bool { WeekStripMetrics.isWide(stripWidth) }

    var body: some View {
        Group {
            if isExpanded {
                pills
            } else {
                collapsedBar
            }
        }
        .padding(.vertical, isWide ? 6 : 8)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
        .onChange(of: plans.week, initial: true) { _, week in
            scrollTo(week)
        }
        .onChange(of: isExpanded) { _, expanded in
            if expanded {
                scrollTo(plans.week)
            } else {
                // There is no horizontal ScrollView while collapsed, and so nothing holding the
                // position. Forget it, so the next strip to appear asks for the selected week
                // instead of starting at its first pill.
                scrolledWeek = nil
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Weeks")
    }

    /// The strip in one line: the week you are on, and a way back to the rest of them.
    private var collapsedBar: some View {
        Button(action: onExpand) {
            HStack(spacing: DS.Space.xs) {
                Text(collapsedTitle)
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(1)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(Color.secondary)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, DS.Space.l)
            // Derived from the line it draws, for the same reason the pills' height is (#455).
            .frame(height: WeekStripMetrics.collapsedHeight(for: dynamicTypeSize))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .dynamicTypeSize(...WeekStripMetrics.largestTypeSize)
        .accessibilityLabel(collapsedLabel)
        .accessibilityHint("Shows every week")
    }

    /// The shown week, named the way its pill names it.
    private var collapsedTitle: String {
        let range = plans.week.rangeLabel(weekStartsOn: plans.weekStartsOn)
        guard let name = MenuFormat.relativeWeekName(plans.week, current: menu.currentWeek) else {
            return range
        }
        return "\(name) · \(range)"
    }

    /// The same, plus the count the pill shows, so collapsing costs VoiceOver nothing.
    private var collapsedLabel: String {
        var parts = [collapsedTitle]
        let item = menu.stripItems.first { $0.week == plans.week }
        if let item, let detail = MenuFormat.weekPillDetail(item.summary, timing: item.timing) {
            parts.append(detail)
        }
        return parts.joined(separator: ", ")
    }

    /// The strip uses the whole width: **Past** rides at the start of the row instead of pinning
    /// a column of its own, and each week is two-fifths of the screen, so the week you're on sits
    /// in the middle with both neighbors readable. The way back to this week only appears once
    /// this week's pill has scrolled out of sight, and floats over the edge rather than taking a
    /// column.
    private var pills: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                // A plain row, not a lazy one: a lazy row guesses where pills it hasn't laid out
                // are, and on iOS 27 (Xcode 27, build 118) that guess left the week you're on off
                // to the side, or the strip back at its first pills after a search, while it
                // reported the week as on screen. A strip is a few dozen small pills.
                HStack(spacing: DS.Space.s) {
                    pastButton
                    if menu.canLoadEarlierWeeks {
                        ProgressView()
                            .frame(width: 44)
                            // Every pill is laid out now, so "on screen" is what asks for more,
                            // the way appearing in the lazy row did.
                            .onScrollVisibilityChange(threshold: 0.1) { isEarlierVisible = $0 }
                            .task(id: "\(menu.oldestWeek)-\(canLoadEarlier)-\(hasDragged)-\(isEarlierVisible)") {
                                guard canLoadEarlier, hasDragged, isEarlierVisible else { return }
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
                                currentWeek: menu.currentWeek, weekStartsOn: menu.weekStartsOn, isWide: isWide)
                        }
                        .buttonStyle(WeekPillButtonStyle())
                        // An explicit width, not `containerRelativeFrame`: the lazy row estimates
                        // the pills it hasn't laid out from the ones it has, and a relative frame
                        // threw that off enough to scroll the selected week to the edge.
                        .frame(width: pillWidth)
                        .id(item.id)
                        .onScrollVisibilityChange(threshold: 0.6) { visible in
                            if item.week == menu.currentWeek { isCurrentWeekVisible = visible }
                            if item.week == plans.week { isSelectedVisible = visible }
                        }
                    }
                }
                .scrollTargetLayout()
            }
            .contentMargins(.horizontal, 16, for: .scrollContent)
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                stripWidth = $0
            }
            .scrollIndicators(.hidden)
            .scrollPosition(id: $scrolledWeek, anchor: .center)
            .onScrollPhaseChange { _, phase in
                if phase == .interacting { hasDragged = true }
            }
            // A strip made again (after collapsing, or when the menu jumps back to All Meals) could
            // start at its first pill, with "loading earlier weeks" on screen; that loaded page
            // after page until the strip showed weeks from a year and a half ago. Ask for the
            // selected week every time the strip appears.
            .onAppear {
                // The position binding alone missed on a lazy row whose selected pill wasn't laid
                // out yet (it opened weeks early); the reader's scroll finds it either way.
                canLoadEarlier = false
                hasDragged = false
                // Pills that went away with the old strip never said they left the screen.
                isSelectedVisible = false
                scrolledWeek = plans.week.description
                settleTask?.cancel()
                settleTask = Task { @MainActor in
                    // A lazy row guesses where pills it hasn't laid out are, so a long jump can
                    // land weeks off (it opened on October after a search). Each try lays out
                    // more of the row, so try again until the selected pill is on screen.
                    let finished = await WeekStripSettle.run(isDone: { hasDragged || isSelectedVisible }) {
                        proxy.scrollTo(plans.week.description, anchor: .center)
                    }
                    if finished { canLoadEarlier = true }
                }
            }
            // Scrolling a strip that has gone (collapsed, or the menu changed view) crashed in
            // SwiftUI's scrollTo: stop trying once it leaves the screen.
            .onDisappear {
                settleTask?.cancel()
                settleTask = nil
            }
        }
        // A horizontal ScrollView takes all the height it is offered, and as a top
        // safe-area inset that is the whole screen, so pin it to the pills' height.
        .frame(height: pillHeight)
        .overlay(alignment: isThisWeekBehind ? .leading : .trailing) {
            if showsThisWeekButton {
                thisWeekButton
                    .padding(.horizontal, DS.Space.m)
                    .transition(.scale(scale: 0.7).combined(with: .opacity))
            }
        }
        .animation(reduceMotion ? nil : .spring(duration: 0.3), value: showsThisWeekButton)
        // The pills stop growing where `pillHeight` stops, so they always fit what it reserves.
        .dynamicTypeSize(...WeekStripMetrics.largestTypeSize)
    }

    private var pillWidth: CGFloat {
        WeekStripMetrics.pillWidth(stripWidth: stripWidth, typeSize: dynamicTypeSize)
    }

    private var pastButton: some View {
        NavigationLink(value: PastWeeksRoute()) {
            Image(systemName: "clock.arrow.circlepath")
                .font(.body.weight(.semibold))
                .frame(width: 44)
                .frame(maxHeight: .infinity)
                .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
                .contentShape(.rect)
        }
        .buttonStyle(WeekPillButtonStyle())
        .foregroundStyle(.tint)
        .accessibilityLabel("Past weeks")
        .accessibilityHint("Shows earlier weeks")
    }

    /// "This Week", on the side this week is on, pointing back to it.
    private var thisWeekButton: some View {
        Button {
            select(menu.currentWeek)
            scrollTo(menu.currentWeek)
        } label: {
            HStack(spacing: DS.Space.xxs) {
                if isThisWeekBehind { Image(systemName: "chevron.backward") }
                Text("This Week")
                if !isThisWeekBehind { Image(systemName: "chevron.forward") }
            }
            .font(.footnote.weight(.semibold))
            .foregroundStyle(Color.onAccent)
            .padding(.horizontal, DS.Space.ml)
            .frame(minHeight: 44)
            .background(.tint, in: .capsule)
            .shadow(color: .black.opacity(0.2), radius: 8, y: 2)
            .contentShape(.capsule)
        }
        .buttonStyle(WeekPillButtonStyle())
        .accessibilityLabel("Go to this week")
    }

    /// Whether the strip is scrolled past this week, so the way back points left.
    private var isThisWeekBehind: Bool {
        menu.currentWeek < (scrolledWeek.flatMap(ISOWeek.init) ?? menu.currentWeek)
    }

    /// Shown once this week's pill is out of sight; while it's showing, the pill is the way back.
    private var showsThisWeekButton: Bool { !isCurrentWeekVisible }

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
    let weekStartsOn: PlanDay
    /// Two lines, the name beside the dates (`WeekStripMetrics.wideWidth`).
    var isWide = false

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.hairline) {
            if isWide {
                HStack(alignment: .firstTextBaseline, spacing: DS.Space.xs) {
                    dates
                    name
                }
            } else {
                name
                dates
            }
            detail
        }
        .lineLimit(1)
        .minimumScaleFactor(0.85)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, DS.Space.m)
        .padding(.vertical, DS.Space.s)
        .background(
            isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(DS.Colors.surface),
            in: .rect(cornerRadius: DS.Radius.large)
        )
        .overlay {
            if item.timing == .current, !isSelected {
                RoundedRectangle(cornerRadius: DS.Radius.large).strokeBorder(.tint, lineWidth: 1.5)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    private var name: some View {
        Text(relativeName ?? item.week.monthAndYear())
            .font(.caption2.weight(.semibold))
            .foregroundStyle(
                isSelected ? AnyShapeStyle(Color.onAccent.opacity(0.9)) : AnyShapeStyle(Color.secondary))
    }

    private var dates: some View {
        Text(item.week.rangeLabel(weekStartsOn: weekStartsOn))
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(isSelected ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
    }

    private var relativeName: String? {
        MenuFormat.relativeWeekName(item.week, current: currentWeek)
    }

    /// Seven nights, one mark each: how full the week is at a glance, then the count in words.
    private var detail: some View {
        HStack(spacing: DS.Space.xs) {
            WeekNightsMeter(
                planned: item.summary?.plannedCount ?? 0, cooked: item.summary?.cookedCount ?? 0,
                isPast: item.timing == .past, isSelected: isSelected)
            Text(MenuFormat.weekPillDetail(item.summary, timing: item.timing) ?? " ")
                .font(.caption2)
                .foregroundStyle(
                    isSelected ? AnyShapeStyle(Color.onAccent.opacity(0.9)) : AnyShapeStyle(Color.secondary))
        }
    }

    private var accessibilityLabel: String {
        var parts = [relativeName ?? item.week.rangeLabel(weekStartsOn: weekStartsOn)]
        if relativeName != nil {
            parts.append(item.week.rangeLabel(weekStartsOn: weekStartsOn))
        }
        if let detail = MenuFormat.weekPillDetail(item.summary, timing: item.timing) {
            parts.append(detail)
        }
        return parts.joined(separator: ", ")
    }
}

/// Seven short bars for the seven nights of a week: planned nights solid, open nights faint. In a
/// past week only the nights actually cooked stay solid; planned but not cooked is half-strength.
/// Drawn within the caption line's height so the pill doesn't grow.
struct WeekNightsMeter: View {
    let planned: Int
    let cooked: Int
    let isPast: Bool
    let isSelected: Bool

    var body: some View {
        HStack(spacing: DS.Space.hairline) {
            ForEach(0..<7, id: \.self) { night in
                Capsule()
                    .fill(color(for: night))
                    .frame(width: 4, height: 9)
            }
        }
        .accessibilityHidden(true)
    }

    private func color(for night: Int) -> Color {
        let base: Color = isSelected ? .onAccent : .accentColor
        let filled = min(max(planned, cooked), 7)
        if night < (isPast ? min(cooked, 7) : filled) { return base }
        if night < filled { return base.opacity(isSelected ? 0.6 : 0.45) }
        return (isSelected ? Color.onAccent : Color.secondary).opacity(0.25)
    }
}

/// Pills and the round buttons press in slightly, so a tap on this busy strip is felt.
struct WeekPillButtonStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.96 : 1)
            .opacity(configuration.isPressed ? 0.85 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

#Preview("Week strip") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
}

#Preview("Week strip, accessibility size") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}

#Preview("Week strip, dark") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip() }
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}

#Preview("Week strip, collapsed") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip(isExpanded: false) }
    }
    .menuPreviewEnvironment()
}

#Preview("Week strip, collapsed, accessibility size") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip(isExpanded: false) }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}

#Preview("Week strip, collapsed, largest accessibility size") {
    NavigationStack {
        DS.Colors.groupedBackground
            .safeAreaInset(edge: .top, spacing: 0) { WeekStrip(isExpanded: false) }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility5)
}
