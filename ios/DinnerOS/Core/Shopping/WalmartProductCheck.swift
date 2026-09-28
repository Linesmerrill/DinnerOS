import Foundation
import WebKit
import os

/// A fetched Walmart page, or a failure to get one.
nonisolated enum WalmartPageAnswer: Sendable, Equatable {
    case page(statusCode: Int, finalURL: URL, html: String)
    case failed
}

/// Fetches one Walmart page over plain HTTP. `URLSessionWalmartFetcher` in the app, a stub in tests.
nonisolated protocol WalmartPageFetching: Sendable {
    /// Returns within about `timeout` seconds.
    func fetch(_ url: URL, timeout: TimeInterval) async -> WalmartPageAnswer
}

/// A page the web view loaded: the main document's status code, the URL after redirects, and the
/// product fields its script read (`nil` when the page had no `__NEXT_DATA__`).
nonisolated struct WalmartRenderedPage: Sendable, Equatable {
    let statusCode: Int
    let finalURL: URL
    let fields: WalmartPageFields?
}

/// Loads one Walmart page in a real browser engine, for when a plain request is refused.
/// `WebViewWalmartRenderer` in the app, a stub in tests.
protocol WalmartPageRendering: AnyObject {
    /// Returns within about `timeout` seconds; `nil` when the page didn't load.
    func render(_ url: URL, timeout: TimeInterval) async -> WalmartRenderedPage?
    /// The run is over: release what the renderer holds.
    func finish()
}

/// Remembers conclusive results on the device for a while, so reopening the sheet doesn't fetch
/// again.
protocol WalmartCheckCache: AnyObject {
    func result(for itemID: String) -> WalmartProductResult?
    func store(_ result: WalmartProductResult, for itemID: String)
}

/// Checks saved products on Walmart. `WalmartProductCheck` in the app, a stub in store tests.
protocol WalmartProductChecking: AnyObject {
    /// Checks each item once, in order, and returns a result for every one. `refreshing` skips
    /// the cache for those items (Check Again). `progress` gets (checked, total) as it goes.
    func check(_ itemIDs: [String], refreshing: Set<String>, progress: @escaping (Int, Int) -> Void) async
        -> [String: WalmartProductResult]
}

/// Checks the saved products a cart will send on Walmart, from the member's own phone, right
/// before the hand-off (docs/shopping-providers.md#checking-saved-products). The server never
/// fetches Walmart: its requests were blocked after a handful of calls.
///
/// One product at a time, a short gap between requests, and a total time budget, since the
/// member is waiting. Each page is fetched with the headers Safari sends. When that request is
/// blocked or refused, the same page is loaded once in an offscreen web view, the engine the
/// member's own Walmart browsing uses, and after one block every later page goes straight there.
/// A block there too stops the run. Whatever is still unreadable is `unknown`, never `found`.
final class WalmartProductCheck: WalmartProductChecking {
    struct Timing: Equatable {
        /// The whole run, in seconds. Products it doesn't reach are `unknown`.
        var budget: TimeInterval = 20
        /// Between two requests.
        var gap: TimeInterval = 0.4
        var requestTimeout: TimeInterval = 8
        var renderTimeout: TimeInterval = 10
        /// How long a conclusive result is reused.
        var cacheLife: TimeInterval = 60 * 60
    }

    private let fetcher: any WalmartPageFetching
    private let renderer: any WalmartPageRendering
    private let cache: any WalmartCheckCache
    private let timing: Timing
    private let now: () -> Date
    private let sleep: (TimeInterval) async -> Void

    private static let logger = Logger(subsystem: "DinnerOS", category: "shopping")

    init(
        fetcher: any WalmartPageFetching = URLSessionWalmartFetcher(),
        renderer: any WalmartPageRendering = WebViewWalmartRenderer(),
        cache: any WalmartCheckCache = UserDefaultsWalmartCheckCache(), timing: Timing = Timing(),
        now: @escaping () -> Date = Date.init,
        sleep: @escaping (TimeInterval) async -> Void = { try? await Task.sleep(for: .seconds($0)) }
    ) {
        self.fetcher = fetcher
        self.renderer = renderer
        self.cache = cache
        self.timing = timing
        self.now = now
        self.sleep = sleep
    }

