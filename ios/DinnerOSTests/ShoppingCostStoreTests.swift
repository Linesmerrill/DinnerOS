import Foundation
import Testing

@testable import DinnerOS

struct ShoppingCostStoreTests {
    nonisolated private static let handoffsPath = "/api/v1/households/household-1/shopping/handoffs"
    nonisolated private static let costPath = "/api/v1/households/household-1/shopping/weeks/2026-W38/cost"
    nonisolated private static let spendPath = "/api/v1/households/household-1/shopping/weeks/2026-W38/spend"

    nonisolated private static func handoff(_ id: String, lineIDs: [String], price: Int? = nil) -> String {
        CostFixtures.handoffJSON(
            id: id,
            lines: lineIDs.map {
                CostFixtures.pricedLineJSON(
                    id: $0, key: "i-\(id)-\($0)", name: "Item \($0)", displayName: "Test item \($0)",
                    priceCents: price, pantry: "tracked")
            })
    }

    private func makeStore(
        costStatus: Int = 200, _ handler: @escaping @Sendable (URLRequest) -> (Int, Data)? = { _ in nil }
    ) async throws -> (ShoppingStore, StubTransport) {
        let transport = StubTransport { request in
            if let answer = handler(request) { return answer }
            switch (request.httpMethod, request.url?.path()) {
            case ("GET", Self.handoffsPath?):
                let items = [
                    Self.handoff("handoff-2", lineIDs: ["l1"]), Self.handoff("handoff-1", lineIDs: ["l1", "l2"]),
                ]
                return (200, Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8))
            case ("GET", Self.costPath?):
                return costStatus == 200
                    ? (200, CostFixtures.weekCostJSON) : (costStatus, Fixtures.errorJSON(code: "not_found"))
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate("2026-09-16T12:00:00Z"))
        let store = ShoppingStore(
            session: session, api: ShoppingAPI(client: client), checks: InMemoryGroceryChecks(), now: { instant },
            openURL: { _ in false })
        store.activate(
            householdID: "household-1", timeZone: TimeZone(identifier: "America/Denver") ?? .gmt, weekStartsOn: .mon)
        store.setPermissions(canEdit: true, canConfirm: true)
        return (store, transport)
    }

