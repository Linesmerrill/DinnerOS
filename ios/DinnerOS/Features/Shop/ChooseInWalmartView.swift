import SwiftUI
import WebKit

/// Choose products without leaving Walmart: Walmart's own site fills the screen, and a bar along
/// the top holds the list item being chosen for. Browsing to a product page lights up Use This
/// Product, which saves that page and searches for the next item. Nothing is copied or pasted.
///
/// The page is the member's own Walmart session in the app's web view; the app reads only the
/// address and the product's public name and price, never anything they type.
struct ChooseInWalmartView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var chooser: WalmartChooser
    @State private var page = WalmartBrowserPage()
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(items: [WalmartChooser.Item]) {
        _chooser = State(initialValue: WalmartChooser(items: items))
    }

    private var productID: String? { WalmartChooser.productItemID(at: page.url) }

    /// The product's name from its address, until the page itself is read.
    private var addressName: String? { page.url.flatMap { ProductLink.walmartProductName(inURL: $0.absoluteString) } }

    var body: some View {
        // In a navigation bar, so Done is the same pill as every other sheet's close button.
        NavigationStack {
            VStack(spacing: 0) {
                if let item = chooser.current {
                    itemBar(item)
                    Divider()
                    WalmartBrowser(page: page)
                        .ignoresSafeArea(edges: .bottom)
                        .overlay { pageState }
                } else {
                    finished
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbar }
        }
        // Choosing one product (a re-choose) goes straight back once it's saved.
        .onChange(of: chooser.isFinished) { _, finished in
            if finished, chooser.items.count == 1, !chooser.chosen.isEmpty { dismiss() }
        }
        .onChange(of: chooser.current?.id, initial: true) { _, _ in
            if let url = chooser.searchURL { page.load(url) }
        }
        .alert(
            "Couldn’t Save That",
            isPresented: Binding(get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(errorMessage ?? "")
        }
    }

    /// A blank page is never left on its own: it says it's loading, or that it didn't load and
    /// offers to try again.
    @ViewBuilder
    private var pageState: some View {
        switch page.state {
        case .loading:
            ProgressView("Opening Walmart…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DS.Colors.background)
        case .failed:
            ContentUnavailableView {
                Label("Walmart Didn’t Load", systemImage: "wifi.exclamationmark")
            } description: {
                Text("Try again in a moment.")
            } actions: {
                Button("Try Again") {
                    if let url = chooser.searchURL { page.load(url) }
                }
                .buttonStyle(.dsPrimary(.small))
            }
            .background(DS.Colors.background)
        case .shown:
            EmptyView()
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Done") { dismiss() }
        }
        // The title with how many are left under it, rather than a count on its own that looks
        // like a button.
        ToolbarItem(placement: .principal) {
            VStack(spacing: 0) {
                Text("Choose in Walmart")
                    .font(DS.Typography.headline)
                Text(chooser.remaining == 1 ? "1 left" : "\(chooser.remaining) left")
                    .font(DS.Typography.caption)
                    .foregroundStyle(DS.Colors.secondaryText)
                    .monospacedDigit()
            }
            .accessibilityElement(children: .combine)
        }
    }

    /// The list item, with Back and Skip, and the one button that matters: Use This Product, live
    /// only on a product page.
    private func itemBar(_ item: WalmartChooser.Item) -> some View {
        HStack(spacing: DS.Space.m) {
            Button("Back", systemImage: "chevron.left") { chooser.back() }
                .labelStyle(.iconOnly)
                .disabled(!chooser.canGoBack || isSaving)
                .frame(minWidth: DS.Size.tapTarget, minHeight: DS.Size.tapTarget)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(item.name)
                    .font(DS.Typography.headline)
                    .lineLimit(2)
                Text(
                    productID == nil
                        ? Self.subtitle(amountText: item.amountText)
                        : (page.productName ?? addressName ?? String(localized: "This product"))
                )
                .font(DS.Typography.footnote)
                .foregroundStyle(DS.Colors.secondaryText)
                .lineLimit(2)
                if !item.note.isEmpty {
                    Text("Your note: \(item.note)")
                        .font(DS.Typography.footnote.weight(.medium))
                        .foregroundStyle(DS.Colors.warning)
                        .lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            Button("Skip") { chooser.skip() }
                .buttonStyle(.dsTextLink)
                .disabled(isSaving)
            Button {
                use(item)
            } label: {
                if isSaving {
                    ProgressView()
                } else {
                    Label("Use This", systemImage: "checkmark")
                }
            }
            .buttonStyle(.dsPrimary(.small))
            .disabled(productID == nil || isSaving)
            .accessibilityLabel("Use This Product for \(item.name)")
        }
        .padding(.horizontal, DS.Space.m)
        .padding(.vertical, DS.Space.s)
        .background(DS.Colors.barBackground)
    }

    /// "Need 6 · Tap a product to pick it.": the amount says what it is, not a bare number
    /// (build 121).
    static func subtitle(amountText: String) -> String {
        let pick = String(localized: "Tap a product to pick it.")
        let amount = amountText.trimmingCharacters(in: .whitespaces)
        return amount.isEmpty ? pick : String(localized: "Need \(amount) · \(pick)")
    }

    private var finished: some View {
        ContentUnavailableView {
            Label("All Set", systemImage: "checkmark.circle")
        } description: {
            Text(
                chooser.chosen.isEmpty
                    ? "No products chosen."
                    : chooser.chosen.count == 1
                        ? "Saved 1 product. It goes in the cart next time."
                        : "Saved \(chooser.chosen.count) products. They go in the cart next time."
            )
        } actions: {
            Button("Done") { dismiss() }
                .buttonStyle(.dsPrimary(.small))
        }
    }

    private func use(_ item: WalmartChooser.Item) {
        guard let url = page.url else { return }
        isSaving = true
        Task {
            defer { isSaving = false }
            // The page's own name and price, read now if they weren't yet.
            if page.productName == nil { await page.readNow() }
            guard
                let request = WalmartChooser.request(
                    for: item, url: url, pageName: page.productName, priceText: page.priceText)
            else { return }
            do {
                _ = try await shopping.savePreference(ingredientKey: item.ingredientKey, request: request)
                chooser.didChoose()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// The browser's state the bar reads: the address, and on a product page, its name and price.
@MainActor @Observable
final class WalmartBrowserPage {
    enum State: Equatable { case loading, shown, failed }

    /// Loading until the first page paints, then shown; failed if it never could.
    private(set) var state: State = .loading
    private(set) var url: URL?
    private(set) var productName: String?
    private(set) var priceText: String?
    @ObservationIgnored fileprivate weak var webView: WKWebView?
    @ObservationIgnored private var pending: URL?

    func load(_ url: URL) {
        state = .loading
        productName = nil
        priceText = nil
        if let webView {
            webView.load(URLRequest(url: url))
        } else {
            pending = url
        }
    }

    fileprivate func attach(_ webView: WKWebView) {
        self.webView = webView
        if let pending {
            webView.load(URLRequest(url: pending))
            self.pending = nil
        }
    }

    func committed() {
        state = .shown
    }

    /// Walmart's own script crashed the page (it did in the iOS 26.2 simulator), leaving it blank
    /// even after it had shown. Say so and offer to try again.
    func crashed() {
        state = .failed
    }

    func failed(_ error: Error) {
        // A newer load replacing this one isn't a failure.
        if (error as NSError).code == NSURLErrorCancelled { return }
        if state != .shown { state = .failed }
    }

    /// Walmart is a single-page app: the address changes without a page load, so it's watched.
    fileprivate func addressChanged(_ url: URL?) {
        guard url != self.url else { return }
        self.url = url
        productName = nil
        priceText = nil
        guard WalmartChooser.productItemID(at: url) != nil else { return }
        readProduct()
    }

    /// Reads the product's name and price from the page as it is now.
    func readNow() async {
        guard let webView, let url, WalmartChooser.productItemID(at: url) != nil else { return }
        let found = await Self.read(webView, itemID: WalmartChooser.productItemID(at: url))
        guard self.url == url, let found else { return }
        productName = found.name
        priceText = found.price
    }

    /// The page's startup data when it describes this item, else the title and price on screen.
    private static func read(_ webView: WKWebView, itemID: String?) async -> (name: String, price: String?)? {
        if let json = try? await webView.evaluateJavaScript(WebViewWalmartRenderer.script) as? String,
            let data = json.data(using: .utf8),
            let fields = try? JSONDecoder().decode(WalmartPageFields.self, from: data),
            let name = fields.name, fields.usItemId == nil || fields.usItemId == itemID
        {
            return (name, fields.priceString)
        }
        if let json = try? await webView.evaluateJavaScript(WalmartOnScreen.script) as? String,
            let data = json.data(using: .utf8),
            let fields = try? JSONDecoder().decode(WalmartOnScreen.Fields.self, from: data),
            let name = fields.name, !name.isEmpty
        {
            return (name, fields.priceString)
        }
        return nil
    }

    /// The product's name and price from the page, read a moment after it settles; the name in
    /// the address stands in until then.
    fileprivate func readProduct() {
        guard let webView, let url else { return }
        Task { @MainActor [weak self] in
            for delay in [0.6, 1.5, 3] {
                try? await Task.sleep(for: .seconds(delay))
                guard let self, self.url == url else { return }
                if let found = await Self.read(webView, itemID: WalmartChooser.productItemID(at: url)) {
                    guard self.url == url else { return }
                    self.productName = found.name
                    self.priceText = found.price
                    return
                }
            }
        }
    }
}

/// Reads a product page's title and price from what's on screen. Walmart moves between pages
/// without reloading, so the page's startup data can still describe the search it came from.
private enum WalmartOnScreen {
    static let script = """
        (() => {
          const text = (el) => el ? el.textContent.replace(/\\s+/g, ' ').trim() : null;
          const name = text(document.querySelector('h1[itemprop="name"]')) || text(document.querySelector('main h1'))
            || text(document.querySelector('h1'));
          const priceEl = document.querySelector('[itemprop="price"]');
          const price = priceEl ? (priceEl.getAttribute('content') || text(priceEl)) : null;
          return JSON.stringify({ name: name, priceString: price });
        })()
        """

    struct Fields: Decodable {
        let name: String?
        let priceString: String?
    }
}

/// Hides the parts of Walmart's mobile site that cover the products: the "Get the app" banner,
/// the "Ship to another country" popover, and the chat button. CSS catches the known selectors;
/// a watcher also hides, by their words, the fixed or floating boxes they live in, in case
/// Walmart renames its classes. Anything missing is skipped, and an error never reaches the
/// page.
enum WalmartPageTidy {
    /// Known selectors, hidden by CSS before a mutation watcher runs.
    static let css = """
        [data-testid*="app-banner" i], [data-testid*="smart-banner" i], [data-testid*="get-the-app" i],
        [data-testid*="download-app" i], [class*="smart-banner" i], [id*="smart-banner" i],
        [data-testid*="chat" i], [aria-label*="chat with" i], [id*="chatbot" i], [class*="chatbot" i],
        [data-testid*="international" i][role="dialog"], [data-testid*="intl-popover" i]
        { display: none !important; }
        """

    /// Words on the boxes to hide, lowercased.
    static let phrases = ["get the app", "ship to another country", "chat with us", "need help? chat"]

    static let script = """
        (() => {
          try {
            if (window.__dinnerosTidy) { return; }
            window.__dinnerosTidy = true;
            const style = document.createElement('style');
            style.textContent = `\(css)`;
            (document.head || document.documentElement).appendChild(style);
            const phrases = \(phrases.debugDescription);
            const floating = (el) => {
              for (let n = el, depth = 0; n && n !== document.body && depth < 12; n = n.parentElement, depth++) {
                const role = n.getAttribute && n.getAttribute('role');
                if (role === 'dialog' || role === 'tooltip') { return n; }
                const position = getComputedStyle(n).position;
                if (position === 'fixed' || position === 'sticky' || position === 'absolute') { return n; }
              }
              return null;
            };
            const tidy = () => {
              try {
                const nodes = document.querySelectorAll('a, button, span, p, h2, h3, [aria-label]');
                for (const el of nodes) {
                  if (el.childElementCount > 3) { continue; }
                  const words = ((el.getAttribute('aria-label') || '') + ' ' + (el.textContent || ''))
                    .replace(/\\s+/g, ' ').trim().toLowerCase();
                  if (!words || words.length > 80 || !phrases.some((p) => words.includes(p))) { continue; }
                  const box = floating(el);
                  // Never the page's own header or search: only a small box with no search field.
                  if (!box || box === document.documentElement || box.querySelector('input, form, main')) { continue; }
                  if (box.getBoundingClientRect().height > window.innerHeight * 0.45) { continue; }
                  box.style.setProperty('display', 'none', 'important');
                }
              } catch (e) {}
            };
            let pending = false;
            const later = () => {
              if (pending) { return; }
              pending = true;
              setTimeout(() => { pending = false; tidy(); }, 250);
            };
            tidy();
            new MutationObserver(later).observe(document.documentElement, { childList: true, subtree: true });
          } catch (e) {}
        })();
        """
}

/// Walmart's site in the app's web view, sharing the member's Walmart sign-in across visits.
private struct WalmartBrowser: UIViewRepresentable {
    let page: WalmartBrowserPage

    func makeUIView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        // Walmart's "Get the app" banner, its "Ship to another country" popover and its chat
        // button cover the products; hide them on every page (build 121).
        configuration.userContentController.addUserScript(
            WKUserScript(source: WalmartPageTidy.script, injectionTime: .atDocumentEnd, forMainFrameOnly: true))
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.allowsBackForwardNavigationGestures = true
        webView.navigationDelegate = context.coordinator
        context.coordinator.observation = webView.observe(\.url, options: [.new]) { webView, _ in
            Task { @MainActor in page.addressChanged(webView.url) }
        }
        page.attach(webView)
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(page: page) }

    final class Coordinator: NSObject, WKNavigationDelegate {
        let page: WalmartBrowserPage
        var observation: NSKeyValueObservation?

        init(page: WalmartBrowserPage) {
            self.page = page
        }

        func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
            Task { @MainActor in page.committed() }
        }

        func webView(
            _ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error
        ) {
            Task { @MainActor in page.failed(error) }
        }

        func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
            Task { @MainActor in page.failed(error) }
        }

        func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
            Task { @MainActor in page.crashed() }
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            Task { @MainActor in
                page.addressChanged(webView.url)
                if WalmartChooser.productItemID(at: webView.url) != nil, page.productName == nil {
                    page.readProduct()
                }
            }
        }
    }
}
