import Foundation
import UIKit

/// One line for the server log about how a meal-kit sign-in attempt ended, so a timeout, a
/// crashed page, or a changed API at the service can be seen and fixed
/// (`POST .../meal-kit/{source}/sign-in-reports`).
///
/// Only fixed codes, counts and versions: never page text, URLs, cookies, tokens or anything
/// the member typed. The server checks every field against an allow-list too.
nonisolated struct MealKitSignInReport: Encodable, Equatable, Sendable {
    enum Outcome: String, Encodable, Sendable {
        case harvested
        case upToDate = "up_to_date"
        case failed
        /// The sign-in was never picked up before the timeout.
        case noSession = "no_session"
        case pageCrashed = "page_crashed"
        /// The sign-in page didn't load.
        case pageFailed = "page_failed"
        /// The member closed the sheet before it finished.
        case cancelled
    }

    var outcome: Outcome
    var reason: String?
    var stage: String?
    var httpStatus: Int?
    var phase: String?
    var elapsedMs: Int64 = 0
    var attempt = 1
    var retried = false
    var pageCrashes = 0
    var navigationError: String?
    var recipes: Int?
    var weeks: Int?
    var pages: Int?
    var stopped: String?
    var appVersion: String? = MealKitSignInReport.currentAppVersion
    var osVersion: String? = MealKitSignInReport.currentOSVersion
    var device: String? = MealKitSignInReport.currentDevice

    private enum CodingKeys: String, CodingKey {
        case outcome, reason, stage, httpStatus, phase, elapsedMs, attempt, retried, pageCrashes, navigationError
        case recipes, weeks, pages, stopped, appVersion, osVersion, device
    }

    func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(outcome, forKey: .outcome)
        try c.encodeIfPresent(reason, forKey: .reason)
        try c.encodeIfPresent(stage, forKey: .stage)
        try c.encodeIfPresent(httpStatus, forKey: .httpStatus)
        try c.encodeIfPresent(phase, forKey: .phase)
        try c.encode(elapsedMs, forKey: .elapsedMs)
        try c.encode(attempt, forKey: .attempt)
        if retried { try c.encode(true, forKey: .retried) }
        if pageCrashes > 0 { try c.encode(pageCrashes, forKey: .pageCrashes) }
        try c.encodeIfPresent(navigationError, forKey: .navigationError)
        try c.encodeIfPresent(recipes, forKey: .recipes)
        try c.encodeIfPresent(weeks, forKey: .weeks)
        try c.encodeIfPresent(pages, forKey: .pages)
        try c.encodeIfPresent(stopped, forKey: .stopped)
        try c.encodeIfPresent(appVersion, forKey: .appVersion)
        try c.encodeIfPresent(osVersion, forKey: .osVersion)
        try c.encodeIfPresent(device, forKey: .device)
    }

    /// "0.1.0 (86)".
    static var currentAppVersion: String? {
        let info = Bundle.main.infoDictionary
        guard let version = info?["CFBundleShortVersionString"] as? String,
            let build = info?["CFBundleVersion"] as? String
        else { return nil }
        return "\(version) (\(build))"
    }

    /// "26.2".
    static var currentOSVersion: String? {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return v.patchVersion > 0
            ? "\(v.majorVersion).\(v.minorVersion).\(v.patchVersion)" : "\(v.majorVersion).\(v.minorVersion)"
    }

    /// The model identifier, e.g. "iPhone17,1".
    static var currentDevice: String? {
        if let simulated = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] {
            return "Simulator " + simulated
        }
        var info = utsname()
        uname(&info)
        let machine = withUnsafeBytes(of: &info.machine) { raw in
            String(decoding: raw.prefix { $0 != 0 }, as: UTF8.self)
        }
        return machine.isEmpty ? nil : machine
    }
}