    @Test func weekCostLoadsWithEveryHandoffOfTheWeek() async throws {
        let (store, transport) = try await makeStore()

        await store.loadWeekCost()

        let handoffRequest = try #require(transport.requests(to: Self.handoffsPath).first)
        // Every status: prices can be added to confirmed lines too.
        #expect(handoffRequest.url?.query() == "week=2026-W38")
        #expect(store.costPhase == .loaded)
        #expect(store.weekCost?.savedCents == 6_880)
        #expect(store.isCostAvailable)
        // Oldest handoff first.
        #expect(
            store.priceableLines.map(\.id) == [
                PriceableLine.ID(handoffID: "handoff-1", lineID: "l1"),
                PriceableLine.ID(handoffID: "handoff-1", lineID: "l2"),
                PriceableLine.ID(handoffID: "handoff-2", lineID: "l1"),
            ])
    }

    @Test func anAPIWithoutCostHidesTheCard() async throws {
        let (store, _) = try await makeStore(costStatus: 404)

        await store.loadWeekCost()

        #expect(!store.isCostAvailable)
        #expect(store.weekCost == nil)
        #expect(store.costPhase == .loaded)
    }

    @Test func savingPricesPostsOncePerHandoffThenReloadsCost() async throws {
        let (store, transport) = try await makeStore { request in
            guard request.httpMethod == "POST", let path = request.url?.path(), path.hasSuffix("/prices") else {
                return nil
            }
            let id = path.contains("handoff-1") ? "handoff-1" : "handoff-2"
            return (200, Data(Self.handoff(id, lineIDs: id == "handoff-1" ? ["l1", "l2"] : ["l1"], price: 499).utf8))
        }
        await store.loadWeekCost()
        let costReads = transport.requests(to: Self.costPath).count

        let prices: [PriceableLine.ID: Int?] = [
            PriceableLine.ID(handoffID: "handoff-1", lineID: "l2"): nil as Int?,
            PriceableLine.ID(handoffID: "handoff-1", lineID: "l1"): 499,
            PriceableLine.ID(handoffID: "handoff-2", lineID: "l1"): 250,
        ]
        try await store.savePrices(prices)

        let first = try #require(transport.requests(to: Self.handoffsPath + "/handoff-1/prices").first)
        let data = try #require(first.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let lines = try #require(body["lines"] as? [[String: Any]])
        #expect(lines.map { $0["lineId"] as? String } == ["l1", "l2"])
        #expect(lines[0]["priceCents"] as? Int == 499)
        #expect(lines[1]["priceCents"] is NSNull)
        #expect(transport.requests(to: Self.handoffsPath + "/handoff-2/prices").count == 1)
        #expect(transport.requests(to: Self.costPath).count == costReads + 1)
    }

    @Test func orderTotalReplacesTheWeeksCost() async throws {
        let (store, transport) = try await makeStore { request in
            request.httpMethod == "PUT" && request.url?.path() == Self.spendPath
                ? (200, CostFixtures.emptyWeekCostJSON.replacingWeek()) : nil
        }

        try await store.setOrderTotal(nil)

        let request = try #require(transport.requests(to: Self.spendPath).first)
        let data = try #require(request.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(body["orderTotalCents"] is NSNull)
        #expect(store.weekCost?.spentCents == nil)
        #expect(!store.isSavingSpend)
    }

    @Test func changingWeeksForgetsTheCost() async throws {
        let (store, _) = try await makeStore()
        await store.loadWeekCost()

        await store.show(week: store.week.next)

        #expect(store.weekCost == nil)
        #expect(store.weekHandoffs.isEmpty)
        #expect(store.costPhase == .idle)
    }

    // MARK: - Why the card doesn't present its own sheets (decision 510)

    /// Whether the Shop tab's cost card is on screen for what the store holds now.
    private func isCardVisible(_ store: ShoppingStore) -> Bool {
        WeekCostCard.isVisible(
            isCostAvailable: store.isCostAvailable, hasCostContent: store.weekCost?.hasContent == true,
            hasHandoffs: !store.weekHandoffs.isEmpty)
    }

    /// The card is a list section that ordinary loading takes off screen and puts back, so
    /// SwiftUI tears down whatever is attached to it and builds it again. That is why the
    /// sheets it opens are held and presented by `ShopView`: a sheet attached to the card was
    /// dismissed as the card went, which on the phone looked like the import sheet opening and
    /// closing itself on the first tap.
    @Test func theCostCardLeavesTheScreenWheneverTheWeekReloads() async throws {
        let (store, _) = try await makeStore()
        // Before anything is read there is no cost and no handoff: no card.
        #expect(!isCardVisible(store))

        await store.loadWeekCost()
        #expect(isCardVisible(store))

        // Another week starts with nothing again, and the card goes with it.
        await store.show(week: store.week.next)
        #expect(!isCardVisible(store))
    }

    /// An API without the cost endpoints hides the card even though the week has handoffs, so
    /// a `404` on a refresh is one more way the card disappears mid-gesture.
    @Test func anAPIWithoutCostTakesTheCardAwayFromAWeekWithHandoffs() async throws {
        let (store, _) = try await makeStore(costStatus: 404)

        await store.loadWeekCost()

        #expect(!store.weekHandoffs.isEmpty)
        #expect(!isCardVisible(store))
    }

    /// The sheet's identity is the sheet, never the week's cost. A `WeekCostSheet` that carried
    /// store values would change identity on every reload, and SwiftUI would tear the open
    /// sheet down and present it again — the bug moved to where the fix put the state.
    @Test func aCostSheetKeepsItsIdentityWhileTheWeekReloads() async throws {
        let (store, _) = try await makeStore()
        let sheet = WeekCostSheet.importScreenshots
        let idBefore = sheet.id

        await store.loadWeekCost()

        #expect(sheet.id == idBefore)
        #expect(Set(WeekCostSheet.allCases.map(\.id)).count == WeekCostSheet.allCases.count)
    }
}

/// The rule that decides whether the Shop tab's cost card is shown, as arithmetic.
struct WeekCostCardTests {
    @Test func aWeekWithNeitherACostNorAHandoffShowsNoCard() {
        #expect(!WeekCostCard.isVisible(isCostAvailable: true, hasCostContent: false, hasHandoffs: false))
    }

    @Test func eitherACostOrAHandoffShowsTheCard() {
        #expect(WeekCostCard.isVisible(isCostAvailable: true, hasCostContent: true, hasHandoffs: false))
        #expect(WeekCostCard.isVisible(isCostAvailable: true, hasCostContent: false, hasHandoffs: true))
    }

    @Test func anAPIWithoutTheCostEndpointsShowsNoCardAtAll() {
        #expect(!WeekCostCard.isVisible(isCostAvailable: false, hasCostContent: true, hasHandoffs: true))
    }
}

nonisolated extension Data {
    fileprivate func replacingWeek() -> Data {
        Data(String(decoding: self, as: UTF8.self).replacingOccurrences(of: "2026-W39", with: "2026-W38").utf8)
    }
}
