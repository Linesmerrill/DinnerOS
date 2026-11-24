import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// Build 121's Menu, Shared Recipes, and Notifications fixes from the screenshot review.
@MainActor
struct Build121MenuLayoutTests {
    // MARK: - Your Meals: one empty message (#31)

    @Test func anEmptyWeekHasNoSubtitleUnderYourMeals() {
        #expect(MenuFormat.yourMealsHeaderSubtitle(counts: MealCounts(), isPast: false) == nil)
        #expect(MenuFormat.yourMealsHeaderSubtitle(counts: MealCounts(), isPast: true) == nil)
    }

    @Test func aWeekWithMealsKeepsItsSubtitle() {
        #expect(
            MenuFormat.yourMealsHeaderSubtitle(counts: MealCounts(meals: 3), isPast: false) == "3 meals planned")
        #expect(MenuFormat.yourMealsHeaderSubtitle(counts: MealCounts(meals: 2), isPast: true) == "2 meals planned")
        #expect(
            MenuFormat.yourMealsHeaderSubtitle(counts: MealCounts(addOns: 1), isPast: false)
                == "Nothing planned yet · 1 add-on")
    }

    @Test func theEmptyWeekTitleIsTitleCase() {
        #expect(MenuFormat.emptyWeekTitle(isPast: false) == "Nothing Planned Yet")
        #expect(MenuFormat.emptyWeekTitle(isPast: true) == "Nothing Was Planned This Week")
    }

    // MARK: - Shared Recipes: one way in (#31)

    @Test func theSharedRecipesRowWaitsForGetStartedToGo() {
        #expect(!MenuEntryPoints.showsSharedRecipesRow(getStartedShown: true))
        #expect(MenuEntryPoints.showsSharedRecipesRow(getStartedShown: false))
    }

    // MARK: - Cards (#26, #18)

    @Test func onlyCardsWithNothingUnderTheNameReserveASecondLine() {
        #expect(MenuCardMetrics.reservesTitleHeight(hasRowBelow: false))
        #expect(!MenuCardMetrics.reservesTitleHeight(hasRowBelow: true))
    }

    @Test(arguments: [(0.0, 1), (279.0, 1), (500.0, 1), (576.0, 2), (800.0, 2), (1100.0, 3), (1300.0, 4)])
    func anIPadRowHoldsAsManyCardsAsFit(width: Double, columns: Int) {
        #expect(AllMealsLayout.columnCount(width: CGFloat(width)) == columns)
    }

    @Test func aLoneResultStaysCardSized() {
        // One card across a landscape iPad is never wider than a card.
        let width: CGFloat = 1100
        let columns = AllMealsLayout.columnCount(width: width)
        let card = AllMealsLayout.cardWidth(width: width, columns: columns)
        #expect(card <= AllMealsLayout.maximumCardWidth)
        #expect(card >= AllMealsLayout.minimumCardWidth)
        // The cards and their gaps fit the row.
        #expect(card * CGFloat(columns) + AllMealsLayout.columnSpacing * CGFloat(columns - 1) <= width)
    }

    /// The cards' width comes from the Menu's width whenever it's known, so All Meals made
    /// again by the lazy stack (its own measurement back at zero) lays out the same rows at once
    /// instead of one card to a row first (the landscape iPad freeze, QA build 121).
    @Test func theCardsWidthDoesNotDependOnARemeasure() {
        let fresh = AllMealsLayout.cardsWidth(containerWidth: 1194, measured: 0)
        let settled = AllMealsLayout.cardsWidth(containerWidth: 1194, measured: 1162)
        #expect(fresh == settled)
        #expect(fresh == 1194 - AllMealsLayout.horizontalMargin * 2)
        #expect(AllMealsLayout.columnCount(width: fresh) == 3)
        // Without the Menu's width (Show More), the section's own measurement is used.
        #expect(AllMealsLayout.cardsWidth(containerWidth: nil, measured: 700) == 700)
        #expect(AllMealsLayout.cardsWidth(containerWidth: 0, measured: 700) == 700)
    }

    @Test func allMealsSplitsIntoRowsInOrder() throws {
        let cards = MenuPreviewData.cards
        try #require(cards.count >= 3)
        let rows = AllMealsLayout.rows(cards, columns: 2)
        #expect(rows.count == (cards.count + 1) / 2)
        #expect(rows.flatMap { $0.cards.map(\.card.id) } == cards.map(\.id))
        #expect(rows.flatMap { $0.cards.map(\.index) } == Array(cards.indices))
        #expect(rows.first?.id == cards.first?.id)
        #expect(AllMealsLayout.rows([], columns: 3).isEmpty)
    }

    @Test func everySharedRecipesRowUsesOneCardSize() {
        #expect(DiscoverLayout.cardWidth(isWide: false) > 0)
        #expect(DiscoverLayout.cardWidth(isWide: true) >= DiscoverLayout.cardWidth(isWide: false))
        // A row's photos are a fixed 4:3, whatever the name under them takes.
        #expect(DiscoverLayout.photoHeight(forWidth: 240) == 180)
    }

    // MARK: - Week strip (#16)

    @Test(arguments: [1366.0, 1194.0, 1024.0, 834.0, 744.0])
    func aWideStripFitsAWholeNumberOfPills(stripWidth: Double) {
        let width = CGFloat(stripWidth)
        let pill = WeekStripMetrics.pillWidth(stripWidth: width, typeSize: .large)
        let row = width - WeekStripMetrics.margin * 2
        let count = ((row + WeekStripMetrics.spacing) / (pill + WeekStripMetrics.spacing)).rounded(.down)
        let used = count * pill + (count - 1) * WeekStripMetrics.spacing
        #expect(row - used < count, "\(row - used) points left over: a pill would be cut off")
        #expect(pill >= WeekStripMetrics.widePillWidth * 0.8)
    }

    @Test func aPhoneStripFitsTwoWholeWeeks() {
        #expect(WeekStripMetrics.pillWidth(stripWidth: 402, typeSize: .large) == 181)
    }

    @Test func aWidePillIsLabeledWithItsDatesNotTheMonth() throws {
        let current = try #require(ISOWeek("2026-W46"))
        let earlier = try #require(ISOWeek("2026-W44"))
        #expect(WeekStripMetrics.pillName(week: earlier, current: current, isWide: true) == nil)
        #expect(WeekStripMetrics.pillName(week: current, current: current, isWide: true) == "This Week")
        #expect(WeekStripMetrics.pillName(week: earlier, current: current, isWide: false) == earlier.monthAndYear())
        let lastYear = try #require(ISOWeek("2025-W44"))
        #expect(WeekStripMetrics.pillName(week: lastYear, current: current, isWide: true) == "2025")
    }

    @Test func aPhoneStripLeadsWithTheSelectedWeekSoNextWeekIsWhole() throws {
        let selected = try #require(ISOWeek("2026-W46"))
        let weeks = [selected.previous, selected, selected.next]
        // iPhone SE, the Duo's cover screen, and a Pro Max: three weeks don't fit, so the
        // selected week leads and next week sits whole beside it.
        for width: CGFloat in [375, 382, 440] {
            let pill = WeekStripMetrics.pillWidth(stripWidth: width, typeSize: .large)
            #expect(
                WeekStripMetrics.scrollTarget(
                    selected: selected, available: weeks, stripWidth: width, pillWidth: pill) == selected)
            let nextEnd = WeekStripMetrics.margin + pill * 2 + WeekStripMetrics.spacing
            #expect(nextEnd <= width, "Next week is cut off at \(width) points")
        }
    }

    @Test func aWideStripShowsLastWeekToo() throws {
        let selected = try #require(ISOWeek("2026-W46"))
        let weeks = [selected.previous, selected, selected.next]
        // An iPad either way up: the week before leads, the selected and next weeks follow whole.
        for width: CGFloat in [834, 1210] {
            let pill = WeekStripMetrics.pillWidth(stripWidth: width, typeSize: .large)
            #expect(WeekStripMetrics.fitsThreeWeeks(stripWidth: width, pillWidth: pill))
            #expect(
                WeekStripMetrics.scrollTarget(
                    selected: selected, available: weeks, stripWidth: width, pillWidth: pill) == selected.previous)
        }
        // The household's first week has nothing before it.
        #expect(
            WeekStripMetrics.scrollTarget(
                selected: selected, available: [selected], stripWidth: 1210, pillWidth: 240) == selected)
    }

    // MARK: - Your Meals: "How was it?" only once it's cooked (#12)

    @Test func onlyACookedMealAsksHowItWas() {
        #expect(MealRating.shows(outcome: .cooked, nightHasPassed: false))
        #expect(MealRating.shows(outcome: nil, nightHasPassed: true))
        #expect(!MealRating.shows(outcome: nil, nightHasPassed: false))
        #expect(!MealRating.shows(outcome: .skipped(nil), nightHasPassed: true))
    }

    /// All Meals' title steps aside while the search field is open, and only then; nothing about
    /// the header follows keyboard focus (the iPad freeze, QA build 121).
    @Test func allMealsHidesItsTitleOnlyWhileSearching() {
        #expect(AllMealsLayout.showsTitle("All Meals", isSearching: false))
        #expect(!AllMealsLayout.showsTitle("All Meals", isSearching: true))
        #expect(!AllMealsLayout.showsTitle(nil, isSearching: false))
    }

    /// Build 121 showed the stars only once a meal was cooked; since Nov 23 (Merrill) every meal
    /// shows them, cooked or not (`Build124MenuCardsTests`). Only "How was it?" waits.
    @Test func everyMealWithItsCardShowsTheStars() {
        #expect(MealRating.row(canRate: true, weekAsks: true) == .shown)
        #expect(MealRating.row(canRate: true, weekAsks: false) == .shown)
    }

    @Test func theEmptyWeekHintIsASentence() {
        #expect(MenuFormat.addMealsYourselfHint == "You can also add meals yourself below.")
    }

    // MARK: - The bell (QA: iPhone Duo)

    /// A badge must not make the bell wider: on the iPhone Duo's cover screen a wider bell no
    /// longer fit the side column and the toolbar moved it to the bottom of the screen.
    @Test func theBellIsTheSameSizeWithABadge() {
        let bounds = CGSize(width: 500, height: 500)
        let plain = UIHostingController(rootView: NotificationBell(count: 0) {}).sizeThatFits(in: bounds)
        for count in [1, 12, 120] {
            let badged = UIHostingController(rootView: NotificationBell(count: count) {}).sizeThatFits(in: bounds)
            #expect(badged == plain, "\(count) unread")
        }
    }

    // MARK: - Notifications (#34)

    private static func notification(
        _ id: String, type: AppNotificationType = .plusReferral, title: String = "Your invite counted",
        read: Bool = true, hoursAgo: Double
    ) -> AppNotification {
        AppNotification(
            id: id, householdID: "household-1", type: type, title: title, body: "Kitchen \(id) joined.",
            subject: AppNotificationSubject(kind: AppNotificationSubject.plusKind, id: "plus"), read: read,
            createdAt: Date(timeIntervalSince1970: 1_800_000_000 - hoursAgo * 3_600))
    }

    @Test func repeatedInviteNotificationsFoldIntoTheNewest() {
        let items = [
            Self.notification("a", hoursAgo: 1),
            Self.notification("low", type: .pantryLow, title: "Carrots are running low", hoursAgo: 2),
            Self.notification("b", read: false, hoursAgo: 3),
            Self.notification("c", hoursAgo: 4),
        ]
        let groups = NotificationGrouping.groups(items)
        #expect(groups.map(\.id) == ["a", "low"])
        #expect(groups[0].items.map(\.id) == ["a", "b", "c"])
        #expect(groups[0].moreCount == 2)
        // Unread while any of them is.
        #expect(!groups[0].isRead)
        #expect(groups[0].unread.map(\.id) == ["b"])
        #expect(groups[1].moreCount == 0)
    }

    @Test func otherTypesNeverFold() {
        let items = [
            Self.notification("1", type: .pantryLow, title: "Carrots are running low", hoursAgo: 1),
            Self.notification("2", type: .pantryLow, title: "Carrots are running low", hoursAgo: 2),
        ]
        #expect(NotificationGrouping.groups(items).count == 2)
    }

    @Test func aGroupSaysHowManyMore() {
        #expect(NotificationGrouping.moreText(0) == nil)
        #expect(NotificationGrouping.moreText(1) == "1 more like this")
        #expect(NotificationGrouping.moreText(12) == "12 more like this")
    }
}

