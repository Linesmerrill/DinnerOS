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
        // Stands in for a cookie read WebKit never answers. Getting nil back at all proves the
        // limit won: the work itself would only ever answer "too late", and much later.
        let value: String? = await MealKitDeadline.value(within: .milliseconds(200)) {
            try? await Task.sleep(for: .seconds(60))
            return "too late"
        }
        #expect(value == nil)
    }

    @Test func theCookieReadLimitLeavesRoomForTheSignInTimeout() {
        #expect(MealKitWebLoginModel.cookieReadLimit < MealKitWebLoginModel.cookieTimeout)
    }
}
