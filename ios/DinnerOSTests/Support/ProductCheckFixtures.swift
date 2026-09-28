import Foundation

@testable import DinnerOS

/// Answers product checks from a table, as the phone's Walmart check would. Items not in the
/// table are found.
final class StubProductCheck: WalmartProductChecking {
    var statuses: [String: WalmartProductStatus] = [:]
    /// Each call's item IDs and the ones it was asked to refresh.
    private(set) var calls: [(ids: [String], refreshing: Set<String>)] = []

    func check(_ itemIDs: [String], refreshing: Set<String>, progress: @escaping (Int, Int) -> Void) async
        -> [String: WalmartProductResult]
    {
        calls.append((itemIDs, refreshing))
        let date = Date(timeIntervalSince1970: 1_790_000_000)
        var out: [String: WalmartProductResult] = [:]
        for (index, id) in itemIDs.enumerated() {
            progress(index, itemIDs.count)
            let status = statuses[id] ?? .found
            out[id] = WalmartProductResult(
                status: status, detail: status == .unknown ? .blocked : nil,
                name: status == .found || status == .unavailable ? "Walmart \(id)" : nil,
                priceCents: status == .found ? 348 : nil, checkedAt: date)
        }
        return out
    }
}

/// Synthesized `__NEXT_DATA__` pages. Minimal, made-up snippets: never captured page HTML.
nonisolated enum WalmartPageFixtures {
    static func page(nextData: String) -> String {
        #"<html><head></head><body><div id="__next"></div><script id="__NEXT_DATA__" type="application/json">"#
            + nextData + "</script></body></html>"
    }

    static func product(
        id: String = "100000001", name: String? = "Test Onions, 3 lb Bag", price: String? = "$3.48",
        fulfillment: String? =
            #"[{"type":"SHIPPING","availabilityStatus":"IN_STOCK"},{"type":"PICKUP","availabilityStatus":"IN_STOCK"},{"type":"DELIVERY","availabilityStatus":"IN_STOCK"}]"#,
        availability: String = "IN_STOCK"
    ) -> String {
        var fields = [#""usItemId":"\#(id)""#, #""availabilityStatus":"\#(availability)""#]
        if let name { fields.append(#""name":"\#(name)""#) }
        if let price { fields.append(#""priceInfo":{"currentPrice":{"priceString":"\#(price)","price":3.48}}"#) }
        if let fulfillment { fields.append(#""fulfillmentOptions":\#(fulfillment)"#) }
        return #"{"props":{"pageProps":{"initialData":{"data":{"product":{"# + fields.joined(separator: ",")
            + "}}}}}}"
    }
}

/// A plain fetcher that answers from a list and moves a fake clock forward per request.
nonisolated final class StubPageFetcher: WalmartPageFetching, @unchecked Sendable {
    var answers: [String: WalmartPageAnswer] = [:]
    var cost: TimeInterval = 0.5
    let clock: FakeCheckClock
    private(set) var fetched: [String] = []
    private(set) var timeouts: [TimeInterval] = []

    init(clock: FakeCheckClock) {
        self.clock = clock
    }

    func fetch(_ url: URL, timeout: TimeInterval) async -> WalmartPageAnswer {
        let id = url.lastPathComponent
        fetched.append(id)
        timeouts.append(timeout)
        guard cost <= timeout else {
            clock.advance(timeout)
            return .failed
        }
        clock.advance(cost)
        return answers[id] ?? .failed
    }
}

/// A web view stand-in behind `WalmartPageRendering`.
final class StubPageRenderer: WalmartPageRendering {
    var pages: [String: WalmartRenderedPage] = [:]
    var cost: TimeInterval = 1
    let clock: FakeCheckClock
    private(set) var rendered: [String] = []
    private(set) var finished = 0

    init(clock: FakeCheckClock) {
        self.clock = clock
    }

    func render(_ url: URL, timeout: TimeInterval) async -> WalmartRenderedPage? {
        let id = url.lastPathComponent
        rendered.append(id)
        clock.advance(min(cost, timeout))
        return pages[id]
    }

    func finish() {
        finished += 1
    }
}

/// A clock the stubs move forward, so the time budget can be tested without waiting.
nonisolated final class FakeCheckClock: @unchecked Sendable {
    private(set) var now = Date(timeIntervalSince1970: 1_790_000_000)
    private(set) var slept: [TimeInterval] = []

    func advance(_ seconds: TimeInterval) {
        now = now.addingTimeInterval(seconds)
    }

    func sleep(_ seconds: TimeInterval) {
        slept.append(seconds)
        advance(seconds)
    }
}
