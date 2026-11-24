import SwiftUI

/// Marks for the UI tests on what a screenshot alone can't tell apart: a photo that fell back to
/// its placeholder art. The photos are hidden from VoiceOver, so the tests can't see them either;
/// a marker is an empty element beside the photo that they can find by its identifier.
///
/// Debug builds only, and only when the UI tests launch the app with `QA_MARKERS=1`
/// (`launchSignedIn`), so nobody using the app, VoiceOver included, ever meets one.
nonisolated enum QAMarkers {
    static let isOn: Bool = {
        #if DEBUG
            ProcessInfo.processInfo.environment["QA_MARKERS"] == "1"
        #else
            false
        #endif
    }()

    /// A recipe or ingredient photo showing its no-photo art.
    static let photoPlaceholder = "photo.placeholder"
    /// The recipe's photo at the top of the cooking screen, where it sits, so a test can check its
    /// pixels: a photo drawn invisible left a blank band there on TestFlight 120.
    static let cookPhoto = "cook.photo"
    /// An ingredient row on the cooking screen's list (an identifier on the row itself).
    static let cookIngredient = "cook.ingredient"

    /// The whole of a Your Meals card, held space included: a card's own accessibility frame
    /// covers only what VoiceOver reads, so the line-up check measures this instead.
    static let mealCardFrame = "menu.mealCardFrame"

    /// By Day's day headings ("byDay.day.sun") and the heading over meals without a day, so a
    /// test can check every day of the week shows, in order, below the week strip.
    static let byDayHeaderPrefix = "byDay.day."
    static let byDayUnscheduledHeader = "byDay.unscheduled"
    /// The week strip under the Menu's toolbar.
    static let weekStrip = "menu.weekStrip"
}

extension View {
    /// An empty element with `identifier` (and `label`, naming the item) over this view while
    /// `isShown`, for the UI tests. Nothing at all without `QA_MARKERS=1`.
    @ViewBuilder
    func qaMarker(_ identifier: String, label: String, isShown: Bool) -> some View {
        if QAMarkers.isOn {
            overlay {
                if isShown {
                    Color.clear
                        .accessibilityElement()
                        .accessibilityIdentifier(identifier)
                        .accessibilityLabel(label)
                }
            }
        } else {
            self
        }
    }
}
