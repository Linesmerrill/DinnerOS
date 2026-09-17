import AppIntents
import Foundation

#if canImport(FoundationModels)
    import FoundationModels
#endif

// Siri and Shortcuts. The intents stay thin: `DinnerIntentActions` decides what happens and
// what to say, through `LiveDinnerIntentServices`, which calls the app's existing stores.

/// "Plan my dinners with DinnerOS": runs Autopilot for the upcoming week and opens the review.
struct PlanDinnersIntent: AppIntent {
    static let title: LocalizedStringResource = "Plan My Dinners"
    static let description = IntentDescription(
        "Runs Autopilot for the upcoming week and opens its suggestions for you to review.")
    /// The suggestions open for review, so the app comes forward.
    static let openAppWhenRun = true

    @MainActor
    func perform() async throws -> some IntentResult & ProvidesDialog {
        let dependencies = AppDependencies.shared
        let actions = DinnerIntentActions(
            services: LiveDinnerIntentServices(dependencies: dependencies), phraser: OnDeviceWeekSummaryPhraser())
        let outcome = await actions.planDinners()
        return .result(dialog: IntentDialog(stringLiteral: await actions.dialog(for: outcome)))
    }
}

/// "What's for dinner tonight in DinnerOS": reads today's plan entries.
struct TonightDinnerIntent: AppIntent {
    static let title: LocalizedStringResource = "What's for Dinner Tonight"
    static let description = IntentDescription("Tells you what's planned for dinner today.")

    @MainActor
    func perform() async throws -> some IntentResult & ProvidesDialog {
        let actions = DinnerIntentActions(services: LiveDinnerIntentServices(dependencies: .shared))
        let outcome = await actions.tonight()
        return .result(dialog: IntentDialog(stringLiteral: DinnerIntentText.tonight(outcome)))
    }
}

struct DinnerShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: PlanDinnersIntent(),
            phrases: [
                "Plan my dinners with \(.applicationName)",
                "Plan dinners with \(.applicationName)",
            ],
            shortTitle: "Plan My Dinners", systemImageName: "sparkles")
        AppShortcut(
            intent: TonightDinnerIntent(),
            phrases: [
                "What's for dinner tonight in \(.applicationName)",
                "What's for dinner in \(.applicationName)",
            ],
            shortTitle: "Dinner Tonight", systemImageName: "fork.knife")
    }
}

/// The intents' view of the app: the shared session, households, and Autopilot stores.
@MainActor
final class LiveDinnerIntentServices: DinnerIntentServices {
    private let dependencies: AppDependencies

    init(dependencies: AppDependencies) {
        self.dependencies = dependencies
    }

    func account() async -> IntentAccount {
        let session = dependencies.session
        await session.restore()
        // A launch may already be restoring; `restore` returns at once for the second caller.
        if !(await waitUntil { session.state != .restoring }) {
            return .unavailable(String(localized: "Try again in a moment."))
        }
        switch session.state {
        case .signedOut, .restoring: return .signedOut
        case .configurationError(let message): return .unavailable(message)
        case .signedIn: break
        }
        let households = dependencies.households
        if households.phase == .idle {
            await households.load()
        }
        _ = await waitUntil { households.phase != .loading && households.phase != .idle }
        switch households.phase {
        case .needsHousehold: return .noHousehold
        case .failed(let message): return .unavailable(message)
        default: break
        }
        guard let current = households.current?.household else { return .noHousehold }
        return .ready(
            IntentHousehold(
                id: current.id, timeZone: current.planningTimeZone,
                canPlan: households.access?.can(.planEdit) == true, weekStartsOn: current.weekStartsOn))
    }

    func isAutopilotConfigured(householdID: String) async throws -> Bool {
        let autopilot = dependencies.autopilot
        await autopilot.activate(householdID: householdID)
        if case .failed(let message) = autopilot.phase {
            throw IntentError(message: message)
        }
        return autopilot.isConfigured
    }

    func plan(week: ISOWeek, householdID: String) async throws -> Plan {
        // Read directly so asking about tonight doesn't move the week the app is showing.
        guard
            let client = dependencies.configuration.apiBaseURL.map({
                APIClient(baseURL: $0, transport: URLSessionTransport())
            })
        else { throw AuthSessionError.notConfigured }
        let api = PlansAPI(client: client)
        return try await dependencies.session.authorized { token in
            try await api.plan(householdID: householdID, week: week, accessToken: token)
        }
    }

    func generate(week: ISOWeek, householdID: String) async throws -> AutopilotProposal {
        let autopilot = dependencies.autopilot
        await autopilot.activate(householdID: householdID)
        // Same path as the app: the store asks `AutopilotDeviceContext` for whatever signals the
        // member already allowed. Siri never shows the permission prompts.
        try await autopilot.generate(week: week)
        guard let proposal = autopilot.proposal, proposal.week == week.description else {
            throw IntentError(message: String(localized: "Try again in a moment."))
        }
        return proposal
    }

    func openReview(week: ISOWeek) {
        dependencies.intentRouter.autopilotReviewWeek = week
    }

    /// Polls for up to ten seconds; `false` when `condition` never held.
    private func waitUntil(_ condition: () -> Bool) async -> Bool {
        for _ in 0..<100 {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(100))
        }
        return condition()
    }

    private struct IntentError: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }
}

/// Uses Apple Intelligence on the device, when it's available, only to phrase the week's
/// summary more warmly. The deterministic text is kept whenever the model is unavailable,
/// slow, or answers with anything but one short sentence that still names the first meal.
@MainActor
final class OnDeviceWeekSummaryPhraser: WeekSummaryPhraser {
    func rephrase(_ summary: String, meals: [String]) async -> String? {
        #if canImport(FoundationModels)
            guard #available(iOS 26.0, *) else { return nil }
            guard case .available = SystemLanguageModel.default.availability else { return nil }
            let session = LanguageModelSession(
                instructions: """
                    Rewrite the dinner-planning summary you're given as one friendly sentence of at most \
                    30 words. Keep every fact and date. Don't add meals, advice, or questions. Reply with \
                    the sentence only.
                    """)
            // Siri is waiting: give the model three seconds, then keep the plain text.
            let response = Task { try? await session.respond(to: summary).content }
            let timeout = Task {
                try? await Task.sleep(for: .seconds(3))
                response.cancel()
            }
            let answer = await response.value
            timeout.cancel()
            return answer.flatMap { Self.accept($0, summary: summary, meals: meals) }
        #else
            return nil
        #endif
    }

    /// One sentence, short, and still naming the first meal the summary named.
    nonisolated static func accept(_ answer: String, summary: String, meals: [String]) -> String? {
        let trimmed = answer.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed.count <= 220, !trimmed.contains("\n") else { return nil }
        if let first = meals.first, summary.contains(first), !trimmed.localizedCaseInsensitiveContains(first) {
            return nil
        }
        return trimmed
    }
}
