import Foundation
import Testing

@testable import DinnerOS

/// Exporting a week's grocery list: the Reminders drafts, the Reminders write itself, and the
/// share and copy text.
@MainActor
struct GroceryExportTests {
    private let locale = Locale(identifier: "en_US")

    private func list() throws -> GroceryList {
        try JSONCoding.makeDecoder().decode(GroceryList.self, from: PlanFixtures.groceryList)
    }

    private func week() throws -> ISOWeek {
        try #require(ISOWeek("2026-W38"))
    }

    // MARK: A fake Reminders database

    /// Stands in for EventKit, so tests never touch the member's real Reminders.
    @MainActor
    private final class FakeRemindersStore: GroceryRemindersStore {
        var access: GroceryRemindersAccess
        /// What `requestAccess` answers.
        var accessAnswer: GroceryRemindersAccess
        /// Reminder titles by list name.
        var lists: [String: [GroceryReminderDraft]] = [:]
        private(set) var accessRequests = 0
        private(set) var createdLists: [String] = []
        private(set) var clearedLists: [String] = []

        init(access: GroceryRemindersAccess = .notDetermined, answers: GroceryRemindersAccess = .granted) {
            self.access = access
            accessAnswer = answers
        }

        func requestAccess() async throws -> GroceryRemindersAccess {
            accessRequests += 1
            access = accessAnswer
            return accessAnswer
        }

        func listID(titled title: String) throws -> String? {
            lists[title] != nil ? title : nil
        }

        func makeList(titled title: String) throws -> String {
            createdLists.append(title)
            lists[title] = []
            return title
        }

        func clearList(withID id: String) async throws {
            clearedLists.append(id)
            lists[id] = []
        }

        func addReminders(_ drafts: [GroceryReminderDraft], toListWithID id: String) throws {
            lists[id, default: []] += drafts
        }
    }

    // MARK: Drafts

    @Test func draftsSkipCheckedLinesAndKeepAisleOrderWithAmountsInTheTitle() throws {
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: ["i-salt"])

