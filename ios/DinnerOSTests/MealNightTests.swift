import Foundation
import Testing

@testable import DinnerOS

/// "Didn't make it?" shows once a meal's night is over, never before (decision 662).
struct MealNightTests {
    private let calendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/Phoenix") ?? .gmt
        return calendar
    }()

    private func at(_ year: Int, _ month: Int, _ day: Int, hour: Int) -> Date {
        calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour)) ?? .distantPast
    }

    @Test func aNightIsOverTheDayAfter() {
        let tuesdayEvening = at(2026, 11, 10, hour: 21)
        #expect(!MealNight.hasPassed(date: "2026-11-10", now: tuesdayEvening, calendar: calendar))
        #expect(MealNight.hasPassed(date: "2026-11-09", now: tuesdayEvening, calendar: calendar))
        #expect(!MealNight.hasPassed(date: "2026-11-11", now: tuesdayEvening, calendar: calendar))
        // Read in the household's own day, not UTC: 9 pm in Phoenix is already tomorrow in UTC.
        #expect(!MealNight.hasPassed(date: "2026-11-10", now: at(2026, 11, 10, hour: 23), calendar: calendar))
    }

    @Test func aMealWithoutADayHasNoNight() {
        #expect(!MealNight.hasPassed(date: nil, now: at(2026, 11, 20, hour: 9), calendar: calendar))
        #expect(!MealNight.hasPassed(date: "", now: at(2026, 11, 20, hour: 9), calendar: calendar))
    }

    @Test func anAssumedMadeMealDecodesAsMade() throws {
        let json = #"{"kind":"cooked","by":"","at":"2026-11-16T02:00:00Z","implicit":true}"#
        #expect(try JSONDecoder().decode(MealOutcome.self, from: Data(json.utf8)) == .cooked)
    }
}
