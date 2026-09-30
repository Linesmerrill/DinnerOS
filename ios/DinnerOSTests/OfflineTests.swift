import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// The app keeps working offline from the last answers it saved.
struct ResponseCacheTests {
    private func makeCache() -> (ResponseCache, URL) {
        let dir = FileManager.default.temporaryDirectory.appending(path: "ResponseCacheTests-\(UUID().uuidString)")
        return (ResponseCache(directory: dir), dir)
    }

    @Test func savesAndReplaysAGet() throws {
        let (cache, dir) = makeCache()
        defer { try? FileManager.default.removeItem(at: dir) }
        let request = URLRequest(url: try #require(URL(string: "https://api.test/api/v1/households/h1/plans/2026-W40")))
        cache.store(Data("plan".utf8), for: request)
        #expect(cache.load(for: request)?.data == Data("plan".utf8))
        var other = request
        other.url = URL(string: "https://api.test/api/v1/households/h1/plans/2026-W41")
        #expect(cache.load(for: other) == nil, "each URL is its own answer")
        cache.clear()
        #expect(cache.load(for: request) == nil, "sign-out clears everything")
    }

    @Test func neverSavesWritesOrCookingSessions() throws {
        var post = URLRequest(url: try #require(URL(string: "https://api.test/api/v1/households/h1/pantry")))
        post.httpMethod = "POST"
        #expect(ResponseCache.key(for: post) == nil)
        var cook = URLRequest(url: try #require(URL(string: "https://api.test/api/v1/households/h1/cook-sessions/r1")))
        cook.httpMethod = "GET"
        #expect(ResponseCache.key(for: cook) == nil, "a stale cooking session would undo newer checks")
    }

    @Test func replaysOnlyWhenTheNetworkCantBeReached() {
        #expect(ResponseCache.shouldReplay(.transport(.notConnectedToInternet)))
        #expect(ResponseCache.shouldReplay(.transport(.timedOut)))
        #expect(ResponseCache.shouldReplay(.server(status: 503, code: "http_503", message: "", requestID: nil)))
        #expect(!ResponseCache.shouldReplay(.transport(.cancelled)))
        #expect(!ResponseCache.shouldReplay(.server(status: 404, code: "not_found", message: "", requestID: nil)))
        #expect(!ResponseCache.shouldReplay(.decoding(type: "Plan")))
    }
}

struct APIClientOfflineTests {
    private nonisolated final class Flags: Sendable {
        let offline = Mutex(false)
        let replays = Mutex(0)
        let lives = Mutex(0)
    }

    @Test func aGetAnsweredOnceIsReplayedOfflineWithoutRetries() async throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "APIClientOffline-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: dir) }
        let flags = Flags()
        let cache = ResponseCache(
            directory: dir, onReplay: { _ in flags.replays.withLock { $0 += 1 } },
            onLive: { flags.lives.withLock { $0 += 1 } })
        let transport = StubTransport { _ in
            if flags.offline.withLock({ $0 }) { throw URLError(.notConnectedToInternet) }
            return (200, Fixtures.meJSON)
        }
        let slept = Mutex(0)
        let policy = RetryPolicy(delays: [.seconds(1), .seconds(2)]) { _ in slept.withLock { $0 += 1 } }
        let client = APIClient(
            baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport, retry: policy,
            cache: cache)
        let request = APIRequest.get("/api/v1/me").authorized(with: "t")

        let live = try await client.send(request, as: MeResponse.self)
        flags.offline.withLock { $0 = true }
        let saved = try await client.send(request, as: MeResponse.self)

        #expect(saved == live)
        #expect(flags.lives.withLock { $0 } == 1)
        #expect(flags.replays.withLock { $0 } == 1)
        #expect(slept.withLock { $0 } == 0, "a saved answer beats waiting on retries")
        // Never answered: the offline error still reaches the screen.
        await #expect(throws: APIError.transport(.notConnectedToInternet)) {
            _ = try await client.send(APIRequest.get("/api/v1/other").authorized(with: "t"), as: MeResponse.self)
        }
    }
}

@MainActor
struct OfflineStatusTests {
    @Test func showsTheOldestSavedTimeUntilSomethingIsLive() {
        let status = OfflineStatus()
        #expect(!status.isShowingSaved)
        let early = Date(timeIntervalSince1970: 1000), late = Date(timeIntervalSince1970: 2000)
        status.replayed(savedAt: late)
        status.replayed(savedAt: early)
        status.replayed(savedAt: late)
        #expect(status.savedAt == early)
        status.live()
        #expect(!status.isShowingSaved)
    }
}

/// The server builds the cooking checklist and names the timers; the app shows them as sent.
struct ServerChecklistTests {
    private static let json = Data(
        #"""
        {"recipeId":"recipe-1","steps":[{"index":1,"text":"Cook until browned, 3-4 minutes.","segments":[],"notes":[],
          "timers":[{"text":"3-4 minutes","lowSeconds":180,"highSeconds":240,"startSeconds":240,"subject":"Ground Beef"}]}],
         "checklist":{
          "all":[{"id":"1-ingredient-Garlic","index":1,"name":"Garlic","amountText":"2 cloves","ingredientKey":"k","parts":[],"components":[]}],
          "byStep":[
            {"index":0,"items":[{"id":"3-ingredient-Olive Oil@mix","ingredientIndex":3,"name":"Southwest Spice Blend","amountText":"1 Tbsp","prep":"mix together first","parts":["2 tsp Chili Powder","1 tsp Ground Cumin"],"ingredientKey":"s"}]},
            {"index":1,"items":[{"id":"1-ingredient-Garlic@1-0","ingredientIndex":1,"name":"Garlic","amountText":"2 cloves","prep":"peeled and minced","parts":[],"ingredientKey":"k"}]}]}}
        """#.utf8)

    @Test func theCookingScreenUsesTheServersChecklist() throws {
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: Self.json)
        let dish = CookDish(recipe: RecipePreviewData.recipe, servings: 2, instructions: instructions)
        #expect(dish.ingredients.map(\.name) == ["Garlic"])
        #expect(dish.ingredients.first?.amountText == "2 cloves")
        let ready = try #require(dish.stepGroups.first { $0.index == 0 })
        #expect(ready.items.first?.prep == "mix together first")
        #expect(ready.items.first?.parts == ["2 tsp Chili Powder", "1 tsp Ground Cumin"])
        #expect(dish.stepGroups.first { $0.index == 1 }?.items.first?.prep == "peeled and minced")
    }

    @Test func aTimerStartsWhereTheServerSays() throws {
        let instructions = try JSONCoding.makeDecoder().decode(RecipeInstructions.self, from: Self.json)
        let timer = try #require(instructions.steps.first?.timers.first)
        #expect(timer.subject == "Ground Beef" && timer.startSeconds == 240)
        let duration = try #require(CookDurations.find(in: "3-4 minutes").first)
        let url = try #require(
            CookTimerText.url(step: 1, duration: duration, subject: timer.subject, start: timer.startSeconds))
        let request = try #require(CookTimerText.request(from: url))
        #expect(request.startSeconds == 240 && request.subject == "Ground Beef")
    }
}

struct FreezerSettingTests {
    @Test func freezerWrapDefaultsToVacuumSealed() throws {
        let json = Data(
            #"{"id":"h","name":"H","defaultServings":2,"timeZone":"America/Denver","createdBy":"u","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"#
                .utf8)
        let household = try JSONCoding.makeDecoder().decode(Household.self, from: json)
        #expect(household.freezerWrap == .vacuumSealed)
        var draft = HouseholdSettingsDraft(household)
        draft.freezerWrap = .freezerBag
        #expect(draft.changes(against: household).freezerWrap == .freezerBag)
    }

    @Test func frozenFoodReadsAsBestBy() throws {
        let today = try #require(PantryDate.date(from: "2026-09-29", timeZone: .gmt))
        let expiry = try #require(PantryExpiry(expiresOn: "2027-01-29", today: today, timeZone: .gmt))
        #expect(expiry.text(locale: Locale(identifier: "en_US"), frozen: true) == "Best by Jan 29, 2027")
        let past = try #require(PantryExpiry(expiresOn: "2026-09-01", today: today, timeZone: .gmt))
        #expect(past.text(frozen: true) == "Past its best-by date")
    }
}
