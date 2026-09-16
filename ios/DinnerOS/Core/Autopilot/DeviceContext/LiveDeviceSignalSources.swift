import CoreLocation
import EventKit
import Foundation
import WeatherKit

// The real calendar, location, and weather sources. Nothing here is unit tested: tests inject
// fakes of the protocols in `DeviceSignalProviders.swift`, so they never touch EventKit,
// CoreLocation, or WeatherKit. Keep these thin.

/// The member's calendars, read through EventKit with full access (read needs it on iOS 17+).
@MainActor
final class EventKitCalendarSource: CalendarSignalSource {
    private let store = EKEventStore()

    var authorization: DeviceSignalAuthorization {
        switch EKEventStore.authorizationStatus(for: .event) {
        case .notDetermined: .notDetermined
        case .fullAccess: .granted
        // Write-only access can't read busy times.
        default: .denied
        }
    }

    func requestAccess() async -> DeviceSignalAuthorization {
        _ = try? await store.requestFullAccessToEvents()
        return authorization
    }

    func busyIntervals(in interval: DateInterval) async throws -> [CalendarBusyInterval] {
        guard authorization == .granted else { return [] }
        let predicate = store.predicateForEvents(withStart: interval.start, end: interval.end, calendars: nil)
        // Only the times, all-day flag, availability, and the member's own response are read.
        return store.events(matching: predicate).map { event in
            let declined = event.attendees?.contains { $0.isCurrentUser && $0.participantStatus == .declined } ?? false
            return CalendarBusyInterval(
                start: event.startDate, end: event.endDate, isAllDay: event.isAllDay,
                isFree: event.availability == .free || declined)
        }
    }
}

/// When In Use location at reduced accuracy: only roughly where the member is, for a forecast.
@MainActor
final class CoreLocationSource: NSObject, LocationSignalSource, @preconcurrency CLLocationManagerDelegate {
    enum LocationError: Error {
        case unavailable
    }

    private let manager: CLLocationManager
    private var authorizationContinuations: [CheckedContinuation<DeviceSignalAuthorization, Never>] = []
    private var locationContinuations: [CheckedContinuation<ApproximateCoordinate, any Error>] = []

    override init() {
        manager = CLLocationManager()
        super.init()
        manager.desiredAccuracy = kCLLocationAccuracyReduced
        manager.delegate = self
    }

    var authorization: DeviceSignalAuthorization {
        switch manager.authorizationStatus {
        case .notDetermined: .notDetermined
        case .authorizedWhenInUse, .authorizedAlways: .granted
        default: .denied
        }
    }

    func requestAccess() async -> DeviceSignalAuthorization {
        guard authorization == .notDetermined else { return authorization }
        return await withCheckedContinuation { continuation in
            authorizationContinuations.append(continuation)
            manager.requestWhenInUseAuthorization()
        }
    }

    func approximateLocation() async throws -> ApproximateCoordinate {
        guard authorization == .granted else { throw LocationError.unavailable }
        // A recent fix is close enough for a daily forecast.
        if let recent = manager.location, recent.timestamp.timeIntervalSinceNow > -3600 {
            return ApproximateCoordinate(latitude: recent.coordinate.latitude, longitude: recent.coordinate.longitude)
        }
        return try await withCheckedThrowingContinuation { continuation in
            locationContinuations.append(continuation)
            if locationContinuations.count == 1 {
                manager.requestLocation()
            }
        }
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        // The first callback arrives on creation, before any request; only a decided status
        // answers a pending request.
        guard manager.authorizationStatus != .notDetermined else { return }
        let result = authorization
        let waiting = authorizationContinuations
        authorizationContinuations = []
        waiting.forEach { $0.resume(returning: result) }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let last = locations.last else { return }
        let coordinate = ApproximateCoordinate(latitude: last.coordinate.latitude, longitude: last.coordinate.longitude)
        let waiting = locationContinuations
        locationContinuations = []
        waiting.forEach { $0.resume(returning: coordinate) }
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: any Error) {
        let waiting = locationContinuations
        locationContinuations = []
        waiting.forEach { $0.resume(throwing: LocationError.unavailable) }
    }
}

/// Daily forecasts and attribution from Apple Weather.
@MainActor
final class WeatherKitSource: WeatherSignalSource {
    func dailyForecast(at coordinate: ApproximateCoordinate, from start: Date, to end: Date) async throws
        -> [DailyForecastSample]
    {
        let location = CLLocation(latitude: coordinate.latitude, longitude: coordinate.longitude)
        let forecast = try await WeatherService.shared.weather(
            for: location, including: .daily(startDate: start, endDate: end))
        return forecast.forecast.map { day in
            DailyForecastSample(
                date: day.date, highFahrenheit: day.highTemperature.converted(to: .fahrenheit).value,
                precipitation: Self.kind(day.precipitation), precipitationChance: day.precipitationChance)
        }
    }

    func attribution() async -> WeatherAttribution {
        guard let attribution = try? await WeatherService.shared.attribution else { return .fallback }
        return WeatherAttribution(
            serviceName: attribution.serviceName, lightMarkURL: attribution.combinedMarkLightURL,
            darkMarkURL: attribution.combinedMarkDarkURL, legalPageURL: attribution.legalPageURL)
    }

    private static func kind(_ precipitation: Precipitation) -> ForecastPrecipitationKind {
        switch precipitation {
        case .rain: .rain
        case .snow: .snow
        case .sleet: .sleet
        case .hail: .hail
        case .mixed: .mixed
        default: .none
        }
    }
}
