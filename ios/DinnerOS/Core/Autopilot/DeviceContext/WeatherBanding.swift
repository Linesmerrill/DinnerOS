import Foundation

/// The kind of precipitation a daily forecast expects, independent of WeatherKit's types.
nonisolated enum ForecastPrecipitationKind: Hashable, Sendable {
    case none, rain, snow, sleet, hail, mixed
}

/// One day of a forecast, reduced to what banding needs. It never leaves the phone.
nonisolated struct DailyForecastSample: Hashable, Sendable {
    /// Any moment on the forecast day; the day is read in the household's time zone.
    let date: Date
    let highFahrenheit: Double
    let precipitation: ForecastPrecipitationKind
    /// 0–1.
    let precipitationChance: Double
}

/// A day's weather bands, the only weather values sent.
nonisolated struct DayWeatherBands: Hashable, Sendable {
    let temperature: AutopilotTemperatureBand
    let precipitation: AutopilotPrecipitation
}

/// Turns a daily forecast into `temperatureBand` and `precipitation` on the device.
///
/// Thresholds use the day's **high**, in °F, since dinner plans follow how the day felt:
/// - **cold**: high below 55 °F (soups, braises, comfort food)
/// - **hot**: high of 85 °F or more (light meals, no long oven time)
/// - **mild**: anything between
///
/// Precipitation counts when its chance is at least 50%: snow, sleet, hail, or a wintry mix
/// is `snow`; rain is `rain`; otherwise `none`.
nonisolated enum WeatherBanding {
    static let coldBelowFahrenheit = 55.0
    static let hotFromFahrenheit = 85.0
    static let precipitationChanceThreshold = 0.5

    static func temperature(highFahrenheit: Double) -> AutopilotTemperatureBand {
        if highFahrenheit < coldBelowFahrenheit { return .cold }
        if highFahrenheit >= hotFromFahrenheit { return .hot }
        return .mild
    }

    static func precipitation(_ kind: ForecastPrecipitationKind, chance: Double) -> AutopilotPrecipitation {
        guard chance >= precipitationChanceThreshold else { return .none }
        switch kind {
        case .none: return .none
        case .rain: return .rain
        case .snow, .sleet, .hail, .mixed: return .snow
        }
    }

    static func bands(_ sample: DailyForecastSample) -> DayWeatherBands {
        DayWeatherBands(
            temperature: temperature(highFahrenheit: sample.highFahrenheit),
            precipitation: precipitation(sample.precipitation, chance: sample.precipitationChance))
    }

    /// The bands for each day of `week` the forecast covers, matching samples to days in
    /// `timeZone`.
    static func week(_ week: ISOWeek, timeZone: TimeZone, forecast: [DailyForecastSample]) -> [PlanDay: DayWeatherBands]
    {
        var local = Calendar(identifier: .gregorian)
        local.timeZone = timeZone
        var result: [PlanDay: DayWeatherBands] = [:]
        for sample in forecast {
            let parts = local.dateComponents([.year, .month, .day], from: sample.date)
            for day in PlanDay.allCases where result[day] == nil {
                guard let date = day.date(in: week) else { continue }
                var utc = Calendar(identifier: .gregorian)
                utc.timeZone = .gmt
                let dayParts = utc.dateComponents([.year, .month, .day], from: date)
                if dayParts.year == parts.year, dayParts.month == parts.month, dayParts.day == parts.day {
                    result[day] = bands(sample)
                }
            }
        }
        return result
    }
}
