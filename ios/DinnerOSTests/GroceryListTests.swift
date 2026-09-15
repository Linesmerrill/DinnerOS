import Foundation
import Testing

@testable import DinnerOS

struct GroceryListModelTests {
    private let week = ISOWeek("2026-W38")

    private func makeModel(
        checks: any GroceryCheckStorage, householdID: String = "household-1", week: ISOWeek? = nil
    ) async throws -> GroceryListModel {
        let server = FakePlanServer()
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return GroceryListModel(
            householdID: householdID, week: try #require(week ?? self.week), session: session,
            api: PlansAPI(client: client), checks: checks)
    }

    private func withDefaults(_ body: (UserDefaults) async throws -> Void) async throws {
        let suiteName = "GroceryListModelTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }
        try await body(defaults)
    }

    @Test func loadsTheListAndCountsWhatsLeft() async throws {
        let model = try await makeModel(checks: InMemoryGroceryChecks())

        await model.load()

        #expect(model.phase == .loaded)
        #expect(model.list?.allItems.count == 3)
        #expect(model.remainingCount == 3)
        let onion = try #require(model.list?.allItems.first)
        model.toggle(onion)
        #expect(model.isChecked(onion))
        #expect(model.remainingCount == 2)
    }

    @Test func checksPersistPerHouseholdAndWeek() async throws {
        try await withDefaults { defaults in
            let first = try await makeModel(checks: UserDefaultsGroceryChecks(defaults: defaults))
            await first.load()
            let items = try #require(first.list?.allItems)
            first.toggle(items[0])
            first.toggle(items[2])
            first.toggle(items[2])

            // A new screen for the same week sees the same checks.
            let reopened = try await makeModel(checks: UserDefaultsGroceryChecks(defaults: defaults))
            #expect(reopened.checked == ["i-onion"])
            #expect(
                defaults.stringArray(
                    forKey: UserDefaultsGroceryChecks.key(householdID: "household-1", week: try #require(week)))
                    == ["i-onion"])

            let nextWeek = try await makeModel(
                checks: UserDefaultsGroceryChecks(defaults: defaults), week: try #require(week).next)
            #expect(nextWeek.checked.isEmpty)
            let otherHousehold = try await makeModel(
                checks: UserDefaultsGroceryChecks(defaults: defaults), householdID: "household-2")
            #expect(otherHousehold.checked.isEmpty)

            reopened.uncheckAll()
            #expect(reopened.checked.isEmpty)
            #expect(
                defaults.object(
                    forKey: UserDefaultsGroceryChecks.key(householdID: "household-1", week: try #require(week))) == nil)
        }
    }
}

struct GroceryListTextTests {
    private let locale = Locale(identifier: "en_US")

    private func list() throws -> GroceryList {
        try JSONCoding.makeDecoder().decode(GroceryList.self, from: PlanFixtures.groceryList)
    }

    @Test func exportsCategoriesInOrderWithChecksStatusesAndSkippedEntries() throws {
        let week = try #require(ISOWeek("2026-W38"))

        let text = GroceryListText.make(try list(), week: week, checked: ["i-salt"], locale: locale)

        #expect(
            text == """
                Grocery List: \(week.rangeLabel(locale: locale))

                Produce
                - [ ] Yellow Onion, 1 ½ + 8 oz

                Spices
                - [x] Salt (pantry staple)
                - [ ] Black Pepper, 1 tsp + as needed (in pantry)

                Not included
                - Retired Stew: The recipe is no longer in this household.
                """)
    }

    @Test func emptyListSaysSo() throws {
        let week = try #require(ISOWeek("2026-W38"))
        let empty = GroceryList(week: "2026-W38", status: .draft, pantryApplied: false, categories: [], skipped: [])

        let text = GroceryListText.make(empty, week: week, checked: [], locale: locale)

        #expect(text.hasSuffix("\n\nNothing to buy."))
        #expect(!text.contains("Not included"))
    }

    @Test func amountTextCombinesQuantityAndUnquantified() throws {
        let items = try list().allItems
        #expect(GroceryListText.amountText(for: items[0]) == "1 ½ + 8 oz")
        #expect(GroceryListText.amountText(for: items[1]) == nil)
        #expect(GroceryListText.amountText(for: items[2]) == "1 tsp + as needed")
    }
}
