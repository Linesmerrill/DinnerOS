import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Invite links, keeping a code through sign-in and setup, the checklist card and the free
/// month confirmation (build 112).
@MainActor
struct InviteFlowTests {
    private let defaults: UserDefaults

    init() throws {
        let suite = "InviteFlowTests.\(UUID().uuidString)"
        defaults = try #require(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
    }

    private func makeStore(_ handler: @escaping StubTransport.Handler) async throws -> (PlusStore, StubTransport) {
        let transport = StubTransport(handler)
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = PlusStore(
            session: session, api: PlusAPI(client: client), purchaser: FakePurchaser(), defaults: defaults)
        store.confirmDelay = .milliseconds(1)
        store.compThanksDelay = .milliseconds(1)
        return (store, transport)
    }

    nonisolated static let preview = Data(
        #"""
        {"code":"W57ZVGUJ","householdName":"Lines","title":"Lines invited you to DinnerOS",
         "message":"You both get a free month of Plus once you're set up.",
         "steps":["Create your household","Save 3 recipes","Plan a dinner"],
         "forExisting":{"title":"This code is for new households","message":"Your household is already set up."},
         "forOwn":{"title":"That's your own code","message":"Share it with another household."}}
        """#.utf8)

    nonisolated static func progress(_ state: String, recipes: Int = 0, dinner: Bool = false) -> String {
        let done = 1 + (recipes >= 3 ? 1 : 0) + (dinner ? 1 : 0)
        return #"""
            ,"referralProgress":{"state":"\#(state)","message":"Save 3 recipes and plan a dinner to unlock your free month",
             "steps":[{"id":"household","title":"Create your household","done":true,"count":1,"target":1},
                      {"id":"recipes","title":"Save 3 recipes","done":\#(recipes >= 3),"count":\#(recipes),"target":3},
                      {"id":"dinner","title":"Plan a dinner","done":\#(dinner),"count":\#(dinner ? 1 : 0),"target":1}],
             "expiresAt":"2026-11-29T12:00:00Z","summary":"Free month: \#(done) of 3 done","timeLeft":"14 days left"}
            """#
    }

    @Test func theAppLinkAndTheWebLinkBothCarryTheCode() throws {
        #expect(ReferralLink.code(in: try #require(URL(string: "dinneros://referral/w57z-vguj"))) == "W57ZVGUJ")
        #expect(ReferralLink.code(in: try #require(URL(string: "https://dinneros.app/invite/lines-fam"))) == "LINESFAM")
        // A household invitation is not an invite code.
        #expect(ReferralLink.code(in: try #require(URL(string: "dinneros://invite?token=abc"))) == nil)
        #expect(ReferralLink.code(in: try #require(URL(string: "dinneros://referral/"))) == nil)
    }

    @Test func openingALinkShowsTheScreenAndKeepsTheCode() async throws {
        let (store, _) = try await makeStore { _ in (200, PlusFixtures.status()) }
        #expect(store.handleOpenURL(try #require(URL(string: "https://dinneros.app/invite/W57ZVGUJ"))))
        #expect(store.incomingInvite?.code == "W57ZVGUJ")
        #expect(store.savedInvite == SavedInvite(code: "W57ZVGUJ", accepted: false))
        // Not Now closes the screen; setup can still fill the code in.
        store.declineInvite()
        #expect(store.incomingInvite == nil && store.savedInvite?.code == "W57ZVGUJ")
    }

    @Test func thePreviewNeedsNoSignInAndDecodes() async throws {
        let (store, transport) = try await makeStore { _ in (200, Self.preview) }
        let preview = try await store.invitePreview("W57ZVGUJ")
        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/referral-codes/W57ZVGUJ")
        #expect(request.value(forHTTPHeaderField: "Authorization") == nil)
        #expect(preview.title == "Lines invited you to DinnerOS")
        #expect(preview.steps == ["Create your household", "Save 3 recipes", "Plan a dinner"])
        #expect(preview.forExisting.title == "This code is for new households")
    }

    @Test func aReplacedCodeShowsTheServersWords() async throws {
        let (store, _) = try await makeStore { _ in
            PlusFixtures.error(404, code: "code_replaced", message: "That code was replaced. Ask for the new one.")
        }
        await #expect {
            _ = try await store.invitePreview("OLDCODE1")
        } throws: { error in
            HouseholdStore.message(for: error) == "That code was replaced. Ask for the new one."
        }
    }

    @Test func anAcceptedCodeLastsThroughARestart() async throws {
        let (store, _) = try await makeStore { _ in (200, PlusFixtures.status()) }
        store.acceptInvite(code: "W57ZVGUJ", title: "Lines invited you to DinnerOS")
        let (again, _) = try await makeStore { _ in (200, PlusFixtures.status()) }
        #expect(again.savedInvite?.accepted == true)
        #expect(again.savedInvite?.title == "Lines invited you to DinnerOS")
    }

    @Test func aNewHouseholdUsesTheAcceptedCodeAndSeesTheChecklist() async throws {
        let redeemed = Counter()
        let (store, transport) = try await makeStore { request in
            if request.url?.path().hasSuffix("/referral/redeem") == true {
                redeemed.increment()
                return (200, PlusFixtures.status(extra: Self.progress("pending")))
            }
            if request.url?.path().hasSuffix("/referral") == true { return (200, PlusFixtures.referral) }
            return (200, PlusFixtures.status())
        }
        let accepted = Date(timeIntervalSince1970: 1_800_000_000)
        store.acceptInvite(code: "W57ZVGUJ", title: nil, now: accepted)
        await store.activate(householdID: "household-1")
        await store.useSavedInvite(householdCreatedAt: accepted.addingTimeInterval(120))
        #expect(redeemed.value == 1)
        let body = try #require(transport.requests.first { $0.url?.path().hasSuffix("/redeem") == true }?.httpBody)
        #expect(String(decoding: body, as: UTF8.self).contains("W57ZVGUJ"))
        #expect(store.checklistWaiting)
        #expect(store.savedInvite == nil)
        #expect(store.pendingReferral?.summaryText == "Free month: 1 of 3 done")
        #expect(store.pendingReferral?.timeLeft == "14 days left")
    }

    @Test func aHouseholdFromBeforeTheCodeNeverUsesIt() async throws {
        let redeemed = Counter()
        let (store, _) = try await makeStore { request in
            if request.url?.path().hasSuffix("/redeem") == true { redeemed.increment() }
            return (200, PlusFixtures.status())
        }
        let accepted = Date(timeIntervalSince1970: 1_800_000_000)
        store.acceptInvite(code: "W57ZVGUJ", title: nil, now: accepted)
        await store.activate(householdID: "household-1")
        await store.useSavedInvite(householdCreatedAt: accepted.addingTimeInterval(-86_400))
        #expect(redeemed.value == 0)
        #expect(!store.checklistWaiting && store.savedInvite == nil)
    }

    @Test func aCodeThatCantBeUsedSaysWhyInTheServersWords() async throws {
        let (store, _) = try await makeStore { request in
            if request.url?.path().hasSuffix("/redeem") == true {
                return PlusFixtures.error(
                    409, code: "code_not_allowed", message: "Codes are for households new to Plus.")
            }
            return (200, PlusFixtures.status())
        }
        let accepted = Date(timeIntervalSince1970: 1_800_000_000)
        store.acceptInvite(code: "W57ZVGUJ", title: nil, now: accepted)
        await store.activate(householdID: "household-1")
        await store.useSavedInvite(householdCreatedAt: accepted)
        #expect(store.inviteError == "Codes are for households new to Plus.")
        #expect(!store.checklistWaiting && store.savedInvite == nil)
    }

    @Test func theMenuCardShowsUntilHidden() async throws {
        let (store, _) = try await makeStore { _ in
            (200, PlusFixtures.status(extra: Self.progress("pending", recipes: 3)))
        }
        await store.activate(householdID: "household-1")
        #expect(store.showsReferralCard)
        #expect(store.pendingReferral?.summaryText == "Free month: 2 of 3 done")
        store.hideReferralCard()
        #expect(!store.showsReferralCard)
        // Give a Free Month still has it.
        #expect(store.pendingReferral != nil)
    }

    @Test func theFreeMonthConfirmationShowsOnceAfterTheChecklist() async throws {
        let state = Mutex("pending")
        let (store, _) = try await makeStore { _ in
            (200, PlusFixtures.status(extra: Self.progress(state.withLock { $0 }, recipes: 3, dinner: true)))
        }
        await store.activate(householdID: "household-1")
        #expect(store.freeMonthOn == nil && store.showsReferralCard)
        state.withLock { $0 = "qualified" }
        await store.reload()
        #expect(store.freeMonthOn?.state == "qualified")
        #expect(!store.showsReferralCard)
        store.freeMonthOn = nil
        store.freeMonthShown()
        await store.reload()
        #expect(store.freeMonthOn == nil)
    }

    @Test func aHouseholdThatFinishedBeforeThisDeviceSawItGetsNoSurprise() async throws {
        let (store, _) = try await makeStore { _ in (200, PlusFixtures.status(extra: Self.progress("qualified"))) }
        await store.activate(householdID: "household-1")
        #expect(store.freeMonthOn == nil)
    }

    // MARK: Step done (decision 729)

    nonisolated static func decoded(_ state: String, recipes: Int = 0, dinner: Bool = false) throws -> ReferralProgress
    {
        let json = "{\"x\":0" + progress(state, recipes: recipes, dinner: dinner) + "}"
        struct Wrapper: Decodable { let referralProgress: ReferralProgress }
        return try JSONCoding.makeDecoder().decode(Wrapper.self, from: Data(json.utf8)).referralProgress
    }

    @Test func theFirstLookRemembersWhatsDoneWithoutACard() throws {
        let (moment, seen) = ReferralMoments.stepJustDone(try Self.decoded("pending", recipes: 3), seen: nil)
        #expect(moment == nil)
        #expect(seen == ["household", "recipes"])
    }

    @Test func aStepThatJustFinishedGetsItsNumberAndCount() throws {
        let (moment, seen) = ReferralMoments.stepJustDone(
            try Self.decoded("pending", recipes: 3), seen: ["household"])
        let step = try #require(moment)
        #expect(step.stepID == "recipes")
        #expect(step.title == "Step 2 done")
        #expect(step.countText == "2 of 3 done")
        #expect(step.message == "Save 3 recipes and plan a dinner to unlock your free month")
        #expect(seen == ["household", "recipes"])
        // Seen once, never again.
        #expect(ReferralMoments.stepJustDone(try Self.decoded("pending", recipes: 3), seen: seen).moment == nil)
    }

    @Test func aStepDoneOutOfOrderKeepsItsOwnNumber() throws {
        let step = try #require(
            ReferralMoments.stepJustDone(try Self.decoded("pending", dinner: true), seen: ["household"]).moment)
        #expect(step.title == "Step 3 done")
        #expect(step.countText == "2 of 3 done")
    }

    @Test func twoStepsAtOnceGetOneCardForTheLast() throws {
        let step = try #require(
            ReferralMoments.stepJustDone(try Self.decoded("pending", recipes: 3, dinner: true), seen: ["household"])
                .moment)
        #expect(step.stepID == "dinner")
        #expect(step.countText == "3 of 3 done")
    }

    @Test func aFinishedOrExpiredChecklistGetsNoStepCard() throws {
        for state in ["qualified", "expired"] {
            let (moment, seen) = ReferralMoments.stepJustDone(
                try Self.decoded(state, recipes: 3, dinner: true), seen: ["household"])
            #expect(moment == nil)
            #expect(seen == ["household", "recipes", "dinner"])
        }
    }

    @Test func theStoreShowsEachStepOnceAndTheFreeMonthTakesOver() async throws {
        let state = Mutex(("pending", 0, false))
        let (store, _) = try await makeStore { _ in
            let (name, recipes, dinner) = state.withLock { $0 }
            return (200, PlusFixtures.status(extra: Self.progress(name, recipes: recipes, dinner: dinner)))
        }
        await store.activate(householdID: "household-1")
        // The household step was done before the code went in: no card for it.
        #expect(store.stepDone == nil)
        state.withLock { $0 = ("pending", 3, false) }
        await store.reload()
        #expect(store.stepDone?.title == "Step 2 done")
        store.stepDone = nil
        await store.reload()
        #expect(store.stepDone == nil)
        // The last step and the free month in one check: only the bigger moment.
        state.withLock { $0 = ("qualified", 3, true) }
        await store.reload()
        #expect(store.stepDone == nil)
        #expect(store.freeMonthOn?.state == "qualified")
    }

    @Test func whatsBeenCelebratedLastsThroughARestart() async throws {
        let state = Mutex(("pending", 0, false))
        let handler: StubTransport.Handler = { _ in
            let (name, recipes, dinner) = state.withLock { $0 }
            return (200, PlusFixtures.status(extra: Self.progress(name, recipes: recipes, dinner: dinner)))
        }
        let (first, _) = try await makeStore(handler)
        await first.activate(householdID: "household-1")
        state.withLock { $0 = ("pending", 3, false) }
        await first.reload()
        #expect(first.stepDone != nil)
        // Same device, app opened again.
        let (second, _) = try await makeStore(handler)
        await second.activate(householdID: "household-1")
        #expect(second.stepDone == nil)
    }

    @Test func anOlderAPIStillGetsASummaryLine() throws {
        let json = #"""
            {"state":"pending","message":"m","steps":[{"id":"dinner","title":"Plan a dinner","done":true},
             {"id":"recipes","title":"Save 3 recipes","done":false}]}
            """#
        let progress = try JSONCoding.makeDecoder().decode(ReferralProgress.self, from: Data(json.utf8))
        #expect(progress.summaryText == "Free month: 1 of 2 done")
        #expect(progress.timeLeft == nil)
    }

    @Test func checklistButtonsNameWhereTheyGo() {
        #expect(ReferralChecklist.goLabel("recipes") == "Add a Recipe")
        #expect(ReferralChecklist.goLabel("dinner") == "Plan a Dinner")
        #expect(ReferralChecklist.goLabel("household") == nil)
    }

    /// The checklist says what Plus is, in the server's words (decision 708).
    @Test func theServerSaysWhatPlusIs() throws {
        let json = #"""
            {"state":"pending","message":"Add a recipe and plan a dinner to unlock your free month",
             "steps":[{"id":"recipes","title":"Add a recipe: write your own, import one from a website, or pick one from Shared Recipes.","done":false,"count":0,"target":1}],
             "plusTitle":"What is Plus?","plusLines":["One.","Two."]}
            """#
        let progress = try JSONCoding.makeDecoder().decode(ReferralProgress.self, from: Data(json.utf8))
        #expect(progress.explainerTitle == "What is Plus?")
        #expect(progress.explainerLines == ["One.", "Two."])
    }

    /// An older API sends no explainer; the app has its own.
    @Test func anOlderAPIStillGetsThePlusExplainer() throws {
        let json = #"{"state":"pending","message":"m","steps":[],"plusLines":[]}"#
        let progress = try JSONCoding.makeDecoder().decode(ReferralProgress.self, from: Data(json.utf8))
        #expect(progress.explainerTitle == "What is DinnerOS Plus?")
        #expect(progress.explainerLines == ReferralProgress.bundledPlusLines)
        #expect(progress.explainerLines.count == 3)
        #expect(progress.explainerLines.joined(separator: " ").contains("Autopilot"))
        #expect(!progress.explainerLines.joined().contains("\u{2014}"))
    }
}

/// The Add a Recipe choices (decision 709).
@MainActor
struct AddRecipeChoiceTests {
    @Test func theFourChoicesComeInOrderWithTheirIdentifiers() {
        #expect(
            AddRecipeChoice.allCases.map(\.title) == [
                "Write Your Own", "Import From a Website", "Browse Shared Recipes", "Add a Companion",
            ])
        #expect(
            AddRecipeChoice.allCases.map(\.accessibilityID) == [
                "AddChoice-write", "AddChoice-import", "AddChoice-shared", "AddChoice-companion",
            ])
    }

    @Test func everyChoiceHasAShortPlainSubtitle() {
        for choice in AddRecipeChoice.allCases {
            #expect(!choice.subtitle.isEmpty)
            #expect(choice.subtitle.count < 70, "\(choice) subtitle is long")
            #expect(choice.subtitle.hasSuffix("."))
            #expect(!choice.subtitle.contains("\u{2014}"))
        }
    }
}
