import Foundation
import Testing

@testable import DinnerOS

// MARK: - The timeline: debounce, coalescing, and which answer counts

struct AutosaveTimelineTests {
    private let start = Date(timeIntervalSince1970: 1_790_000_000)

    private func at(_ seconds: TimeInterval) -> Date { start.addingTimeInterval(seconds) }

    @Test func aBurstOfEditsBecomesOneSaveAfterTheLastEditsDelay() {
        var timeline = AutosaveTimeline()
        timeline.edit(at: at(0), delay: .milliseconds(800))
        timeline.edit(at: at(0.3), delay: .milliseconds(800))
        timeline.edit(at: at(0.6), delay: .milliseconds(800))

        // The first edit's delay has passed, but later edits pushed the save back.
        let sent1 = timeline.begin(at: at(0.9))
        #expect(sent1 == nil)
        #expect(timeline.wait(at: at(0.9)) == .milliseconds(500))
        // One save, carrying the newest revision.
        let sent2 = timeline.begin(at: at(1.4))
        #expect(sent2 == 3)
        let sent3 = timeline.begin(at: at(5))
        #expect(sent3 == nil)
    }

    @Test func flushMakesAPendingSaveDueNow() {
        var timeline = AutosaveTimeline()
        timeline.edit(at: at(0), delay: .milliseconds(1500))

        timeline.flush(at: at(0.2))

        #expect(timeline.wait(at: at(0.2)) == .zero)
        let sent4 = timeline.begin(at: at(0.2))
        #expect(sent4 == 1)
    }

    @Test func flushWithNothingUnsavedSchedulesNothing() {
        var timeline = AutosaveTimeline()
        timeline.flush(at: at(0))
        #expect(!timeline.isPending)
        let sent5 = timeline.begin(at: at(1))
        #expect(sent5 == nil)
    }

    @Test func aSaveDueWhileAnotherIsOutWaitsForItThenCarriesTheNewerEdits() {
        var timeline = AutosaveTimeline()
        timeline.edit(at: at(0), delay: .milliseconds(800))
        let sent6 = timeline.begin(at: at(0.8))
        #expect(sent6 == 1)

        timeline.edit(at: at(1), delay: .milliseconds(800))
        // Due, but one save at a time.
        let sent7 = timeline.begin(at: at(2))
        #expect(sent7 == nil)
        #expect(timeline.isPending)

        let accepted8 = timeline.finish(1, succeeded: true)
        #expect(accepted8)
        // Revision 1 is saved; revision 2 isn't, whatever the answer to 1 said.
        #expect(timeline.hasUnsavedEdits)
        let sent9 = timeline.begin(at: at(2))
        #expect(sent9 == 2)
        let accepted10 = timeline.finish(2, succeeded: true)
        #expect(accepted10)
        #expect(!timeline.hasUnsavedEdits)
    }

    @Test func anAnswerToASaveThatIsNotOutIsIgnored() {
        var timeline = AutosaveTimeline()
        timeline.edit(at: at(0), delay: .zero)
        let sent11 = timeline.begin(at: at(0))
        #expect(sent11 == 1)

        let acceptedStray = timeline.finish(7, succeeded: true)
        #expect(!acceptedStray)
        #expect(timeline.isSaving)
        #expect(timeline.savedRevision == 0)
    }

    @Test func aFailedSaveStaysUnsavedAndFlushRetriesIt() {
        var timeline = AutosaveTimeline()
        timeline.edit(at: at(0), delay: .zero)
        let sent = timeline.begin(at: at(0))

        let accepted12 = timeline.finish(1, succeeded: false)
        #expect(accepted12)
        #expect(sent == 1)
        #expect(timeline.hasUnsavedEdits)
        #expect(timeline.failedRevision == 1)
        // Nothing retries on its own…
        let sent13 = timeline.begin(at: at(60))
        #expect(sent13 == nil)
        // …until the member asks, or edits again.
        timeline.flush(at: at(60))
        let sent14 = timeline.begin(at: at(60))
        #expect(sent14 == 1)
        let accepted15 = timeline.finish(1, succeeded: true)
        #expect(accepted15)
        #expect(timeline.failedRevision == nil)
    }
}

