import Foundation
import Testing

@testable import DinnerOS

private func utc(_ string: String) -> Date {
    ISO8601DateFormatter().date(from: string) ?? .distantPast
}

private let denver = TimeZone(identifier: "America/Denver") ?? .gmt
private let tokyo = TimeZone(identifier: "Asia/Tokyo") ?? .gmt
/// Monday September 14 to Sunday September 20, 2026.
private let week = ISOWeek("2026-W38") ?? .current(in: .gmt)

// MARK: - Calendar busyness

struct CalendarBusynessTests {
    /// Monday's evening in Denver (MDT, UTC−6): 4 pm is 22:00 UTC.
    private var monday: DateInterval {
        DateInterval(start: utc("2026-09-14T22:00:00Z"), end: utc("2026-09-15T02:00:00Z"))
    }

    @Test func eveningWindowIsFourToEightInTheHouseholdsTimeZone() throws {
        #expect(CalendarBusyness.window(day: .mon, week: week, timeZone: denver) == monday)
        // Tokyo (UTC+9): Sunday 4 pm is 07:00 UTC the same day.
        let sunday = try #require(CalendarBusyness.window(day: .sun, week: week, timeZone: tokyo))
        #expect(sunday.start == utc("2026-09-20T07:00:00Z"))
        #expect(sunday.duration == 4 * 3600)
    }

    @Test func eveningWindowFollowsDaylightSavingTime() throws {
        // US daylight saving time ends Sunday November 1, 2026: 4 pm is MST (UTC−7) that day.
        let week = try #require(ISOWeek("2026-W44"))
        let saturday = try #require(CalendarBusyness.window(day: .sat, week: week, timeZone: denver))
        let sunday = try #require(CalendarBusyness.window(day: .sun, week: week, timeZone: denver))
        #expect(saturday.start == utc("2026-10-31T22:00:00Z"))
        #expect(sunday.start == utc("2026-11-01T23:00:00Z"))
        #expect(sunday.duration == 4 * 3600)
    }

    @Test func anEmptyEveningIsFree() {
        let evening = CalendarBusyness.evening(monday, intervals: [])
        #expect(evening == EveningBusyness(freeMinutes: 240, band: .free))
    }

    @Test func overlappingEventsCountOnce() {
        // 5–6:30 pm and 6–7 pm overlap: 2 hours taken, not 2½.
        let intervals = [
            CalendarBusyInterval(start: utc("2026-09-14T23:00:00Z"), end: utc("2026-09-15T00:30:00Z")),
            CalendarBusyInterval(start: utc("2026-09-15T00:00:00Z"), end: utc("2026-09-15T01:00:00Z")),
        ]
        let evening = CalendarBusyness.evening(monday, intervals: intervals)
        #expect(evening.freeMinutes == 120)
        #expect(evening.band == .some)
    }

    @Test func eventsAreClippedToTheWindow() {
        // A 2–5 pm meeting takes the first hour; a 7:30–11 pm event the last half hour.
        let intervals = [
            CalendarBusyInterval(start: utc("2026-09-14T20:00:00Z"), end: utc("2026-09-14T23:00:00Z")),
            CalendarBusyInterval(start: utc("2026-09-15T01:30:00Z"), end: utc("2026-09-15T05:00:00Z")),
            // Entirely before the evening.
            CalendarBusyInterval(start: utc("2026-09-14T15:00:00Z"), end: utc("2026-09-14T16:00:00Z")),
        ]
        #expect(CalendarBusyness.evening(monday, intervals: intervals).freeMinutes == 150)
    }

    @Test func allDayFreeAndDeclinedEventsDontBlockTheEvening() {
        let intervals = [
            CalendarBusyInterval(start: utc("2026-09-14T06:00:00Z"), end: utc("2026-09-15T06:00:00Z"), isAllDay: true),
            CalendarBusyInterval(start: utc("2026-09-14T22:00:00Z"), end: utc("2026-09-15T02:00:00Z"), isFree: true),
        ]
        #expect(CalendarBusyness.evening(monday, intervals: intervals).band == .free)
    }

    @Test func aFullyBookedEveningIsBusyAndSendsOneMinute() {
        let intervals = [CalendarBusyInterval(start: utc("2026-09-14T21:00:00Z"), end: utc("2026-09-15T03:00:00Z"))]
        let evening = CalendarBusyness.evening(monday, intervals: intervals)
        #expect(evening.freeMinutes == 0)
        #expect(evening.band == .busy)
        #expect(evening.requestFreeMinutes == 1)
    }

    @Test func bandThresholds() {
        #expect(CalendarBusyness.band(freeMinutes: 240) == .free)
        #expect(CalendarBusyness.band(freeMinutes: 210) == .free)
        #expect(CalendarBusyness.band(freeMinutes: 209) == .some)
        #expect(CalendarBusyness.band(freeMinutes: 90) == .some)
        #expect(CalendarBusyness.band(freeMinutes: 89) == .busy)
    }

