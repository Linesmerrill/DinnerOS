import Foundation
import UIKit
import UserNotifications

/// One kitchen timer on the cooking screen.
nonisolated struct CookTimer: Identifiable, Equatable, Sendable {
    enum State: Equatable, Sendable {
        case running(endsAt: Date)
        case paused(remaining: TimeInterval)
        case finished
    }

    let id: UUID
    /// What it's for: "Step 2", with the dish when more than one is being cooked.
    let label: String
    let total: TimeInterval
    var state: State

    func remaining(at now: Date) -> TimeInterval {
        switch state {
        case .running(let end): max(0, end.timeIntervalSince(now))
        case .paused(let remaining): remaining
        case .finished: 0
        }
    }

    /// 0 when started, 1 when done.
    func progress(at now: Date) -> Double {
        total > 0 ? 1 - remaining(at: now) / total : 1
    }

    /// "7:42", or "1:05:00" past an hour.
    static func clock(_ seconds: TimeInterval) -> String {
        let s = Int(seconds.rounded(.up))
        let h = s / 3600, m = (s % 3600) / 60, r = s % 60
        return h > 0 ? String(format: "%d:%02d:%02d", h, m, r) : String(format: "%d:%02d", m, r)
    }
}

/// The cooking screen's timers. Several run at once; each one that finishes buzzes, plays a
/// chime until it's stopped, and — when the app isn't open — arrives as a notification.
@MainActor @Observable
final class CookTimers {
    private(set) var timers: [CookTimer] = []
    @ObservationIgnored private var watchers: [UUID: Task<Void, Never>] = [:]
    @ObservationIgnored private let alarm = CookAlarm()
    @ObservationIgnored private let now: () -> Date

    init(now: @escaping () -> Date = Date.init) {
        self.now = now
    }

    // MARK: Sharing with the household's other devices

    /// Told when this device starts, changes, or removes a timer.
    @ObservationIgnored var onChange: ((CookSyncTimer, String) -> Void)?
    @ObservationIgnored var onRemove: ((String, String) -> Void)?
    /// The dish the changes are sent under (timers are the kitchen's, not the dish's).
    @ObservationIgnored var recipe = ""
    @ObservationIgnored private var createdAt: [UUID: Date] = [:]

    private func shared(_ id: UUID) {
        guard let timer = timers.first(where: { $0.id == id }) else { return }
        onChange?(Self.syncTimer(timer), recipe)
    }

    static func syncTimer(_ timer: CookTimer) -> CookSyncTimer {
        switch timer.state {
        case .running(let end):
            CookSyncTimer(
                id: timer.id.uuidString, label: timer.label, totalSeconds: Int(timer.total), endsAt: end,
                remainingSeconds: nil, finished: false)
        case .paused(let remaining):
            CookSyncTimer(
                id: timer.id.uuidString, label: timer.label, totalSeconds: Int(timer.total), endsAt: nil,
                remainingSeconds: Int(remaining.rounded()), finished: false)
        case .finished:
            CookSyncTimer(
                id: timer.id.uuidString, label: timer.label, totalSeconds: Int(timer.total), endsAt: nil,
                remainingSeconds: nil, finished: true)
        }
    }

    /// Takes the kitchen's timers from another device: adds the new ones, follows changes, and
    /// drops ones stopped elsewhere. A timer this device only just started is left alone until
    /// its own change has reached the server.
    func reconcile(_ remote: [CookSyncTimer], recipe _: String) {
        let byID = Dictionary(
            remote.compactMap { t in UUID(uuidString: t.id).map { ($0, t) } }, uniquingKeysWith: { a, _ in a })
        for local in timers where byID[local.id] == nil {
            if let made = createdAt[local.id], now().timeIntervalSince(made) < 10 { continue }
            unwatch(local.id)
            timers.removeAll { $0.id == local.id }
            stopChime(local.id)
        }
        for (id, t) in byID {
            let state: CookTimer.State
            if t.finished {
                state = .finished
            } else if let end = t.endsAt {
                state = .running(endsAt: end)
            } else {
                state = .paused(remaining: TimeInterval(t.remainingSeconds ?? 0))
            }
            if let i = index(id) {
                guard timers[i].state != state else { continue }
                if state == .finished {
                    finish(id, announce: false)
                } else {
                    timers[i].state = state
                    stopChime(id)
                    if case .running = state { watch(timers[i]) } else { unwatch(id) }
                }
            } else {
                let timer = CookTimer(id: id, label: t.label, total: TimeInterval(t.totalSeconds), state: state)
                timers.append(timer)
                if case .running = state { watch(timer) }
            }
        }
    }

    var runningCount: Int {
        timers.filter { if case .finished = $0.state { false } else { true } }.count
    }

