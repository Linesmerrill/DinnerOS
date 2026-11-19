import Foundation
import UIKit
import WebKit

/// Loads a recipe page on this phone, for a site that turns the server away. The page's HTML
/// is sent to the server, which does the parsing. A site that only answers a real browser is
/// opened in a hidden private web view, the way Safari would show it.
nonisolated enum RecipePageLoader {
    /// The most HTML sent to the server; its request limit is a little over 1 MB.
    static let maxBytes = 900_000

    /// The first web address in what was pasted ("Look at this: https://…"), or the text
    /// itself, trimmed.
    static func link(in text: String) -> String {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue),
            let match = detector.firstMatch(in: trimmed, range: NSRange(trimmed.startIndex..., in: trimmed)),
            let url = match.url, url.scheme == "https" || url.scheme == "http"
        {
            return url.absoluteString
        }
        return trimmed
    }

    /// Opens the page in a hidden web view, waits for its scripts to put the recipe on the
    /// page, and returns the page as rendered, trimmed for sending. Store sites like Kroger's
    /// only have the recipe after this; their raw page is an empty shell.
    @Sendable static func render(_ link: String) async throws -> String {
        guard let url = URL(string: link), url.scheme == "https" || url.scheme == "http" else {
            throw URLError(.badURL)
        }
        let page = try await WebPageReader.read(url)
        return trimmed(page)
    }

    /// Fetches the page with a browser's headers.
    @Sendable static func fetch(_ link: String) async throws -> String {
        guard let url = URL(string: link), url.scheme == "https" || url.scheme == "http" else {
            throw URLError(.badURL)
        }
        var request = URLRequest(url: url, timeoutInterval: 15)
        request.setValue(
            "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
            forHTTPHeaderField: "User-Agent")
        request.setValue("text/html,application/xhtml+xml", forHTTPHeaderField: "Accept")
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpShouldSetCookies = false
        let (data, response) = try await URLSession(configuration: configuration).data(for: request)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let page = String(decoding: data, as: UTF8.self)
        return trimmed(page)
    }

    /// The page without what the server never reads (scripts other than structured data,
    /// styles, inline images, comments), cut to `maxBytes`.
    static func trimmed(_ page: String) -> String {
        var out = page
        for pattern in [
            #"(?is)<script(?![^>]*application/ld\+json)[^>]*>.*?</script>"#,
            #"(?is)<style[^>]*>.*?</style>"#,
            #"(?is)<svg[^>]*>.*?</svg>"#,
            #"(?s)<!--.*?-->"#,
            #"(?is)<noscript[^>]*>.*?</noscript>"#,
        ] {
            out = out.replacingOccurrences(of: pattern, with: "", options: .regularExpression)
        }
        guard out.utf8.count > maxBytes else { return out }
        let cut = out.utf8.prefix(maxBytes)
        return String(decoding: cut, as: UTF8.self)
    }
}

/// Opens a page in a hidden, private web view and reads its HTML once the recipe is on it. No
/// cookies or website data are kept, and the view is thrown away afterwards.
@MainActor
final class WebPageReader: NSObject, WKNavigationDelegate {
    /// How long to wait, after the page loads, for its scripts to show the recipe.
    static let renderWait: Duration = .seconds(15)
    static let pollEvery: Duration = .milliseconds(500)

    /// True once the page shows a recipe the server can read: schema.org data, a recipe
    /// card, or list items marked as ingredients (decision 728).
    static let recipeReadyScript = """
        (() => {
          for (const s of document.querySelectorAll('script[type="application/ld+json"]')) {
            if (/"@type"\\s*:\\s*(\\[[^\\]]*)?"Recipe"/.test(s.textContent)) return true;
          }
          if (document.querySelector('[itemprop="recipeIngredient"], [itemprop="ingredients"], ' +
              '.wprm-recipe-container, .tasty-recipes, .mv-create-card')) return true;
          const scope = document.querySelector('[itemtype*="schema.org/recipe" i]');
          const items = scope
            ? scope.querySelectorAll('li[data-testid*="ingredient" i], li[class*="ingredient" i], li[itemprop*="ingredient" i]')
            : document.querySelectorAll('li[data-testid*="recipe" i][data-testid*="ingredient" i]');
          return items.length > 0;
        })()
        """