    @Test func weekLeavesOutEveningsAlreadyOver() {
        // Wednesday 9 pm in Denver: Monday to Wednesday evenings are over.
        let now = utc("2026-09-17T03:00:00Z")
        let evenings = CalendarBusyness.week(week, timeZone: denver, intervals: [], onOrAfter: now)
        #expect(Set(evenings.keys) == [.thu, .fri, .sat, .sun])
    }

    @Test func theSameEventFallsOnDifferentEveningsByTimeZone() {
        // 23:00–01:00 UTC is 5–7 pm Monday in Denver, but Tuesday morning in Tokyo.
        let event = CalendarBusyInterval(start: utc("2026-09-14T23:00:00Z"), end: utc("2026-09-15T01:00:00Z"))
        #expect(CalendarBusyness.week(week, timeZone: denver, intervals: [event])[.mon]?.freeMinutes == 120)
        #expect(CalendarBusyness.week(week, timeZone: tokyo, intervals: [event])[.mon]?.freeMinutes == 240)
        #expect(CalendarBusyness.week(week, timeZone: tokyo, intervals: [event])[.tue]?.freeMinutes == 240)
    }
}

// MARK: - Weather banding

struct WeatherBandingTests {
    @Test func temperatureThresholds() {
        #expect(WeatherBanding.temperature(highFahrenheit: 20) == .cold)
        #expect(WeatherBanding.temperature(highFahrenheit: 54.9) == .cold)
        #expect(WeatherBanding.temperature(highFahrenheit: 55) == .mild)
        #expect(WeatherBanding.temperature(highFahrenheit: 84.9) == .mild)
        #expect(WeatherBanding.temperature(highFahrenheit: 85) == .hot)
    }

    @Test func precipitationNeedsAnEvenChance() {
        #expect(WeatherBanding.precipitation(.rain, chance: 0.49) == .none)
        #expect(WeatherBanding.precipitation(.rain, chance: 0.5) == .rain)
        #expect(WeatherBanding.precipitation(.snow, chance: 0.8) == .snow)
        #expect(WeatherBanding.precipitation(.sleet, chance: 0.8) == .snow)
        #expect(WeatherBanding.precipitation(.hail, chance: 0.8) == .snow)
        #expect(WeatherBanding.precipitation(.mixed, chance: 0.8) == .snow)
        #expect(WeatherBanding.precipitation(.none, chance: 1) == .none)
    }

    @Test func forecastDaysMatchWeekDaysInTheHouseholdsTimeZone() {
        let forecast = [
            // Tuesday in Denver (06:00 UTC is midnight MDT).
            DailyForecastSample(
                date: utc("2026-09-15T06:00:00Z"), highFahrenheit: 48, precipitation: .rain, precipitationChance: 0.7),
            DailyForecastSample(
                date: utc("2026-09-19T06:00:00Z"), highFahrenheit: 91, precipitation: .none, precipitationChance: 0),
            // The next week: ignored.
            DailyForecastSample(
                date: utc("2026-09-22T06:00:00Z"), highFahrenheit: 60, precipitation: .none, precipitationChance: 0),
        ]
        let bands = WeatherBanding.week(week, timeZone: denver, forecast: forecast)
        #expect(
            bands == [
                .tue: DayWeatherBands(temperature: .cold, precipitation: .rain),
                .sat: DayWeatherBands(temperature: .hot, precipitation: .none),
            ])
    }
}

// MARK: - Signals in requests and proposals

