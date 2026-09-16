import Foundation

/// When the app may ask a household how a planned meal went.
///
/// Feedback is only worth asking for about a night that has already happened: asking about
/// Friday on Tuesday is a prompt about a meal nobody has cooked, and an answer to it would be a
/// guess rather than a record. This decides that one question, and nothing else — it never
/// concludes that a meal *was* cooked, only that its night has passed, so the household is being
/// asked rather than answered for.
nonisolated enum MealFeedback {
    /// Whether a planned meal's night has arrived in the week being shown.
    ///
    /// Every meal in a past week has had its night. In the current week, a meal has had its night
    /// once its day is today or earlier. A meal planned for the week without a day hasn't: there
    /// is no night to ask about. Nothing in an upcoming week has happened yet.
    static func hasHappened(day: PlanDay?, timing: WeekTiming, today: PlanDay?) -> Bool {
        if timing == .past {
            return true
        }
        guard timing == .current, let day, let today else { return false }
        return day.offset <= today.offset
    }
}