    func check(_ itemIDs: [String], refreshing: Set<String>, progress: @escaping (Int, Int) -> Void) async
        -> [String: WalmartProductResult]
    {
        let deadline = now().addingTimeInterval(timing.budget)
        var results: [String: WalmartProductResult] = [:]
        var toFetch: [String] = []
        for id in itemIDs where results[id] == nil && !toFetch.contains(id) {
            if !refreshing.contains(id), let cached = cache.result(for: id),
                now().timeIntervalSince(cached.checkedAt) < timing.cacheLife
            {
                results[id] = cached
            } else {
                toFetch.append(id)
            }
        }
        defer { renderer.finish() }

        var browserOnly = false
        var stopped = false
        for (index, id) in toFetch.enumerated() {
            progress(index, toFetch.count)
            if stopped {
                results[id] = .unknown(.blocked, at: now())
                continue
            }
            if index > 0, deadline.timeIntervalSince(now()) > timing.gap {
                await sleep(timing.gap)
            }
            guard let url = WalmartProductPage.url(itemID: id) else {
                results[id] = .unknown(.noProduct, at: now())
                continue
            }
            var result: WalmartProductResult?
            if !browserOnly {
                let remaining = deadline.timeIntervalSince(now())
                guard remaining > 0 else {
                    results[id] = .unknown(.outOfTime, at: now())
                    continue
                }
                result = read(id, fetcher: await fetcher.fetch(url, timeout: min(timing.requestTimeout, remaining)))
            }
            if result == nil || result?.status == .unknown {
                // Fall back once to the browser engine for this page.
                if result?.detail == .blocked { browserOnly = true }
                let remaining = deadline.timeIntervalSince(now())
                if remaining > 0 {
                    let page = await renderer.render(url, timeout: min(timing.renderTimeout, remaining))
                    result = read(id, rendered: page)
                    if result?.detail == .blocked { stopped = true }
                } else if result == nil {
                    result = .unknown(.outOfTime, at: now())
                }
            }
            let final = result ?? .unknown(.network, at: now())
            if final.status != .unknown {
                cache.store(final, for: id)
            }
            results[id] = final
        }
        progress(toFetch.count, toFetch.count)
        let unknown = toFetch.filter { results[$0]?.status == .unknown }.count
        Self.logger.info(
            """
            Walmart product check: \(toFetch.count, privacy: .public) fetched, \
            \(itemIDs.count - toFetch.count, privacy: .public) cached, \(unknown, privacy: .public) unknown, \
            browser \(browserOnly, privacy: .public), stopped \(stopped, privacy: .public)
            """)
        return results
    }

    private func read(_ id: String, fetcher answer: WalmartPageAnswer) -> WalmartProductResult {
        switch answer {
        case .failed:
            return .unknown(.network, at: now())
        case .page(let statusCode, let finalURL, let html):
            return WalmartProductPage.parse(
                itemID: id, statusCode: statusCode, finalURL: finalURL, html: html, at: now())
        }
    }

    private func read(_ id: String, rendered page: WalmartRenderedPage?) -> WalmartProductResult {
        guard let page else { return .unknown(.network, at: now()) }
        return WalmartProductPage.parse(
            itemID: id, statusCode: page.statusCode, finalURL: page.finalURL, fields: page.fields, at: now())
    }
}

// MARK: - Plain requests

