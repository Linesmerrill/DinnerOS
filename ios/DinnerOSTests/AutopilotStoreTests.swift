import Foundation
import Synchronization
import Testing

@testable import DinnerOS

struct AutopilotStoreTests {
    private struct Harness {
        let store: AutopilotStore
        let server: FakeAutopilotServer
        let prompts: InMemoryAutopilotPrompts
        let plans: PlanRecorder
    }

    /// Plans handed to `planDidChange`.
    @MainActor
    private final class PlanRecorder {
        var plans: [Plan] = []
    }

    private let week = ISOWeek("2026-W38") ?? .current(in: .gmt, weekStartsOn: .mon)

    private func makeHarness(server: FakeAutopilotServer = FakeAutopilotServer()) async throws -> Harness {
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let prompts = InMemoryAutopilotPrompts()
        let store = AutopilotStore(session: session, api: AutopilotAPI(client: client), prompts: prompts)
        let recorder = PlanRecorder()
        store.planDidChange = { recorder.plans.append($0) }
        return Harness(store: store, server: server, prompts: prompts, plans: recorder)
    }

    private func activated(_ server: FakeAutopilotServer = FakeAutopilotServer()) async throws -> Harness {
        let harness = try await makeHarness(server: server)
        await harness.store.activate(householdID: "household-1")
        await harness.store.showWeek(week, householdID: "household-1")
        return harness
    }

    private func generated(_ server: FakeAutopilotServer = FakeAutopilotServer()) async throws -> Harness {
        let harness = try await activated(server)
        try await harness.store.generate(week: week)
        return harness
    }

    // MARK: Profile and onboarding

    @Test func activateLoadsProfileAndVocabularyAndOffersOnboardingOnce() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        #expect(!store.shouldOfferOnboarding)

        await store.activate(householdID: "household-1")