struct DeviceSignalsCodingTests {
    @Test func combineMergesCalendarAndWeatherAndDropsEmptyDays() throws {
        let signals = try #require(
            AutopilotDeviceSignals.combine(
                calendar: [.tue: EveningBusyness(freeMinutes: 45, band: .busy)],
                weather: [
                    .tue: DayWeatherBands(temperature: .cold, precipitation: .rain),
                    .mon: DayWeatherBands(temperature: .mild, precipitation: .none),
                ]))
        #expect(signals.days.map(\.day) == [.mon, .tue])
        let data = try JSONEncoder().encode(AutopilotGenerateRequest(signals: signals))
        let json = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let days = try #require((json["signals"] as? [String: Any])?["days"] as? [[String: Any]])
        #expect(days[0] as NSDictionary == ["day": "mon", "temperatureBand": "mild", "precipitation": "none"])
        #expect(
            days[1] as NSDictionary
                == [
                    "day": "tue", "busyness": "busy", "eveningFreeMinutes": 45, "temperatureBand": "cold",
                    "precipitation": "rain",
                ])
        // Only the banded fields: no events, times, forecasts, or coordinates.
        let keys = Set(days.flatMap(\.keys))
        #expect(keys.isSubset(of: ["day", "busyness", "eveningFreeMinutes", "temperatureBand", "precipitation"]))
    }

    @Test func combineWithNothingIsNil() {
        #expect(AutopilotDeviceSignals.combine(calendar: [:], weather: [:]) == nil)
    }

    @Test func aRequestWithoutSignalsLeavesThemOut() throws {
        let data = try JSONEncoder().encode(AutopilotGenerateRequest())
        #expect(String(decoding: data, as: UTF8.self) == "{}")
    }

    @Test func proposalContextSaysWhenWeatherShapedTheWeek() throws {
        let base = AutopilotFixtures.proposal()
        let decoder = JSONCoding.makeDecoder()
        let plain = try decoder.decode(AutopilotProposal.self, from: Data(base.utf8))
        #expect(plain.context == nil)
        #expect(!plain.usesWeather)

        func with(_ context: String) throws -> AutopilotProposal {
            let json = String(base.dropLast()) + #","context":\#(context)}"#
            return try decoder.decode(AutopilotProposal.self, from: Data(json.utf8))
        }
        let calendarOnly = try with(
            #"{"season":"fall","orderDate":null,"holidays":[],"signals":{"days":[{"day":"mon","busyness":"busy"}]}}"#)
        #expect(!calendarOnly.usesWeather)
        let weather = try with(
            #"{"season":"fall","orderDate":null,"holidays":[],"signals":{"days":[{"day":"mon","busyness":"hectic"},{"day":"tue","temperatureBand":"cold"}]}}"#
        )
        #expect(weather.usesWeather)
        // A value this app doesn't know drops that day, not the proposal.
        #expect(weather.context?.signals?.days.map(\.day) == [.tue])
    }
}

// MARK: - Permissions and the context engine

@MainActor
final class FakeCalendarSource: CalendarSignalSource {
    var authorization: DeviceSignalAuthorization
    var answer: DeviceSignalAuthorization
    var intervals: [CalendarBusyInterval] = []
    var fails = false
    private(set) var requests = 0
    private(set) var reads = 0

    init(_ authorization: DeviceSignalAuthorization = .notDetermined, answer: DeviceSignalAuthorization = .granted) {
        self.authorization = authorization
        self.answer = answer
    }

    func requestAccess() async -> DeviceSignalAuthorization {
        requests += 1
        authorization = answer
        return answer
    }

    func busyIntervals(in interval: DateInterval) async throws -> [CalendarBusyInterval] {
        reads += 1
        if fails { throw URLError(.unknown) }
        return intervals
    }
}

@MainActor
final class FakeLocationSource: LocationSignalSource {
    var authorization: DeviceSignalAuthorization
    var answer: DeviceSignalAuthorization
    var fails = false
    private(set) var requests = 0

    init(_ authorization: DeviceSignalAuthorization = .notDetermined, answer: DeviceSignalAuthorization = .granted) {
        self.authorization = authorization
        self.answer = answer
    }

    func requestAccess() async -> DeviceSignalAuthorization {
        requests += 1
        authorization = answer
        return answer
    }

    func approximateLocation() async throws -> ApproximateCoordinate {
        if fails { throw URLError(.notConnectedToInternet) }
        return ApproximateCoordinate(latitude: 39.7, longitude: -105)
    }
}

@MainActor
final class FakeWeatherSource: WeatherSignalSource {
    var forecast: [DailyForecastSample] = []
    private(set) var reads = 0

    func dailyForecast(at coordinate: ApproximateCoordinate, from start: Date, to end: Date) async throws
        -> [DailyForecastSample]
    {
        reads += 1
        return forecast
    }

    func attribution() async -> WeatherAttribution { .fallback }
}

@MainActor
struct AutopilotDeviceContextTests {
    private struct Harness {
        let context: AutopilotDeviceContext
        let calendar: FakeCalendarSource
        let location: FakeLocationSource
        let weather: FakeWeatherSource
        let settings: InMemoryDeviceContextSettings
    }

    /// Sunday September 13, before the week starts.
    private func harness(
        calendar: FakeCalendarSource = FakeCalendarSource(), location: FakeLocationSource = FakeLocationSource(),
        settings: InMemoryDeviceContextSettings = InMemoryDeviceContextSettings(),
        now: Date = utc("2026-09-13T18:00:00Z")
    ) -> Harness {
        let weather = FakeWeatherSource()
        weather.forecast = [
            DailyForecastSample(
                date: utc("2026-09-15T12:00:00Z"), highFahrenheit: 40, precipitation: .snow, precipitationChance: 0.9)
        ]
        calendar.intervals = [
            CalendarBusyInterval(start: utc("2026-09-14T22:00:00Z"), end: utc("2026-09-15T01:30:00Z"))
        ]
        let context = AutopilotDeviceContext(
            calendar: calendar, location: location, weather: weather, settings: settings, now: { now })
        return Harness(context: context, calendar: calendar, location: location, weather: weather, settings: settings)
    }

