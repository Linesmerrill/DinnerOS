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
        VStack(spacing: 0) {
            header
            if let item = chooser.current {
                itemBar(item)
                Divider()
                WalmartBrowser(page: page)
                    .ignoresSafeArea(edges: .bottom)
            } else {
                finished
            }
        }
        .onChange(of: chooser.current?.id, initial: true) { _, _ in
            if let url = chooser.searchURL { page.load(url) }
        }
        .alert(
            "Couldn't Save That",
            isPresented: Binding(get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(errorMessage ?? "")
        }
    }

    private var header: some View {
        HStack {
            Button("Done") { dismiss() }
                .fontWeight(.semibold)
            Spacer()
            Text("Choose in Walmart")
                .font(.headline)
            Spacer()
            Text(chooser.remaining == 1 ? "1 left" : "\(chooser.remaining) left")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .monospacedDigit()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    /// The list item, with Back and Skip, and the one button that matters: Use This Product, live
    /// only on a product page.
    private func itemBar(_ item: WalmartChooser.Item) -> some View {
        HStack(spacing: 12) {
            Button("Back", systemImage: "chevron.left") { chooser.back() }
                .labelStyle(.iconOnly)
                .disabled(!chooser.canGoBack || isSaving)
                .frame(minWidth: 44, minHeight: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.name)
                    .font(.headline)
                    .lineLimit(2)
                Text(
                    productID == nil
                        ? subtitle(item) : (page.productName ?? addressName ?? String(localized: "This product"))
                )
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            Button("Skip") { chooser.skip() }
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
            .buttonStyle(.borderedProminent)
            .disabled(productID == nil || isSaving)
            .accessibilityLabel("Use This Product for \(item.name)")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.bar)
    }

    private func subtitle(_ item: WalmartChooser.Item) -> String {
        let pick = String(localized: "Tap a product to pick it.")
        return item.amountText.isEmpty ? pick : "\(item.amountText) · \(pick)"
    }

    private var finished: some View {
        ContentUnavailableView {
            Label("All Set", systemImage: "checkmark.circle")
        } description: {
            Text(
                chooser.chosen.isEmpty
                    ? "No products chosen." : "Saved \(chooser.chosen.count) products. They go in the cart next time."
            )
        } actions: {
            Button("Done") { dismiss() }
                .buttonStyle(.borderedProminent)
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
    private(set) var url: URL?
    private(set) var productName: String?
    private(set) var priceText: String?
    @ObservationIgnored fileprivate weak var webView: WKWebView?
    @ObservationIgnored private var pending: URL?

    func load(_ url: URL) {
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

/// Walmart's site in the app's web view, sharing the member's Walmart sign-in across visits.
private struct WalmartBrowser: UIViewRepresentable {
    let page: WalmartBrowserPage

    func makeUIView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
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