// MARK: - The draft: what goes out, and what a server answer may change

struct HouseholdSettingsDraftTests {
    @Test func sendsOnlyTheFieldsThatDiffer() {
        let base = SettingsFixtures.household()
        var draft = HouseholdSettingsDraft(base)
        #expect(draft.changes(against: base).isEmpty)

        draft.defaultServings = 3
        draft.orderDay = ""
        draft.name = "  The Lovelace Kitchen  "

        #expect(draft.changes(against: base) == HouseholdChanges(defaultServings: 3, orderDay: ""))
    }

    @Test func anInvalidFieldIsLeftOutWithoutHoldingBackTheOthers() {
        let base = SettingsFixtures.household()
        var draft = HouseholdSettingsDraft(base)
        draft.name = "   "
        draft.mealKitAmount = "abc"
        draft.thawReminderHour = 7

        #expect(draft.nameError != nil)
        #expect(draft.mealKitError != nil)
        #expect(draft.changes(against: base) == HouseholdChanges(thawReminderHour: 7))
    }

    @Test func aServerAnswerFillsInSomeoneElsesChangeButNotTheMembersOwn() {
        let old = SettingsFixtures.household()
        var draft = HouseholdSettingsDraft(old)
        draft.name = "Our Kitchen"

        let new = SettingsFixtures.household(name: "Their Kitchen", servings: 6, updated: 10)
        draft.rebase(from: old, to: new, keeping: nil)

        #expect(draft.name == "Our Kitchen")
        #expect(draft.defaultServings == 6)
    }

    @Test func aFieldStillOnItsWayIsNotPutBackByAnOlderAnswer() {
        // Servings 2 → 3 went out; before it answered, the member stepped back to 2.
        let old = SettingsFixtures.household(servings: 2)
        var draft = HouseholdSettingsDraft(old)
        draft.defaultServings = 2

        let answer = SettingsFixtures.household(servings: 3, updated: 10)
        draft.rebase(from: old, to: answer, keeping: HouseholdChanges(defaultServings: 3))

        #expect(draft.defaultServings == 2)
        // Without the in-flight exclusion 2 would look untouched and 3 would come back.
        var unguarded = HouseholdSettingsDraft(old)
        unguarded.rebase(from: old, to: answer, keeping: nil)
        #expect(unguarded.defaultServings == 3)
    }

    @Test func mealKitComesInOnlyWhenTheMemberHasntTypedOne() {
        let old = SettingsFixtures.household()
        let new = SettingsFixtures.household(mealKit: MealKitBaseline(weeklyCents: 13_000, meals: 5), updated: 10)

        var untouched = HouseholdSettingsDraft(old)
        untouched.rebase(from: old, to: new, keeping: nil)
        #expect(untouched.mealKitAmount == "130.00")

        var typing = HouseholdSettingsDraft(old)
        typing.mealKitAmount = "12"
        typing.rebase(from: old, to: new, keeping: nil)
        #expect(typing.mealKitAmount == "12")
    }
}

// MARK: - The autosave: timers, one request at a time, failures, permissions

@MainActor
struct HouseholdSettingsAutosaveTests {
    private let start = Date(timeIntervalSince1970: 1_790_000_000)

    @Test func threeStepperTapsSendOnePatchWithTheLastValue() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave

        autosave.edit(\.defaultServings, to: 3)
        harness.clock.advance(by: 0.3)
        autosave.edit(\.defaultServings, to: 4)
        harness.clock.advance(by: 0.3)
        autosave.edit(\.defaultServings, to: 5)

