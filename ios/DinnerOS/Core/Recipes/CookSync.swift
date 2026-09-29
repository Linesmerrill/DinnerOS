import Foundation
import OSLog

/// One change to a shared cooking session.
nonisolated enum CookSyncOp: Equatable, Sendable {
    case check([String])
    case uncheck([String])
    case step(Int?)
    case timer(CookSyncTimer)
    case removeTimer(String)
}

/// A kitchen timer as every device shows it: running (`endsAt`), paused (`remainingSeconds`),
/// or finished.
nonisolated struct CookSyncTimer: Codable, Equatable, Sendable {
    let id: String
    let label: String
    let totalSeconds: Int
    var endsAt: Date?
    var remainingSeconds: Int?
    var finished: Bool
}

/// A dish's shared cooking state, with the day's timers.
nonisolated struct CookSyncState: Decodable, Equatable, Sendable {
    let checked: [String]
    let currentStep: Int?
    let timers: [CookSyncTimer]
    let updatedAt: Date?
}

extension CookSyncOp: Encodable {
    private enum Keys: String, CodingKey { case op, ids, step, timer, id }

    nonisolated func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        switch self {
        case .check(let ids):
            try c.encode("check", forKey: .op)
            try c.encode(ids, forKey: .ids)
        case .uncheck(let ids):
            try c.encode("uncheck", forKey: .op)
            try c.encode(ids, forKey: .ids)
        case .step(let step):
            try c.encode("step", forKey: .op)
            try c.encodeIfPresent(step, forKey: .step)
        case .timer(let timer):
            try c.encode("timer", forKey: .op)
            try c.encode(timer, forKey: .timer)
        case .removeTimer(let id):
            try c.encode("removeTimer", forKey: .op)
            try c.encode(id, forKey: .id)
        }
    }
}

private nonisolated struct CookSyncRequest: Encodable {
    let date: String
    let ops: [CookSyncOp]
}

extension RecipesAPI {
    func cookSession(householdID: String, recipeID: String, date: String, accessToken: String) async throws
        -> CookSyncState
    {
        let encoded = APIRequest.encodePathSegment(recipeID)
        var request = APIRequest.get("/api/v1/households/\(householdID)/cook-sessions/\(encoded)")
        request.queryItems = [URLQueryItem(name: "date", value: date)]
        return try await client.send(request.authorized(with: accessToken))
    }

    func applyCookOps(householdID: String, recipeID: String, date: String, ops: [CookSyncOp], accessToken: String)
        async throws -> CookSyncState
    {
        let encoded = APIRequest.encodePathSegment(recipeID)
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/cook-sessions/\(encoded)/ops",
            body: CookSyncRequest(date: date, ops: ops))
        return try await client.send(request.authorized(with: accessToken))
    }
}

/// Keeps a cooking screen in step with the household's other devices: sends each change as it
/// happens, and while the screen is open, checks every few seconds for everyone else's.
/// Offline, changes wait and go when the connection comes back.
@MainActor
final class CookSync {
    private let library: RecipeLibrary
    private let session: CookSession
    private let timers: CookTimers
    private var queue: [(recipe: String, op: CookSyncOp)] = []
    private var sending = false
    private var polling: Task<Void, Never>?
    private let date: String
    private static let logger = Logger(subsystem: "com.linesmerrill.dinneros", category: "cook-sync")

    init(library: RecipeLibrary, session: CookSession, timers: CookTimers, now: Date = .now) {
        self.library = library
        self.session = session
        self.timers = timers
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        date = formatter.string(from: now)
        session.onChange = { [weak self] recipe, ops in self?.enqueue(ops, recipe: recipe) }
        timers.onChange = { [weak self] timer, recipe in self?.enqueue([.timer(timer)], recipe: recipe) }
        timers.onRemove = { [weak self] id, recipe in self?.enqueue([.removeTimer(id)], recipe: recipe) }
    }

    /// Follows `recipe` until the next call or `stop()`.
    func follow(recipe: String) {
        timers.recipe = recipe
        polling?.cancel()
        polling = Task { [weak self] in
            while !Task.isCancelled {
                await self?.pull(recipe: recipe)
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
            }
        }
    }

    func stop() {
        polling?.cancel()
        polling = nil
    }

    private func enqueue(_ ops: [CookSyncOp], recipe: String) {
        queue += ops.map { (recipe, $0) }
        Task { await flush() }
    }

    private func flush() async {
        guard !sending, let first = queue.first else { return }
        sending = true
        defer { sending = false }
        let recipe = first.recipe
        let batch = queue.prefix { $0.recipe == recipe }.map(\.op)
        do {
            let state = try await library.applyCookOps(recipeID: recipe, date: date, ops: Array(batch))
            queue.removeFirst(batch.count)
            if queue.isEmpty { apply(state, recipe: recipe) }
        } catch {
            // Offline or the server is asleep: keep the changes and try again shortly.
            Self.logger.notice("Cook sync send failed; retrying")
            try? await Task.sleep(for: .seconds(3))
        }
        if !queue.isEmpty { await flush() }
    }

    private func pull(recipe: String) async {
        // A change of ours still on its way wins over what the server says right now.
        guard queue.isEmpty, !sending else { return }
        guard let state = try? await library.cookSession(recipeID: recipe, date: date), queue.isEmpty else { return }
        apply(state, recipe: recipe)
    }

    private func apply(_ state: CookSyncState, recipe: String) {
        session.apply(checked: Set(state.checked), step: state.currentStep, recipe: recipe)
        timers.reconcile(state.timers, recipe: recipe)
    }
}
