import Foundation
import Network

/// Whether the screens are showing saved answers because the network can't be reached, and
/// since when. Drives the "Offline" line and refreshes the app once the connection is back.
@MainActor @Observable
final class OfflineStatus {
    /// When the oldest saved answer on screen was saved; `nil` while everything is live.
    private(set) var savedAt: Date?
    /// The phone has no usable network path at all.
    private(set) var isDisconnected = false

    var isShowingSaved: Bool { savedAt != nil || isDisconnected }

    /// Run when the connection comes back after being lost, to reload what's on screen.
    @ObservationIgnored var onReconnect: (() -> Void)?

    @ObservationIgnored private var monitor: NWPathMonitor?

    func replayed(savedAt date: Date) {
        if let savedAt, savedAt <= date { return }
        savedAt = date
    }

    func live() {
        guard savedAt != nil else { return }
        savedAt = nil
    }

    /// Watches the network path; losing it shows the Offline line at once, and getting it back
    /// reloads.
    func start() {
        guard monitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            let connected = path.status == .satisfied
            Task { @MainActor in self?.pathChanged(connected: connected) }
        }
        monitor.start(queue: DispatchQueue(label: "DinnerOS.OfflineStatus"))
        self.monitor = monitor
    }

    private func pathChanged(connected: Bool) {
        let wasDisconnected = isDisconnected
        isDisconnected = !connected
        if connected, wasDisconnected || savedAt != nil {
            onReconnect?()
        }
    }

    /// The cache's callbacks, which arrive off the main actor.
    nonisolated static func cache(reportingTo status: OfflineStatus) -> ResponseCache {
        ResponseCache(
            onReplay: { date in Task { @MainActor in status.replayed(savedAt: date) } },
            onLive: { Task { @MainActor in status.live() } })
    }
}
