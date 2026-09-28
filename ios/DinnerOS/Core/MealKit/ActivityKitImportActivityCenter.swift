import ActivityKit
import Foundation

/// `ImportActivityCenter` over ActivityKit.
///
/// Every activity it starts asks for a push token (`pushType: .token`) so the server can keep it
/// current while the app is closed. Tokens rotate; each one is handed on through `onPushToken`.
/// Activities left from an earlier launch are picked up at init, so a token that rotates after a
/// relaunch still reaches the server.
final class ActivityKitImportActivityCenter: ImportActivityCenter {
    typealias Attributes = MealKitImportActivityAttributes

    var onPushToken: ((Attributes, String) -> Void)?
    var onDismissed: ((Attributes) -> Void)?

    /// Activities whose token and state streams are already being watched.
    private var observed: Set<String> = []

    /// An update is current for this long; past it the activity says it is waiting for word
    /// rather than showing an old count as live. The server uses the same interval.
    private static let staleAfter: TimeInterval = 30 * 60

    init() {}

    /// Starts watching activities from an earlier launch. Call once the callbacks are set.
    func resume() {
        for activity in Activity<Attributes>.activities {
            observe(activity)
        }
    }

    var activitiesEnabled: Bool { ActivityAuthorizationInfo().areActivitiesEnabled }

    func runningActivities() -> [Attributes] {
        Activity<Attributes>.activities
            .filter { $0.activityState == .active || $0.activityState == .stale }
            .map(\.attributes)
    }

    func start(_ attributes: Attributes, state: Attributes.ContentState) throws {
        let activity = try Activity.request(
            attributes: attributes,
            content: ActivityContent(state: state, staleDate: Date.now.addingTimeInterval(Self.staleAfter)),
            pushType: .token)
        observe(activity)
    }

    func update(jobID: String, state: Attributes.ContentState) async {
        await Self.update(
            jobID: jobID,
            content: ActivityContent(state: state, staleDate: Date.now.addingTimeInterval(Self.staleAfter)))
    }

    func end(jobID: String, state: Attributes.ContentState, linger: Duration) async {
        let dismissAt = Date.now.addingTimeInterval(TimeInterval(linger.components.seconds))
        await Self.end(
            jobID: jobID, content: ActivityContent(state: state, staleDate: nil),
            policy: linger == .zero ? .immediate : .after(dismissAt))
    }

    // `Activity` is not Sendable, so the activities are looked up and changed in one nonisolated
    // function rather than handed across from the main actor.
    private nonisolated static func update(jobID: String, content: ActivityContent<Attributes.ContentState>) async {
        for activity in Activity<Attributes>.activities
        where activity.attributes.jobID == jobID && activity.content.state != content.state {
            await activity.update(content)
        }
    }

    private nonisolated static func end(
        jobID: String, content: ActivityContent<Attributes.ContentState>, policy: ActivityUIDismissalPolicy
    ) async {
        for activity in Activity<Attributes>.activities where activity.attributes.jobID == jobID {
            await activity.end(content, dismissalPolicy: policy)
        }
    }

    private func observe(_ activity: Activity<Attributes>) {
        guard observed.insert(activity.id).inserted else { return }
        let attributes = activity.attributes
        Task { [weak self] in
            for await data in activity.pushTokenUpdates {
                self?.onPushToken?(attributes, data.pushTokenHex)
            }
        }
        Task { [weak self] in
            for await state in activity.activityStateUpdates {
                if state == .dismissed {
                    self?.onDismissed?(attributes)
                }
                if state == .dismissed || state == .ended {
                    self?.observed.remove(activity.id)
                    return
                }
            }
        }
    }
}
