import Foundation
import os

/// ActivityKit, behind a seam so the decisions around it can be tested without a Lock Screen.
/// `ActivityKitImportActivityCenter` is the real one.
protocol ImportActivityCenter: AnyObject {
    /// False when the member turned Live Activities off for DinnerOS (or for the phone).
    var activitiesEnabled: Bool { get }
    /// The attributes of every activity still on screen, active or stale.
    func runningActivities() -> [MealKitImportActivityAttributes]
    /// Starts an activity whose updates can arrive by push (`pushType: .token`).
    func start(_ attributes: MealKitImportActivityAttributes, state: MealKitImportActivityAttributes.ContentState)
        throws
    func update(jobID: String, state: MealKitImportActivityAttributes.ContentState) async
    /// Ends the job's activity, leaving it on the Lock Screen for `linger` (zero: at once).
    func end(jobID: String, state: MealKitImportActivityAttributes.ContentState, linger: Duration) async
    /// Called with every push token an activity is given, including each rotation.
    var onPushToken: ((MealKitImportActivityAttributes, String) -> Void)? { get set }
    /// Called when the member swipes an activity away.
    var onDismissed: ((MealKitImportActivityAttributes) -> Void)? { get set }
}

/// Keeps the meal-kit import's Live Activity in step with the job
/// (`docs/meal-kit-import.md#live-activity`).
///
/// The app starts the activity when the member queues an import, updates it from the status poll
/// it already runs while open, and ends it when the poll sees the run finish. While the app is
/// closed the server does the same through Live Activity pushes, using the push token handed to it
/// here. A member with Live Activities off gets none of this and an import that works exactly as
/// before.
final class MealKitLiveActivities {
    /// What `importQueued` decided, so the choice is testable.
    enum StartDecision: Equatable {
        case started
        /// This run already has an activity (a second tap, or another trip to the screen).
        case alreadyRunning
        /// The member has Live Activities off. The import goes ahead without one.
        case disabled
        /// The run is not going to do anything, so there is nothing to follow.
        case notWorking
        /// ActivityKit refused (too many activities, or the app is not in the foreground).
        case failed
    }

    /// Sends a token to the API: household, service, job, token.
    typealias TokenSink = (_ householdID: String, _ service: String, _ jobID: String, _ token: String) async -> Void
    /// Tells the API a member dismissed the activity: household, service, job.
    typealias DismissSink = (_ householdID: String, _ service: String, _ jobID: String) async -> Void

    private let center: any ImportActivityCenter
    private static let logger = Logger(subsystem: "DinnerOS", category: "live-activity")

    /// - Parameters:
    ///   - sendToken: registers a (rotated) push token for the job. Never logs it.
    ///   - dropToken: forgets the token when the member dismisses the activity.
    init(center: any ImportActivityCenter, sendToken: TokenSink?, dropToken: DismissSink?) {
        self.center = center
        center.onPushToken = { attributes, token in
            guard let sendToken else { return }
            Task { await sendToken(attributes.householdID, attributes.service, attributes.jobID, token) }
        }
        center.onDismissed = { attributes in
            guard let dropToken else { return }
            Task { await dropToken(attributes.householdID, attributes.service, attributes.jobID) }
        }
    }

    /// The member just queued `job`: start following it, if they allow Live Activities.
    @discardableResult
    func importQueued(_ job: MealKitImportJob, householdID: String, service: MealKitService) async -> StartDecision {
        guard job.isWorking else { return .notWorking }
        let state = Self.contentState(for: job)
        let running = center.runningActivities()
        if running.contains(where: { $0.jobID == job.id }) {
            await center.update(jobID: job.id, state: state)
            return .alreadyRunning
        }
        guard center.activitiesEnabled else { return .disabled }
        // One household imports one run at a time; an activity left over from an older run of
        // this household is out of date, not a second import.
        for old in running where old.householdID == householdID {
            await center.end(jobID: old.jobID, state: state.withPhase(.canceled), linger: .zero)
        }
        do {
            try center.start(
                MealKitImportActivityAttributes(
                    serviceName: service.displayName, service: service.rawValue, householdID: householdID,
                    jobID: job.id),
                state: state)
            return .started
        } catch {
            Self.logger.notice("Live Activity not started: \(String(describing: type(of: error)), privacy: .public)")
            return .failed
        }
    }

    /// The status poll read `job`. Keeps its activity current and ends it when the run finishes.
    /// A run without an activity is left alone: another member's import, or one started with Live
    /// Activities off, never grows one here.
    func statusLoaded(_ job: MealKitImportJob?) async {
        guard let job, center.runningActivities().contains(where: { $0.jobID == job.id }) else { return }
        let state = Self.contentState(for: job)
        if state.isFinal {
            await center.end(jobID: job.id, state: state, linger: Self.linger(for: state.phase))
        } else {
            await center.update(jobID: job.id, state: state)
        }
    }

    /// What the activity shows for `job`. The server maps its jobs the same way
    /// (`liveactivity.StateFor`), so a push and a poll never disagree.
    static func contentState(for job: MealKitImportJob) -> MealKitImportActivityAttributes.ContentState {
        let failed = job.failures.count
        var done = job.recipesDone
        let phase: MealKitImportActivityAttributes.ContentState.Phase
        switch job.state {
        case .queued: phase = .waiting
        case .running: phase = .importing
        case .finished:
            phase = .done
            done = job.imported + job.updated + job.unchanged
        case .failed: phase = .failed
        case .canceled: phase = .canceled
        }
        if job.recipesFound > 0 { done = min(done, job.recipesFound) }
        return .init(phase: phase, done: done, total: job.recipesFound, failed: failed)
    }

    /// How long an ending stays on the Lock Screen: a finished import the whole four hours Apple
    /// allows (it often finishes while nobody is looking), a failed one an hour, a stopped one not
    /// at all — the member stopped it on purpose. The server uses the same values.
    static func linger(for phase: MealKitImportActivityAttributes.ContentState.Phase) -> Duration {
        switch phase {
        case .done: .seconds(4 * 60 * 60)
        case .failed: .seconds(60 * 60)
        case .canceled, .importing, .waiting: .zero
        }
    }
}

extension MealKitImportActivityAttributes.ContentState {
    fileprivate func withPhase(_ phase: Phase) -> Self {
        var copy = self
        copy.phase = phase
        return copy
    }
}

extension MealKitLiveActivities {
    /// The app's real one: ActivityKit, with tokens sent to the API as the signed-in member.
    static func live(session: AuthSession, api: MealKitAPI?) -> MealKitLiveActivities {
        let center = ActivityKitImportActivityCenter()
        let activities = MealKitLiveActivities(
            center: center,
            sendToken: { householdID, service, jobID, token in
                guard let api else { return }
                do {
                    try await session.authorized { access in
                        try await api.registerLiveActivity(
                            householdID: householdID, service: service, jobID: jobID, token: token,
                            environment: .current, accessToken: access)
                    }
                } catch {
                    // A 404 means the run already ended; the poll ends the activity. Never the token.
                    logger.notice("Live Activity token not registered: \(Self.describe(error), privacy: .public)")
                }
            },
            dropToken: { householdID, service, jobID in
                guard let api else { return }
                try? await session.authorized { access in
                    try await api.unregisterLiveActivity(
                        householdID: householdID, service: service, jobID: jobID, accessToken: access)
                }
            })
        center.resume()
        return activities
    }

    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, _): "\(status) \(code)"
        case .transport(let code): "transport \(code.rawValue)"
        default: String(describing: type(of: error))
        }
    }
}
