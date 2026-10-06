import Foundation
import SwiftUI

/// When the shown week reloads without a pull: the kitchen iPad stays open for days, so another
/// member's change (a meal moved to Tuesday on a phone) has to arrive on its own.
nonisolated enum PlanRefresh {
    /// How often the Menu reloads while it's on screen and the app is in front.
    static let interval: Duration = .seconds(120)

    /// Remembers whether the app went to the background, so coming back reloads. The return trip
    /// passes through inactive (background → inactive → active), so the previous phase alone can't
    /// tell; the first launch and a glance at Control Center (inactive only) don't reload.
    @MainActor final class Tracker {
        private var wasInBackground = false

        /// Whether reaching `phase` should reload the shown week.
        func phaseChanged(to phase: ScenePhase) -> Bool {
            switch phase {
            case .background:
                wasInBackground = true
                return false
            case .active:
                defer { wasInBackground = false }
                return wasInBackground
            default:
                return false
            }
        }
    }

    /// Runs `work` in its own task and waits for it. SwiftUI cancels a `.refreshable` action when
    /// the list redraws mid-pull, which dropped the menu request half-way and left the old meals on
    /// screen; the pull still waits for the answer, but can no longer cancel it.
    static func uncancellable(_ work: @escaping @Sendable () async -> Void) async {
        await Task { await work() }.value
    }
}