/// The scrim under the floating tab bar (Hark's review items 11 and 4, build 122).
struct TabBarScrimLayoutTests {
    @Test func itFadesInAboveTheBarAndIsSolidAtTheEdge() {
        let fade = TabBarScrimLayout.fadeStops
        let bar = TabBarScrimLayout.barStops
        // Clear where it starts, so there's no hard edge on the content.
        #expect(fade.first?.location == 0)
        #expect(fade.first?.opacity == 0)
        // Nearly full strength by the top of the bar, where the labels' backdrop begins.
        #expect(fade.last?.location == 1)
        #expect((fade.last?.opacity ?? 0) >= 0.9)
        // The bar's part carries on from there, solid at the screen's edge.
        #expect(bar.first?.opacity == fade.last?.opacity)
        #expect(bar.last?.location == 1)
        #expect(bar.last?.opacity == 1)
    }

    /// The scrim fades into the screen's own background: on a grouped screen (Shop, Pantry,
    /// Household) a white scrim showed as a white block over the gray in light mode (Hark's
    /// review, build 123).
    @MainActor
    @Test func onAGroupedScreenItFadesIntoTheGray() {
        let gray = bottomPixel(
            DS.Colors.groupedBackground.ignoresSafeArea().tabBarEdge(DS.Colors.groupedBackground))
        let screenGray = bottomPixel(DS.Colors.groupedBackground.ignoresSafeArea())
        #expect(gray == screenGray, "scrim \(gray), screen \(screenGray)")

        let white = bottomPixel(DS.Colors.groupedBackground.ignoresSafeArea().tabBarEdge())
        #expect(white != screenGray, "the default scrim is the bar's white, for a plain screen")
    }

    /// The RGB of the bottom-middle pixel of `view` drawn in light mode.
    @MainActor
    private func bottomPixel(_ view: some View) -> [UInt8] {
        let size = CGSize(width: 200, height: 300)
        let controller = UIHostingController(rootView: AnyView(view))
        controller.overrideUserInterfaceStyle = .light
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        window.overrideUserInterfaceStyle = .light
        window.rootViewController = controller
        window.isHidden = false
        controller.view.frame = CGRect(origin: .zero, size: size)
        window.layoutIfNeeded()
        controller.view.layoutIfNeeded()
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            window.layer.render(in: context.cgContext)
        }
        guard let cgImage = image.cgImage else { return [] }
        var pixel = [UInt8](repeating: 0, count: 4)
        let context = CGContext(
            data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        // The pixel two points above the bottom edge, in the middle.
        context?.draw(cgImage, in: CGRect(x: -100, y: -2, width: size.width, height: size.height))
        return Array(pixel.prefix(3))
    }
}
