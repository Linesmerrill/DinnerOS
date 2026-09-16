import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// What the redesigned Menu has to do for someone using it with large text, VoiceOver, Reduce
/// Motion, or hands that miss a 24-point target: contrast that holds in both appearances, a week
/// strip that can't take the screen, a card name that grows rather than truncates, and controls
/// big enough to hit.
@MainActor
struct AccessibilityTests {
    private static let longName = "One-Pan Santa Fe Pork Tacos with Charred Corn Salsa"
    private static let shortName = "Skillet Tacos"

    // MARK: - Contrast

    /// The WCAG relative luminance of `color` as it resolves in `style`.
    private func luminance(_ color: UIColor, in style: UIUserInterfaceStyle) -> Double {
        let resolved = color.resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
        var red: CGFloat = 0
        var green: CGFloat = 0
        var blue: CGFloat = 0
        var alpha: CGFloat = 0
        resolved.getRed(&red, green: &green, blue: &blue, alpha: &alpha)
        func channel(_ value: CGFloat) -> Double {
            let value = Double(value)
            return value <= 0.040_45 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(red) + 0.7152 * channel(green) + 0.0722 * channel(blue)
    }

    /// The WCAG contrast ratio between two colors in one appearance.
    private func contrast(_ first: UIColor, _ second: UIColor, in style: UIUserInterfaceStyle) -> Double {
        let one = luminance(first, in: style)
        let other = luminance(second, in: style)
        return (max(one, other) + 0.05) / (min(one, other) + 0.05)
    }

    /// Everything that fills a shape with the accent writes `Color.onAccent` on it, so this pair
    /// has to clear AA in both appearances — including dark, where the accent lightens and white
    /// (what this replaced) falls to about 2.2:1.
    @Test func textOnTheAccentColorIsReadableInBothAppearances() throws {
        let accent = try #require(UIColor(named: "AccentColor"))
        let onAccent = try #require(UIColor(named: "OnAccent"))
        for style in [UIUserInterfaceStyle.light, .dark] {
            #expect(contrast(accent, onAccent, in: style) >= 4.5)
        }
        #expect(contrast(accent, .white, in: .dark) < 4.5)
    }

    // MARK: - The week strip

    @Test func theWeekStripStopsGrowingBeforeItTakesTheScreen() {
        let base = WeekStripMetrics.height(for: .large)
        #expect((56...80).contains(base), "three short lines and the pill's padding, at the default size")

        let largest = WeekStripMetrics.height(for: .accessibility5)
        #expect(largest == WeekStripMetrics.height(for: WeekStripMetrics.largestTypeSize))
        #expect(largest > base, "the strip still grows with Dynamic Type")
        #expect(largest <= 120, "a strip taller than this leaves a phone showing nothing else")

        var previous: CGFloat = 0
        for size in DynamicTypeSize.allCases {
            let height = WeekStripMetrics.height(for: size)
            #expect(height >= previous, "the strip never shrinks as text grows")
            previous = height
        }
    }

    /// The pills are clamped to the same size the reserved height is computed from, so whatever
    /// the member's text size, what's drawn fits the box that holds it.
    @Test func aWeekPillFitsTheHeightTheStripReserves() throws {
        let week = try #require(ISOWeek("2026-W38"))
        let item = WeekStripItem(
            week: week,
            summary: WeekSummary(week: week.description, timing: .current, plannedCount: 5, addOnCount: 1),
            timing: .current)
        for size in [DynamicTypeSize.large, .accessibility3, .accessibility5] {
            let pill = WeekPill(item: item, isSelected: true, currentWeek: week)
                .dynamicTypeSize(...WeekStripMetrics.largestTypeSize)
                .environment(\.dynamicTypeSize, size)
            let controller = UIHostingController(rootView: pill)
            let fitted = controller.sizeThatFits(
                in: CGSize(width: 200, height: UIView.layoutFittingCompressedSize.height))
            #expect(fitted.height <= WeekStripMetrics.height(for: size))
        }
    }

    // MARK: - Card names

    private func titleHeight(_ name: String, size: DynamicTypeSize, width: CGFloat = 300) -> CGFloat {
        let controller = UIHostingController(
            rootView: CardTitle(name: name).environment(\.dynamicTypeSize, size))
        return controller.sizeThatFits(in: CGSize(width: width, height: UIView.layoutFittingCompressedSize.height))
            .height
    }

    @Test func aCardNameGrowsInsteadOfTruncatingAtAccessibilitySizes() {
        #expect(MenuCardMetrics.titleLines(for: .accessibility3) == nil)
        let long = titleHeight(Self.longName, size: .accessibility3)
        let short = titleHeight(Self.shortName, size: .accessibility3)
        #expect(long > short * 2, "a long name needs several lines at this size and should get them")
    }

    /// #360 and #447 still hold where the cards are side by side at ordinary sizes.
    @Test func aCardNameReservesOneHeightAtDefaultSizes() {
        #expect(MenuCardMetrics.titleLines(for: .large) == 2)
        #expect(titleHeight(Self.longName, size: .large) == titleHeight(Self.shortName, size: .large))
    }

    // MARK: - Controls

    @Test func theServingsStepperMeetsTheMinimumTapTarget() {
        #expect(ServingsStepper.minimumTapTarget >= 44)
        let controller = UIHostingController(
            rootView: ServingsStepper(label: "4 servings", decrease: {}, increase: {}))
        let fitted = controller.sizeThatFits(
            in: CGSize(width: 320, height: UIView.layoutFittingCompressedSize.height))
        #expect(fitted.height >= ServingsStepper.minimumTapTarget)
    }

    @Test func theJumpToAllMealsIsNotAnimatedUnderReduceMotion() {
        #expect(MenuScroll.animation(reduceMotion: true) == nil)
        #expect(MenuScroll.animation(reduceMotion: false) != nil)
    }

    // MARK: - What a card says

    /// The card draws a photo, a name, and two badges it hides from VoiceOver, so the label is
    /// the only place the time and the context badge are spoken (#363).
    @Test func aCardSpeaksWhatItsBadgesShow() {
        let label = MenuFormat.cardAccessibilityLabel(
            name: "Skillet Tacos", minutes: 20, isQuick: true, badge: "Often Ordered", isAddOn: false,
            inPlan: true)
        #expect(label.contains("Skillet Tacos"))
        #expect(label.contains("20 minutes"))
        #expect(label.contains("quick"))
        #expect(label.contains("often ordered"))
        #expect(label.contains("in your week"))
    }
}
