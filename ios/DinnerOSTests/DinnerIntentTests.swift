import Foundation
import Testing

@testable import DinnerOS

private func utc(_ string: String) -> Date {
    ISO8601DateFormatter().date(from: string) ?? .distantPast
}

private let denver = TimeZone(identifier: "America/Denver") ?? .gmt

/// The Autopilot store sends the device's signals with generate.
struct AutopilotGenerateSignalsTests {
    @Test func generateSendsTheDeviceSignalsForTheWeek() async throws {
        let server = FakeAutopilotServer()
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = AutopilotStore(
            session: session, api: AutopilotAPI(client: client), prompts: InMemoryAutopilotPrompts())
        let week = try #require(ISOWeek("2026-W38"))
        await store.activate(householdID: "household-1")

        let line = "POST /households/household-1/autopilot/weeks/2026-W38/generate"
        try await store.generate(week: week)
        #expect(server.body(of: line)?["signals"] == nil)

        var asked: [ISOWeek] = []
        store.deviceSignals = { requested in
            asked.append(requested)
            return AutopilotDeviceSignals(days: [
                AutopilotDeviceDaySignals(day: .wed, busyness: .busy, eveningFreeMinutes: 20, temperatureBand: .hot)
            ])
        }
        try await store.generate(week: week)

        #expect(asked == [week])
        let days = try #require((server.body(of: line)?["signals"] as? [String: Any])?["days"] as? [[String: Any]])
        #expect(days.count == 1)
        #expect(
            days[0] as NSDictionary == [
                "day": "wed", "busyness": "busy", "eveningFreeMinutes": 20, "temperatureBand": "hot",
            ])
    }
}

// MARK: - Siri

@MainActor
final class FakeDinnerIntentServices: DinnerIntentServices {
    var accountState: IntentAccount = .ready(IntentHousehold(id: "household-1", timeZone: denver, canPlan: true))
    var configured = true
    var planJSON = PlanFixtures.plan()
    var proposalJSON = AutopilotFixtures.proposal()
    var generateError: (any Error)?
    private(set) var generatedWeeks: [ISOWeek] = []
    private(set) var reviewed: [ISOWeek] = []

    func account() async -> IntentAccount { accountState }

    func isAutopilotConfigured(householdID: String) async throws -> Bool { configured }

    func plan(week: ISOWeek, householdID: String) async throws -> Plan {
        try JSONCoding.makeDecoder().decode(Plan.self, from: Data(planJSON.utf8))
    }

    func generate(week: ISOWeek, householdID: String) async throws -> AutopilotProposal {
        generatedWeeks.append(week)
        if let generateError { throw generateError }
        return try JSONCoding.makeDecoder().decode(AutopilotProposal.self, from: Data(proposalJSON.utf8))
    }

    func openReview(week: ISOWeek) {
        reviewed.append(week)
    }
}

@MainActor
final class FakePhraser: WeekSummaryPhraser {
    var answer: String?
    func rephrase(_ summary: String, meals: [String]) async -> String? { answer }
}

@MainActor
struct DinnerIntentTests {
    private let en = Locale(identifier: "en_US")
    /// Wednesday September 16, 2026, noon in Denver.
    private let wednesday = utc("2026-09-16T18:00:00Z")

    @Test func upcomingWeekIsThisWeekUntilFriday() {
        let thisWeek = ISOWeek("2026-W38")
        let nextWeek = ISOWeek("2026-W39")
        #expect(DinnerIntentActions.upcomingWeek(now: utc("2026-09-14T15:00:00Z"), timeZone: denver) == thisWeek)
        #expect(DinnerIntentActions.upcomingWeek(now: utc("2026-09-18T05:59:00Z"), timeZone: denver) == thisWeek)
        // Friday 12:01 am in Denver.
        #expect(DinnerIntentActions.upcomingWeek(now: utc("2026-09-18T06:01:00Z"), timeZone: denver) == nextWeek)
        #expect(DinnerIntentActions.upcomingWeek(now: utc("2026-09-20T20:00:00Z"), timeZone: denver) == nextWeek)
    }

    @Test func planDinnersGeneratesOpensTheReviewAndSummarizes() async throws {
        let services = FakeDinnerIntentServices()
        let actions = DinnerIntentActions(services: services, now: { wednesday })

        let outcome = await actions.planDinners()

        let week = try #require(ISOWeek("2026-W38"))
        #expect(services.generatedWeeks == [week])
        #expect(services.reviewed == [week])
        guard case .planned(let planned, let meals) = outcome else {
            Issue.record("outcome = \(outcome)")
            return
        }
        #expect(planned == week)
        #expect(meals.first?.day == .mon)
        #expect(meals.map(\.day) == meals.sorted { $0.day.offset < $1.day.offset }.map(\.day))
        let text = DinnerIntentText.planDinners(outcome, locale: en)
        #expect(text.hasPrefix("Autopilot suggested \(meals.count) dinners for \(week.rangeLabel(locale: en))"))
        #expect(text.contains("starting with \(meals[0].name) on Monday"))
        #expect(await actions.dialog(for: outcome) == text)
    }

