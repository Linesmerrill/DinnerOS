import Foundation
import Testing

@testable import DinnerOS

/// The HelloFresh sign-in polls WebKit's cookie store, which can leave a read unanswered. A read
/// that never answers must count as "no cookie yet" so the sign-in's own timeout still runs out,
/// instead of the sheet saying "Finishing up…" forever.
@MainActor
struct MealKitDeadlineTests {
    @Test func workThatAnswersInTimeGivesItsValue() async {
        let value = await MealKitDeadline.value(within: .seconds(2)) { "session" }
        #expect(value == "session")
    }

    @Test func workThatNeverAnswersGivesNothingAtTheLimit() async {
        let clock = ContinuousClock()
        let started = clock.now
        let value: String? = await MealKitDeadline.value(within: .milliseconds(200)) {
            // Stands in for a cookie read WebKit never answers.
            try? await Task.sleep(for: .seconds(30))
            return "too late"
        }
        #expect(value == nil)
        #expect(clock.now - started < .seconds(5))
    }

    @Test func theCookieReadLimitLeavesRoomForTheSignInTimeout() {
        #expect(MealKitWebLoginModel.cookieReadLimit < MealKitWebLoginModel.cookieTimeout)
    }
}