    private let webView: WKWebView
    private var continuation: CheckedContinuation<String, any Error>?
    private var timeout: Task<Void, Never>?
    private var polling: Task<Void, Never>?

    private override init() {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.mediaTypesRequiringUserActionForPlayback = .all
        // Name it the way Safari does; some sites turn away a web view that doesn't.
        configuration.applicationNameForUserAgent = "Version/26.0 Mobile/15E148 Safari/604.1"
        webView = WKWebView(frame: CGRect(x: 0, y: 0, width: 390, height: 844), configuration: configuration)
        super.init()
        webView.navigationDelegate = self
    }

    static func read(_ url: URL) async throws -> String {
        try await read { $0.load(URLRequest(url: url, timeoutInterval: 20)) }
    }

    /// Reads a page given as HTML, the same way; for tests of the wait.
    static func read(html: String, baseURL: URL) async throws -> String {
        try await read { $0.loadHTMLString(html, baseURL: baseURL) }
    }

    private static func read(_ start: (WKWebView) -> Void) async throws -> String {
        let reader = WebPageReader()
        return try await withCheckedThrowingContinuation { continuation in
            reader.continuation = continuation
            reader.attachToWindow()
            start(reader.webView)
            reader.timeout = Task { @MainActor [weak reader] in
                try? await Task.sleep(for: .seconds(20) + renderWait)
                reader?.finish(.failure(URLError(.timedOut)))
            }
        }
    }

    /// A page that isn't there fails now rather than after the wait. Other errors stay: a
    /// bot check answers 403 and then reloads the real page once it passes.
    func webView(
        _ webView: WKWebView, decidePolicyFor navigationResponse: WKNavigationResponse
    ) async -> WKNavigationResponsePolicy {
        if navigationResponse.isForMainFrame, let http = navigationResponse.response as? HTTPURLResponse,
            [404, 410].contains(http.statusCode)
        {
            finish(.failure(URLError(.fileDoesNotExist)))
            return .cancel
        }
        return .allow
    }

    /// Once the page has loaded, checks every half second until its scripts have put the
    /// recipe on it, for up to `renderWait`, then sends what's there either way. A bot check
    /// that reloads the page finishes again; the same wait carries on.
    nonisolated func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        Task { @MainActor [self] in
            guard polling == nil else { return }
            polling = Task { @MainActor [weak self] in
                let clock = ContinuousClock()
                let deadline = clock.now + Self.renderWait
                while clock.now < deadline, !Task.isCancelled {
                    if let ready = try? await webView.evaluateJavaScript(Self.recipeReadyScript) as? Bool, ready {
                        break
                    }
                    try? await Task.sleep(for: Self.pollEvery)
                }
                let html = try? await webView.evaluateJavaScript("document.documentElement.outerHTML") as? String
                if let html, !html.isEmpty {
                    self?.finish(.success(html))
                } else {
                    self?.finish(.failure(URLError(.cannotParseResponse)))
                }
            }
        }
    }

    nonisolated func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        Task { @MainActor in finish(.failure(error)) }
    }

    nonisolated func webView(
        _ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error
    ) {
        Task { @MainActor in finish(.failure(error)) }
    }

    /// The page's own script crashed the web content process: the page didn't load.
    nonisolated func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        Task { @MainActor in finish(.failure(URLError(.cannotLoadFromNetwork))) }
    }

    private func finish(_ result: Result<String, any Error>) {
        guard let continuation else { return }
        self.continuation = nil
        timeout?.cancel()
        polling?.cancel()
        webView.stopLoading()
        webView.removeFromSuperview()
        continuation.resume(with: result)
    }

    /// WebKit slows the scripts of a web view that isn't in a window, so a page that builds its
    /// recipe with a timer or a late request may never show it. The view goes in the app's
    /// window, behind everything, invisible and untouchable.
    private func attachToWindow() {
        let windows = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
        let window = windows.first { $0.isKeyWindow } ?? windows.first
        guard let window else { return }
        webView.alpha = 0.01
        webView.isUserInteractionEnabled = false
        webView.accessibilityElementsHidden = true
        window.insertSubview(webView, at: 0)
    }
}