    @discardableResult
    func start(label: String, seconds: Int) -> CookTimer {
        let timer = CookTimer(
            id: UUID(), label: label, total: TimeInterval(seconds),
            state: .running(endsAt: now().addingTimeInterval(TimeInterval(seconds))))
        timers.append(timer)
        createdAt[timer.id] = now()
        watch(timer)
        shared(timer.id)
        return timer
    }

    func pause(_ id: UUID) {
        guard let i = index(id), case .running = timers[i].state else { return }
        timers[i].state = .paused(remaining: timers[i].remaining(at: now()))
        unwatch(id)
        shared(id)
    }

    func resume(_ id: UUID) {
        guard let i = index(id), case .paused(let remaining) = timers[i].state else { return }
        timers[i].state = .running(endsAt: now().addingTimeInterval(remaining))
        watch(timers[i])
        shared(id)
    }

    /// Adds a minute, bringing a finished timer back.
    func addMinute(_ id: UUID) {
        guard let i = index(id) else { return }
        let timer = timers[i]
        let remaining = timer.remaining(at: now()) + 60
        timers[i] = CookTimer(
            id: timer.id, label: timer.label, total: max(timer.total, remaining),
            state: {
                if case .paused = timer.state { return .paused(remaining: remaining) }
                return .running(endsAt: now().addingTimeInterval(remaining))
            }())
        stopChime(id)
        if case .running = timers[i].state { watch(timers[i]) }
        shared(id)
    }

    /// Takes a minute off, for a timer set too long. Not offered with a minute or less left: the
    /// timer would just finish.
    func removeMinute(_ id: UUID) {
        guard let i = index(id) else { return }
        let timer = timers[i]
        let remaining = timer.remaining(at: now()) - 60
        guard remaining > 0 else { return }
        timers[i] = CookTimer(
            id: timer.id, label: timer.label, total: max(60, timer.total - 60),
            state: {
                if case .paused = timer.state { return .paused(remaining: remaining) }
                return .running(endsAt: now().addingTimeInterval(remaining))
            }())
        if case .running = timers[i].state { watch(timers[i]) }
        shared(id)
    }

    /// Moves a timer to where another one is, for reordering the dock by dragging.
    func move(_ id: UUID, to target: UUID) {
        guard id != target, let from = index(id), let to = index(target) else { return }
        let timer = timers.remove(at: from)
        timers.insert(timer, at: to)
    }

    /// Stops and removes it, finished or not.
    func remove(_ id: UUID) {
        unwatch(id)
        stopChime(id)
        let existed = timers.contains { $0.id == id }
        timers.removeAll { $0.id == id }
        if existed { onRemove?(id.uuidString, recipe) }
    }

    func removeAll() {
        for timer in timers { remove(timer.id) }
    }

    /// Marks it finished now (the watcher calls this when time's up).
    func finish(_ id: UUID) {
        finish(id, announce: true)
    }

    private func finish(_ id: UUID, announce: Bool) {
        guard let i = index(id) else { return }
        if case .finished = timers[i].state { return }
        timers[i].state = .finished
        watchers[id] = nil
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        alarm.ring()
        if announce { shared(id) }
    }

    private func index(_ id: UUID) -> Int? { timers.firstIndex { $0.id == id } }

    private func watch(_ timer: CookTimer) {
        unwatch(timer.id)
        guard case .running(let end) = timer.state else { return }
        let id = timer.id
        watchers[id] = Task { @MainActor [weak self] in
            let wait = end.timeIntervalSinceNow
            if wait > 0 {
                do { try await Task.sleep(for: .seconds(wait)) } catch { return }
            }
            self?.finish(id)
        }
        schedule(timer, at: end)
    }

    private func unwatch(_ id: UUID) {
        watchers[id]?.cancel()
        watchers[id] = nil
        UNUserNotificationCenter.current().removePendingNotificationRequests(withIdentifiers: [id.uuidString])
    }

    /// The alarm is shared: it stops once no finished timer is left to ring for.
    private func stopChime(_ id: UUID) {
        let stillFinished = timers.contains { $0.id != id && $0.state == .finished }
        if !stillFinished { alarm.stop() }
    }

    /// A local notification for when the app isn't open. It only goes out if the member
    /// already allowed notifications; the timer never asks.
    private func schedule(_ timer: CookTimer, at end: Date) {
        let wait = end.timeIntervalSinceNow
        guard wait > 1 else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Timer done")
        content.body = timer.label
        content.sound = .default
        content.interruptionLevel = .timeSensitive
        let request = UNNotificationRequest(
            identifier: timer.id.uuidString, content: content,
            trigger: UNTimeIntervalNotificationTrigger(timeInterval: wait, repeats: false))
        UNUserNotificationCenter.current().add(request) { _ in }
    }
}
