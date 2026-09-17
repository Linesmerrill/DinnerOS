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
    private func makeHarness(
        _ state: FakeShoppingServer.State = .init(), sharing shared: FakeShoppingServer? = nil
    ) async throws -> Harness {
        let server = shared ?? FakeShoppingServer(state)
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
        store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        store.setPermissions(canEdit: true, canConfirm: true)
        return Harness(store: store, server: server, checks: checks, recorder: recorder)
    }

    private func week() throws -> ISOWeek {
        try #require(ISOWeek("2026-W38"))
    }

    // MARK: Order reminder

    private static let orderRoute = "GET /households/household-1/shopping/weeks/2026-W38/order"
    private static let setOrderRoute = "PUT /households/household-1/shopping/weeks/2026-W38/order"

    /// A new start day drops the week's order state so the next load asks the server again,
    /// whose meals for the week may have moved.
    @Test func aNewWeekStartDayForgetsTheWeeksStateUntilItLoadsAgain() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        #expect(store.orderReminder != nil)
        let orderLoads = harness.server.log.filter { $0 == Self.orderRoute }.count

        store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .sun)

        #expect(store.weekStartsOn == .sun)
        #expect(store.week == (try week()))
        #expect(store.orderReminder == nil)
        await store.load()
        #expect(store.orderReminder != nil)
        #expect(harness.server.log.filter { $0 == Self.orderRoute }.count == orderLoads + 1)
    }

    @Test func theWeeksOrderReminderLoadsWithTheWeek() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        await store.load()

        let reminder = try #require(store.orderReminder)
        #expect(reminder.week == "2026-W38")
        #expect(reminder.orderDay == "thu")
        #expect(reminder.orderDayName == "Thursday")
        #expect(reminder.remind)
        #expect(!reminder.ordered)
        #expect(harness.server.log.last == Self.orderRoute)
    }

    @Test func aHouseholdWithNoOrderDayGetsNoReminder() async throws {
        let harness = try await makeHarness(.init(orderDay: nil))
        let store = harness.store

        await store.load()

        let reminder = try #require(store.orderReminder)
        #expect(reminder.orderDay == nil)
        #expect(reminder.orderDayName == nil)
        #expect(!reminder.due)
        #expect(!reminder.remind)
    }

    @Test func markingTheWeekOrderedSilencesTheReminderAndIsUndoable() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        let refreshes = harness.recorder.pantryRefreshes

        try await store.setWeekOrdered(true)

        #expect(harness.server.bodies(Self.setOrderRoute).last?["ordered"] as? Bool == true)
        #expect(store.orderReminder?.ordered == true)
        #expect(store.orderReminder?.remind == false)
        #expect(!store.isSettingOrdered)
        // Marking reads the bell's copy server-side, so the badge refreshes with the pantry.
        #expect(harness.recorder.pantryRefreshes == refreshes + 1)

        // A mis-tap must not cost the household its reminder for the week.
        try await store.setWeekOrdered(false)

        #expect(harness.server.bodies(Self.setOrderRoute).last?["ordered"] as? Bool == false)
        #expect(store.orderReminder?.ordered == false)
        #expect(store.orderReminder?.remind == true)
    }

    @Test func handingTheListToWalmartNeverMarksTheWeekOrdered() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()

        try await store.openInWalmart()

        #expect(!harness.recorder.opened.isEmpty)
        // Opening a cart link is not placing an order, so nothing infers it.
        #expect(store.orderReminder?.ordered == false)
        #expect(store.orderReminder?.remind == true)
        #expect(!harness.server.log.contains(Self.setOrderRoute))
    }

    @Test func nextWeekStartsFresh() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.setWeekOrdered(true)
        #expect(store.orderReminder?.ordered == true)

        await store.show(week: try week().next)

        #expect(store.week.description == "2026-W39")
        #expect(store.orderReminder?.week == "2026-W39")
        #expect(store.orderReminder?.ordered == false)
        #expect(store.orderReminder?.remind == true)
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
        // The week's open handoff and then its order reminder are read after the match.
        #expect(harness.server.log.contains("GET /households/household-1/shopping/handoffs?week=2026-W38&status=open"))
        #expect(harness.server.log.last == Self.orderRoute)
        #expect(store.openHandoff == nil)

        // Loading again doesn't match again; it re-reads the handoff and the reminder.
        let requests = harness.server.log.count
        harness.checks.setCheckedItems([], householdID: "household-1", week: try week())
        await store.load()
        #expect(harness.server.log.filter { $0 == Self.matchRoute }.count == 1)
        #expect(harness.server.log.count == requests + 2)

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

    // MARK: Sending again

    private static let cartURL = "https://www.walmart.com/sc/cart/addToCart?items="

    /// The TestFlight bug: the list went to Walmart, the member came back to add more, and
    /// opening Walmart again re-added everything, doubling the cart.
    @Test func openingWalmartAgainWithNothingNewOpensNoLink() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        #expect(store.proposal?.cart == nil)
        #expect(!store.isEverythingInCart)

        try await store.openInWalmart()

        #expect(harness.recorder.opened.map(\.absoluteString) == [Self.cartURL + "100000001_3,100000002&storeId=5435"])
        // Matched again: both lines are now in the cart.
        let proposal = try #require(store.proposal)
        #expect(proposal.cart?.handoffID == "handoff-1")
        #expect(proposal.linesToSend.isEmpty)
        #expect(proposal.linesInCart.map(\.ingredientKey) == ["i-beef", "i-cilantro"])
        #expect(proposal.lines.first?.cart == ShoppingLineCart(sentPackages: 3, addPackages: 0, removePackages: 0))
        #expect(store.isEverythingInCart)
        #expect(store.linkNotice == nil)

        try await store.openInWalmart()

        #expect(harness.recorder.opened.count == 1)
        #expect(store.linkNotice == "Everything is already in your Walmart cart")
        #expect(store.linkProgress == nil)
        #expect(harness.server.handoffs.count == 1)
        #expect(harness.server.handoffs.first?.lines.map(\.packages) == [3, 1])
    }

    @Test func aHigherCountAndANewLineSendOnlyTheDifference() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        let beef = try #require(store.readyLines.first)

        store.setPackages(5, for: beef)

        #expect(store.addPackages(for: beef) == 2)
        #expect(store.linesToAdd.map(\.ingredientKey) == ["i-beef"])
        #expect(!store.isEverythingInCart)
        #expect(ShoppingText.cartStatus(sent: beef.sentPackages, wanted: 5) == "In Walmart cart · 3 · Adds 2 more")

        try await store.openInWalmart()

        #expect(harness.recorder.opened.last?.absoluteString == Self.cartURL + "100000001_2&storeId=5435")
        #expect(harness.server.handoffs.count == 1)
        #expect(harness.server.handoffs.first?.lines.map(\.packages) == [5, 1])
        #expect(store.isEverythingInCart)

        // The member plans another meal: only its line goes to Walmart.
        harness.server.update { state in
            state.grocery.append(.init(key: "i-lime", name: "Lime", category: "produce", computed: 2))
            state.products["i-lime"] = .init(
                productID: "100000003", displayName: "Test limes", ingredientName: "Lime", sizeQuantity: "1",
                sizeUnit: "count")
        }
        await store.reload()
        #expect(store.proposal?.linesToSend.map(\.ingredientKey) == ["i-lime"])

        try await store.openInWalmart()

        #expect(harness.recorder.opened.last?.absoluteString == Self.cartURL + "100000003_2&storeId=5435")
        #expect(harness.server.handoffs.first?.lines.map(\.id) == ["l1", "l2", "l3"])
    }

    @Test func aLowerCountSaysToRemoveItInTheWalmartApp() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        let beef = try #require(store.proposal?.linesInCart.first)

        store.setPackages(1, for: beef)

        #expect(store.removePackages(for: beef) == 2)
        #expect(store.addPackages(for: beef) == 0)
        #expect(ShoppingText.cartStatus(sent: 3, wanted: 1) == "In Walmart cart · 3 · Remove 2 in the Walmart app")
        // A link can only add, so there's still nothing to open.
        #expect(store.isEverythingInCart)
        try await store.openInWalmart()
        #expect(harness.recorder.opened.count == 1)
    }

    @Test func sendAgainAndStartOverPutLinesBackInTheCart() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        let cilantro = try #require(store.proposal?.linesInCart.last)

        try await store.sendAgain(cilantro)

        #expect(
            harness.server.bodies("POST /households/household-1/plans/2026-W38/shopping/walmart/handoffs/send-again")
                .last?["ingredientKey"] as? String == "i-cilantro")
        #expect(store.proposal?.linesToSend.map(\.ingredientKey) == ["i-cilantro"])
        #expect(!store.isResending)
        try await store.openInWalmart()
        #expect(harness.recorder.opened.last?.absoluteString == Self.cartURL + "100000002&storeId=5435")

        try await store.startOverAndSendEverything()

        #expect(
            harness.server.log.contains(
                "POST /households/household-1/plans/2026-W38/shopping/walmart/handoffs/start-over"))
        #expect(harness.server.handoffs.map(\.active) == [false, true])
        #expect(harness.server.handoffs.first?.lines.map(\.status) == ["skipped", "skipped"])
        #expect(harness.recorder.opened.last?.absoluteString == Self.cartURL + "100000001_3,100000002&storeId=5435")
        #expect(store.linkProgress?.handoff.id == "handoff-2")
        #expect(store.proposal?.cart?.handoffID == "handoff-2")
    }

    /// The sent state lives on the API, so a second member's phone sees the first one's send.
    @Test func anotherMembersPhoneSeesWhatWasSent() async throws {
        let first = try await makeHarness()
        let second = try await makeHarness(sharing: first.server)
        await first.store.load()
        await second.store.load()
        try await first.store.openInWalmart()

        await second.store.reload()

        #expect(second.store.proposal?.linesInCart.map(\.ingredientKey) == ["i-beef", "i-cilantro"])
        #expect(second.store.isEverythingInCart)
        try await second.store.openInWalmart()
        #expect(second.recorder.opened.isEmpty)
        #expect(second.store.linkNotice == ShoppingText.everythingInCart)
    }

    @Test func markingTheWeekOrderedStartsTheNextSendFresh() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()
        try await store.openInWalmart()
        #expect(store.isEverythingInCart)

        try await store.setWeekOrdered(true)

        #expect(store.proposal?.cart == nil)
        #expect(!store.isEverythingInCart)
        #expect(store.linkProgress == nil)

        // Taking the mark back brings the cart state back, so a mis-tap can't double the cart.
        try await store.setWeekOrdered(false)
        #expect(store.isEverythingInCart)

        try await store.setWeekOrdered(true)
        try await store.openInWalmart()
        #expect(harness.server.handoffs.count == 2)
        #expect(harness.recorder.opened.last?.absoluteString == Self.cartURL + "100000001_3,100000002&storeId=5435")
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

    /// Check Amount for a missing size opens the product on its package size; saving one
    /// matches again and the warning is gone. Fresh food without a size never warns.
    @Test func addingAMissingPackageSizeFromTheWarningClearsIt() async throws {
        var state = FakeShoppingServer.State()
        state.grocery += [
            .init(key: "i-paprika", name: "Paprika", category: "spices"),
            .init(key: "i-garlic", name: "Garlic", category: "produce"),
        ]
        state.products["i-paprika"] = .init(
            productID: "100000010", displayName: "Test paprika", ingredientName: "Paprika")
        state.products["i-garlic"] = .init(productID: "100000011", displayName: "Test garlic", ingredientName: "Garlic")
        let harness = try await makeHarness(state)
        let store = harness.store
        await store.load()

        let garlic = try #require(store.proposal?.lines.first { $0.ingredientKey == "i-garlic" })
        #expect(!garlic.checkAmount)
        #expect(garlic.coversWeek)
        #expect(garlic.packageSizeFix == nil)

        let paprika = try #require(store.proposal?.lines.first { $0.ingredientKey == "i-paprika" })
        #expect(paprika.checkAmount)
        let fix = try #require(paprika.packageSizeFix)
        #expect(fix == .add)
        #expect(fix.title == "Add Package Size")

        let choice = ProductChoice(line: paprika, packageSizeFix: fix)
        #expect(choice.packageSizeFix == .add)
        #expect(choice.draft.hasPackageSize)
        var draft = choice.draft
        draft.packageQuantityText = "2 1/2"
        draft.packageUnit = "oz"
        try await store.savePreference(
            ingredientKey: choice.ingredientKey,
            request: try #require(draft.request(ingredientName: choice.ingredientName)))

        #expect(harness.server.products["i-paprika"]?.sizeUnit == "oz")
        let fixed = try #require(store.proposal?.lines.first { $0.ingredientKey == "i-paprika" })
        #expect(!fixed.checkAmount)
        #expect(fixed.packageSizeFix == nil)
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
        store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        #expect(store.proposal != nil)
        #expect(store.linkProgress != nil)

        store.activate(householdID: "household-2", timeZone: Self.denver, weekStartsOn: .mon)

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
