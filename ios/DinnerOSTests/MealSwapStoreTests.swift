import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Synthetic JSON shaped like the "try something similar" endpoints. No real recipes.
nonisolated enum MealSwapFixtures {
    static func alternative(
        id: String, name: String, similarity: Double = 0.7, reasons: [String] = ["Also Thai", "30 min"]
    ) -> String {
        let reasonList = reasons.map { #"{"code":"similarCuisine","text":"\#($0)"}"# }.joined(separator: ",")
        return #"""
            {"recipe":{"id":"\#(id)","name":"\#(name)","imageUrl":"https://img.example.test/\#(id).jpg"},
             "servings":2,"cookMinutes":30,"timeBand":"medium","similarity":\#(similarity),"score":1.2,
             "reasons":[\#(reasonList)]}
            """#
    }

    static func alternatives(_ items: [String], message: String? = nil) -> Data {
        let messages = message.map { #"{"code":"no_similar","text":"\#($0)"}"# } ?? ""
        return Data(
            #"""
            {"entryId":"entry-1","week":"2026-W38","day":"tue",
             "recipe":{"id":"recipe-tacos","name":"Beef Tacos"},"servings":2,
             "alternatives":[\#(items.joined(separator: ","))],"messages":[\#(messages)],
             "modelVersion":"baseline-2026.7"}
            """#.utf8)
    }

    /// A swap result whose plan has the new meal on the same day.
    static func swapResult(recipeID: String, name: String) -> Data {
        Data(
            #"""
            {"plan":{"householdId":"household-1","week":"2026-W38","startDate":"2026-09-14",
                     "endDate":"2026-09-20","status":"draft","entries":[
                {"id":"entry-1","recipe":{"id":"\#(recipeID)","name":"\#(name)","isAddon":false},
                 "day":"tue","servings":2,"note":"","addedBy":"user-1","addedAt":"2026-09-14T00:00:00Z",
                 "origin":"autopilot","customizations":[]}],
                     "createdAt":"2026-09-14T00:00:00Z","updatedAt":"2026-09-14T00:00:00Z"},
             "entry":{"id":"entry-1","recipe":{"id":"\#(recipeID)","name":"\#(name)","isAddon":false},
                      "day":"tue","servings":2,"note":"","addedBy":"user-1",
                      "addedAt":"2026-09-14T00:00:00Z","origin":"autopilot","customizations":[]},
             "previousRecipe":{"id":"recipe-tacos","name":"Beef Tacos"}}
            """#.utf8)
    }
}

/// A stand-in for the alternatives and swap endpoints, recording what it was asked.
nonisolated final class FakeMealSwapServer: Sendable {
    struct State: Sendable {
        var pages: [Data]
        var swapResult: Data
        var swapStatus: Int = 200
        var swapError: Data = Data(#"{"error":{"code":"plan_finalized","message":"the plan is finalized"}}"#.utf8)
    }

    private let state: Mutex<State>
    private let calls = Mutex<[String]>([])
    private let asks = Mutex<Int>(0)

    init(_ state: State) {
        self.state = Mutex(state)
    }

    /// Paths requested, in order, each as "METHOD path?query".
    var requestedPaths: [String] { calls.withLock { $0 } }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url, let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            return (400, Data(#"{"error":{"code":"invalid_request","message":"bad url"}}"#.utf8))
        }
        let method = request.httpMethod ?? "GET"
        calls.withLock { $0.append(method + " " + components.path + (components.query.map { "?" + $0 } ?? "")) }
        return state.withLock { state in
            switch true {
            case components.path.hasSuffix("/alternatives"):
                let index = asks.withLock { count -> Int in
                    defer { count += 1 }
                    return count
                }
                return (200, state.pages.indices.contains(index) ? state.pages[index] : state.pages.last ?? Data())
            case components.path.hasSuffix("/swap"):
                return state.swapStatus == 200
                    ? (200, state.swapResult) : (state.swapStatus, state.swapError)
            default:
                return (404, Data(#"{"error":{"code":"not_found","message":"no"}}"#.utf8))
            }
        }
    }
}

struct MealSwapStoreTests {
    private struct Harness {
        let store: MealSwapStore
        let server: FakeMealSwapServer
    }

    private func makeHarness(
        pages: [Data] = [
            MealSwapFixtures.alternatives([MealSwapFixtures.alternative(id: "r-burger", name: "Smash Burgers")])
        ],
        swapStatus: Int = 200
    ) async throws -> Harness {
        let server = FakeMealSwapServer(
            .init(
                pages: pages,
                swapResult: MealSwapFixtures.swapResult(recipeID: "r-burger", name: "Smash Burgers"),
                swapStatus: swapStatus))
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let households = HouseholdPreviewData.store(session: session)
        let store = MealSwapStore(session: session, api: AutopilotAPI(client: client), households: households)
        return Harness(store: store, server: server)
    }

    private func entry(id: String = "entry-1", recipeID: String = "recipe-tacos") -> PlanEntry {
        PlanEntry(
            id: id,
            recipe: PlanEntryRecipe(id: recipeID, name: "Beef Tacos", imageURLString: nil, isAddon: false),
            day: .tue, date: "2026-09-15", servings: 2, note: "", addedBy: "user-1", addedAt: Date(),
            origin: .manual, customizations: [])
    }

    @Test func offersAlternativesForThePlannedMeal() async throws {
        let harness = try await makeHarness()
        harness.store.start(entry(), week: try #require(ISOWeek("2026-W38")))
        await harness.store.load()

        #expect(harness.store.phase == .loaded)
        #expect(harness.store.alternatives.map(\.recipe.name) == ["Smash Burgers"])
        let alternative = try #require(harness.store.alternatives.first)
        #expect(alternative.reasonText == "Also Thai · 30 min")
        #expect(alternative.servings == 2)
        let path = try #require(harness.server.requestedPaths.last)
        #expect(path.contains("/weeks/2026-W38/entries/entry-1/alternatives"))
        #expect(path.contains("limit=3"))
        #expect(!path.contains("seen="))
    }

    @Test func showingOthersSendsWhatWasAlreadySeen() async throws {
        let harness = try await makeHarness(pages: [
            MealSwapFixtures.alternatives([MealSwapFixtures.alternative(id: "r-burger", name: "Smash Burgers")]),
            MealSwapFixtures.alternatives([MealSwapFixtures.alternative(id: "r-chili", name: "Weeknight Chili")]),
        ])
        harness.store.start(entry(), week: try #require(ISOWeek("2026-W38")))
        await harness.store.load()
        await harness.store.showOthers()

        #expect(harness.store.alternatives.map(\.recipe.name) == ["Weeknight Chili"])
        #expect(try #require(harness.server.requestedPaths.last).contains("seen=r-burger"))
    }

    @Test func aNoteExplainsWhenNothingIsSimilar() async throws {
        let harness = try await makeHarness(pages: [
            MealSwapFixtures.alternatives(
                [MealSwapFixtures.alternative(id: "r-omelet", name: "Garden Omelet", similarity: 0.1)],
                message: "Nothing in your library is much like this one.")
        ])
        harness.store.start(entry(), week: try #require(ISOWeek("2026-W38")))
        await harness.store.load()

        #expect(harness.store.notice == "Nothing in your library is much like this one.")
        #expect(harness.store.alternatives.count == 1)
    }

    @Test func applyingSwapsTheMealAndClosesTheSheet() async throws {
        let harness = try await makeHarness()
        let changed = Mutex<[Plan]>([])
        harness.store.planDidChange = { plan in changed.withLock { $0.append(plan) } }
        harness.store.start(entry(), week: try #require(ISOWeek("2026-W38")))
        await harness.store.load()

        let replaced = await harness.store.apply(try #require(harness.store.alternatives.first))
        #expect(replaced == "Beef Tacos")
        #expect(harness.store.target == nil)
        let plan = try #require(changed.withLock { $0.first })
        #expect(plan.entries.map(\.recipe.name) == ["Smash Burgers"])
        #expect(plan.entries.first?.day == .tue)
        #expect(plan.entries.first?.servings == 2)
        #expect(try #require(harness.server.requestedPaths.last).hasPrefix("POST"))
    }

    @Test func aFinalizedWeekClosesTheSheetWithAnExplanation() async throws {
        let harness = try await makeHarness(swapStatus: 409)
        harness.store.start(entry(), week: try #require(ISOWeek("2026-W38")))
        await harness.store.load()

        let replaced = await harness.store.apply(try #require(harness.store.alternatives.first))
        #expect(replaced == nil)
        #expect(harness.store.target == nil)
        #expect(harness.store.errorMessage != nil)
    }

    @Test func theActionIsOfferedOnlyForAMealThatCanStillChange() async throws {
        let harness = try await makeHarness()
        let planned = entry()
        #expect(harness.store.canSwap(planned, isDraft: true, isCooked: false))
        #expect(!harness.store.canSwap(planned, isDraft: false, isCooked: false))
        #expect(!harness.store.canSwap(planned, isDraft: true, isCooked: true))

        let addOn = PlanEntry(
            id: "entry-2",
            recipe: PlanEntryRecipe(id: "r-bread", name: "Garlic Bread", imageURLString: nil, isAddon: true),
            day: .tue, date: "2026-09-15", servings: 2, note: "", addedBy: "user-1", addedAt: Date(),
            origin: .manual, customizations: [])
        #expect(!harness.store.canSwap(addOn, isDraft: true, isCooked: false))

        let unscheduled = PlanEntry(
            id: "entry-3",
            recipe: PlanEntryRecipe(id: "r-soup", name: "Onion Soup", imageURLString: nil, isAddon: false),
            day: nil, date: nil, servings: 2, note: "", addedBy: "user-1", addedAt: Date(),
            origin: .manual, customizations: [])
        #expect(!harness.store.canSwap(unscheduled, isDraft: true, isCooked: false))
    }
}