    @Test func explainerComesFirstAndContinueAsksForBoth() async {
        let h = harness()
        #expect(h.context.needsExplainer)
        // Planning before the explainer never prompts and sends nothing.
        #expect(await h.context.signals(for: week, timeZone: denver) == nil)
        #expect(h.calendar.requests == 0 && h.location.requests == 0)

        await h.context.continueFromExplainer()

        #expect(h.calendar.requests == 1 && h.location.requests == 1)
        #expect(h.context.calendarAuthorization == .granted && h.context.locationAuthorization == .granted)
        #expect(!h.context.needsExplainer)
        #expect(h.settings.hasSeenExplainer)
        let signals = await h.context.signals(for: week, timeZone: denver)
        #expect(signals?.days.first { $0.day == .mon }?.busyness == .busy)
        #expect(signals?.days.first { $0.day == .mon }?.eveningFreeMinutes == 30)
        #expect(
            signals?.days.first { $0.day == .tue }
                == AutopilotDeviceDaySignals(
                    day: .tue, busyness: .free, eveningFreeMinutes: 240, temperatureBand: .cold, precipitation: .snow))
    }

    @Test func notNowNeverAsksAndDoesntExplainAgain() async {
        let h = harness()
        h.context.skipExplainer()
        #expect(!h.context.needsExplainer)
        #expect(await h.context.signals(for: week, timeZone: denver) == nil)
        #expect(h.calendar.requests == 0 && h.location.requests == 0)
    }

    @Test func alreadyDecidedPermissionsNeedNoExplainer() {
        let h = harness(calendar: FakeCalendarSource(.granted), location: FakeLocationSource(.denied))
        #expect(!h.context.needsExplainer)
    }

    @Test func deniedSignalsAreLeftOutAndPlanningStillWorks() async {
        let h = harness(calendar: FakeCalendarSource(.denied), location: FakeLocationSource(.granted))
        let signals = await h.context.signals(for: week, timeZone: denver)
        #expect(h.calendar.reads == 0)
        #expect(signals?.days.allSatisfy { !$0.hasCalendar } == true)
        #expect(signals?.hasWeather == true)

        let none = harness(calendar: FakeCalendarSource(.denied), location: FakeLocationSource(.denied))
        #expect(await none.context.signals(for: week, timeZone: denver) == nil)
        #expect(none.weather.reads == 0)
    }

    @Test func switchedOffSignalsAreNotRead() async {
        let settings = InMemoryDeviceContextSettings(usesCalendar: false, usesWeather: true, hasSeenExplainer: true)
        let h = harness(
            calendar: FakeCalendarSource(.granted), location: FakeLocationSource(.granted), settings: settings)
        #expect(!h.context.usesCalendar)
        _ = await h.context.signals(for: week, timeZone: denver)
        #expect(h.calendar.reads == 0)

        await h.context.setUsesWeather(false)
        #expect(!h.settings.usesWeather)
        #expect(await h.context.signals(for: week, timeZone: denver) == nil)
        #expect(h.weather.reads == 1)
    }

    @Test func switchingOnAnUnaskedSignalAsks() async {
        let settings = InMemoryDeviceContextSettings(usesCalendar: false, usesWeather: false, hasSeenExplainer: true)
        let h = harness(calendar: FakeCalendarSource(answer: .denied), settings: settings)
        await h.context.setUsesCalendar(true)
        #expect(h.calendar.requests == 1)
        #expect(h.context.calendarAuthorization == .denied)
        #expect(h.settings.usesCalendar)
    }

    @Test func failuresAreLeftOut() async {
        let calendar = FakeCalendarSource(.granted)
        calendar.fails = true
        let location = FakeLocationSource(.granted)
        location.fails = true
        let h = harness(calendar: calendar, location: location)
        #expect(await h.context.signals(for: week, timeZone: denver) == nil)
    }

    @Test func permissionChangedInSettingsIsNoticed() {
        let h = harness(calendar: FakeCalendarSource(.granted))
        h.calendar.authorization = .denied
        h.context.refreshAuthorization()
        #expect(h.context.calendarAuthorization == .denied)
    }

    @Test func aPastWeekHasNoForecast() async {
        let h = harness(
            calendar: FakeCalendarSource(.denied), location: FakeLocationSource(.granted),
            now: utc("2026-09-30T18:00:00Z"))
        #expect(await h.context.signals(for: week, timeZone: denver) == nil)
        #expect(h.weather.reads == 0)
    }
}