        #expect(store.phase == .loaded)
        #expect(store.profile?.configured == false)
        #expect(store.vocabulary?.catalogRecipeCount == 429)
        #expect(store.limits.maxListValues == 30)
        #expect(
            Set(harness.server.log) == [
                "GET /households/household-1/autopilot/profile", "GET /households/household-1/autopilot/vocabulary",
            ])
        #expect(store.shouldOfferOnboarding)

        store.markOnboardingOffered()
        #expect(!store.shouldOfferOnboarding)
        #expect(harness.prompts.hasPromptedOnboarding(userID: Fixtures.user.id, householdID: "household-1"))

        // Activating again doesn't refetch.
        await store.activate(householdID: "household-1")
        #expect(harness.server.log.count == 2)
    }

    @Test func configuredHouseholdsAreNotOfferedOnboarding() async throws {
        let server = FakeAutopilotServer(.init(profile: AutopilotFixtures.profile(configured: true)))
        let harness = try await makeHarness(server: server)

        await harness.store.activate(householdID: "household-1")

        #expect(harness.store.isConfigured)
        #expect(!harness.store.shouldOfferOnboarding)
    }

    @Test func finishingOnboardingPutsEverySectionOnce() async throws {
        let harness = try await activated()
        let store = harness.store
        let limits = store.limits
        var settings = try #require(store.profile?.settings)
        settings.setPreference(.liked, for: "mexican", kind: .cuisine, limits: limits)
        settings.setPreference(.disliked, for: "lamb", kind: .protein, limits: limits)
        settings.restrictions.allergens = ["peanuts"]
        AutopilotInput.add("Cilantro ", to: &settings.restrictions.excludedIngredients, maxCount: 50, maxLength: 60)
        settings.setPlanDay(.sun, included: true)
        settings.schedule.mealsPerWeek = 5
        settings.schedule.weeknightMaxMinutes = 30
        settings.cookTime.maxLongPerWeek = 1
        settings.cookTime.minQuickPerWeek = 2
        settings.setEquipment("smoker", owned: true, order: ["smoker", "grill"])
        settings.setRule(.smokerNight(on: .sun), for: .sun)
        settings.novelty = .adventurous
        #expect(settings.validationMessage == nil)

        try await store.saveProfile(settings)

        let line = "PUT /households/household-1/autopilot/profile"
        #expect(harness.server.log.filter { $0.hasPrefix("PUT") || $0.hasPrefix("PATCH") } == [line])
        let body = try #require(harness.server.body(of: line))
        #expect(
            Set(body.keys) == ["taste", "restrictions", "schedule", "cookTime", "novelty", "equipment", "weekdayRules"])
        let likes = try #require((body["taste"] as? [String: Any])?["likes"] as? [String: Any])
        #expect(likes["cuisines"] as? [String] == ["mexican"])
        let restrictions = try #require(body["restrictions"] as? [String: Any])
        #expect(restrictions["excludedIngredients"] as? [String] == ["cilantro"])
        let schedule = try #require(body["schedule"] as? [String: Any])
        #expect(schedule["planDays"] as? [String] == ["mon", "tue", "wed", "thu", "fri", "sun"])
        #expect(schedule["mealsPerWeek"] as? Int == 5)
        #expect(body["equipment"] as? [String] == ["smoker"])
        let rule = try #require((body["weekdayRules"] as? [[String: Any]])?.first)
        #expect(rule["day"] as? String == "sun")
        #expect(rule["proteins"] as? [String] == ["chicken", "pork"])
        #expect(rule["timeBand"] as? String == "long")
        #expect(body["novelty"] as? String == "adventurous")

        #expect(store.isConfigured)
        #expect(store.profile?.settings.rule(for: .sun)?.methods == ["smoker"])
        #expect(!store.shouldOfferOnboarding)
    }

    /// Setup asks three questions, but the `PUT` still sends every replaceable section: the
    /// three it asked about, and the API's defaults for the ones that moved to preferences,
    /// which is also what a skipped step leaves behind (#330).
    @Test func finishingThreeStepSetupSendsDefaultsForWhatItNoLongerAsks() async throws {
        let harness = try await activated()
        let store = harness.store
        var settings = try #require(store.profile?.settings)
        settings.setPreference(.liked, for: "mexican", kind: .cuisine, limits: store.limits)
        settings.restrictions.allergens = ["peanuts"]
        settings.schedule.mealsPerWeek = 3

        try await store.saveProfile(settings)

        let line = "PUT /households/household-1/autopilot/profile"
        let body = try #require(harness.server.body(of: line))
        #expect(
            Set(body.keys) == ["taste", "restrictions", "schedule", "cookTime", "novelty", "equipment", "weekdayRules"])
        let likes = try #require((body["taste"] as? [String: Any])?["likes"] as? [String: Any])
        #expect(likes["cuisines"] as? [String] == ["mexican"])
        #expect((body["restrictions"] as? [String: Any])?["allergens"] as? [String] == ["peanuts"])
        #expect((body["schedule"] as? [String: Any])?["mealsPerWeek"] as? Int == 3)
        // Moved to preferences: the defaults go up untouched.
        #expect(body["equipment"] as? [String] == [])
        #expect((body["weekdayRules"] as? [[String: Any]])?.isEmpty == true)
        #expect(body["novelty"] as? String == "balanced")
        #expect((body["cookTime"] as? [String: Any])?["maxLongPerWeek"] as? Int == 2)
        #expect(store.isConfigured)
    }

    @Test func savingASectionPatchesOnlyThatSection() async throws {
        let server = FakeAutopilotServer(.init(profile: AutopilotFixtures.profile(configured: true)))
        let harness = try await activated(server)
        let store = harness.store
        var settings = try #require(store.profile?.settings)
        settings.cookTime.maxLongPerWeek = 3

        try await store.saveSections([.cookTime], from: settings)

        let line = "PATCH /households/household-1/autopilot/profile"
        let body = try #require(harness.server.body(of: line))
        #expect(Set(body.keys) == ["cookTime"])
        #expect((body["cookTime"] as? [String: Any])?["maxLongPerWeek"] as? Int == 3)
        #expect(store.profile?.cookTime.maxLongPerWeek == 3)
        #expect(store.profile?.sections[.cookTime]?.updatedBy == Fixtures.user.id)
    }

    @Test func removingEquipmentAlsoSavesTheRulesThatUsedIt() async throws {
        let server = FakeAutopilotServer(.init(profile: AutopilotFixtures.profile(configured: true)))
        let harness = try await activated(server)
        let store = harness.store
        var settings = try #require(store.profile?.settings)
        settings.setEquipment("smoker", owned: false, order: ["smoker", "grill"])

        try await store.saveSections([.equipment], from: settings)

        let body = try #require(harness.server.body(of: "PATCH /households/household-1/autopilot/profile"))
        #expect(Set(body.keys) == ["equipment", "weekdayRules"])
        #expect(body["equipment"] as? [String] == [])
        let sunday = try #require((body["weekdayRules"] as? [[String: Any]])?.last)
        // Chicken or pork on a long-cook day still says something, so the rule stays.
        #expect(sunday["methods"] as? [String] == [])
        #expect(sunday["proteins"] as? [String] == ["chicken", "pork"])
    }

    // MARK: Proposals

    @Test func generateShowsTheProposalApartFromThePlan() async throws {
        let harness = try await generated()
        let store = harness.store

        let proposal = try #require(store.pendingProposal)
        #expect(proposal.slots.map(\.id) == ["mon", "wed", "sun"])
        #expect(store.includedSlots.count == 3)
        #expect(harness.plans.plans.isEmpty)
        #expect(harness.server.log.contains("POST /households/household-1/autopilot/weeks/2026-W38/generate"))
        #expect(!store.isGenerating)
        #expect(!store.isSaving)
    }

    @Test func swapUpdatesTheSlotAndSendsTheCurrentVersion() async throws {
        let server = FakeAutopilotServer(
            .init(alternatives: [
                AutopilotFixtures.Slot(day: "", recipeID: "recipe-4", name: "Swapped Stir Fry", cookMinutes: 15),
                AutopilotFixtures.Slot(day: "", recipeID: "recipe-5", name: "Second Swap Salad", cookMinutes: 10),
            ]))
        let harness = try await generated(server)
        let store = harness.store

        try await store.swap(slotID: "sun")

        let line = "POST /households/household-1/autopilot/weeks/2026-W38/proposal/slots/sun/swap"
        #expect(harness.server.body(of: line)?["version"] as? Int == 1)
        let sunday = try #require(store.pendingProposal?.slot(id: "sun"))
        #expect(sunday.recipe.name == "Swapped Stir Fry")
        #expect(sunday.swapCount == 1)
        #expect(store.pendingProposal?.version == 2)
        #expect(store.swappingSlotIDs.isEmpty)

        // The next change sends the new version.
        try await store.swap(slotID: "mon")
        #expect(harness.server.body(of: line.replacingOccurrences(of: "sun", with: "mon"))?["version"] as? Int == 2)
    }

    @Test func aStaleVersionReloadsTheProposalAndExplains() async throws {
        let harness = try await generated()
        let store = harness.store
        // Another member swaps first.
        harness.server.update {
            $0.proposal?.version = 5
            $0.proposal?.slots[0].name = "Someone Else's Pick"
        }

        try await store.swap(slotID: "sun")

        #expect(store.pendingProposal?.version == 5)
        #expect(store.pendingProposal?.slot(id: "mon")?.recipe.name == "Someone Else's Pick")
        #expect(store.pendingProposal?.slot(id: "sun")?.recipe.name == "Placeholder Pork Shoulder")
        #expect(store.notice?.contains("changed these suggestions") == true)
        #expect(harness.server.log.last == "GET /households/household-1/autopilot/weeks/2026-W38/proposal")
    }

    @Test func noAlternativeKeepsTheMealAndThrowsTheReason() async throws {
        let server = FakeAutopilotServer(.init(alternatives: []))
        let harness = try await generated(server)
        let store = harness.store

        do {
            try await store.swap(slotID: "sun")
            Issue.record("expected no_alternative")
        } catch {
            #expect(AutopilotConflict(error) == .noAlternative)
            #expect(HouseholdStore.message(for: error) == "No other recipe fits Sunday.")
        }
        #expect(store.pendingProposal?.slot(id: "sun")?.recipe.name == "Placeholder Pork Shoulder")
        #expect(store.pendingProposal?.version == 1)
        #expect(store.notice == nil)
    }

    @Test func acceptSendsExcludedSlotsAndHandsOverThePlan() async throws {
        let server = FakeAutopilotServer(.init(plannedDays: ["mon"]))
        let harness = try await generated(server)
        let store = harness.store

        store.setSlot("wed", included: false)
        store.setSlot("nope", included: false)
        #expect(store.excludedSlotIDs == ["wed"])
        #expect(store.includedSlots.map(\.id) == ["mon", "sun"])

        let result = try #require(try await store.accept())

        let body = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/proposal/accept"))
        #expect(body["version"] as? Int == 1)
        #expect(body["excludeSlotIds"] as? [String] == ["wed"])
        #expect(result.added.map(\.recipe.id) == ["recipe-3"])
        let allFromAutopilot = result.added.allSatisfy { $0.isFromAutopilot }
        #expect(allFromAutopilot)
        #expect(result.skipped.map(\.slotID) == ["mon"])
        #expect(harness.plans.plans.map(\.week) == ["2026-W38"])
        #expect(harness.plans.plans.first?.entries.first?.isFromAutopilot == true)
        #expect(store.proposal?.status == .accepted)
        #expect(store.pendingProposal == nil)
    }

    /// A slot's pairings start checked when the proposal includes them (the household's
    /// `always` rules), and the member can switch each one.
    @Test func slotPairingsStartFromTheProposalsIncludedOnes() async throws {
        let harness = try await generated()
        let store = harness.store
        harness.server.update {
            $0.proposal?.slots[0].pairings = [
                .init(key: "recipe:addon-1", name: "Sample Garlic Bread", included: true)
            ]
            $0.proposal?.slots[1].pairings = [
                .init(key: "grocery:club crackers", name: "Club Crackers", included: false, isRecipe: false)
            ]
        }
        await store.reloadWeek()

        #expect(store.chosenPairingIDs == ["mon/recipe:addon-1"])
        #expect(store.isPairingChosen("mon/recipe:addon-1"))
        #expect(!store.isPairingChosen("wed/grocery:club crackers"))

        store.setPairing("wed/grocery:club crackers", included: true)
        store.setPairing("mon/recipe:addon-1", included: false)
        // An id that isn't a pairing of this proposal is ignored.
        store.setPairing("nope/recipe:addon-9", included: true)

        #expect(store.chosenPairingIDs == ["wed/grocery:club crackers"])
        #expect(store.chosenPairings == ["wed/grocery:club crackers"])
    }

    /// Accepting sends exactly the checked pairings, never leaving the field out, and drops
    /// the pairings of slots the member excluded (#315).
    @Test func acceptSendsExactlyTheCheckedPairingsAndDropsExcludedSlots() async throws {
        let harness = try await generated()
        let store = harness.store
        harness.server.update {
            $0.proposal?.slots[0].pairings = [
                .init(key: "recipe:addon-1", name: "Sample Garlic Bread", included: true)
            ]
            $0.proposal?.slots[1].pairings = [
                .init(key: "grocery:club crackers", name: "Club Crackers", included: true, isRecipe: false)
            ]
        }
        await store.reloadWeek()
        #expect(store.chosenPairingIDs.count == 2)

        // Wednesday's meal goes, so its add-on goes with it.
        store.setSlot("wed", included: false)
        let result = try #require(try await store.accept())

        #expect(harness.server.acceptedPairingIDs == ["mon/recipe:addon-1"])
        let body = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/proposal/accept"))
        #expect(body["pairingIds"] as? [String] == ["mon/recipe:addon-1"])
        #expect(result.pairingsAdded.map(\.name) == ["Sample Garlic Bread"])
    }

    /// With every pairing switched off, an empty array is sent — which adds none, unlike
    /// leaving the field out.
    @Test func unpickingEveryPairingSendsAnEmptyArray() async throws {
        let harness = try await generated()
        let store = harness.store
        harness.server.update {
            $0.proposal?.slots[0].pairings = [
                .init(key: "recipe:addon-1", name: "Sample Garlic Bread", included: true)
            ]
        }
        await store.reloadWeek()
        store.setPairing("mon/recipe:addon-1", included: false)

        _ = try await store.accept()

        #expect(harness.server.acceptedPairingIDs == [])
        #expect(harness.store.chosenPairings.isEmpty)
    }

    @Test func acceptingWhenSomeoneElseAlreadyDidReloadsInsteadOfFailing() async throws {
        let harness = try await generated()
        let store = harness.store
        harness.server.update { $0.proposal?.status = "accepted" }

        let result = try await store.accept()

        #expect(result == nil)
        #expect(store.proposal?.status == .accepted)
        #expect(store.pendingProposal == nil)
        #expect(store.notice?.contains("already accepted") == true)
        #expect(harness.plans.plans.isEmpty)
    }

    @Test func dismissRejectsWithTheVersion() async throws {
        let harness = try await generated()
        let store = harness.store

        try await store.dismissProposal()

        #expect(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/proposal/reject")?["version"]
                as? Int == 1)
        #expect(store.proposal?.status == .rejected)
        #expect(store.pendingProposal == nil)
    }

    @Test func regeneratingReplacesTheProposalAndForgetsExclusions() async throws {
        let harness = try await generated()
        let store = harness.store
        store.setSlot("wed", included: false)

        try await store.generate(week: week)

        #expect(store.pendingProposal?.id == "proposal-2")
        #expect(store.excludedSlotIDs.isEmpty)
    }

    @Test func aFinalizedWeekCantBeGenerated() async throws {
        let server = FakeAutopilotServer(.init(planStatus: "finalized"))
        let harness = try await activated(server)

        do {
            try await harness.store.generate(week: week)
            Issue.record("expected plan_finalized")
        } catch {
            #expect(AutopilotConflict(error) == .planFinalized)
        }
        #expect(harness.store.proposal == nil)
        #expect(!harness.store.isGenerating)
    }

    /// A failed reload left no trace at all: the profile stayed on screen and pull to refresh
    /// looked exactly like one that worked. Every other store remembers it in `refreshError`.
    @Test func aFailedProfileReloadIsReportedAndKeepsTheProfile() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        #expect(store.phase == .loaded)

        harness.server.failProfile()
        await store.reloadProfile()

        // The profile that was already loaded stays readable.
        #expect(store.phase == .loaded)
        #expect(store.profile != nil)
        #expect(store.vocabulary != nil)
        let message = try #require(store.refreshError)
        #expect(!message.isEmpty)

        harness.server.failProfile(false)
        await store.reloadProfile()

        #expect(store.refreshError == nil)
        #expect(store.phase == .loaded)
    }

    /// The first load has nothing to fall back on, so it fails the screen rather than showing
    /// an empty one with a quiet banner.
    @Test func aFailedFirstProfileLoadFailsThePhase() async throws {
        let server = FakeAutopilotServer()
        server.failProfile()
        let harness = try await makeHarness(server: server)

        await harness.store.activate(householdID: "household-1")

        guard case .failed(let message) = harness.store.phase else {
            Issue.record("Expected the profile load to fail")
            return
        }
        #expect(!message.isEmpty)
        #expect(harness.store.profile == nil)
        #expect(harness.store.refreshError == nil)
    }

    /// `weekError` was set but nothing ever read it, so a failed proposal load was silent and
    /// the week's rows read as "nothing suggested" instead.
    @Test func aFailedProposalLoadIsRememberedAndClearedByARetry() async throws {
        let server = FakeAutopilotServer()
        let harness = try await makeHarness(server: server)
        await harness.store.activate(householdID: "household-1")
        server.failProposal()

        await harness.store.showWeek(week, householdID: "household-1")

        let message = try #require(harness.store.weekError)
        #expect(!message.isEmpty)
        #expect(harness.store.proposal == nil)
        #expect(!harness.store.isLoadingWeek)

        server.failProposal(false)
        await harness.store.reloadWeek()

        // The week has no proposal, which is not the same as having failed to ask.
        #expect(harness.store.weekError == nil)
        #expect(harness.store.proposal == nil)
    }

    @Test func aWeekWithoutAProposalShowsNone() async throws {
        let harness = try await activated()

        #expect(harness.store.proposal == nil)
        #expect(harness.store.weekError == nil)
        #expect(harness.store.context?.configured == false)
        #expect(!harness.store.isLoadingWeek)
    }

    // MARK: Week context

    @Test func savingAndClearingTheWeekContext() async throws {
        let harness = try await activated()
        let store = harness.store
        var draft = try #require(store.context?.draft)
        draft.busy = true
        draft[.fri] = AutopilotDayOverride(day: .fri, servings: 6)

        try await store.saveContext(draft, week: week)

        #expect(store.context?.configured == true)
        #expect(store.context?.busy == true)
        #expect(store.context?.draft[.fri].servings == 6)

        try await store.clearContext(week: week)

        #expect(harness.server.log.contains("DELETE /households/household-1/autopilot/weeks/2026-W38/context"))
        #expect(store.context?.configured == false)
    }

    // MARK: Reset

    @Test func switchingHouseholdsClearsTheOldHouseholdsState() async throws {
        let server = FakeAutopilotServer(.init(profile: AutopilotFixtures.profile(configured: true)))
        let harness = try await generated(server)
        let store = harness.store
        store.setSlot("wed", included: false)
        #expect(store.pendingProposal != nil)

        server.update {
            $0.householdID = "household-2"
            $0.profile = AutopilotFixtures.profile(householdID: "household-2", configured: false)
            $0.proposal = nil
        }
        await store.showWeek(week, householdID: "household-2")

        #expect(store.householdID == "household-2")
        #expect(store.proposal == nil)
        #expect(store.excludedSlotIDs.isEmpty)
        #expect(store.profile == nil)

        await store.activate(householdID: "household-2")
        #expect(store.profile?.householdID == "household-2")
        #expect(store.profile?.configured == false)
    }

    @Test func resetForgetsEverything() async throws {
        let harness = try await generated()
        let store = harness.store

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.phase == .idle)
        #expect(store.profile == nil)
        #expect(store.vocabulary == nil)
        #expect(store.week == nil)
        #expect(store.proposal == nil)
        #expect(store.context == nil)
        #expect(!store.shouldOfferOnboarding)
    }
}
