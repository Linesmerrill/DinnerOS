import Foundation
import Observation
import os

/// A tab or row that can carry a next-step dot. Raw values are the API's row keys
/// (`api/internal/nextsteps`). A new rule needs a new case only when it badges a row the app
/// hasn't badged before.
nonisolated enum NextStepRow: String, CaseIterable, Sendable {
    case household
    case planning
    case autopilotPreferences
    case importReview
    case giveFreeMonth
}

/// Something the household should do next, and the rows from the tab down to it.
nonisolated struct NextStep: Codable, Equatable, Sendable {
    let id: String
    let path: [String]

    static let autopilotSetup = "autopilotSetup"
    static let importReview = "importReview"
    static let freeMonthSteps = "freeMonthSteps"

    /// The rows to badge, or `nil` when the path names a row this version doesn't have: a step
    /// from a newer API never leaves a dot that leads nowhere.
    var rows: [NextStepRow]? {
        let rows = path.compactMap(NextStepRow.init(rawValue:))
        return rows.isEmpty || rows.count != path.count ? nil : rows
    }
}

/// `GET /api/v1/households/{householdId}/next-steps`. Needs only `household.view`; the API
/// leaves out steps the member's role can't act on. Like every `GET`, the last answer is
/// replayed offline.
nonisolated struct NextStepsAPI: Sendable {
    let client: APIClient

    func list(householdID: String, accessToken: String) async throws -> [NextStep] {
        let response: Response = try await client.send(
            APIRequest.get("/api/v1/households/\(APIRequest.encodePathSegment(householdID))/next-steps")
                .withPercentEncodedPath()
                .authorized(with: accessToken))
        return response.items
    }

    private struct Response: Decodable {
        let items: [NextStep]
    }
}

/// The current household's next steps, as red dots: on the Household tab, the row that leads
/// there, and the item itself (decisions 736-739). No counts and no pushes.
///
/// The API decides what's pending. The app reloads it when a household opens, when the app
/// comes back, when the Household tab shows, and after a save that may finish a step. A step
/// finished here (`finish`) loses its dots at once, and stays hidden even if a saved answer
/// replayed offline still lists it, until the API agrees.
@Observable
final class NextStepsStore {
    private(set) var householdID: String?
    /// As the API sent them.
    private(set) var steps: [NextStep] = []
    /// Steps finished in the app that the API hasn't confirmed yet.
    private(set) var finished: Set<String> = []

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: NextStepsAPI?
    @ObservationIgnored private var userID: String?
    /// Incremented when the household or user changes, so a slow answer for an old one can't
    /// bring its dots back.
    @ObservationIgnored private var scope = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "next-steps")

    init(session: AuthSession, api: NextStepsAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with the given steps, for previews and tests. It has no network access.
    static func preview(session: AuthSession, steps: [NextStep], householdID: String = "household-1")
        -> NextStepsStore
    {
        let store = NextStepsStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.steps = steps
        return store
    }

    /// The steps still to do, less the ones finished here.
    var pending: [NextStep] { steps.filter { !finished.contains($0.id) } }

    /// Every row a pending step's path passes through.
    var badgedRows: Set<NextStepRow> {
        Set(pending.compactMap(\.rows).joined())
    }

    func shows(_ row: NextStepRow) -> Bool { badgedRows.contains(row) }

    /// Starts showing `householdID`'s steps. Another household or user drops the old dots first.
    func activate(householdID: String) {
        let currentUserID = session.currentUser?.id
        guard householdID != self.householdID || currentUserID != userID else { return }
        scope += 1
        self.householdID = householdID
        userID = currentUserID
        steps = []
        finished = []
    }

    /// Reads what's pending. A failure keeps the dots that were showing.
    func load() async {
        guard let api, let householdID else { return }
        let started = scope
        do {
            let loaded = try await session.authorized { token in
                try await api.list(householdID: householdID, accessToken: token)
            }
            guard started == scope else { return }
            apply(loaded)
        } catch let error as APIError where error.status == 404 {
            // An API without next steps: no dots.
            guard started == scope else { return }
            apply([])
        } catch is CancellationError {
        } catch {
            Self.logger.notice("Next steps load failed: \(String(describing: type(of: error)), privacy: .public)")
        }
    }

    /// Takes a step's dots away now, after the app saved what finishes it.
    func finish(_ stepID: String) {
        guard steps.contains(where: { $0.id == stepID }) else { return }
        finished.insert(stepID)
    }

    /// Undoes `finish` when the app sees the step is pending again, such as new Import Review
    /// items after the last one was resolved.
    func reopen(_ stepID: String) {
        finished.remove(stepID)
    }

    /// Shows `loaded`. A finished step the API no longer lists is confirmed and forgotten; one it
    /// still lists stays hidden, because the answer may be a saved one from before the save.
    func apply(_ loaded: [NextStep]) {
        let listed = Set(loaded.map(\.id))
        finished.formIntersection(listed)
        if steps != loaded { steps = loaded }
    }

    /// Forgets everything, for sign-out.
    func reset() {
        scope += 1
        householdID = nil
        userID = nil
        steps = []
        finished = []
    }
}
