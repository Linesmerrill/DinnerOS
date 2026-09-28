import Foundation
import Observation

/// Where an App Intent that opens the app wants to go. The views that own those screens read
/// it and clear it once they've shown the destination.
@Observable
final class AppIntentRouter {
    /// Siri planned this week; the Menu tab opens its suggestions for review.
    var autopilotReviewWeek: ISOWeek?
    /// A widget was tapped; the tab shell opens what it links to.
    var widgetLink: DinnerWidgetLink?
    /// A widget asked to plan this week; the Menu tab starts Autopilot.
    var planWeekRequested = false
}
