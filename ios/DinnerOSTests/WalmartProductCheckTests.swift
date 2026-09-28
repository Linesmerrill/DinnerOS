import Foundation
import Testing

@testable import DinnerOS

/// The page parser and the phone's check, against synthesized pages and stubs. Nothing here
/// sends a request to Walmart.
struct WalmartProductPageTests {
    private let date = Date(timeIntervalSince1970: 1_790_000_000)
    private let itemURL = URL(string: "https://www.walmart.com/ip/Test-Onions-3-lb-Bag/100000001")
    private let missingURL = URL(string: "https://www.walmart.com/ip/seort/100000001")
    private let blockedURL = URL(string: "https://www.walmart.com/blocked?url=L2lwLzEwMDAwMDAwMQ==")

    private func parse(_ html: String, status: Int = 200, url: URL?) throws -> WalmartProductResult {
        WalmartProductPage.parse(
            itemID: "100000001", statusCode: status, finalURL: try #require(url), html: html, at: date)
    }

    @Test func aProductInStockIsFoundWithItsNameAndPrice() throws {
        let result = try parse(WalmartPageFixtures.page(nextData: WalmartPageFixtures.product()), url: itemURL)
        #expect(result.status == .found)
        #expect(result.name == "Test Onions, 3 lb Bag")
        #expect(result.priceCents == 348)
        #expect(result.detail == nil)
        #expect(result.checkedAt == date)
    }

    @Test func aProductOutOfStockForPickupAndDeliveryIsUnavailable() throws {
        let fulfillment = #"""
            [{"type":"SHIPPING","availabilityStatus":"IN_STOCK"},
             {"type":"PICKUP","availabilityStatus":"OUT_OF_STOCK"},
             {"type":"DELIVERY","availabilityStatus":"OUT_OF_STOCK"}]
            """#
        let html = WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(fulfillment: fulfillment))
        let result = try parse(html, url: itemURL)
        #expect(result.status == .unavailable)
        #expect(result.name == "Test Onions, 3 lb Bag")
        // Delivery alone in stock is enough.
        let delivery =
            #"[{"type":"PICKUP","availabilityStatus":"OUT_OF_STOCK"},{"type":"DELIVERY","availabilityStatus":"LIMITED_STOCK"}]"#
        #expect(
            try parse(
                WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(fulfillment: delivery)), url: itemURL
            )
            .status == .found)
    }

    @Test func anotherItemsPageIsUnknownNeverFound() throws {
        let html = WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(id: "100000009"))
        let result = try parse(html, url: itemURL)
        #expect(result.status == .unknown)
        #expect(result.detail == .differentItem)
    }

    @Test func aPageMissingWhatItNeedsIsUnknown() throws {
        let cases: [(String, WalmartCheckDetail)] = [
            (WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(name: nil)), .noProduct),
            (WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(name: "   ")), .noProduct),
            (WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(fulfillment: nil)), .noFulfillment),
            (WalmartPageFixtures.page(nextData: #"{"props":{"pageProps":{"initialData":{"data":{}}}}}"#), .noProduct),
            (WalmartPageFixtures.page(nextData: "{not json"), .noPageData),
            ("<html><body>Robot or human?</body></html>", .noPageData),
        ]
        for (html, detail) in cases {
            let result = try parse(html, url: itemURL)
            #expect(result.status == .unknown)
            #expect(result.detail == detail)
            #expect(result.name == nil)
        }
        // No price is fine: it's found, just without one.
        let noPrice = try parse(
            WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(price: nil)), url: itemURL)
        #expect(noPrice.status == .found)
        #expect(noPrice.priceCents == nil)
    }

    @Test func statusComesFromTheAnswerBeforeThePage() throws {
        // Walmart answers a missing item with 404 at /ip/seort/<id>.
        #expect(try parse("", status: 404, url: missingURL).status == .gone)
        // A bot challenge redirects to /blocked and answers 200.
        let blocked = try parse(WalmartPageFixtures.page(nextData: WalmartPageFixtures.product()), url: blockedURL)
        #expect(blocked.status == .unknown)
        #expect(blocked.detail == .blocked)
        #expect(WalmartProductPage.isBlocked(try #require(blockedURL)))
        // A 404 somewhere else isn't "gone".
        let offSite = try parse("", status: 404, url: URL(string: "https://example.com/ip/100000001"))
        #expect(offSite.status == .unknown)
        #expect(offSite.detail == .offSite)
        #expect(
            try parse("", status: 404, url: URL(string: "https://www.walmart.com/search?q=onions")).status == .unknown)
        let throttled = try parse("", status: 429, url: itemURL)
        #expect(throttled.status == .unknown)
        #expect(throttled.detail == .unexpectedStatus)
    }

    @Test func theWebViewsFieldsReadTheSameWay() throws {
        let fields = WalmartPageFields(
            usItemId: "100000001", name: "Test Onions", availabilityStatus: "IN_STOCK", priceString: "$1,024.50",
            fulfillmentOptions: [.init(type: "PICKUP", availabilityStatus: "IN_STOCK")])
        let found = WalmartProductPage.parse(
            itemID: "100000001", statusCode: 200, finalURL: try #require(itemURL), fields: fields, at: date)
        #expect(found.status == .found)
        #expect(found.priceCents == 102_450)
        let none = WalmartProductPage.parse(
            itemID: "100000001", statusCode: 200, finalURL: try #require(itemURL), fields: nil, at: date)
        #expect(none.status == .unknown)
        let gone = WalmartProductPage.parse(
            itemID: "100000001", statusCode: 404, finalURL: try #require(missingURL), fields: nil, at: date)
        #expect(gone.status == .gone)
    }

    @Test func pricesAreReadOnlyExactlyAsWalmartPrintsThem() {
        #expect(WalmartProductPage.price("$0.85") == 85)
        #expect(WalmartProductPage.price("$12") == 1200)
        #expect(WalmartProductPage.price("$1,024.50") == 102_450)
        #expect(WalmartProductPage.price("$1.24/lb") == nil)
        #expect(WalmartProductPage.price("From $3") == nil)
        #expect(WalmartProductPage.price("$1.5") == nil)
        #expect(WalmartProductPage.price("$10,00") == nil)
        #expect(WalmartProductPage.price(nil) == nil)
    }
}

struct WalmartProductCheckTests {
    private func page(_ id: String, status: Int = 200, html: String? = nil) -> WalmartPageAnswer {
        let url = URL(string: "https://www.walmart.com/ip/Test/\(id)") ?? URL(fileURLWithPath: "/")
        return .page(
            statusCode: status, finalURL: url,
            html: html ?? WalmartPageFixtures.page(nextData: WalmartPageFixtures.product(id: id)))
    }

    private var blocked: WalmartPageAnswer {
        .page(
            statusCode: 200, finalURL: URL(string: "https://www.walmart.com/blocked?x=1") ?? URL(fileURLWithPath: "/"),
            html: "<html></html>")
    }

    private func rendered(_ id: String) -> WalmartRenderedPage {
        WalmartRenderedPage(
            statusCode: 200,
            finalURL: URL(string: "https://www.walmart.com/ip/Test/\(id)") ?? URL(fileURLWithPath: "/"),
            fields: WalmartPageFields(
                usItemId: id, name: "Rendered \(id)", availabilityStatus: "IN_STOCK", priceString: "$2.00",
                fulfillmentOptions: [.init(type: "PICKUP", availabilityStatus: "IN_STOCK")]))
    }

    private struct Rig {
        let check: WalmartProductCheck
        let fetcher: StubPageFetcher
        let renderer: StubPageRenderer
        let cache: InMemoryWalmartCheckCache
        let clock: FakeCheckClock
    }

    private func rig(timing: WalmartProductCheck.Timing = .init()) -> Rig {
        let clock = FakeCheckClock()
        let fetcher = StubPageFetcher(clock: clock)
        let renderer = StubPageRenderer(clock: clock)
        let cache = InMemoryWalmartCheckCache()
        let check = WalmartProductCheck(
            fetcher: fetcher, renderer: renderer, cache: cache, timing: timing, now: { clock.now },
            sleep: { clock.sleep($0) })
        return Rig(check: check, fetcher: fetcher, renderer: renderer, cache: cache, clock: clock)
    }

    @Test func checksEachProductInOrderWithAGapBetweenRequests() async {
        let rig = rig()
        rig.fetcher.answers = ["1000001": page("1000001"), "1000002": page("1000002", status: 404)]
        var progress: [Int] = []

        let results = await rig.check.check(["1000001", "1000002", "1000001"], refreshing: []) { done, _ in
            progress.append(done)
        }

        #expect(rig.fetcher.fetched == ["1000001", "1000002"])
        #expect(results["1000001"]?.status == .found)
        #expect(results["1000002"]?.status == .gone)
        #expect(rig.clock.slept == [0.4])
        #expect(progress == [0, 1, 2])
        #expect(rig.renderer.rendered.isEmpty)
        #expect(rig.renderer.finished == 1)
    }

    @Test func aBlockedRequestFallsBackOnceToTheWebViewAndStaysThere() async {
        let rig = rig()
        rig.fetcher.answers = ["1000001": blocked, "1000002": page("1000002")]
        rig.renderer.pages = ["1000001": rendered("1000001"), "1000002": rendered("1000002")]

        let results = await rig.check.check(["1000001", "1000002"], refreshing: []) { _, _ in }

        #expect(results["1000001"]?.status == .found)
        #expect(results["1000001"]?.name == "Rendered 1000001")
        // After one block, later pages go straight to the web view.
        #expect(rig.fetcher.fetched == ["1000001"])
        #expect(rig.renderer.rendered == ["1000001", "1000002"])
        #expect(results["1000002"]?.status == .found)
    }

    @Test func aFailedRequestAndAnUnreadableWebViewPageAreUnknownNeverFound() async {
        let rig = rig()
        rig.fetcher.answers = ["1000002": page("1000002", html: "<html>changed page</html>")]
        rig.renderer.pages = [
            "1000002": WalmartRenderedPage(
                statusCode: 200,
                finalURL: URL(string: "https://www.walmart.com/ip/1000002") ?? URL(fileURLWithPath: "/"),
                fields: nil)
        ]

        let results = await rig.check.check(["1000001", "1000002"], refreshing: []) { _, _ in }

        // Each unreadable page is tried once in the web view.
        #expect(rig.renderer.rendered == ["1000001", "1000002"])
        #expect(results["1000001"]?.status == .unknown)
        #expect(results["1000002"]?.status == .unknown)
        #expect(results["1000002"]?.detail == .noPageData)
    }

    @Test func aBlockInTheWebViewTooStopsTheRun() async {
        let rig = rig()
        rig.fetcher.answers = ["1000001": blocked]
        let blockedPage = WalmartRenderedPage(
            statusCode: 200, finalURL: URL(string: "https://www.walmart.com/blocked?x") ?? URL(fileURLWithPath: "/"),
            fields: nil)
        rig.renderer.pages = ["1000001": blockedPage]

        let results = await rig.check.check(["1000001", "1000002", "1000003"], refreshing: []) { _, _ in }

        #expect(rig.fetcher.fetched == ["1000001"])
        #expect(rig.renderer.rendered == ["1000001"])
        #expect(results.values.allSatisfy { $0.status == .unknown && $0.detail == .blocked })
        #expect(results.count == 3)
    }

    @Test func productsPastTheTimeBudgetAreUnknown() async {
        var timing = WalmartProductCheck.Timing()
        timing.budget = 12
        timing.gap = 2
        let rig = rig(timing: timing)
        rig.fetcher.cost = 5
        rig.fetcher.answers = ["1000001": page("1000001"), "1000002": page("1000002"), "1000003": page("1000003")]

        let results = await rig.check.check(["1000001", "1000002", "1000003"], refreshing: []) { _, _ in }

        // 5 s, a 2 s gap, then 5 s: the budget is spent before the third.
        #expect(results["1000001"]?.status == .found)
        #expect(results["1000002"]?.status == .found)
        #expect(results["1000003"]?.status == .unknown)
        #expect(results["1000003"]?.detail == .outOfTime)
        #expect(rig.fetcher.fetched == ["1000001", "1000002"])
        // No request is given longer than the budget has left.
        #expect(rig.fetcher.timeouts == [8, 5])
    }

    @Test func conclusiveResultsAreCachedForAnHourAndCheckAgainSkipsTheCache() async {
        let rig = rig()
        rig.fetcher.answers = ["1000001": page("1000001"), "1000002": page("1000002", status: 404)]

        _ = await rig.check.check(["1000001", "1000002", "1000003"], refreshing: []) { _, _ in }
        #expect(rig.fetcher.fetched == ["1000001", "1000002", "1000003"])

        // Reopening: found and gone come from the cache; unknown is fetched again.
        let again = await rig.check.check(["1000001", "1000002", "1000003"], refreshing: []) { _, _ in }
        #expect(rig.fetcher.fetched.suffix(1) == ["1000003"])
        #expect(again["1000001"]?.status == .found)
        #expect(again["1000002"]?.status == .gone)

        // Check Again skips the cache for what it names.
        _ = await rig.check.check(["1000001"], refreshing: ["1000001"]) { _, _ in }
        #expect(rig.fetcher.fetched.last == "1000001")

        // After an hour, it's fetched again.
        let before = rig.fetcher.fetched.count
        rig.clock.advance(60 * 60 + 1)
        _ = await rig.check.check(["1000002"], refreshing: []) { _, _ in }
        #expect(rig.fetcher.fetched.count == before + 1)
    }

    @Test func theDefaultsCacheKeepsResultsAcrossInstances() throws {
        let defaults = try #require(UserDefaults(suiteName: "WalmartProductCheckTests"))
        defaults.removePersistentDomain(forName: "WalmartProductCheckTests")
        let date = Date(timeIntervalSince1970: 1_790_000_000)
        let first = UserDefaultsWalmartCheckCache(defaults: defaults, now: { date })
        first.store(WalmartProductResult(status: .gone, checkedAt: date), for: "1000001")
        let second = UserDefaultsWalmartCheckCache(defaults: defaults, now: { date })
        #expect(second.result(for: "1000001")?.status == .gone)
        #expect(second.result(for: "1000002") == nil)
        defaults.removePersistentDomain(forName: "WalmartProductCheckTests")
    }
}

struct ProductCheckReviewTests {
    private let date = Date(timeIntervalSince1970: 1_790_000_000)

    private func line(_ key: String, _ status: WalmartProductStatus) -> ProductCheckLine {
        ProductCheckLine(
            ingredientKey: key, name: key.capitalized, productID: "id-\(key)", productName: "Product \(key)",
            productURL: nil, result: WalmartProductResult(status: status, checkedAt: date))
    }

    @Test func walmartStaysClosedUntilEveryGoneAndUnknownLineIsDecided() {
        var review = ProductCheckReview(lines: [
            line("beef", .found), line("onion", .gone), line("beans", .unknown), line("milk", .unavailable),
        ])
        #expect(!review.canOpen)
        #expect(review.needsDecision.map(\.ingredientKey) == ["onion", "beans"])
        #expect(review.unavailable.map(\.ingredientKey) == ["milk"])

        review.decide(.leaveOut, for: "onion")
        #expect(!review.canOpen)
        review.decide(.sendAnyway, for: "beans")
        #expect(review.canOpen)
        #expect(review.sendsAnything)

        review.undecide("beans")
        #expect(!review.canOpen)
    }

    @Test func aGoneProductCanNeverBeSentAnyway() {
        var review = ProductCheckReview(lines: [line("onion", .gone)])
        let accepted = review.decide(.sendAnyway, for: "onion")
        #expect(!accepted)
        #expect(!review.canOpen)
        #expect(review.reports.first?.decision == nil)

        review.decide(.leaveOut, for: "onion")
        #expect(review.canOpen)
        #expect(!review.sendsAnything)
        let report = review.reports.first
        #expect(report?.status == .gone)
        #expect(report?.decision == .leaveOut)
    }

    @Test func sendAnywaySendsAnUnknownProductAndSaysSo() {
        var review = ProductCheckReview(lines: [line("beans", .unknown)])
        #expect(!review.sendsAnything)
        review.decide(.sendAnyway, for: "beans")
        #expect(review.sendsAnything)
        #expect(
            review.reports == [
                ShoppingProductCheckReport(
                    ingredientKey: "beans", productID: "id-beans", status: .unknown, checkedAt: date,
                    decision: .sendAnyway)
            ])
    }

    @Test func checkingAgainDropsADecisionMadeAboutAnotherAnswer() {
        var review = ProductCheckReview(lines: [line("beans", .unknown), line("rice", .unknown)])
        review.decide(.sendAnyway, for: "beans")
        review.decide(.leaveOut, for: "rice")

        review.update(WalmartProductResult(status: .gone, checkedAt: date), for: "beans")
        review.update(WalmartProductResult(status: .unknown, checkedAt: date), for: "rice")

        #expect(review.decisions["beans"] == nil)
        #expect(review.decisions["rice"] == .leaveOut)
        #expect(review.needsDecision.map(\.ingredientKey) == ["beans"])
    }

    @Test func aReChosenProductStartsOverAndTheRestKeepTheirAnswers() {
        var review = ProductCheckReview(lines: [line("onion", .gone), line("beef", .found)])
        review.decide(.leaveOut, for: "onion")
        var replaced = line("onion", .unknown)
        replaced = ProductCheckLine(
            ingredientKey: "onion", name: "Onion", productID: "id-new", productName: "New onions", productURL: nil,
            result: replaced.result)

        let next = review.replacingLines([replaced, line("beef", .unknown)])

        #expect(next.decisions["onion"] == nil)
        #expect(next.lines.first { $0.ingredientKey == "beef" }?.result.status == .found)
        #expect(next.lines.first { $0.ingredientKey == "onion" }?.productID == "id-new")
    }
}
