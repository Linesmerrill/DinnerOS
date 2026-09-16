import Foundation
import Observation
import os

/// Calendar and weather context for Autopilot, derived on the iPhone.
///
/// It asks for permission only after the member has seen the explainer, the first time they
/// plan with Autopilot, and never from Siri. Signals are read only from sources the member
/// allowed and left switched on; anything denied, off, or failing is simply left out, and
/// Autopilot plans without it. Nothing but the banded per-day values is returned: event
/// details, forecasts, and coordinates stay in this type's locals.
@Observable
final class AutopilotDeviceContext {
    private(set) var calendarAuthorization: DeviceSignalAuthorization
    private(set) var locationAuthorization: DeviceSignalAuthorization
    private(set) var usesCalendar: Bool
    private(set) var usesWeather: Bool
    private(set) var hasSeenExplainer: Bool
    /// Apple Weather's attribution, once loaded.
    private(set) var weatherAttribution: WeatherAttribution?

    @ObservationIgnored private let calendar: any CalendarSignalSource
    @ObservationIgnored private let location: any LocationSignalSource
    @ObservationIgnored private let weather: any WeatherSignalSource
    @ObservationIgnored private let settings: any DeviceContextSettingsStorage
    @ObservationIgnored private let now: () -> Date

    private static let logger = Logger(subsystem: "DinnerOS", category: "autopilot-context")

    init(
        calendar: any CalendarSignalSource, location: any LocationSignalSource, weather: any WeatherSignalSource,
        settings: any DeviceContextSettingsStorage, now: @escaping () -> Date = Date.init
    ) {
        self.calendar = calendar
        self.location = location
        self.weather = weather
        self.settings = settings
        self.now = now
        calendarAuthorization = calendar.authorization
        locationAuthorization = location.authorization
        usesCalendar = settings.usesCalendar
        usesWeather = settings.usesWeather
        hasSeenExplainer = settings.hasSeenExplainer
    }

    /// Whether planning should first explain what's read: never shown, and a switched-on
    /// signal hasn't been asked for yet.
    var needsExplainer: Bool {
        guard !hasSeenExplainer else { return false }
        // Read live rather than from the observed copies, which may predate a Settings change.
        return (usesCalendar && calendar.authorization == .notDetermined)
            || (usesWeather && location.authorization == .notDetermined)
    }

    /// Re-reads permissions, which the member can change in Settings at any time.
    func refreshAuthorization() {
        let calendarNow = calendar.authorization
        if calendarNow != calendarAuthorization { calendarAuthorization = calendarNow }
        let locationNow = location.authorization
        if locationNow != locationAuthorization { locationAuthorization = locationNow }
    }

    /// The explainer's Continue: asks for each switched-on signal that hasn't been asked for.
    func continueFromExplainer() async {
        markExplainerSeen()
        if usesCalendar, calendar.authorization == .notDetermined {
            calendarAuthorization = await calendar.requestAccess()
        }
        if usesWeather, location.authorization == .notDetermined {
            locationAuthorization = await location.requestAccess()
        }
        refreshAuthorization()
    }

    /// The explainer's Not Now: plans without asking, and doesn't explain again.
    func skipExplainer() {
        markExplainerSeen()
    }

    /// The Autopilot settings toggle. Switching on a signal that was never asked for asks.
    func setUsesCalendar(_ uses: Bool) async {
        usesCalendar = uses
        settings.usesCalendar = uses
        if uses, calendar.authorization == .notDetermined {
            calendarAuthorization = await calendar.requestAccess()
        }
    }

    func setUsesWeather(_ uses: Bool) async {
        usesWeather = uses
        settings.usesWeather = uses
        if uses, location.authorization == .notDetermined {
            locationAuthorization = await location.requestAccess()
        }
    }

    /// The signals for `week` in the household's `timeZone`, or `nil` when there are none.
    /// Never prompts.
    func signals(for week: ISOWeek, timeZone: TimeZone) async -> AutopilotDeviceSignals? {
        refreshAuthorization()
        async let calendarBands = calendarSignals(week: week, timeZone: timeZone)
        async let weatherBands = weatherSignals(week: week, timeZone: timeZone)
        let (busy, bands) = await (calendarBands, weatherBands)
        let signals = AutopilotDeviceSignals.combine(calendar: busy, weather: bands)
        Self.logger.info(
            "Device signals: \(busy.count, privacy: .public) calendar days, \(bands.count, privacy: .public) weather days"
        )
        return signals
    }

    /// Loads Apple Weather's attribution for showing next to weather-shaped suggestions.
    func loadWeatherAttribution() async {
        guard weatherAttribution == nil else { return }
        weatherAttribution = await weather.attribution()
    }

    private func calendarSignals(week: ISOWeek, timeZone: TimeZone) async -> [PlanDay: EveningBusyness] {
        guard usesCalendar, calendar.authorization == .granted,
            let span = CalendarBusyness.weekSpan(week: week, timeZone: timeZone)
        else { return [:] }
        do {
            let intervals = try await calendar.busyIntervals(in: span)
            return CalendarBusyness.week(week, timeZone: timeZone, intervals: intervals, onOrAfter: now())
        } catch {
            // The error type only; never event details.
            Self.logger.notice("Calendar read failed: \(String(describing: type(of: error)), privacy: .public)")
            return [:]
        }
    }

    private func weatherSignals(week: ISOWeek, timeZone: TimeZone) async -> [PlanDay: DayWeatherBands] {
        guard usesWeather, location.authorization == .granted,
            let span = CalendarBusyness.weekSpan(week: week, timeZone: timeZone)
        else { return [:] }
        // A forecast reaches about ten days out; a week entirely past or beyond it has none.
        let start = max(span.start, now())
        guard start < span.end else { return [:] }
        do {
            let coordinate = try await location.approximateLocation()
            let forecast = try await weather.dailyForecast(at: coordinate, from: start, to: span.end)
            return WeatherBanding.week(week, timeZone: timeZone, forecast: forecast)
        } catch {
            Self.logger.notice("Weather read failed: \(String(describing: type(of: error)), privacy: .public)")
            return [:]
        }
    }

    private func markExplainerSeen() {
        hasSeenExplainer = true
        settings.hasSeenExplainer = true
    }
}
