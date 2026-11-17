import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Free months as Apple offer codes (decision 755): what the API sends, and Redeem's reveal,
/// copy, sheet, fallback and refresh, with StoreKit stood in for.
nonisolated enum FreeMonthFixtures {
    static let ready = #"""
        {"id":"m1","state":"ready","title":"Your free month is ready",
         "message":"Redeem it to start DinnerOS Plus with the first month free.",
         "earnedAt":"2026-11-17T12:00:00Z","redeemText":"Redeem",
         "terms":"Redeeming starts DinnerOS Plus with the first month free. After that it renews at the normal price unless you cancel in your Apple account settings."}
        """#
    static let owed = #"""
        {"id":"m2","state":"owed","title":"Your free month is on the way",
         "message":"We'll let you know when it's ready.","earnedAt":"2026-11-16T12:00:00Z"}
        """#
    static let redeemed = #"""
        {"id":"m0","state":"redeemed","title":"Free month redeemed",
         "message":"Your free month went through Apple.","earnedAt":"2026-10-01T12:00:00Z"}
        """#

    static func status(_ months: [String], active: Bool = false) -> Data {
        PlusFixtures.status(active: active, extra: #","freeMonths":[\#(months.joined(separator: ","))]"#)
    }

    static func reveal(url: Bool = true) -> Data {
        let link = url ? #","redeemUrl":"https://apps.apple.com/redeem?ctx=offercodes&id=123&code=TESTCODE1""# : ""
        return Data(
            #"{"code":"TESTCODE1","copiedText":"Your code is copied. Paste it in Apple's screen."\#(link)}"#.utf8)
    }
}

/// Stands in for the clipboard, StoreKit's sheet and the App Store's redeem page.
@MainActor
final class FakeRedeemer: OfferCodeRedeeming {
    var copied: [String] = []
    var sheets = 0
    var opened: [URL] = []
    var sheetError: Error?
    var sheetResult = OfferCodeSheetResult.closed
    var opens = true
    /// What had been copied when the sheet opened.
    var copiedBeforeSheet: [String] = []

    func copy(_ code: String) { copied.append(code) }

    func presentRedeemSheet() async throws -> OfferCodeSheetResult {
        sheets += 1
        copiedBeforeSheet = copied
        if let sheetError { throw sheetError }
        return sheetResult
    }

    func open(_ url: URL) async -> Bool {
        opened.append(url)
        return opens
    }
}

/// Counts RevenueCat refreshes.
nonisolated final class RefreshCountingPurchaser: PlusPurchasing {
    private let count = Mutex(0)
    var refreshes: Int { count.withLock { $0 } }
    var canPurchase: Bool { true }
    func products(ids: [String]) async -> [PlusProduct] { [] }
    func purchase(productID: String, householdID: String, userID: String?) async throws -> PlusPurchaseResult {
        .cancelled
    }
    func restore(householdID: String) async throws {}
    func refreshCustomerInfo(householdID: String) async { count.withLock { $0 += 1 } }
}

struct FreeMonthModelTests {
    @Test func statusDecodesFreeMonthsAndPicksTheReadyOne() throws {
        let status = try JSONCoding.makeDecoder().decode(
            PlusStatus.self,
            from: FreeMonthFixtures.status([
                FreeMonthFixtures.redeemed, FreeMonthFixtures.owed, FreeMonthFixtures.ready,
            ]))
        #expect(status.freeMonths?.count == 3)
        let open = try #require(status.openFreeMonth)
        #expect(open.id == "m1" && open.isReady && open.buttonTitle == "Redeem")
        #expect(open.termsText.contains("renews at the normal price unless you cancel"))
    }

    @Test func anOwedMonthHasNoRedeemAndTheRedeemedOneIsNotShown() throws {
        let status = try JSONCoding.makeDecoder().decode(
            PlusStatus.self, from: FreeMonthFixtures.status([FreeMonthFixtures.redeemed, FreeMonthFixtures.owed]))
        let open = try #require(status.openFreeMonth)
        #expect(open.isOwed && open.redeemText == nil && open.terms == nil)
        #expect(open.title == "Your free month is on the way")
        let onlyRedeemed = try JSONCoding.makeDecoder().decode(
            PlusStatus.self, from: FreeMonthFixtures.status([FreeMonthFixtures.redeemed]))
        #expect(onlyRedeemed.openFreeMonth == nil)
    }

    @Test func anOlderAPIWithoutFreeMonthsStillDecodes() throws {
        let status = try JSONCoding.makeDecoder().decode(
            PlusStatus.self, from: PlusFixtures.status(extra: #","freeUntil":"2026-12-01T12:00:00Z""#))
        #expect(status.freeMonths == nil && status.openFreeMonth == nil && status.freeUntil != nil)
    }

    @Test func aMissingTermsLineFallsBackToTheBundledOne() {
        let month = FreeMonth(id: "x", state: "ready", title: "t", message: "m", earnedAt: .now)
        #expect(month.termsText == FreeMonth.bundledTerms && month.buttonTitle == "Redeem")
    }

    @Test func theRevealAnswerDecodes() throws {
        let code = try JSONCoding.makeDecoder().decode(FreeMonthCode.self, from: FreeMonthFixtures.reveal())
        #expect(code.code == "TESTCODE1")
        #expect(code.redeemURL?.absoluteString.hasPrefix("https://apps.apple.com/redeem?ctx=offercodes") == true)
        let bare = try JSONCoding.makeDecoder().decode(FreeMonthCode.self, from: FreeMonthFixtures.reveal(url: false))
        #expect(bare.redeemURL == nil)
    }

    @Test func invitesCarryFreeMonths() throws {
        let json = Data(
            #"""
            {"code":"W57ZVGUJ","shareText":"s","explainer":"e","rewarded":1,"monthsLeft":11,
             "invites":{"joined":1,"monthsEarned":1,"monthsCap":12,"capText":"1 of 12","emptyText":"e","joins":[],
                        "freeMonths":[\#(FreeMonthFixtures.ready)]}}
            """#.utf8)
        let referral = try JSONCoding.makeDecoder().decode(PlusReferral.self, from: json)
        #expect(referral.invites?.freeMonths?.first?.isReady == true)
        #expect(referral.invites?.usedCode == nil)
    }
}

@MainActor
struct FreeMonthRedeemTests {
    private func makeStore(
        purchaser: any PlusPurchasing = RefreshCountingPurchaser(),
        handler: @escaping StubTransport.Handler
    ) async throws -> (PlusStore, StubTransport, FakeRedeemer) {
        let transport = StubTransport(handler)
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = PlusStore(session: session, api: PlusAPI(client: client), purchaser: purchaser)
        store.confirmDelay = .milliseconds(1)
        store.redeemSheetDelay = .zero
        let redeemer = FakeRedeemer()
        store.redeemer = redeemer
        return (store, transport, redeemer)
    }

    private static let revealPath = "/api/v1/households/household-1/plus/free-months/m1/reveal"

    @Test func redeemRevealsCopiesThenOpensApplesSheetAndRefreshes() async throws {
        let purchaser = RefreshCountingPurchaser()
        let (store, transport, redeemer) = try await makeStore(purchaser: purchaser) { request in
            if request.url?.path().hasSuffix("/reveal") == true { return (200, FreeMonthFixtures.reveal()) }
            return (200, FreeMonthFixtures.status([FreeMonthFixtures.ready]))
        }
        await store.activate(householdID: "household-1")
        let month = try #require(store.openFreeMonth)
        await store.redeemFreeMonth(month)

        let reveal = try #require(transport.requests(to: Self.revealPath).first)
        #expect(reveal.httpMethod == "POST")
        #expect(redeemer.copied == ["TESTCODE1"])
        // Copied before Apple's sheet opened, so it's there to paste.
        #expect(redeemer.copiedBeforeSheet == ["TESTCODE1"])
        #expect(redeemer.sheets == 1 && redeemer.opened.isEmpty)
        #expect(purchaser.refreshes == 1)
        // Plus was checked again after the sheet closed.
        #expect(transport.requests(to: "/api/v1/households/household-1/plus").count >= 2)
        #expect(store.freeMonthNotice?.text == "Your code is copied. Paste it in Apple's screen.")
        #expect(store.freeMonthNotice?.isError == false && store.redeemingMonthID == nil)
    }

    @Test func whenTheSheetWontShowApplesRedeemPageOpensWithTheCode() async throws {
        let (store, _, redeemer) = try await makeStore { request in
            if request.url?.path().hasSuffix("/reveal") == true { return (200, FreeMonthFixtures.reveal()) }
            return (200, FreeMonthFixtures.status([FreeMonthFixtures.ready]))
        }
        redeemer.sheetError = OfferCodeRedeemError.noWindow
        await store.activate(householdID: "household-1")
        await store.redeemFreeMonth(try #require(store.openFreeMonth))
        #expect(
            redeemer.opened.map(\.absoluteString) == [
                "https://apps.apple.com/redeem?ctx=offercodes&id=123&code=TESTCODE1"
            ])
        #expect(store.freeMonthNotice?.isError == false)
    }

    @Test func withNoSheetAndNoPageItSaysWhereToPasteTheCode() async throws {
        let (store, _, redeemer) = try await makeStore { request in
            if request.url?.path().hasSuffix("/reveal") == true { return (200, FreeMonthFixtures.reveal(url: false)) }
            return (200, FreeMonthFixtures.status([FreeMonthFixtures.ready]))
        }
        redeemer.sheetError = OfferCodeRedeemError.noWindow
        await store.activate(householdID: "household-1")
        await store.redeemFreeMonth(try #require(store.openFreeMonth))
        #expect(redeemer.opened.isEmpty && redeemer.copied == ["TESTCODE1"])
        let notice = try #require(store.freeMonthNotice)
        #expect(notice.isError && notice.text.contains("Redeem Gift Card or Code"))
    }

    @Test func aRedemptionStoreKitConfirmsWaitsForPlus() async throws {
        let active = Mutex(false)
        let (store, _, redeemer) = try await makeStore { request in
            if request.url?.path().hasSuffix("/reveal") == true {
                active.withLock { $0 = true }
                return (200, FreeMonthFixtures.reveal())
            }
            let on = active.withLock { $0 }
            return (
                200, FreeMonthFixtures.status([on ? FreeMonthFixtures.redeemed : FreeMonthFixtures.ready], active: on)
            )
        }
        redeemer.sheetResult = .redeemed
        await store.activate(householdID: "household-1")
        await store.redeemFreeMonth(try #require(store.openFreeMonth))
        #expect(store.status?.active == true && store.openFreeMonth == nil)
        #expect(store.freeMonthNotice?.text == "Your free month went through. Plus is starting.")
    }

    @Test func aMonthStillOnTheWayShowsTheAPIsWordsAndNoSheet() async throws {
        let (store, _, redeemer) = try await makeStore { request in
            if request.url?.path().hasSuffix("/reveal") == true {
                return PlusFixtures.error(
                    409, code: "free_month_on_the_way",
                    message: "Your free month is on the way. We'll let you know when it's ready.")
            }
            return (200, FreeMonthFixtures.status([FreeMonthFixtures.ready]))
        }
        await store.activate(householdID: "household-1")
        await store.redeemFreeMonth(try #require(store.openFreeMonth))
        #expect(redeemer.copied.isEmpty && redeemer.sheets == 0)
        let notice = try #require(store.freeMonthNotice)
        #expect(notice.isError && notice.text == "Your free month is on the way. We'll let you know when it's ready.")
    }

    @Test func anOwedMonthIsNeverRevealed() async throws {
        let (store, transport, redeemer) = try await makeStore { _ in
            (200, FreeMonthFixtures.status([FreeMonthFixtures.owed]))
        }
        await store.activate(householdID: "household-1")
        let month = try #require(store.openFreeMonth)
        #expect(month.isOwed)
        await store.redeemFreeMonth(month)
        #expect(transport.requests.allSatisfy { $0.url?.path().hasSuffix("/reveal") != true })
        #expect(redeemer.sheets == 0)
    }

    @Test func theMenuCardShowsAReadyMonthUntilHidden() async throws {
        let (store, _, _) = try await makeStore { _ in (200, FreeMonthFixtures.status([FreeMonthFixtures.ready])) }
        store.defaults.removeObject(
            forKey: PlusStore.freeMonthCardHiddenKey("household-1", monthID: "m1"))
        await store.activate(householdID: "household-1")
        let month = try #require(store.menuFreeMonth)
        store.hideFreeMonthCard(month)
        #expect(store.menuFreeMonth == nil)
        store.defaults.removeObject(forKey: PlusStore.freeMonthCardHiddenKey("household-1", monthID: "m1"))
    }
}
