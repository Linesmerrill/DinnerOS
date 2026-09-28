import ActivityKit
import Foundation

/// The Live Activity for a meal-kit recipe import (`docs/meal-kit-import.md#live-activity`).
///
/// Compiled into the app, which starts and updates the activity, and into the
/// `DinnerOSLiveActivities` widget extension, which draws it. The server sends the same
/// `ContentState` as the `content-state` of its Live Activity pushes (`internal/liveactivity`),
/// so the field names here are a wire format: change them on both sides or not at all.
///
/// What it may show is deliberately narrow — the service's name and recipe counts. The Lock
/// Screen is readable by anyone holding the phone, so nothing about the meal-kit account, no
/// recipe names, and nothing about the household ever goes in here.
nonisolated struct MealKitImportActivityAttributes: ActivityAttributes {
    /// The part that changes. Four small fields, far under the 4 KB push payload limit.
    nonisolated struct ContentState: Codable, Hashable, Sendable {
        nonisolated enum Phase: String, Codable, Hashable, Sendable {
            /// A worker is fetching recipe pages right now.
            case importing
            /// Queued: not started yet, between polite batches, or waiting out a retry.
            case waiting
            /// Finished. The activity turns green.
            case done
            /// The import gave up.
            case failed
            /// Someone stopped it.
            case canceled

            /// A phase a newer server invented reads as waiting: it never claims progress or an
            /// ending that did not happen.
            init(from decoder: any Decoder) throws {
                let raw = try decoder.singleValueContainer().decode(String.self)
                self = Phase(rawValue: raw) ?? .waiting
            }
        }

        var phase: Phase
        /// Recipes handled so far; once done, recipes now in the library from this history.
        var done: Int
        /// Recipes on the order history that was read.
        var total: Int
        /// Recipes that could not be imported.
        var failed: Int

        init(phase: Phase, done: Int, total: Int, failed: Int = 0) {
            self.phase = phase
            self.done = done
            self.total = total
            self.failed = failed
        }

        /// How far along, 0...1. A finished import is full whatever the counts say.
        var fraction: Double {
            if phase == .done { return 1 }
            guard total > 0 else { return 0 }
            return min(1, max(0, Double(done) / Double(total)))
        }

        /// True once nothing more will happen.
        var isFinal: Bool { phase == .done || phase == .failed || phase == .canceled }

        /// True for the two endings that are not good news.
        var isStopped: Bool { phase == .failed || phase == .canceled }

        /// The short line under the count.
        var status: String {
            switch phase {
            case .importing: String(localized: "Importing")
            case .waiting: done == 0 ? String(localized: "Starting soon") : String(localized: "Next batch soon")
            case .done:
                failed > 0 ? String(localized: "\(failed) couldn’t be read") : String(localized: "All done")
            case .failed: String(localized: "Open DinnerOS to see why")
            case .canceled: String(localized: "Recipes already imported stay")
            }
        }

        /// The headline: the count while working, the outcome once final.
        var headline: String {
            switch phase {
            case .importing, .waiting: String(localized: "\(done) of \(total) recipes")
            case .done:
                done == 1 ? String(localized: "1 recipe imported") : String(localized: "\(done) recipes imported")
            case .failed, .canceled: String(localized: "Import stopped")
            }
        }

        /// The compact count for the Dynamic Island: "40/740", or a check once done.
        var compactCount: String { "\(done)/\(total)" }
    }

    /// The service as a member reads it: "HelloFresh".
    let serviceName: String
    /// The service's API name: "hellofresh".
    let service: String
    /// Our own household ID, never shown. The app needs it to hand a rotated push token to the
    /// right route after a relaunch.
    let householdID: String
    /// The import run this activity follows.
    let jobID: String
}