        // The first tap's delay is over, not the last one's: nothing goes out.
        harness.clock.advance(by: 0.5)
        harness.sleeper.wakeAll()
        try await harness.sleeper.waitForSleepers(1)
        #expect(harness.server.patches.isEmpty)
        #expect(autosave.status == .pending)

        harness.clock.advance(by: 0.5)
        harness.sleeper.wakeAll()
        await autosave.work?.value

        #expect(harness.server.patches == [HouseholdChanges(defaultServings: 5)])
        #expect(autosave.status == .saved)
        #expect(autosave.saveCount == 1)
        #expect(autosave.confirmed.defaultServings == 5)
    }

    @Test func changingAValueAndBackSendsNothing() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave

        autosave.edit(\.thawReminderHour, to: 9)
        autosave.edit(\.thawReminderHour, to: Household.defaultThawReminderHour)
        autosave.flush()
        await autosave.work?.value

        #expect(harness.server.patches.isEmpty)
        #expect(!autosave.timeline.hasUnsavedEdits)
    }

    @Test func typingWaitsLongerAndLeavingTheFieldSendsAtOnce() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave

        autosave.edit(\.name, to: "Lovelace", delay: HouseholdSettingsAutosave.typingDelay)
        harness.clock.advance(by: 1)
        harness.sleeper.wakeAll()
        try await harness.sleeper.waitForSleepers(1)
        #expect(harness.server.patches.isEmpty)

        autosave.flush()
        await autosave.work?.value

        #expect(harness.server.patches == [HouseholdChanges(name: "Lovelace")])
    }

    @Test func anEditDuringASaveGoesOutAfterItAndTheOldAnswerDoesntUndoIt() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave
        harness.server.holdNextSave()

        autosave.edit(\.defaultServings, to: 3, delay: .zero)
        let first = try #require(autosave.work)
        try await harness.server.waitForHeldSave()
        #expect(autosave.status == .saving)

        // The member steps back to 2 while 3 is on its way, and the reload that follows the
        // save lands first, carrying 3.
        autosave.edit(\.defaultServings, to: 2, delay: .zero)
        autosave.serverDidChange(SettingsFixtures.household(servings: 3, updated: 1), canEdit: true)
        #expect(autosave.draft.defaultServings == 2)

        harness.server.releaseHeldSave()
        await first.value
        await autosave.work?.value

        #expect(autosave.draft.defaultServings == 2)
        #expect(
            harness.server.patches == [HouseholdChanges(defaultServings: 3), HouseholdChanges(defaultServings: 2)])
        #expect(autosave.confirmed.defaultServings == 2)
        #expect(autosave.status == .saved)
    }

    @Test func aReloadOlderThanWhatsAppliedIsIgnored() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave
        let before = harness.server.household

        autosave.edit(\.orderDay, to: "mon", delay: .zero)
        await autosave.work?.value
        #expect(autosave.confirmed.orderDay == "mon")

        // A reload that left before the save answered, arriving after it.
        autosave.serverDidChange(before, canEdit: true)

        #expect(autosave.confirmed.orderDay == "mon")
        #expect(autosave.draft.orderDay == "mon")
    }

    @Test func aFailedSaveSaysSoKeepsTheValueAndRetries() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave
        harness.server.failure = APIError.transport(.notConnectedToInternet)

        autosave.edit(\.defaultServings, to: 3, delay: .zero)
        await autosave.work?.value

        guard case .failed(let message) = autosave.status else {
            Issue.record("expected failed, got \(autosave.status)")
            return
        }
        #expect(message.contains("default servings"))
        #expect(message.localizedCaseInsensitiveContains("offline"))
        // Still what the member picked, not the server's value.
        #expect(autosave.draft.defaultServings == 3)
        #expect(autosave.confirmed.defaultServings == 2)

        harness.server.failure = nil
        autosave.flush()
        await autosave.work?.value

        #expect(autosave.status == .saved)
        #expect(autosave.confirmed.defaultServings == 3)
        #expect(harness.server.patches.count == 2)
    }

    @Test func withoutPermissionNothingIsEditableOrSent() async throws {
        let harness = AutosaveHarness(start: start, canEdit: false)
        let autosave = harness.autosave

        autosave.edit(\.name, to: "Mine Now", delay: .zero)
        autosave.flush()
        await autosave.work?.value

        #expect(!autosave.canEdit)
        #expect(autosave.draft.name == SettingsFixtures.household().name)
        #expect(harness.server.patches.isEmpty)
    }

    @Test func losingPermissionDropsAPendingSave() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave

        autosave.edit(\.defaultServings, to: 4)
        autosave.serverDidChange(harness.server.household, canEdit: false)
        harness.clock.advance(by: 5)
        harness.sleeper.wakeAll()
        await autosave.work?.value

        #expect(harness.server.patches.isEmpty)
        #expect(!autosave.timeline.isPending)
    }

    @Test func aConfirmedWeekStartGoesOutAtOnceAsOneChange() async throws {
        let harness = AutosaveHarness(start: start)
        let autosave = harness.autosave

        autosave.edit(\.weekStartsOn, to: .mon, delay: .zero)
        await autosave.work?.value

        #expect(harness.server.patches == [HouseholdChanges(weekStartsOn: .mon)])
        #expect(harness.sleeper.requested.isEmpty)
    }
}