/// Fetches a Walmart page with the headers Safari on this iPhone sends. Redirects are followed
/// only within walmart.com.
nonisolated struct URLSessionWalmartFetcher: WalmartPageFetching {
    private let session: URLSession
    private let userAgent: String

    init(session: URLSession = .shared) {
        self.session = session
        let os = ProcessInfo.processInfo.operatingSystemVersion
        let version = "\(os.majorVersion)_\(os.minorVersion)"
        userAgent =
            "Mozilla/5.0 (iPhone; CPU iPhone OS \(version) like Mac OS X) AppleWebKit/605.1.15 "
            + "(KHTML, like Gecko) Version/\(os.majorVersion).\(os.minorVersion) Mobile/15E148 Safari/604.1"
    }

    func fetch(_ url: URL, timeout: TimeInterval) async -> WalmartPageAnswer {
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
        request.setValue(userAgent, forHTTPHeaderField: "User-Agent")
        request.setValue(
            "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", forHTTPHeaderField: "Accept")
        request.setValue("en-US,en;q=0.9", forHTTPHeaderField: "Accept-Language")
        let session = session
        let finalRequest = request
        return await withTimeLimit(timeout, otherwise: .failed) {
            do {
                let (data, response) = try await session.data(for: finalRequest, delegate: WalmartRedirects())
                guard let http = response as? HTTPURLResponse, let finalURL = http.url else { return .failed }
                return .page(
                    statusCode: http.statusCode, finalURL: finalURL, html: String(decoding: data, as: UTF8.self))
            } catch {
                return .failed
            }
        }
    }
}

/// Follows a redirect only when it stays on walmart.com.
nonisolated private final class WalmartRedirects: NSObject, URLSessionTaskDelegate {
    func urlSession(
        _ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void
    ) {
        guard let url = request.url, WalmartProductPage.isWalmart(url) else {
            completionHandler(nil)
            return
        }
        completionHandler(request)
    }
}

/// Runs `operation`, or gives `fallback` once `seconds` pass, cancelling the operation.
nonisolated func withTimeLimit<T: Sendable>(
    _ seconds: TimeInterval, otherwise fallback: T, _ operation: @escaping @Sendable () async -> T
) async -> T {
    await withTaskGroup(of: T.self) { group in
        group.addTask { await operation() }
        group.addTask {
            try? await Task.sleep(for: .seconds(seconds))
            return fallback
        }
        let first = await group.next() ?? fallback
        group.cancelAll()
        return first
    }
}

// MARK: - Browser fallback

/// Loads a Walmart page in an offscreen `WKWebView` and reads only the product fields DinnerOS
/// needs from its `__NEXT_DATA__`. It uses the default website data store, the one the member's
/// own browsing in the app would, and never leaves walmart.com.
final class WebViewWalmartRenderer: NSObject, WalmartPageRendering, WKNavigationDelegate {
    private var webView: WKWebView?
    private var continuation: CheckedContinuation<Bool, Never>?
    private var statusCode: Int?
    /// Counts loads, so a timeout left over from an earlier page can't end this one.
    private var loadID = 0

    /// Reads `props.pageProps.initialData.data.product` and returns only the fields
    /// `WalmartPageFields` holds, as JSON, or `null` without `__NEXT_DATA__`.
    static let script = """
        (() => {
          const el = document.getElementById('__NEXT_DATA__');
          if (!el) { return null; }
          let data;
          try { data = JSON.parse(el.textContent); } catch (e) { return null; }
          const p = data && data.props && data.props.pageProps && data.props.pageProps.initialData
            && data.props.pageProps.initialData.data && data.props.pageProps.initialData.data.product;
          if (!p) { return JSON.stringify({}); }
          const price = p.priceInfo && p.priceInfo.currentPrice && p.priceInfo.currentPrice.priceString;
          const options = Array.isArray(p.fulfillmentOptions)
            ? p.fulfillmentOptions.map(f => ({
                type: typeof f.type === 'string' ? f.type : null,
                availabilityStatus: typeof f.availabilityStatus === 'string' ? f.availabilityStatus : null }))
            : null;
          return JSON.stringify({
            usItemId: p.usItemId == null ? null : String(p.usItemId),
            name: typeof p.name === 'string' ? p.name : null,
            availabilityStatus: typeof p.availabilityStatus === 'string' ? p.availabilityStatus : null,
            priceString: typeof price === 'string' ? price : null,
            fulfillmentOptions: options });
        })()
        """

