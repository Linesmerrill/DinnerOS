import Foundation
import Testing

@testable import DinnerOS

struct PlansAPITests {
    private let week = ISOWeek("2026-W38")

    private func makeAPI(_ transport: StubTransport) throws -> PlansAPI {
        PlansAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func jsonBody(_ request: URLRequest?) throws -> [String: Any] {
        let data = try #require(request?.httpBody)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @Test func getPlanDecodesScheduledAndUnscheduledEntries() async throws {
        let body = PlanFixtures.plan(
            entries: [
                PlanFixtures.entry(
                    id: "e1", day: "tue", servings: 2, note: "extra lime", imageURL: "https://img.example.test/t.jpg"),
                PlanFixtures.entry(
                    id: "e2", recipeID: "recipe-2", name: "Sample Soup", day: nil, date: nil, servings: 4),
            ])
        let transport = StubTransport { _ in (200, Data(body.utf8)) }

        let plan = try await makeAPI(transport).plan(
            householdID: "household-1", week: try #require(week), accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38")
        #expect(request.bearerToken == "token-1")
        #expect(plan.week == "2026-W38")
        #expect(plan.startDate == "2026-09-14")
        #expect(plan.endDate == "2026-09-20")
        #expect(plan.status == .draft)
        #expect(plan.createdAt != nil)
        #expect(plan.entries.map(\.id) == ["e1", "e2"])
        #expect(plan.entries[0].day == .tue)
        #expect(plan.entries[0].date == "2026-09-15")
        #expect(plan.entries[0].note == "extra lime")
        #expect(plan.entries[0].recipe.imageURL == URL(string: "https://img.example.test/t.jpg"))
        #expect(plan.entries[1].day == nil)
        #expect(plan.entries[1].date == nil)
        #expect(plan.entries[1].recipe.imageURL == nil)
        #expect(plan.entries(on: nil).map(\.id) == ["e2"])
        #expect(plan.entries(on: .tue).map(\.id) == ["e1"])
    }

    @Test func unplannedWeekDecodesAsEmptyDraftWithNullTimestamps() async throws {
        let transport = StubTransport { _ in (200, Data(PlanFixtures.plan(stored: false).utf8)) }

        let plan = try await makeAPI(transport).plan(householdID: "h", week: try #require(week), accessToken: "t")

        #expect(plan.entries.isEmpty)
        #expect(plan.status == .draft)
        #expect(plan.createdAt == nil)
        #expect(plan.updatedAt == nil)
    }

    @Test func listPlansSendsRangeAndDecodesSummaries() async throws {
        let body = #"""
            {"items":[
              {"week":"2026-W52","startDate":"2026-12-21","status":"finalized","entryCount":3,"updatedAt":"2026-12-18T09:00:00Z"},
              {"week":"2026-W53","startDate":"2026-12-28","status":"draft","entryCount":0,"updatedAt":null}
            ]}
            """#
        let transport = StubTransport { _ in (200, Data(body.utf8)) }

        let items = try await makeAPI(transport).listPlans(
            householdID: "household-1", from: try #require(ISOWeek("2026-W52")), to: try #require(ISOWeek("2026-W53")),
            accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/plans")
        #expect(request.url?.query(percentEncoded: true) == "from=2026-W52&to=2026-W53")
        #expect(items.map(\.week) == ["2026-W52", "2026-W53"])
        #expect(items[0].status == .finalized)
        #expect(items[0].entryCount == 3)
        #expect(items[1].updatedAt == nil)
    }

    @Test func addEntryPostsEveryField() async throws {
        let entry = PlanFixtures.entry(id: "e1", day: "fri", servings: 4, note: "no cilantro")
        let response = #"{"entry":\#(entry),"plan":\#(PlanFixtures.plan())}"#
        let transport = StubTransport { _ in (201, Data(response.utf8)) }

        let result = try await makeAPI(transport).addEntry(
            householdID: "household-1", week: try #require(week),
            entry: NewPlanEntry(recipeID: "recipe-1", day: .fri, servings: 4, note: "no cilantro"), accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38/entries")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let body = try jsonBody(request)
        #expect(body["recipeId"] as? String == "recipe-1")
        #expect(body["day"] as? String == "fri")
        #expect(body["servings"] as? Int == 4)
        #expect(body["note"] as? String == "no cilantro")
        #expect(result.entry.id == "e1")
        #expect(result.plan.week == "2026-W38")
    }

    @Test func addEntryOmitsMissingDayAndEmptyNote() async throws {
        let entry = PlanFixtures.entry(id: "e1", day: nil, date: nil)
        let response = #"{"entry":\#(entry),"plan":\#(PlanFixtures.plan())}"#
        let transport = StubTransport { _ in (201, Data(response.utf8)) }

        _ = try await makeAPI(transport).addEntry(
            householdID: "h", week: try #require(week), entry: NewPlanEntry(recipeID: "recipe-1", servings: 2),
            accessToken: "t")

        let body = try jsonBody(transport.requests.first)
        #expect(Set(body.keys) == ["recipeId", "servings"])
    }

    @Test func updateEntrySendsExplicitNullToUnscheduleAndOmitsUnchangedFields() async throws {
        let transport = StubTransport { _ in (200, Data(PlanFixtures.plan().utf8)) }
        let api = try makeAPI(transport)

        _ = try await api.updateEntry(
            householdID: "household-1", week: try #require(week), entryID: "entry-7",
            changes: PlanEntryChanges(day: .some(nil)), accessToken: "t")
        _ = try await api.updateEntry(
            householdID: "household-1", week: try #require(week), entryID: "entry-7",
            changes: PlanEntryChanges(day: .some(.sat), servings: 4, note: ""), accessToken: "t")
        _ = try await api.updateEntry(
            householdID: "household-1", week: try #require(week), entryID: "entry-7",
            changes: PlanEntryChanges(servings: 2), accessToken: "t")

        let requests = transport.requests
        #expect(requests[0].httpMethod == "PATCH")
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/plans/2026-W38/entries/entry-7")
        let unschedule = try jsonBody(requests[0])
        #expect(Set(unschedule.keys) == ["day"])
        #expect(unschedule["day"] is NSNull)

        let full = try jsonBody(requests[1])
        #expect(full["day"] as? String == "sat")
        #expect(full["servings"] as? Int == 4)
        #expect(full["note"] as? String == "")

        #expect(Set(try jsonBody(requests[2]).keys) == ["servings"])
        #expect(PlanEntryChanges().isEmpty)
        #expect(!PlanEntryChanges(day: .some(nil)).isEmpty)
    }

    @Test func deleteEntryAcceptsNoContent() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await makeAPI(transport).deleteEntry(
            householdID: "household-1", week: try #require(week), entryID: "entry-7", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38/entries/entry-7")
        #expect(request.httpBody == nil)
    }

    @Test func setStatusPutsStatus() async throws {
        let transport = StubTransport { _ in (200, Data(PlanFixtures.plan(status: "finalized").utf8)) }

        let plan = try await makeAPI(transport).setStatus(
            householdID: "household-1", week: try #require(week), status: .finalized, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38/status")
        #expect(try jsonBody(request)["status"] as? String == "finalized")
        #expect(plan.status == .finalized)
    }

    @Test func groceryListDecodesCategoriesStatusesAndSkippedEntries() async throws {
        let transport = StubTransport { _ in (200, PlanFixtures.groceryList) }

        let list = try await makeAPI(transport).groceryList(
            householdID: "household-1", week: try #require(week), accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/plans/2026-W38/grocery")
        #expect(list.week == "2026-W38")
        #expect(!list.pantryApplied)
        #expect(list.categories.map(\.category) == ["produce", "spices"])
        #expect(list.categories.map(\.title) == ["Produce", "Spices"])
        let onion = try #require(list.categories.first?.items.first)
        #expect(onion.quantityText == "1 ½ + 8 oz")
        #expect(onion.amounts.map(\.unit) == ["count", "oz"])
        #expect(onion.amounts[0].quantity == "3/2")
        #expect(onion.amounts[0].quantityValue == 1.5)
        #expect(onion.recipes.map(\.name) == ["Test Kitchen Tacos", "Sample Soup"])
        #expect(onion.status == .toBuy)
        #expect(list.allItems.map(\.status) == [.toBuy, .pantryHint, .inPantry])
        #expect(list.allItems[1].unquantified)
        #expect(
            list.skipped == [
                GrocerySkippedEntry(
                    entryID: "entry-9", recipeID: "recipe-9", recipeName: "Retired Stew", reason: .recipeUnavailable)
            ])
        #expect(!list.isEmpty)
    }

    @Test func unknownStatusesDecodeAsIs() async throws {
        let body = PlanFixtures.plan(status: "archived")
        let transport = StubTransport { _ in (200, Data(body.utf8)) }

        let plan = try await makeAPI(transport).plan(householdID: "h", week: try #require(week), accessToken: "t")

        #expect(plan.status.rawValue == "archived")
        #expect(plan.status.title == "Archived")
        #expect(GroceryCategory.title(for: "frozen-desserts") == "Frozen Desserts")
    }

    @Test(arguments: [("plan_finalized", "finalized"), ("plan_full", "50")])
    func planConflictsHaveClearMessages(code: String, expectedFragment: String) async throws {
        let transport = StubTransport { _ in (409, Fixtures.errorJSON(code: code)) }

        do {
            _ = try await makeAPI(transport).addEntry(
                householdID: "h", week: try #require(week), entry: NewPlanEntry(recipeID: "r", servings: 2),
                accessToken: "t")
            Issue.record("expected a conflict")
        } catch let error as APIError {
            #expect(error.status == 409)
            #expect(error.code == code)
            #expect(error.localizedDescription.contains(expectedFragment))
        }
    }
}
