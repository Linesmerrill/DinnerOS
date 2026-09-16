import Foundation

/// Where the member stands on a device permission, reduced to what Autopilot needs.
nonisolated enum DeviceSignalAuthorization: Hashable, Sendable {
    /// Never asked.
    case notDetermined
    /// Readable.
    case granted
    /// Declined, restricted, or only partly allowed (write-only calendars). Only Settings can
    /// change it, and Autopilot plans without the signal.
    case denied
}

/// An approximate position, kept on the device only to ask for a forecast.
nonisolated struct ApproximateCoordinate: Hashable, Sendable {
    let latitude: Double
    let longitude: Double
}

/// The member's calendars. `EventKitCalendarSource` is the real one; tests use a fake so they
/// never touch EventKit.
@MainActor
protocol CalendarSignalSource: AnyObject {
    var authorization: DeviceSignalAuthorization { get }
    /// Shows the system prompt when not yet asked.
    func requestAccess() async -> DeviceSignalAuthorization
    /// Busy spans overlapping `interval`, across every calendar.
    func busyIntervals(in interval: DateInterval) async throws -> [CalendarBusyInterval]
}

/// Reduced-accuracy location. `CoreLocationSource` is the real one.
@MainActor
protocol LocationSignalSource: AnyObject {
    var authorization: DeviceSignalAuthorization { get }
    /// Asks for When In Use access when not yet asked.
    func requestAccess() async -> DeviceSignalAuthorization
    func approximateLocation() async throws -> ApproximateCoordinate
}

/// What Apple Weather requires wherever its data shapes what's shown.
nonisolated struct WeatherAttribution: Hashable, Sendable {
    /// "Weather" (with the Apple logo) when the mark can't be loaded.
    let serviceName: String
    let lightMarkURL: URL?
    let darkMarkURL: URL?
    let legalPageURL: URL?

    /// Used when WeatherKit's attribution can't be fetched (offline, or the simulator).
    static let fallback = WeatherAttribution(
        serviceName: "\u{F8FF} Weather", lightMarkURL: nil, darkMarkURL: nil,
        legalPageURL: URL(string: "https://weatherkit.apple.com/legal-attribution.html"))
}

/// Daily forecasts. `WeatherKitSource` is the real one.
@MainActor
protocol WeatherSignalSource: AnyObject {
    func dailyForecast(at coordinate: ApproximateCoordinate, from start: Date, to end: Date) async throws
        -> [DailyForecastSample]
    func attribution() async -> WeatherAttribution
}

/// Which signals this member allows on this device, and whether they've seen the explainer.
@MainActor
protocol DeviceContextSettingsStorage: AnyObject {
    var usesCalendar: Bool { get set }
    var usesWeather: Bool { get set }
    var hasSeenExplainer: Bool { get set }
}

@MainActor
final class UserDefaultsDeviceContextSettings: DeviceContextSettingsStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    var usesCalendar: Bool {
        get { defaults.object(forKey: Keys.calendar) as? Bool ?? true }
        set { defaults.set(newValue, forKey: Keys.calendar) }
    }

    var usesWeather: Bool {
        get { defaults.object(forKey: Keys.weather) as? Bool ?? true }
        set { defaults.set(newValue, forKey: Keys.weather) }
    }

    var hasSeenExplainer: Bool {
        get { defaults.bool(forKey: Keys.explainer) }
        set { defaults.set(newValue, forKey: Keys.explainer) }
    }

    private enum Keys {
        static let calendar = "autopilot.deviceContext.usesCalendar"
        static let weather = "autopilot.deviceContext.usesWeather"
        static let explainer = "autopilot.deviceContext.hasSeenExplainer"
    }
}

@MainActor
final class InMemoryDeviceContextSettings: DeviceContextSettingsStorage {
    var usesCalendar: Bool
    var usesWeather: Bool
    var hasSeenExplainer: Bool

    init(usesCalendar: Bool = true, usesWeather: Bool = true, hasSeenExplainer: Bool = false) {
        self.usesCalendar = usesCalendar
        self.usesWeather = usesWeather
        self.hasSeenExplainer = hasSeenExplainer
    }
}
