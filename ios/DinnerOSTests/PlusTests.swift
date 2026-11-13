import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Synthetic Plus answers shaped like `GET /households/{id}/plus`.
nonisolated enum PlusFixtures {
    static let offer = #"""
        {"title":"DinnerOS Plus","subtitle":"Let DinnerOS plan your week and track what you spend.",
         "benefits":["Autopilot plans the week's dinners for you."],
         "plans":[{"productId":"com.linesmerrill.dinneros.plus.yearly","name":"Yearly","priceText":"$39.99 a year","note":"Save 33%"},
                  {"productId":"com.linesmerrill.dinneros.plus.monthly","name":"Monthly","priceText":"$4.99 a month"}],
         "trialDays":14,"trialText":"Try it free for 14 days","buyText":"Get DinnerOS Plus",
         "terms":"Plus renews automatically until you cancel.","restoreText":"Restore purchases",
         "freeText":"Sending your list to a store is always free.",
         "links":[{"title":"Privacy Policy","url":"https://dinneros.app/privacy/"},
                  {"title":"Terms of Use","url":"https://dinneros.app/terms/"}]}
        """#

    static let offerWithoutLinks = #"""
        {"title":"DinnerOS Plus","subtitle":"s","benefits":[],"plans":[],"trialDays":14,"trialText":"t",
         "buyText":"b","terms":"r","restoreText":"Restore purchases","freeText":"f"}
        """#

    static func status(enabled: Bool = true, active: Bool = false, extra: String = "") -> Data {
        let offerJSON = enabled ? #","offer":\#(offer)"# : ""
        return Data(
            #"""
            {"enabled":\#(enabled),"active":\#(active),"trial":false,"willRenew":false,"billingIssue":false\#(extra)\#(offerJSON)}
            """#.utf8)
    }

    static let referral = Data(
        #"""
        {"code":"ABCD2345","shareText":"Use my code ABCD2345 and we both get a free month of DinnerOS Plus.",
         "explainer":"Invite another household.","rewarded":0,"monthsLeft":12}
        """#.utf8)

    static let referralWithInvites = Data(
        #"""
        {"code":"W57ZVGUJ","shareText":"Use my code W57ZVGUJ.","explainer":"Invite another household.",
         "rewarded":12,"monthsLeft":0,
         "invites":{"joined":13,"monthsEarned":12,"monthsCap":12,"capText":"12 of 12",
                    "freeUntil":"2027-11-13T12:00:00Z",
                    "emptyText":"No one has used your code yet. Share it and you both get a free month of Plus.",
                    "joins":[{"at":"2026-11-14T12:00:00Z","counted":false,"status":"Limit reached"},
                             {"at":"2026-11-13T12:00:00Z","counted":true,"status":"Counted"}],
                    "usedCode":{"at":"2026-11-01T12:00:00Z","freeUntil":"2026-12-01T12:00:00Z"}}}
        """#.utf8)

    static let referralSummary = Data(
        #"""
        {"codesUsed":2,"joinedThisWeek":1,"joinedAllTime":3,"topReferrers":[{"name":"Lines","count":2}]}
        """#.utf8)

    static func error(_ status: Int, code: String, message: String) -> (status: Int, body: Data) {
        (status, Data(#"{"error":{"code":"\#(code)","message":"\#(message)"}}"#.utf8))
    }
}

/// A purchaser that answers from a script instead of the App Store.
nonisolated final class FakePurchaser: PlusPurchasing {
    let canPurchase: Bool
    private let result: PlusPurchaseResult
    private let purchases = Mutex<[String]>([])

    init(canPurchase: Bool = true, result: PlusPurchaseResult = .purchased) {
        self.canPurchase = canPurchase
        self.result = result
    }

    var bought: [String] { purchases.withLock { $0 } }

    func products(ids: [String]) async -> [PlusProduct] {
        ids.map { PlusProduct(id: $0, displayPrice: $0.hasSuffix("yearly") ? "$39.99" : "$4.99", trialEligible: false) }
    }

    func purchase(productID: String, householdID: String) async throws -> PlusPurchaseResult {
        purchases.withLock { $0.append(productID) }
        return result
    }

    func restore(householdID: String) async throws {}
}

struct PlusStoreTests {
    private func makeStore(
        purchaser: any PlusPurchasing = FakePurchaser(),
        handler: @escaping StubTransport.Handler
    ) async throws -> (PlusStore, StubTransport) {
        let transport = StubTransport(handler)
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = PlusStore(session: session, api: PlusAPI(client: client), purchaser: purchaser)
        store.confirmDelay = .milliseconds(1)
        return (store, transport)
    }

    @Test func plusOffOpensEverything() async throws {
        let (store, _) = try await makeStore { _ in (200, PlusFixtures.status(enabled: false)) }
        // Before anything loads, nothing is locked.
        #expect(store.hasPlus && !store.isEnabled)
        await store.activate(householdID: "household-1")
        #expect(store.hasPlus && !store.isEnabled && store.status?.offer == nil)
    }

    @Test func aFreeHouseholdSeesTheOfferWithAppStorePrices() async throws {
        let (store, transport) = try await makeStore { _ in (200, PlusFixtures.status()) }
        await store.activate(householdID: "household-1")
        #expect(store.isEnabled && !store.hasPlus)
        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/plus")
        let offer = try #require(store.status?.offer)
        #expect(offer.plans.map(\.productID).first == "com.linesmerrill.dinneros.plus.yearly")
        let yearly = offer.plans[0]
        #expect(PlusText.price(yearly, product: store.products[yearly.productID]) == "$39.99 a year")
        #expect(PlusText.price(offer.plans[1], product: nil) == "$4.99 a month")
        #expect(offer.shownLinks.map(\.title) == ["Privacy Policy", "Terms of Use"])
    }

    @Test func anOlderAPIWithoutPlusLocksNothing() async throws {
        let (store, _) = try await makeStore { _ in
            PlusFixtures.error(404, code: "not_found", message: "resource not found")
        }
        await store.activate(householdID: "household-1")
        #expect(store.hasPlus && !store.isEnabled)
    }

    @Test func buyingWaitsForTheServerToTurnPlusOn() async throws {
        let calls = Counter()
        let purchaser = FakePurchaser()
        let (store, _) = try await makeStore(purchaser: purchaser) { _ in
            // RevenueCat tells the API on the third check.
            calls.increment()
            return (200, PlusFixtures.status(active: calls.value >= 3))
        }
        await store.activate(householdID: "household-1")
        let on = try await store.purchase(productID: "com.linesmerrill.dinneros.plus.monthly")
        #expect(on && store.hasPlus && !store.isPurchasing)
        #expect(purchaser.bought == ["com.linesmerrill.dinneros.plus.monthly"])
    }

    @Test func aCancelledPurchaseChangesNothing() async throws {
        let (store, transport) = try await makeStore(purchaser: FakePurchaser(result: .cancelled)) { _ in
            (200, PlusFixtures.status())
        }
        await store.activate(householdID: "household-1")
        let before = transport.requests.count
        #expect(try await store.purchase(productID: "x") == false)
        #expect(transport.requests.count == before && !store.hasPlus)
    }

    @Test func buyingIsClosedWithoutRevenueCat() async throws {
        let purchaser = StoreKitPlusPurchaser()
        #expect(!purchaser.canPurchase)
        await #expect(throws: PlusPurchaseError.notOpen) {
            try await purchaser.purchase(productID: "x", householdID: "h")
        }
    }

    @Test func anOfferWithoutLinksStillShowsPrivacyAndTerms() throws {
        let offer = try JSONCoding.makeDecoder().decode(PlusOffer.self, from: Data(PlusFixtures.offerWithoutLinks.utf8))
        #expect(offer.links == nil)
        #expect(
            offer.shownLinks.map(\.url.absoluteString) == [
                "https://dinneros.app/privacy/", "https://dinneros.app/terms/",
            ])
    }

    @Test func theBuildsRevenueCatKeyOpensBuying() {
        #expect(!PlusPurchasers.make(apiKey: nil).canPurchase)
        #expect(!PlusPurchasers.make(apiKey: "").canPurchase)
        let purchaser = PlusPurchasers.make(apiKey: "appl_example")
        #expect(purchaser is RevenueCatPlusPurchaser && purchaser.canPurchase)
    }

    @Test func aPlusRequiredErrorOpensThePurchaseScreen() async throws {
        let (store, _) = try await makeStore { _ in (200, PlusFixtures.status()) }
        let required = APIError.server(
            status: 402, code: "plus_required", message: "This is part of DinnerOS Plus.", requestID: nil)
        #expect(required.localizedDescription == "This is part of DinnerOS Plus.")
        #expect(!store.handle(APIError.transport(.notConnectedToInternet)) && !store.showsPurchase)
        #expect(store.handle(required) && store.showsPurchase)
    }

    @Test func inviteCodes() async throws {
        let (store, transport) = try await makeStore { request in
            switch (request.httpMethod, request.url?.path()) {
            case ("GET", "/api/v1/households/household-1/plus/referral"):
                return (200, PlusFixtures.referral)
            case ("POST", "/api/v1/households/household-1/plus/referral/redeem"):
                let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
                if body.contains("ABCD2345") {
                    return PlusFixtures.error(
                        409, code: "code_not_allowed", message: "That's your own code. Share it with another household."
                    )
                }
                return (200, PlusFixtures.status(active: true, extra: #","freeUntil":"2026-12-13T12:00:00Z""#))
            default:
                return (200, PlusFixtures.status())
            }
        }
        await store.activate(householdID: "household-1")
        try await store.loadReferral()
        #expect(store.referral?.code == "ABCD2345")
        #expect(PlusInviteView.spaced("ABCD2345") == "ABCD 2345")

        await #expect {
            try await store.redeem(code: "ABCD2345")
        } throws: { error in
            error.localizedDescription == "That's your own code. Share it with another household."
        }
        try await store.redeem(code: "wxyz-6789")
        #expect(store.hasPlus && store.status?.freeUntil != nil)
        let redeem = try #require(transport.requests(to: "/api/v1/households/household-1/plus/referral/redeem").last)
        #expect(String(decoding: redeem.httpBody ?? Data(), as: UTF8.self).contains("wxyz-6789"))
    }

    @Test func yourInvitesShowWhoUsedTheCode() async throws {
        let (store, _) = try await makeStore { request in
            request.url?.path().hasSuffix("/referral") == true
                ? (200, PlusFixtures.referralWithInvites) : (200, PlusFixtures.status())
        }
        await store.activate(householdID: "household-1")
        try await store.loadReferral()
        let invites = try #require(store.referral?.invites)
        #expect(invites.joined == 13 && invites.capText == "12 of 12" && invites.freeUntil != nil)
        #expect(invites.joins.map(\.status) == ["Limit reached", "Counted"])
        #expect(invites.joins.map(\.counted) == [false, true])
        let used = try #require(invites.usedCode)
        let before = try #require(ISO8601DateFormatter().date(from: "2026-11-13T12:00:00Z"))
        let after = try #require(ISO8601DateFormatter().date(from: "2027-01-01T12:00:00Z"))
        #expect(PlusText.usedCode(used, now: before).hasPrefix("You joined with a code. Free month of Plus until"))
        #expect(PlusText.usedCode(used, now: after).contains("ended"))
        for line in [PlusText.usedCode(used, now: before), PlusText.usedCode(used, now: after), invites.emptyText] {
            #expect(!line.contains("\u{2014}"))
        }
    }

    @Test func anOlderAPIWithoutInvitesStillShowsTheCode() async throws {
        let (store, _) = try await makeStore { request in
            request.url?.path().hasSuffix("/referral") == true
                ? (200, PlusFixtures.referral) : (200, PlusFixtures.status())
        }
        await store.activate(householdID: "household-1")
        try await store.loadReferral()
        #expect(store.referral?.code == "ABCD2345" && store.referral?.invites == nil)
    }

    @Test func usingACodeReloadsYourInvites() async throws {
        let (store, transport) = try await makeStore { request in
            switch (request.httpMethod, request.url?.path()) {
            case ("GET", "/api/v1/households/household-1/plus/referral"):
                return (200, PlusFixtures.referralWithInvites)
            case ("POST", _):
                return (200, PlusFixtures.status(active: true))
            default:
                return (200, PlusFixtures.status())
            }
        }
        await store.activate(householdID: "household-1")
        try await store.redeem(code: "WXYZ6789")
        #expect(transport.requests(to: "/api/v1/households/household-1/plus/referral").count == 1)
        #expect(store.referral?.invites?.usedCode != nil)
    }

    @Test func theCopyButtonDropsTheSpace() {
        #expect(PlusText.copyableCode("W57Z VGUJ") == "W57ZVGUJ")
        #expect(PlusText.copyableCode(PlusInviteView.spaced("W57ZVGUJ")) == "W57ZVGUJ")
    }

    @Test func referralTotalsAreForOperatorsOnly() async throws {
        let (operatorStore, _) = try await makeStore { _ in (200, PlusFixtures.referralSummary) }
        let summary = try #require(try await operatorStore.referralSummary())
        #expect(summary.joinedAllTime == 3 && summary.joinedThisWeek == 1 && summary.codesUsed == 2)
        #expect(summary.topReferrers.first?.name == "Lines")

        let (memberStore, _) = try await makeStore { _ in
            PlusFixtures.error(403, code: "forbidden", message: "Only the people who run DinnerOS can see this.")
        }
        #expect(try await memberStore.referralSummary() == nil)
    }

    @Test func statusLines() throws {
        let decoder = JSONCoding.makeDecoder()
        let now = try #require(ISO8601DateFormatter().date(from: "2026-11-13T12:00:00Z"))
        let trial = try decoder.decode(
            PlusStatus.self,
            from: Data(
                #"{"enabled":true,"active":true,"trial":true,"expiresAt":"2026-11-27T12:00:00Z","willRenew":true,"billingIssue":false}"#
                    .utf8))
        #expect(PlusText.status(trial, now: now).hasPrefix("Free trial until"))
        let ending = try decoder.decode(
            PlusStatus.self,
            from: Data(
                #"{"enabled":true,"active":true,"trial":false,"expiresAt":"2026-12-13T12:00:00Z","willRenew":false,"billingIssue":false}"#
                    .utf8))
        #expect(PlusText.status(ending, now: now).hasPrefix("Ends"))
        let free = try decoder.decode(
            PlusStatus.self,
            from: Data(
                #"{"enabled":true,"active":true,"trial":false,"willRenew":false,"billingIssue":false,"freeUntil":"2026-12-13T12:00:00Z"}"#
                    .utf8))
        #expect(PlusText.status(free, now: now).hasPrefix("Free until"))
        let billing = try decoder.decode(
            PlusStatus.self,
            from: Data(#"{"enabled":true,"active":true,"trial":false,"willRenew":true,"billingIssue":true}"#.utf8))
        #expect(PlusText.status(billing, now: now).hasPrefix("Apple couldn't charge"))
        for line in [trial, ending, free, billing].map({ PlusText.status($0, now: now) }) {
            #expect(!line.contains("\u{2014}"))
        }
    }
}

/// Planning a week the household hasn't got Plus for opens the purchase screen when someone
/// asked, and stays quiet when the app asked on its own.
struct AutopilotPlusTests {
    @Test func generateOpensThePurchaseScreenOnlyWhenAsked() async throws {
        let transport = StubTransport { request in
            if request.url?.path().hasSuffix("/generate") == true {
                return PlusFixtures.error(402, code: "plus_required", message: "This is part of DinnerOS Plus.")
            }
            return (404, Data(#"{"error":{"code":"not_found","message":"not found"}}"#.utf8))
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let autopilot = AutopilotStore(
            session: session, api: AutopilotAPI(client: client), prompts: InMemoryAutopilotPrompts())
        await autopilot.activate(householdID: "household-1")
        let opened = Counter()
        autopilot.plusRequired = { opened.increment() }
        let week = try #require(ISOWeek("2026-W47"))

        await #expect(throws: CancellationError.self) { try await autopilot.generate(week: week, prompted: false) }
        #expect(opened.value == 0)
        await #expect(throws: CancellationError.self) { try await autopilot.generate(week: week) }
        #expect(opened.value == 1)
        #expect(!autopilot.isGenerating)
    }
}

/// Tapping Plan with Autopilot without Plus shows the purchase screen, not the setup questions.
struct AutopilotFlowPlusTests {
    @Test func planShowsThePurchaseScreenFirst() async throws {
        let session = AuthSession.preview(.signedOut)
        let plus = PlusStore(session: session, api: nil, purchaser: FakePurchaser())
        let flow = WeekAutopilotFlow()
        flow.plus = plus
        let autopilot = AutopilotStore(session: session, api: nil, prompts: InMemoryAutopilotPrompts())
        let plans = PlanStore.preview(session: session, plan: nil)

        plus.preview(status: PlusPreview.free)
        flow.plan(autopilot: autopilot, plans: plans)
        #expect(plus.showsPurchase && flow.sheet == nil)

        plus.showsPurchase = false
        plus.preview(status: .off)
        flow.plan(autopilot: autopilot, plans: plans)
        #expect(!plus.showsPurchase && flow.sheet == .onboarding)
    }
}