    @Test func aPhrasedSummaryReplacesTheTextOnlyWhenOffered() async {
        let services = FakeDinnerIntentServices()
        let phraser = FakePhraser()
        let actions = DinnerIntentActions(services: services, phraser: phraser, now: { wednesday })
        let outcome = await actions.planDinners()
        #expect(await actions.dialog(for: outcome) == DinnerIntentText.planDinners(outcome))
        phraser.answer = "Your week is planned!"
        #expect(await actions.dialog(for: outcome) == "Your week is planned!")
        // Other outcomes are never rephrased.
        #expect(await actions.dialog(for: .notSetUp) == DinnerIntentText.planDinners(.notSetUp))
    }

    @Test func phrasedSummariesMustStayOneShortSentenceNamingTheFirstMeal() {
        let summary = "Autopilot suggested 3 dinners, starting with Tacos on Monday."
        #expect(
            OnDeviceWeekSummaryPhraser.accept("  Tacos kick off your week!  ", summary: summary, meals: ["Tacos"])
                == "Tacos kick off your week!")
        #expect(OnDeviceWeekSummaryPhraser.accept("Pizza night all week!", summary: summary, meals: ["Tacos"]) == nil)
        #expect(OnDeviceWeekSummaryPhraser.accept("Tacos.\nAnd more.", summary: summary, meals: ["Tacos"]) == nil)
        #expect(OnDeviceWeekSummaryPhraser.accept("", summary: summary, meals: ["Tacos"]) == nil)
    }

    @Test func planDinnersExplainsWhyItCouldntPlan() async throws {
        let week = try #require(ISOWeek("2026-W38"))
        func run(_ configure: (FakeDinnerIntentServices) -> Void) async -> (
            PlanDinnersOutcome, FakeDinnerIntentServices
        ) {
            let services = FakeDinnerIntentServices()
            configure(services)
            let outcome = await DinnerIntentActions(services: services, now: { wednesday }).planDinners()
            return (outcome, services)
        }

        var (outcome, services) = await run { $0.accountState = .signedOut }
        #expect(outcome == .signedOut)
        #expect(services.generatedWeeks.isEmpty)
        #expect(DinnerIntentText.planDinners(outcome).contains("signed out"))

        (outcome, _) = await run { $0.accountState = .noHousehold }
        #expect(outcome == .noHousehold)
        #expect(DinnerIntentText.planDinners(outcome).contains("not in a household"))

        (outcome, services) = await run {
            $0.accountState = .ready(IntentHousehold(id: "household-1", timeZone: denver, canPlan: false))
        }
        #expect(outcome == .notAllowed)
        #expect(services.generatedWeeks.isEmpty)

        (outcome, services) = await run { $0.configured = false }
        #expect(outcome == .notSetUp)
        #expect(services.generatedWeeks.isEmpty)

        (outcome, services) = await run { $0.planJSON = PlanFixtures.plan(status: "finalized") }
        #expect(outcome == .finalized(week))
        #expect(services.generatedWeeks.isEmpty)
        #expect(
            DinnerIntentText.planDinners(outcome, locale: en).contains("\(week.rangeLabel(locale: en)) is finalized"))

        (outcome, services) = await run {
            $0.generateError = APIError.server(status: 409, code: "plan_finalized", message: "", requestID: nil)
        }
        #expect(outcome == .finalized(week))
        #expect(services.reviewed.isEmpty)

        (outcome, services) = await run {
            $0.proposalJSON = AutopilotFixtures.proposal(slots: [], messages: [])
        }
        #expect(outcome == .nothingSuggested(week))
        #expect(services.reviewed.isEmpty)

        (outcome, _) = await run {
            $0.generateError = APIError.transport(.notConnectedToInternet)
        }
        guard case .unavailable = outcome else {
            Issue.record("outcome = \(outcome)")
            return
        }
    }

    @Test func tonightReadsTodaysEntries() async throws {
        let services = FakeDinnerIntentServices()
        services.planJSON = PlanFixtures.plan(entries: [
            AutopilotFixtures.entry(id: "e1", recipeID: "r1", name: "Chicken Tacos", day: "wed", origin: "manual"),
            AutopilotFixtures.entry(id: "e2", recipeID: "r2", name: "Pork Shoulder", day: "sun", origin: "autopilot"),
        ])
        let actions = DinnerIntentActions(services: services, now: { wednesday })

        let outcome = await actions.tonight()

        #expect(outcome == .planned(mains: ["Chicken Tacos"], addOns: []))
        #expect(DinnerIntentText.tonight(outcome, locale: en) == "Tonight's dinner is Chicken Tacos.")
        #expect(
            DinnerIntentText.tonight(.planned(mains: ["Pasta", "Salad"], addOns: ["Garlic Bread"]), locale: en)
                == "Tonight's dinner is Pasta and Salad, with Garlic Bread.")
    }

    @Test func tonightHandlesNothingPlannedAndSignedOut() async {
        let services = FakeDinnerIntentServices()
        let actions = DinnerIntentActions(services: services, now: { wednesday })
        #expect(await actions.tonight() == .nothingPlanned)
        #expect(DinnerIntentText.tonight(.nothingPlanned) == "Nothing's planned for dinner tonight.")

        services.accountState = .signedOut
        #expect(await actions.tonight() == .signedOut)
        services.accountState = .noHousehold
        #expect(await actions.tonight() == .noHousehold)
    }
}