    func render(_ url: URL, timeout: TimeInterval) async -> WalmartRenderedPage? {
        let webView = self.webView ?? makeWebView()
        statusCode = nil
        loadID += 1
        let load = loadID
        let loaded = await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
            self.continuation = continuation
            webView.load(URLRequest(url: url, timeoutInterval: timeout))
            Task { [weak self] in
                try? await Task.sleep(for: .seconds(timeout))
                guard let self, self.loadID == load else { return }
                self.complete(false)
            }
        }
        guard loaded, let finalURL = webView.url else {
            webView.stopLoading()
            return nil
        }
        var fields: WalmartPageFields?
        if let json = try? await webView.evaluateJavaScript(Self.script) as? String, let data = json.data(using: .utf8)
        {
            fields = try? JSONDecoder().decode(WalmartPageFields.self, from: data)
        }
        return WalmartRenderedPage(statusCode: statusCode ?? 0, finalURL: finalURL, fields: fields)
    }

    func finish() {
        complete(false)
        webView?.stopLoading()
        webView?.navigationDelegate = nil
        webView = nil
    }

    private func makeWebView() -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        let webView = WKWebView(frame: CGRect(x: 0, y: 0, width: 390, height: 844), configuration: configuration)
        webView.navigationDelegate = self
        self.webView = webView
        return webView
    }

    private func complete(_ loaded: Bool) {
        continuation?.resume(returning: loaded)
        continuation = nil
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async
        -> WKNavigationActionPolicy
    {
        // Subresources and frames load as the page wants; the page itself stays on walmart.com,
        // and nothing opens a new window.
        guard let frame = navigationAction.targetFrame else { return .cancel }
        guard frame.isMainFrame else { return .allow }
        guard let url = navigationAction.request.url, WalmartProductPage.isWalmart(url) else { return .cancel }
        return .allow
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationResponse: WKNavigationResponse) async
        -> WKNavigationResponsePolicy
    {
        if navigationResponse.isForMainFrame, let http = navigationResponse.response as? HTTPURLResponse {
            statusCode = http.statusCode
        }
        return .allow
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation?) {
        complete(true)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation?, withError error: any Error) {
        complete(false)
    }

    func webView(
        _ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation?, withError error: any Error
    ) {
        complete(false)
    }
}

// MARK: - Cache

/// Keeps conclusive results in `UserDefaults`, so they last across launches for their short life.
/// Entries older than a day are dropped on write.
final class UserDefaultsWalmartCheckCache: WalmartCheckCache {
    private let defaults: UserDefaults
    private let key: String
    private let now: () -> Date

    init(
        defaults: UserDefaults = .standard, key: String = "shopping.walmartProductChecks",
        now: @escaping () -> Date = Date.init
    ) {
        self.defaults = defaults
        self.key = key
        self.now = now
    }

    func result(for itemID: String) -> WalmartProductResult? {
        entries()[itemID]
    }

    func store(_ result: WalmartProductResult, for itemID: String) {
        var entries = entries().filter { now().timeIntervalSince($0.value.checkedAt) < 24 * 60 * 60 }
        entries[itemID] = result
        if let data = try? JSONEncoder().encode(entries) {
            defaults.set(data, forKey: key)
        }
    }

    private func entries() -> [String: WalmartProductResult] {
        guard let data = defaults.data(forKey: key),
            let entries = try? JSONDecoder().decode([String: WalmartProductResult].self, from: data)
        else { return [:] }
        return entries
    }
}

/// Keeps results in memory only: for tests and previews.
final class InMemoryWalmartCheckCache: WalmartCheckCache {
    private var entries: [String: WalmartProductResult] = [:]

    func result(for itemID: String) -> WalmartProductResult? { entries[itemID] }
    func store(_ result: WalmartProductResult, for itemID: String) { entries[itemID] = result }
}