// MARK: - Through the store

@MainActor
struct HouseholdStoreSettingsTests {
    private func makeStore(role: String, patch: (@Sendable (URLRequest) -> (Int, Data))? = nil) async throws -> (
        HouseholdStore, StubTransport
    ) {
        let server = FakeHouseholdServer(
            .init(memberships: [.init(householdID: "household-1", name: "Lovelace", role: role)]))
        let transport = StubTransport { request in
            if request.httpMethod == "PATCH", let patch { return patch(request) }
            return server.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = HouseholdStore(
            session: session, api: HouseholdsAPI(client: client), selection: InMemoryHouseholdSelection())
        await store.load()
        return (store, transport)
    }

    @Test func aMemberWithoutHouseholdUpdateGetsReadOnlySettings() async throws {
        let (store, _) = try await makeStore(role: "member")
        let settings = try #require(store.settings)
        #expect(!settings.canEdit)
        #expect(settings.confirmed.name == "Lovelace")
    }

    @Test func anAdminGetsEditableSettingsThatSurviveAReload() async throws {
        let (store, _) = try await makeStore(role: "admin")
        let settings = try #require(store.settings)
        #expect(settings.canEdit)

        await store.load()

        #expect(store.settings === settings)
    }

    @Test func oneSaveMeansOnePatchAndOneReload() async throws {
        let (store, transport) = try await makeStore(role: "admin") { _ in
            (200, Data(HouseholdFixtures.household(id: "household-1", name: "Renamed").utf8))
        }
        let settings = try #require(store.settings)
        let reloadsBefore = transport.requests(to: "/api/v1/households/household-1").filter { $0.httpMethod == "GET" }
            .count

        settings.edit(\.name, to: "Rename", delay: HouseholdSettingsAutosave.typingDelay)
        settings.edit(\.name, to: "Renamed", delay: HouseholdSettingsAutosave.typingDelay)
        settings.flush()
        await settings.work?.value

        let requests = transport.requests(to: "/api/v1/households/household-1")
        #expect(requests.filter { $0.httpMethod == "PATCH" }.map(\.jsonBody) == [["name": "Renamed"]])
        #expect(requests.filter { $0.httpMethod == "GET" }.count == reloadsBefore + 1)
    }

    @Test func aRejectedSaveSurfacesAndKeepsWhatWasTyped() async throws {
        let (store, _) = try await makeStore(role: "admin") { _ in
            (500, Fixtures.errorJSON(code: "internal", message: "boom"))
        }
        let settings = try #require(store.settings)

        settings.edit(\.name, to: "Lovelace & Co", delay: HouseholdSettingsAutosave.typingDelay)
        settings.flush()
        await settings.work?.value

        guard case .failed(let message) = settings.status else {
            Issue.record("expected failed, got \(settings.status)")
            return
        }
        #expect(message.contains("name"))
        #expect(settings.draft.name == "Lovelace & Co")
        #expect(store.current?.household.name == "Lovelace")
    }
}

// MARK: - Support

nonisolated enum SettingsFixtures {
    static func household(
        name: String = "The Lovelace Kitchen", servings: Int = 2, orderDay: String? = "thu",
        mealKit: MealKitBaseline? = nil, updated: TimeInterval = 0
    ) -> Household {
        let base = Date(timeIntervalSince1970: 1_790_000_000)
        return Household(
            id: "household-1", name: name, defaultServings: servings, timeZone: "America/Denver",
            orderDay: orderDay, createdBy: "user-ada", createdAt: base, updatedAt: base.addingTimeInterval(updated),
            mealKit: mealKit, weekStartsOn: .sun)
    }
}

/// A `sleep` that returns only when the test wakes it, whatever it was asked for.
@MainActor
final class ManualSleeper {
    private var sleepers: [CheckedContinuation<Void, Never>] = []
    private(set) var requested: [Duration] = []

