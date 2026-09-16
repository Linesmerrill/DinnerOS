import Foundation

/// How busy an evening is, from the member's calendars (`busyness`).
nonisolated enum AutopilotBusyness: String, Codable, Hashable, Sendable, CaseIterable {
    case free, some, busy
}

/// A day's forecast temperature, banded on the phone (`temperatureBand`).
nonisolated enum AutopilotTemperatureBand: String, Codable, Hashable, Sendable, CaseIterable {
    case cold, mild, hot
}

/// A day's forecast precipitation, banded on the phone (`precipitation`).
nonisolated enum AutopilotPrecipitation: String, Codable, Hashable, Sendable, CaseIterable {
    case none, rain, snow
}

/// One day's signals derived on the device (`AutopilotDeviceDaySignals`). Only these
/// banded values ever leave the phone: never events, times, a forecast, or a location.
nonisolated struct AutopilotDeviceDaySignals: Codable, Hashable, Sendable {
    let day: PlanDay
    var busyness: AutopilotBusyness?
    /// 1–1440, as the API requires.
    var eveningFreeMinutes: Int?
    var temperatureBand: AutopilotTemperatureBand?
    var precipitation: AutopilotPrecipitation?

    /// A day with nothing set is left out of the request.
    var isEmpty: Bool {
        busyness == nil && eveningFreeMinutes == nil && temperatureBand == nil && precipitation == nil
    }

    var hasWeather: Bool { temperatureBand != nil || precipitation != nil }
    var hasCalendar: Bool { busyness != nil || eveningFreeMinutes != nil }
}

/// Calendar and weather context for a generate request (`AutopilotDeviceSignals`).
nonisolated struct AutopilotDeviceSignals: Codable, Hashable, Sendable {
    /// At most seven, one per day, in day order.
    var days: [AutopilotDeviceDaySignals]

    init(days: [AutopilotDeviceDaySignals]) {
        self.days = days.filter { !$0.isEmpty }.sorted { $0.day.offset < $1.day.offset }
    }

    /// Merges calendar and weather values for the same day. `nil` when nothing is set.
    static func combine(
        calendar: [PlanDay: EveningBusyness], weather: [PlanDay: DayWeatherBands]
    ) -> AutopilotDeviceSignals? {
        let days = PlanDay.allCases.map { day in
            AutopilotDeviceDaySignals(
                day: day, busyness: calendar[day]?.band, eveningFreeMinutes: calendar[day]?.requestFreeMinutes,
                temperatureBand: weather[day]?.temperature, precipitation: weather[day]?.precipitation)
        }
        let signals = AutopilotDeviceSignals(days: days)
        return signals.days.isEmpty ? nil : signals
    }

    var hasWeather: Bool { days.contains(where: \.hasWeather) }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // A day from a newer server that this app can't read is skipped rather than failing
        // the whole proposal.
        let lenient = try container.decodeIfPresent([LenientDay].self, forKey: .days) ?? []
        days = lenient.compactMap(\.value)
    }

    private enum CodingKeys: String, CodingKey {
        case days
    }

    private struct LenientDay: Decodable {
        let value: AutopilotDeviceDaySignals?

        init(from decoder: any Decoder) throws {
            value = try? AutopilotDeviceDaySignals(from: decoder)
        }
    }
}

/// The context a proposal was planned with (`AutopilotProposalContext`). The app reads only
/// the device signals, to show weather attribution when a forecast shaped the week.
nonisolated struct AutopilotProposalContext: Decodable, Hashable, Sendable {
    let signals: AutopilotDeviceSignals?
}