        // Produce before Spices, the server's aisle order. Salt is checked off, and Black
        // Pepper is already in the pantry, so neither is a reminder to buy.
        #expect(drafts.map(\.title) == ["1 ½ + 8 oz Yellow Onion"])
        #expect(drafts.map(\.notes) == ["Produce"])
    }

    @Test func aLineThePantryAlreadyHasIsNotAReminderToBuy() throws {
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: [])

        // Black Pepper is `inPantry`: the screen says "in your pantry" and the shared text
        // says "(in pantry)", so a reminder saying "buy this" would contradict both. Salt is
        // only a `pantryHint` — a guess that it's a staple — so it stays.
        #expect(!drafts.map(\.title).contains { $0.contains("Black Pepper") })
        #expect(drafts.map(\.title) == ["1 ½ + 8 oz Yellow Onion", "Salt"])
    }

    @Test func aLineWithNoAmountIsJustItsName() throws {
        // Salt has no quantity at all, so there's nothing to put in front of the name.
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: [])

        #expect(drafts.map(\.title).contains("Salt"))
    }

    @Test func everythingCheckedOffLeavesNothingToAdd() throws {
        let drafts = GroceryReminderPlan.drafts(
            for: try list(), checked: ["i-onion", "i-salt", "i-pepper"])

        #expect(drafts.isEmpty)
    }

    @Test func theListIsNamedForTheAppAndTheWeek() throws {
        let week = try week()

        let name = GroceryReminderPlan.listName(appName: "DinnerOS", week: week, weekStartsOn: .mon, locale: locale)

        #expect(name == "DinnerOS · \(week.rangeLabel(weekStartsOn: .mon, locale: locale))")
        #expect(name.hasPrefix("DinnerOS · "))
    }

    // MARK: Writing to Reminders

    @Test func deniedAccessWritesNothingAndSaysSo() async throws {
        let store = FakeRemindersStore(answers: .denied)
        let export = GroceryRemindersExport(store: store)
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: [])

        await #expect(throws: GroceryRemindersError.accessDenied) {
            try await export.export(drafts, to: "DinnerOS · Sep 14 – 20", merge: .add)
        }

        #expect(store.accessRequests == 1)
        #expect(store.lists.isEmpty)
    }

    @Test func aNewListIsCreatedAndFilled() async throws {
        let store = FakeRemindersStore()
        let export = GroceryRemindersExport(store: store)
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: ["i-salt"])
        let name = "DinnerOS · Sep 14 – 20"

        let count = try await export.export(drafts, to: name, merge: .add)

        #expect(count == 1)
        #expect(store.createdLists == [name])
        #expect(store.clearedLists.isEmpty)
        #expect(store.lists[name]?.map(\.title) == ["1 ½ + 8 oz Yellow Onion"])
    }

    @Test func anExistingListIsEmptiedFirstWhenReplacing() async throws {
        let name = "DinnerOS · Sep 14 – 20"
        let store = FakeRemindersStore(access: .granted)
        store.lists[name] = [GroceryReminderDraft(title: "Old item", notes: nil)]
        let export = GroceryRemindersExport(store: store)
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: ["i-salt"])

        #expect(try export.hasExistingList(named: name))
        let count = try await export.export(drafts, to: name, merge: .replace)

        #expect(count == 1)
        // No second list with the same name, and the hand-added reminder is gone.
        #expect(store.createdLists.isEmpty)
        #expect(store.clearedLists == [name])
        #expect(store.lists[name]?.map(\.title) == ["1 ½ + 8 oz Yellow Onion"])
    }

    @Test func anExistingListKeepsItsItemsWhenAdding() async throws {
        let name = "DinnerOS · Sep 14 – 20"
        let store = FakeRemindersStore(access: .granted)
        store.lists[name] = [GroceryReminderDraft(title: "Old item", notes: nil)]
        let export = GroceryRemindersExport(store: store)
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: ["i-salt"])

        _ = try await export.export(drafts, to: name, merge: .add)

        #expect(store.clearedLists.isEmpty)
        #expect(store.lists[name]?.first?.title == "Old item")
        #expect(store.lists[name]?.count == 2)
    }

    @Test func alreadyGrantedAccessIsNotAskedForAgain() async throws {
        let store = FakeRemindersStore(access: .granted)
        let export = GroceryRemindersExport(store: store)
        let drafts = GroceryReminderPlan.drafts(for: try list(), checked: [])

        _ = try await export.export(drafts, to: "DinnerOS · Sep 14 – 20", merge: .add)

        #expect(store.accessRequests == 0)
    }

    @Test func anEmptyExportIsRefusedBeforeAccessIsAskedFor() async throws {
        let store = FakeRemindersStore()
        let export = GroceryRemindersExport(store: store)

        await #expect(throws: GroceryRemindersError.nothingToAdd) {
            try await export.export([], to: "DinnerOS · Sep 14 – 20", merge: .add)
        }

        #expect(store.accessRequests == 0)
    }

    @Test func existingListsAreNotLookedForWithoutAccess() throws {
        let store = FakeRemindersStore(access: .notDetermined)
        store.lists["DinnerOS · Sep 14 – 20"] = []
        let export = GroceryRemindersExport(store: store)

        // Answering this needs access, so it reports "no" rather than prompting behind the scenes.
        #expect(try export.hasExistingList(named: "DinnerOS · Sep 14 – 20") == false)
    }

    // MARK: First-time explainer

    /// A `UserDefaults` suite no other test shares, so the explainer starts unseen.
    private func freshDefaults() -> String {
        "GroceryExportTests.\(UUID().uuidString)"
    }

    @Test func theExplainerShowsBeforeTheFirstExportOnlyAndBeforeAccessIsAsked() async throws {
        let store = FakeRemindersStore()
        let controller = GroceryExportController(remindersStore: store, defaultsSuite: freshDefaults())
        let week = try week()
        let expectedName = GroceryReminderPlan.listName(appName: "DinnerOS", week: week, weekStartsOn: .mon)

        await controller.addToReminders(
            list: try list(), week: week, weekStartsOn: .mon, checked: [], appName: "DinnerOS")

        // Nothing is written, and the system's access prompt waits for the explainer.
        let explainer = try #require(controller.explainer)
        #expect(explainer.name == expectedName)
        #expect(explainer.preview.map(\.title) == ["1 ½ + 8 oz Yellow Onion", "Salt"])
        #expect(explainer.moreCount == 0)
        #expect(store.accessRequests == 0)
        #expect(store.lists.isEmpty)

        await controller.confirmExplainer(explainer)

        #expect(controller.explainer == nil)
        #expect(store.createdLists == [expectedName])
        #expect(controller.hasExplained)

        // Later taps go straight through.
        await controller.addToReminders(
            list: try list(), week: week, weekStartsOn: .mon, checked: [], appName: "DinnerOS")
        #expect(controller.explainer == nil)
        #expect(controller.existingList?.name == expectedName)
    }

    @Test func cancellingTheExplainerDoesntCountAsSeen() async throws {
        let store = FakeRemindersStore()
        let controller = GroceryExportController(remindersStore: store, defaultsSuite: freshDefaults())

        await controller.addToReminders(
            list: try list(), week: try week(), weekStartsOn: .mon, checked: [], appName: "DinnerOS")
        controller.explainer = nil
        await controller.addToReminders(
            list: try list(), week: try week(), weekStartsOn: .mon, checked: [], appName: "DinnerOS")

        #expect(controller.explainer != nil)
        #expect(!controller.hasExplained)
        #expect(store.accessRequests == 0)
    }

    @Test func remindersFollowTheStoreAisleOrder() throws {
        let json = #"""
            {"week": "2026-W38", "status": "draft", "pantryApplied": true,
             "categories": [
               {"category": "produce", "items": [
                 {"ingredientKey": "i-zucchini", "name": "Zucchini", "amounts": [], "quantityText": "",
                  "unquantified": true, "status": "toBuy", "recipes": []},
                 {"ingredientKey": "i-pepper", "name": "Pepper", "amounts": [], "quantityText": "",
                  "unquantified": true, "status": "toBuy", "recipes": []}]},
               {"category": "bakery", "items": [
                 {"ingredientKey": "i-tortillas", "name": "Tortillas", "amounts": [], "quantityText": "",
                  "unquantified": true, "status": "toBuy", "recipes": []}]},
               {"category": "pantry", "items": [
                 {"ingredientKey": "i-cavatappi", "name": "Cavatappi", "amounts": [], "quantityText": "",
                  "unquantified": true, "status": "toBuy", "recipes": []},
                 {"ingredientKey": "i-oil", "name": "Cooking Oil", "amounts": [], "quantityText": "",
                  "unquantified": true, "status": "inPantry", "recipes": []}]}
             ],
             "skipped": [], "skippedItems": []}
            """#
        let list = try JSONCoding.makeDecoder().decode(GroceryList.self, from: Data(json.utf8))

        let drafts = GroceryReminderPlan.drafts(for: list, checked: [])

        #expect(drafts.map(\.title) == ["Zucchini", "Pepper", "Tortillas", "Cavatappi"])
        #expect(drafts.map(\.notes) == ["Produce", "Produce", "Bakery", "Pantry"])
    }

    // MARK: Share and copy text

    @Test func shareAndCopyTextIsExactlyTheGroceryListFormatterOutput() throws {
        let week = try week()
        let list = try list()

        // Both actions send `GroceryListModel.plainText()`, which is this call.
        let text = GroceryListText.make(list, week: week, weekStartsOn: .mon, checked: ["i-salt"], locale: locale)

        #expect(
            text == """
                Grocery List: \(week.rangeLabel(weekStartsOn: .mon, locale: locale))

                Produce
                - [ ] Yellow Onion, 1 ½ + 8 oz

                Spices
                - [x] Salt (pantry staple)
                - [ ] Black Pepper, 1 tsp + as needed (in pantry)

                Not included
                - Retired Stew: The recipe is no longer in this household.
                """)
    }

    @Test func theModelsPlainTextMatchesTheFormatterForTheSameChecks() throws {
        let session = AuthSession(
            api: AuthAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)))),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        let model = GroceryListModel.preview(session: session, list: try list(), checked: ["i-salt"])

        let fromModel = try #require(model.plainText(locale: locale))

        #expect(
            fromModel
                == GroceryListText.make(
                    try list(), week: model.week, weekStartsOn: model.weekStartsOn, checked: ["i-salt"], locale: locale)
        )
    }
}