    func sleep(_ duration: Duration) async {
        requested.append(duration)
        await withCheckedContinuation { sleepers.append($0) }
    }

    func wakeAll() {
        let woken = sleepers
        sleepers = []
        woken.forEach { $0.resume() }
    }

    /// Lets woken tasks run until `count` of them are asleep again.
    func waitForSleepers(_ count: Int) async throws {
        for _ in 0..<1000 where sleepers.count < count {
            await Task.yield()
        }
        #expect(sleepers.count == count)
    }
}

/// The household endpoint as the autosave sees it: applies each PATCH and bumps `updatedAt`.
@MainActor
final class FakeSettingsServer {
    var household = SettingsFixtures.household()
    private(set) var patches: [HouseholdChanges] = []
    var failure: (any Error)?
    private var holdsNext = false
    private var held: CheckedContinuation<Void, Never>?

    func holdNextSave() { holdsNext = true }

    func waitForHeldSave() async throws {
        for _ in 0..<1000 where held == nil {
            await Task.yield()
        }
        #expect(held != nil)
    }

    func releaseHeldSave() {
        held?.resume()
        held = nil
    }

    func save(_ changes: HouseholdChanges) async throws -> Household {
        patches.append(changes)
        if holdsNext {
            holdsNext = false
            await withCheckedContinuation { held = $0 }
        }
        if let failure { throw failure }
        let next = Household(
            id: household.id, name: changes.name ?? household.name,
            defaultServings: changes.defaultServings ?? household.defaultServings,
            timeZone: changes.timeZone ?? household.timeZone,
            orderDay: changes.orderDay.map { $0.isEmpty ? nil : $0 } ?? household.orderDay,
            createdBy: household.createdBy, createdAt: household.createdAt,
            updatedAt: household.updatedAt.addingTimeInterval(1), mealKit: household.mealKit,
            weekStartsOn: changes.weekStartsOn ?? household.weekStartsOn,
            thawReminderHour: changes.thawReminderHour ?? household.thawReminderHour)
        household = next
        return next
    }
}

@MainActor
struct AutosaveHarness {
    let clock: ManualClock
    let sleeper: ManualSleeper
    let server: FakeSettingsServer
    let autosave: HouseholdSettingsAutosave

    init(start: Date, canEdit: Bool = true) {
        let clock = ManualClock(start)
        let sleeper = ManualSleeper()
        let server = FakeSettingsServer()
        self.clock = clock
        self.sleeper = sleeper
        self.server = server
        autosave = HouseholdSettingsAutosave(
            household: server.household, canEdit: canEdit, now: { clock.now },
            sleep: { await sleeper.sleep($0) },
            save: { try await server.save($0) })
    }
}
