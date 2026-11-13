import Foundation
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

    /// Loads the page: a plain request first, then, for a site that wants a real browser,
    /// a hidden web view. Returns the page trimmed for sending.
    @Sendable static func load(_ link: String) async throws -> String {
        do {
            return try await fetch(link)
        } catch {
            guard let url = URL(string: link) else { throw error }
            let page = try await WebPageReader.read(url)
            return trimmed(page)
        }
    }

    /// Fetches the page with a browser's headers.
    static func fetch(_ link: String) async throws -> String {
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

/// Opens a page in a hidden, private web view and reads its HTML once it has loaded. No
/// cookies or website data are kept, and the view is thrown away afterwards.
@MainActor
private final class WebPageReader: NSObject, WKNavigationDelegate {
    private let webView: WKWebView
    private var continuation: CheckedContinuation<String, any Error>?
    private var timeout: Task<Void, Never>?

    private override init() {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.mediaTypesRequiringUserActionForPlayback = .all
        webView = WKWebView(frame: CGRect(x: 0, y: 0, width: 390, height: 844), configuration: configuration)
        super.init()
        webView.navigationDelegate = self
    }

    static func read(_ url: URL) async throws -> String {
        let reader = WebPageReader()
        return try await withCheckedThrowingContinuation { continuation in
            reader.continuation = continuation
            reader.webView.load(URLRequest(url: url, timeoutInterval: 20))
            reader.timeout = Task { @MainActor [weak reader] in
                try? await Task.sleep(for: .seconds(25))
                reader?.finish(.failure(URLError(.timedOut)))
            }
        }
    }

    nonisolated func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        Task { @MainActor in
            // A bot check may reload the page once it passes; give it a moment.
            try? await Task.sleep(for: .seconds(1.5))
            let html = try? await webView.evaluateJavaScript("document.documentElement.outerHTML") as? String
            if let html, !html.isEmpty {
                finish(.success(html))
            } else {
                finish(.failure(URLError(.cannotParseResponse)))
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

    private func finish(_ result: Result<String, any Error>) {
        guard let continuation else { return }
        self.continuation = nil
        timeout?.cancel()
        webView.stopLoading()
        continuation.resume(with: result)
    }
}
