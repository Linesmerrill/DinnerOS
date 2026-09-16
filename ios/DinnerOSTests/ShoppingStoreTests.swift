import Foundation
import Testing

@testable import DinnerOS

struct ShoppingStoreTests {
    private final class Recorder {
        var opened: [URL] = []
        var opens = true
        var pantryRefreshes = 0
    }

    private struct Harness {
        let store: ShoppingStore
        let server: FakeShoppingServer
        let checks: InMemoryGroceryChecks
        let recorder: Recorder
    }

    private static let denver = TimeZone(identifier: "America/Denver") ?? .gmt
    private static let matchRoute = "POST /households/household-1/plans/2026-W38/shopping/walmart/match"
    private static let handoffRoute = "POST /households/household-1/plans/2026-W38/shopping/walmart/handoffs"
    private static let confirmRoute = "POST /households/household-1/shopping/handoffs/handoff-1/confirm"

    /// "Now" is Wednesday, September 16, 2026 at noon UTC: 2026-W38 in Denver.
    private func makeHarness(_ state: FakeShoppingServer.State = .init()) async throws -> Harness {
        let server = FakeShoppingServer(state)
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate("2026-09-16T12:00:00Z"))
        let checks = InMemoryGroceryChecks()
        let recorder = Recorder()
        let store = ShoppingStore(
            session: session, api: ShoppingAPI(client: client), checks: checks, now: { instant },
            openURL: { url in
                recorder.opened.append(url)
                return recorder.opens
            })
        store.onPantryChanged = { recorder.pantryRefreshes += 1 }
        store.activate(householdID: "household-1", timeZone: Self.denver)
        store.setPermissions(canEdit: true, canConfirm: true)
        return Harness(store: store, server: server, checks: checks, recorder: recorder)
    }

    private func week() throws -> ISOWeek {
        try #require(ISOWeek("2026-W38"))
    }

    // MARK: Setup and match

    @Test func theFirstRunNeedsAStoreBeforeMatching() async throws {
        let harness = try await makeHarness(.init(provider: nil, storeID: nil))
        let store = harness.store

        await store.load()

        #expect(store.setupPhase == .loaded)
        #expect(!store.isConfigured)
        #expect(store.walmart?.isSupported == true)
        #expect(store.proposal == nil)
        #expect(harness.server.log == ["GET /shopping/providers", "GET /households/household-1/shopping/settings"])

        try await store.saveSettings(storeNumber: " 5435 ")

        let body = try #require(harness.server.bodies("PUT /households/household-1/shopping/settings").last)
        #expect(body["provider"] as? String == "walmart")
        #expect(body["storeId"] as? String == "5435")
        #expect(store.isConfigured)
        #expect(store.settings?.storeID == "5435")
        #expect(store.proposalPhase == .loaded)
        #expect(store.proposal?.week == "2026-W38")

        try await store.saveSettings(storeNumber: "")
        #expect(harness.server.bodies("PUT /households/household-1/shopping/settings").last?["storeId"] is NSNull)
        #expect(store.settings?.storeID == nil)
    }

    @Test func theMatchLeavesOutThisDevicesCheckOffsAndSplitsTheSections() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        harness.checks.setCheckedItems(["i-cilantro"], householdID: "household-1", week: try week())

        await store.load()

        #expect(store.week.description == "2026-W38")
        #expect(harness.server.bodies(Self.matchRoute).last?["checkedOffKeys"] as? [String] == ["i-cilantro"])
        let proposal = try #require(store.proposal)
        #expect(proposal.lines.map(\.ingredientKey) == ["i-beef"])
        #expect(proposal.needsProduct.map(\.ingredientKey) == ["name:flour tortillas"])
        #expect(proposal.notIncluded.map(\.reason) == [.checkedOff, .pantryHint, .inPantry])
        #expect(harness.server.log.last == "GET /households/household-1/shopping/handoffs?week=2026-W38&status=open")
        #expect(store.openHandoff == nil)

        // Loading again doesn't match again.
        let requests = harness.server.log.count
        harness.checks.setCheckedItems([], householdID: "household-1", week: try week())
        await store.load()
        #expect(harness.server.log.filter { $0 == Self.matchRoute }.count == 1)
        #expect(harness.server.log.count == requests + 1)

        await store.reload()
        #expect(harness.server.bodies(Self.matchRoute).last?["checkedOffKeys"] == nil)
        #expect(store.proposal?.lines.map(\.ingredientKey) == ["i-beef", "i-cilantro"])
    }

    @Test func changingWeeksMatchesThatWeekAndForgetsCounts() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        store.setPackages(5, for: try #require(store.readyLines.first))

        await store.show(week: store.week.next)

        #expect(store.week.description == "2026-W39")
        #expect(!store.hasPackageEdits)
        #expect(store.proposal?.week == "2026-W39")
        #expect(harness.server.log.contains("POST /households/household-1/plans/2026-W39/shopping/walmart/match"))

        await store.showCurrentWeek()
        #expect(store.week.description == "2026-W38")
        #expect(store.proposal?.week == "2026-W38")
    }

    // MARK: Following the plan

    private func plan(week: String = "2026-W38", entries: [String]) throws -> Plan {
        try JSONCoding.makeDecoder().decode(
            Plan.self, from: Data(PlanFixtures.plan(week: week, entries: entries).utf8))
    }

    /// The match is computed from the week's grocery list, so a serving size changed on the
    /// Menu changes every quantity here. Nothing used to ask again — the tab's `.task` is keyed
    /// on the household and `load()` keeps a proposal it already has — so the list and its
    /// exports kept the old numbers with no way to refresh them.
    @Test func aServingSizeChangedOnTheMenuReMatchesTheShownWeek() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        func matches() -> Int { harness.server.log.filter { $0 == Self.matchRoute }.count }
        #expect(matches() == 1)
        #expect(store.planRevision == 0)

        // The first plan for the week is the one the tab's own load already matched.
        await store.planDidChange(try plan(entries: [PlanFixtures.entry(id: "e1", servings: 2)]))
        #expect(matches() == 1)
        #expect(store.planRevision == 1)

        // Every load hands the plan over again; an unchanged plan costs no request.
        await store.planDidChange(try plan(entries: [PlanFixtures.entry(id: "e1", servings: 2)]))
        #expect(matches() == 1)
        #expect(store.planRevision == 1)

        // A serving size change re-matches, and tells the export section to rebuild its list.
        await store.planDidChange(try plan(entries: [PlanFixtures.entry(id: "e1", servings: 4)]))
        #expect(matches() == 2)
        #expect(store.planRevision == 2)

        // A note can't change a quantity, so it doesn't buy a request.
        await store.planDidChange(
            try plan(entries: [PlanFixtures.entry(id: "e1", servings: 4, note: "extra hot")]))
        #expect(matches() == 2)
        #expect(store.planRevision == 2)

        // Neither does a meal moved to another day.
        await store.planDidChange(
            try plan(entries: [PlanFixtures.entry(id: "e1", day: "thu", servings: 4, note: "extra hot")]))
        #expect(matches() == 2)
        #expect(store.planRevision == 2)

        // Adding a meal does.
        await store.planDidChange(
            try plan(entries: [
                PlanFixtures.entry(id: "e1", day: "thu", servings: 4, note: "extra hot"),
                PlanFixtures.entry(id: "e2", recipeID: "recipe-2"),
            ]))
        #expect(matches() == 3)
        #expect(store.planRevision == 3)
    }

    /// `planDidChange` is handed every week's plan, not only the shown one.
    @Test func anotherWeeksPlanNeverDisturbsTheShownWeek() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        func matches() -> Int { harness.server.log.filter { $0 == Self.matchRoute }.count }
        await store.planDidChange(try plan(entries: [PlanFixtures.entry(id: "e1", servings: 2)]))
        #expect(matches() == 1)

        await store.planDidChange(try plan(week: "2026-W39", entries: [PlanFixtures.entry(id: "e9", servings: 8)]))
        await store.planDidChange(try plan(week: "2026-W39", entries: [PlanFixtures.entry(id: "e9", servings: 6)]))

        #expect(matches() == 1)
        #expect(store.planRevision == 1)

        // The shown week still re-matches afterwards: the other week didn't take its place.
        await store.planDidChange(try plan(entries: [PlanFixtures.entry(id: "e1", servings: 4)]))
        #expect(matches() == 2)
        #expect(store.planRevision == 2)
    }

    /// A plan for a household this store isn't showing is ignored outright.
    @Test func anotherHouseholdsPlanIsIgnored() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()

        let other = try JSONCoding.makeDecoder().decode(
            Plan.self,
            from: Data(
                PlanFixtures.plan(householdID: "household-2", entries: [PlanFixtures.entry(id: "e1", servings: 4)])
                    .utf8))
        await store.planDidChange(other)

        #expect(harness.server.log.filter { $0 == Self.matchRoute }.count == 1)
        #expect(store.planRevision == 0)
    }

    // MARK: Package counts

    @Test func changedCountsNameEveryReadyLine() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        let lines = store.readyLines
        try #require(lines.count == 2)
        let (beef, cilantro) = (lines[0], lines[1])

        #expect(store.handoffRequest == ShoppingMatchRequest())

        store.setPackages(5, for: beef)
        #expect(store.packages(for: beef) == 5)
        #expect(
            store.handoffRequest.lines == [
                ShoppingLineSelection(ingredientKey: "i-beef", packages: 5),
                ShoppingLineSelection(ingredientKey: "i-cilantro"),
            ])

        // Back to the matched count forgets the change.
        store.setPackages(3, for: beef)
        #expect(!store.hasPackageEdits)
        #expect(store.handoffRequest.lines == nil)

        store.setPackages(500, for: cilantro)
        #expect(store.packages(for: cilantro) == 99)
        store.setPackages(0, for: cilantro)
        #expect(store.packages(for: cilantro) == 1)
        #expect(!store.hasPackageEdits)
    }

    // MARK: Handoff

    @Test func openInWalmartStoresAHandoffAndOpensEachLinkInOrder() async throws {
        let harness = try await makeHarness(.init(productsPerLink: 1))
        let store = harness.store
        await store.load()
        store.setPackages(2, for: try #require(store.readyLines.first))

        try await store.openInWalmart()

        let body = try #require(harness.server.bodies(Self.handoffRoute).last)
        let lines = try #require(body["lines"] as? [[String: Any]])
        #expect(lines.compactMap { $0["ingredientKey"] as? String } == ["i-beef", "i-cilantro"])
        #expect(lines[0]["packages"] as? Int == 2)
        #expect(lines[1]["packages"] == nil)

        let progress = try #require(store.linkProgress)
        #expect(progress.handoff.id == "handoff-1")
        #expect(progress.total == 2)
        #expect(progress.openedCount == 1)
        #expect(progress.nextIndex == 1)
        #expect(store.weekLinkProgress == progress)
        #expect(store.openHandoff?.id == "handoff-1")
        #expect(!store.isCreatingHandoff)
        #expect(
            harness.recorder.opened.map(\.absoluteString) == [
                "https://www.walmart.com/sc/cart/addToCart?items=100000001_2&storeId=5435"
            ])

        await store.openNextCartLink()
        #expect(
            harness.recorder.opened.last?.absoluteString
                == "https://www.walmart.com/sc/cart/addToCart?items=100000002&storeId=5435")
        #expect(store.linkProgress?.isFinished == true)
        #expect(store.linkProgress?.nextIndex == nil)

        await store.openNextCartLink()
        #expect(harness.recorder.opened.count == 2)
    }

    @Test func aLinkThatDoesntOpenCanBeTriedAgain() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        harness.recorder.opens = false

        try await store.openInWalmart()

        #expect(store.linkError != nil)
        #expect(store.linkProgress?.openedCount == 0)

        harness.recorder.opens = true
        await store.openNextCartLink()
        #expect(store.linkError == nil)
        #expect(store.linkProgress?.isFinished == true)
        #expect(harness.recorder.opened.count == 2)
        #expect(harness.server.handoffs.count == 1)
    }

    @Test func aHandoffWithNothingToAddShowsTheAPIMessage() async throws {
        let harness = try await makeHarness(.init(products: [:]))
        let store = harness.store
        await store.load()
        #expect(store.readyLines.isEmpty)

        do {
            try await store.openInWalmart()
            Issue.record("expected validation_failed")
        } catch {
            #expect(ShoppingStore.message(for: error) == "no line to hand off")
        }
        #expect(store.linkProgress == nil)
        #expect(!store.isCreatingHandoff)
        #expect(harness.recorder.opened.isEmpty)
    }

    // MARK: Did you order these?

    @Test func theOrderQuestionWaitsForEveryLinkAndRespectsNotYet() async throws {
        let harness = try await makeHarness(.init(productsPerLink: 1))
        let store = harness.store
        await store.load()
        try await store.openInWalmart()

        // Back from the first link: there's still a link to open.
        await store.appDidBecomeActive()
        #expect(store.confirmationPrompt == nil)
        #expect(store.openHandoff?.id == "handoff-1")

        await store.openNextCartLink()
        await store.appDidBecomeActive()
        #expect(store.confirmationPrompt?.id == "handoff-1")

        store.dismissConfirmation()
        #expect(store.confirmationPrompt == nil)
        await store.appDidBecomeActive()
        #expect(store.confirmationPrompt == nil)
        #expect(store.openHandoff?.id == "handoff-1")

        // The banner still offers it.
        store.reviewOpenHandoff()
        #expect(store.confirmationPrompt?.id == "handoff-1")

        // Without `pantry.edit` nobody is asked.
        store.setPermissions(canEdit: true, canConfirm: false)
        #expect(store.confirmationPrompt == nil)
        store.reviewOpenHandoff()
        #expect(store.confirmationPrompt == nil)
    }

    @Test func orderingEverythingChecksTheLinesOffAndRefreshesThePantry() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        store.reviewOpenHandoff()
        let handoff = try #require(store.confirmationPrompt)

        let response = try await store.confirm(.all, for: handoff)

        let body = try #require(harness.server.bodies(Self.confirmRoute).last)
        #expect(Set(body.keys) == ["all"])
        #expect(response.purchases.map(\.ingredientKey) == ["i-beef", "i-cilantro"])
        #expect(response.purchases.first?.purchase?.provider?.handoffID == "handoff-1")
        #expect(
            harness.checks.checkedItems(householdID: "household-1", week: try week()) == ["i-beef", "i-cilantro"])
        #expect(harness.recorder.pantryRefreshes == 1)
        #expect(store.openHandoff == nil)
        #expect(store.confirmationPrompt == nil)
        #expect(store.linkProgress == nil)
        // The week is matched again, without the lines just ordered.
        #expect(
            harness.server.bodies(Self.matchRoute).last?["checkedOffKeys"] as? [String] == ["i-beef", "i-cilantro"])
        #expect(store.proposal?.lines.isEmpty == true)
    }

    @Test func orderingSomeLinesSkipsTheRest() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        let handoff = try #require(store.openHandoff)
        var draft = OrderConfirmationDraft(handoff: handoff)
        draft.toggle(handoff.lines[1])
        draft.setPackages(2, for: handoff.lines[0])

        try await store.confirm(try #require(draft.selectedRequest), for: handoff)

        let body = try #require(harness.server.bodies(Self.confirmRoute).last)
        #expect(body["skipRest"] as? Bool == true)
        let lines = try #require(body["lines"] as? [[String: Any]])
        #expect(lines.count == 1)
        #expect(lines.first?["lineId"] as? String == "l1")
        #expect(lines.first?["packages"] as? Int == 2)
        #expect(harness.checks.checkedItems(householdID: "household-1", week: try week()) == ["i-beef"])
        #expect(harness.server.handoffs.first?.lines.map(\.status) == ["confirmed", "skipped"])
        #expect(harness.server.handoffs.first?.lines.first?.confirmedPackages == 2)
        #expect(store.openHandoff == nil)
        #expect(store.proposal?.lines.map(\.ingredientKey) == ["i-cilantro"])
    }

    @Test func aConflictIsRetriedOnce() async throws {
        let harness = try await makeHarness(.init(conflictsRemaining: 1))
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        let handoff = try #require(store.openHandoff)

        try await store.confirm(.all, for: handoff)

        #expect(harness.server.log.filter { $0 == Self.confirmRoute }.count == 2)
        #expect(harness.server.handoffs.first?.isOpen == false)

        harness.server.update { $0.conflictsRemaining = 2 }
        do {
            try await store.confirm(.all, for: handoff)
            Issue.record("expected a conflict")
        } catch let error as APIError {
            #expect(error.status == 409)
        }
        #expect(harness.server.log.filter { $0 == Self.confirmRoute }.count == 4)
    }

    // MARK: Saved products

    @Test func choosingAProductSavesItUnderTheEncodedKeyAndMatchesAgain() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        await store.loadPreferences()
        var draft = SavedProductDraft()
        draft.linkText = "Look at these https://www.walmart.com/ip/Test-Tortillas/100000009 okay"
        draft.displayName = "Test tortillas"
        draft.hasPackageSize = true
        draft.packageQuantityText = "10"
        draft.packageUnit = "count"

        try await store.savePreference(
            ingredientKey: "name:flour tortillas",
            request: try #require(draft.request(ingredientName: "Flour Tortillas")))

        #expect(
            harness.server.log.contains(
                "PUT /households/household-1/shopping/walmart/preferences/name:flour%20tortillas"))
        #expect(harness.server.products["name:flour tortillas"]?.productID == "100000009")
        #expect(store.proposal?.needsProduct.isEmpty == true)
        #expect(store.proposal?.lines.map(\.ingredientKey) == ["i-beef", "i-cilantro", "name:flour tortillas"])
        #expect(store.preferences.map(\.ingredientName) == ["Cilantro", "Flour Tortillas", "Ground Beef"])
    }

    @Test func aRejectedLinkKeepsTheAPIMessage() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()

        do {
            try await store.savePreference(
                ingredientKey: "name:flour tortillas",
                request: ShoppingPreferenceRequest(product: .url("https://walmrt.us/3AbCdEf"), displayName: "Tortillas")
            )
            Issue.record("expected validation_failed")
        } catch {
            #expect(ShoppingStore.message(for: error) == "productUrl must be a walmart.com/ip product link")
        }
        #expect(store.proposal?.needsProduct.map(\.ingredientKey) == ["name:flour tortillas"])
    }

    @Test func removingASavedProductPutsTheLineBackUnderNeedsAProduct() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        await store.loadPreferences()
        #expect(store.preferencesPhase == .loaded)
        #expect(store.preferences.map(\.ingredientName) == ["Cilantro", "Ground Beef"])

        try await store.deletePreference(ingredientKey: "i-cilantro")

        #expect(store.preferences.map(\.ingredientKey) == ["i-beef"])
        #expect(store.proposal?.needsProduct.map(\.ingredientKey) == ["i-cilantro", "name:flour tortillas"])

        // Someone else already removed it: still removed.
        try await store.deletePreference(ingredientKey: "i-cilantro")
        #expect(store.preferences.map(\.ingredientKey) == ["i-beef"])
    }

    // MARK: Reset

    @Test func switchingHouseholdsStartsOverAndSignOutForgetsEverything() async throws {
        let harness = try await makeHarness(.init(productsPerLink: 1))
        let store = harness.store
        await store.load()
        store.setPackages(4, for: try #require(store.readyLines.first))
        try await store.openInWalmart()
        await store.show(week: store.week.next)
        await store.showCurrentWeek()

        // The same household keeps its state.
        store.activate(householdID: "household-1", timeZone: Self.denver)
        #expect(store.proposal != nil)
        #expect(store.linkProgress != nil)

        store.activate(householdID: "household-2", timeZone: Self.denver)

        #expect(store.householdID == "household-2")
        #expect(store.setupPhase == .idle)
        #expect(store.settings == nil)
        #expect(store.providers.isEmpty)
        #expect(store.proposal == nil)
        #expect(store.proposalPhase == .idle)
        #expect(!store.hasPackageEdits)
        #expect(store.linkProgress == nil)
        #expect(store.openHandoff == nil)
        #expect(store.confirmationPrompt == nil)
        #expect(store.preferences.isEmpty)
        #expect(store.week.description == "2026-W38")
        #expect(store.canEdit)

        store.reset()

        #expect(store.householdID == nil)
        #expect(!store.canEdit)
        #expect(!store.canConfirm)
        let requests = harness.server.log.count
        await store.load()
        await store.appDidBecomeActive()
        #expect(harness.server.log.count == requests)
        await #expect(throws: AuthSessionError.self) {
            try await store.openInWalmart()
        }
    }
}
