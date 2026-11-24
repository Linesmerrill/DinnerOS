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

    /// The strip's margin at each end, and the gap between pills.
    static let margin: CGFloat = 16
    static let spacing: CGFloat = 8

    /// Every screen shows a whole number of weeks that fills the row exactly, so lined up at
    /// the leading edge no pill is cut off at either end: a phone's third week, two-fifths of
    /// the screen wide, ran off the edge mid-date (Hark's review item 9, build 122), and an iPad
    /// showed half a week at the left edge (build 121).
    ///
    /// On a phone, two weeks (the selected one and the next), or one at the larger text sizes
    /// so the dates still fit. On a wide screen, about `widePillWidth` each, so the extra room
    /// shows more weeks rather than bigger pills.
    static func pillWidth(stripWidth: CGFloat, typeSize: DynamicTypeSize) -> CGFloat {
        let row = stripWidth - margin * 2
        let count: CGFloat
        if isWide(stripWidth) {
            let target = widePillWidth + (typeSize >= .xxxLarge ? 80 : 0)
            count = max(1, ((row + spacing) / (target + spacing)).rounded(.down))
        } else {
            count = typeSize >= .xxxLarge ? 1 : 2
        }
        return max(minimumPillWidth, ((row - spacing * (count - 1)) / count).rounded(.down))
    }

    /// The narrowest a pill gets, whatever the screen: room for "Nov 29 – Dec 5".
    static let minimumPillWidth: CGFloat = 140

    /// The pill's small name line: "This Week", "Next Week", or "Last Week". Other weeks: on a
    /// phone, the month and year over the dates; on a wide screen, where the name sits beside
    /// the dates, nothing (or the year, for another year), so a pill never reads "Nov 2026"
    /// where its dates should be.
    static func pillName(week: ISOWeek, current: ISOWeek, isWide: Bool) -> String? {
        if let relative = MenuFormat.relativeWeekName(week, current: current) { return relative }
        guard isWide else { return week.monthAndYear() }
        return week.year == current.year ? nil : String(week.year)
    }

    /// The week the strip lines up at its leading edge to show `selected`, so the selected week
    /// and the week after it are both whole and tappable (Hark's review item 21, and a small
    /// iPhone whose Next Week started past the screen's edge, build 122).
    ///
    /// Where three weeks fit (an iPad), the week before leads, so last week shows too. Where they
    /// don't (a phone, the Duo's cover screen), the selected week leads: with last week first,
    /// next week started a pill too far right and was cut off. The household's first week has
    /// nothing before it and leads itself.
    static func scrollTarget(
        selected: ISOWeek, available: [ISOWeek], stripWidth: CGFloat, pillWidth: CGFloat
    ) -> ISOWeek {
        guard available.contains(selected.previous), fitsThreeWeeks(stripWidth: stripWidth, pillWidth: pillWidth)
        else { return selected }
        return selected.previous
    }

    /// Whether three whole pills fit between the strip's margins.
    static func fitsThreeWeeks(stripWidth: CGFloat, pillWidth: CGFloat) -> Bool {
        margin * 2 + pillWidth * 3 + spacing * 2 <= stripWidth
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
    /// Bumped by the This Week button to bring the strip back to its start position.
    @State private var lineUpRequests = 0
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
        .padding(.vertical, isWide ? DS.Space.xs : DS.Space.s)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
        .onChange(of: isExpanded) { _, expanded in
            if !expanded {
                // There is no horizontal ScrollView while collapsed, and so nothing holding the
                // position. Forget it, so the next strip to appear asks for the selected week
                // instead of starting at its first pill. The pills line up when they appear.
                scrolledWeek = nil
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Weeks")
        .accessibilityIdentifier(QAMarkers.weekStrip)
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
                HStack(spacing: WeekStripMetrics.spacing) {
                    pastButton
                        // Every pill is laid out now, so the start of the row on screen is what
                        // asks for earlier weeks, the way appearing in the lazy row did.
                        .onScrollVisibilityChange(threshold: 0.1) { isEarlierVisible = $0 }
                        .task(
                            id:
                                "\(menu.oldestWeek)-\(canLoadEarlier)-\(hasDragged)-\(isEarlierVisible)-\(menu.canLoadEarlierWeeks)"
                        ) {
                            guard canLoadEarlier, hasDragged, isEarlierVisible, menu.canLoadEarlierWeeks else { return }
                            await menu.loadEarlierWeeks()
                        }
                    // Only while loading, which only starts once the person has dragged to the
                    // start. A spinner that was always there arrived with the week list, after
                    // the strip had lined up, and pushed every pill 52 points right: a small
                    // iPhone and the Duo opened with next week cut off, and an iPad on the week
                    // two before (build 122).
                    if menu.isLoadingEarlierWeeks {
                        ProgressView()
                            .frame(width: DS.Size.tapTarget)
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
            .contentMargins(.horizontal, WeekStripMetrics.margin, for: .scrollContent)
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                stripWidth = $0
            }
            .scrollIndicators(.hidden)
            // Inside the safe area, with both margins empty: whole pills fill the row between
            // them (`pillWidth`), so at rest no sliver of a week shows at either edge and no pill
            // is cut mid-date (Hark's review items 21 and 9).
            .mask {
                Rectangle()
                    .padding(.horizontal, WeekStripMetrics.margin)
                    .padding(.vertical, -DS.Space.huge)
            }
            .scrollPosition(id: $scrolledWeek, anchor: .leading)
            // The strip comes to rest on a pill's edge, so a drag never leaves half a week at
            // the leading edge.
            .scrollTargetBehavior(.viewAligned)
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
                scrolledWeek = scrollTarget(for: plans.week).description
                settleTask?.cancel()
                settleTask = Task { @MainActor in
                    // A lazy row guesses where pills it hasn't laid out are, so a long jump can
                    // land weeks off (it opened on October after a search). Each try lays out
                    // more of the row, so try again until the selected pill is on screen.
                    let finished = await WeekStripSettle.run(isDone: { hasDragged || isSelectedVisible }) {
                        proxy.scrollTo(scrollTarget(for: plans.week).description, anchor: .leading)
                    }
                    if finished {
                        // Once more, now that the row is laid out: the strip opened a few points
                        // off a pill's edge when the last try landed before the final layout.
                        if !hasDragged { lineUp(proxy) }
                        canLoadEarlier = true
                    }
                }
            }
            // The pills' width comes from the strip's, which is only measured after the first
            // scroll: the strip lined up for phone-sized pills, then the pills widened and an iPad
            // opened on weeks from a month before. Line up again whenever the width changes.
            .onChange(of: pillWidth) { _, _ in
                lineUp(proxy)
            }
            // The weeks can arrive after the strip does, and a week before the selected one
            // changes where it lines up; until the person drags it, keep the start position.
            .onChange(of: menu.stripItems.map(\.week)) { _, _ in
                if !hasDragged { lineUp(proxy) }
            }
            // Another week picked (a pill, This Week, or another screen): back to the start
            // position, through the reader like every other line-up.
            .onChange(of: plans.week) { _, _ in
                lineUp(proxy, animated: true)
            }
            .onChange(of: lineUpRequests) { _, _ in
                lineUp(proxy, animated: true)
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
                .frame(width: DS.Size.tapTarget)
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
            // Also when this week is already the selected one and the strip was dragged away.
            lineUpRequests += 1
        } label: {
            HStack(spacing: DS.Space.xxs) {
                if isThisWeekBehind { Image(systemName: "chevron.backward") }
                Text("This Week")
                if !isThisWeekBehind { Image(systemName: "chevron.forward") }
            }
            .font(.footnote.weight(.semibold))
            .foregroundStyle(DS.Colors.onAccent)
            .padding(.horizontal, DS.Space.ml)
            .frame(minHeight: DS.Size.tapTarget)
            .background(.tint, in: .capsule)
            .shadow(color: .black.opacity(0.2), radius: 8, y: 2)
            .contentShape(.capsule)
        }
        .buttonStyle(WeekPillButtonStyle())
        .accessibilityLabel("Go to this week")
    }

    /// Whether the strip is scrolled past this week, so the way back points left.
    private var isThisWeekBehind: Bool {
        // The strip's position names its leading pill.
        menu.currentWeek < (scrolledWeek.flatMap(ISOWeek.init) ?? menu.currentWeek)
    }

    /// Shown once this week's pill is out of sight; while it's showing, the pill is the way back.
    private var showsThisWeekButton: Bool { !isCurrentWeekVisible }

    private func select(_ week: ISOWeek) {
        guard week != plans.week else { return }
        // `PlanStore` is the week everything follows; the Menu screen loads the menu from it.
        Task { await plans.show(week: week) }
    }

    /// Puts the strip at its one start position (`WeekStripMetrics.scrollTarget`).
    private func lineUp(_ proxy: ScrollViewProxy, animated: Bool = false) {
        let target = scrollTarget(for: plans.week).description
        let move = {
            scrolledWeek = target
            proxy.scrollTo(target, anchor: .leading)
        }
        if animated && !reduceMotion {
            withAnimation(.easeInOut(duration: 0.25), move)
        } else {
            move()
        }
    }

    /// The pill the strip lines up on to show `week` (`WeekStripMetrics.scrollTarget`).
    private func scrollTarget(for week: ISOWeek) -> ISOWeek {
        WeekStripMetrics.scrollTarget(
            selected: week, available: menu.stripItems.map(\.week), stripWidth: stripWidth, pillWidth: pillWidth)
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

    @ViewBuilder
    private var name: some View {
        if let text = WeekStripMetrics.pillName(week: item.week, current: currentWeek, isWide: isWide) {
            nameText(text)
        }
    }

    private func nameText(_ text: String) -> some View {
        Text(text)
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
