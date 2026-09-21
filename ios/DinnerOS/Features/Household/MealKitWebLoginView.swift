import SwiftUI
import WebKit
import os

/// The meal-kit import's one screen: the service's own login page, in a web view, and then the
/// reading of the household's order history inside that same session.
///
/// The member types their password into HelloFresh's real page on HelloFresh's real domain, which
/// their password manager can fill, and which they can see in the bar above. Neither this app nor
/// our server ever receives it — and, since the redesign, neither receives their session either:
/// the account requests happen here, in the web view, and what leaves is a list of recipe ids and
/// public page URLs.
///
/// The page is untrusted data throughout. The harvest script runs in an isolated content world,
/// reads nothing from the DOM, and returns nothing but that list. Nothing here screenshots the
/// page or logs anything about it beyond which stage of the flow we are in.
struct MealKitWebLoginView: View {
    let service: MealKitService
    /// Called with the household's order history. The caller queues the import and dismisses.
    var onHarvest: (MealKitHarvest) -> Void
    /// Called when the member backs out without signing in.
    var onCancel: () -> Void = {}

    @Environment(\.dismiss) private var dismiss
    @State private var model: MealKitWebLoginModel

    init(
        service: MealKitService, onHarvest: @escaping (MealKitHarvest) -> Void,
        onCancel: @escaping () -> Void = {}
    ) {
        self.service = service
        self.onHarvest = onHarvest
        self.onCancel = onCancel
        _model = State(initialValue: MealKitWebLoginModel(service: service))
    }

    var body: some View {
        Group {
            if case .unavailable(let message) = model.phase {
                MealKitWebLoginMessage(
                    title: String(localized: "Sign-in unavailable"), message: message,
                    retry: nil, cancel: cancel)
            } else {
                webView
            }
        }
        .navigationTitle("Sign In to \(service.displayName)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { cancel() }
            }
            ToolbarItem(placement: .principal) {
                VStack(spacing: 0) {
                    Text("Sign In to \(service.displayName)").font(.headline)
                    // The real domain, so it is obvious whose page is asking.
                    Text(model.host.isEmpty ? service.cookieDomain : model.host)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .task { await model.start() }
        .onChange(of: model.phase) { _, phase in
            guard case .done(let harvest) = phase else { return }
            onHarvest(harvest)
        }
        .onDisappear {
            let model = model
            Task { await model.forgetEverything() }
        }
    }

    @ViewBuilder
    private var webView: some View {
        ZStack {
            MealKitWebViewHost(webView: model.webView)
                .ignoresSafeArea(edges: .bottom)
            switch model.phase {
            case .reading, .harvesting, .done:
                MealKitWebLoginOverlay(message: model.progressMessage)
            case .failed(let message, let canRetry):
                MealKitWebLoginMessage(
                    title: String(localized: "That didn't finish"), message: message,
                    retry: canRetry ? { model.retry() } : nil, cancel: cancel)
            case .signingIn, .unavailable:
                EmptyView()
            }
        }
    }

    private func cancel() {
        onCancel()
        dismiss()
    }
}

/// The state of the sign-in web view: the only code that touches its cookie store, and the only
/// code that runs the harvest.
@MainActor
@Observable
final class MealKitWebLoginModel: NSObject, WKNavigationDelegate {
    enum Phase: Equatable {
        /// The member is on the service's pages, signing in.
        case signingIn
        /// They appear to be through; we're waiting for the session cookie.
        case reading
        /// Walking their order history inside their own session.
        case harvesting
        /// The history is in hand. It holds no credential.
        case done(MealKitHarvest)
        /// Something went wrong. `canRetry` is false when trying again cannot help.
        case failed(String, canRetry: Bool)
        /// We can't offer the sign-in at all.
        case unavailable(String)
    }

    private(set) var phase: Phase = .signingIn
    /// The host the member is actually on, shown above the page.
    private(set) var host = ""

    let service: MealKitService
    let webView: WKWebView

    /// How long to wait for the session cookie once the member has left the sign-in pages. Long
    /// enough for a slow redirect chain, short enough not to look stuck.
    static let cookieTimeout: Duration = .seconds(30)
    /// How often the cookie store is checked while the sheet is open. The cookie is written
    /// without a navigation we can see, so there is nothing to observe instead.
    static let pollInterval: Duration = .milliseconds(750)

    /// Paths that are still part of signing in. Leaving them is what starts the clock.
    private static let signInPathFragments = ["login", "signin", "sign-in", "auth", "oauth", "sso", "register"]

    private var leftSignInAt: ContinuousClock.Instant?
    private var clock = ContinuousClock()
    private var harvesting = false

    private static let logger = Logger(subsystem: "DinnerOS", category: "meal-kit-import")

    init(service: MealKitService) {
        self.service = service
        let configuration = WKWebViewConfiguration()
        // A non-persistent store: whatever the login writes lives only as long as this view, and
        // never reaches the app's own cookie storage.
        configuration.websiteDataStore = .nonPersistent()
        webView = WKWebView(frame: .zero, configuration: configuration)
        super.init()
        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = true
    }

    /// What the veil over the page says while it is up.
    var progressMessage: String {
        switch phase {
        case .harvesting:
            String(localized: "Reading your \(service.displayName) order history…")
        default:
            String(localized: "Finishing up…")
        }
    }

    /// Loads the sign-in page and waits for the session it produces.
    func start() async {
        guard let url = service.loginURL else {
            phase = .unavailable(
                String(localized: "We don't have a sign-in page for \(service.displayName) in this version."))
            return
        }
        if webView.url == nil {
            webView.load(URLRequest(url: url))
        }
        await watchForSession()
    }

    /// Reloads the sign-in page after a failure.
    func retry() {
        guard let url = service.loginURL else { return }
        leftSignInAt = nil
        harvesting = false
        phase = .signingIn
        webView.load(URLRequest(url: url))
    }

    /// Polls the web view's own cookie store until the session appears, the member gives up, or
    /// the wait after a finished sign-in runs out. The session then drives the harvest.
    ///
    /// Cancelled when the screen goes away, because it is driven by the view's `task`: closing
    /// the sheet mid-harvest simply stops everything and wipes the web view.
    func watchForSession() async {
        while !Task.isCancelled {
            switch phase {
            case .done, .harvesting:
                return
            default:
                break
            }
            if let session = await currentSession() {
                await harvest(with: session)
                return
            }
            if let left = leftSignInAt, clock.now - left > Self.cookieTimeout {
                Self.logger.notice("Meal-kit web sign-in produced no session before the timeout")
                phase = .failed(
                    String(
                        localized: """
                            We couldn't pick up your \(service.displayName) sign-in. \
                            Try signing in again, or add your recipes by hand for now.
                            """), canRetry: true)
                leftSignInAt = nil
            }
            try? await Task.sleep(for: Self.pollInterval)
        }
    }

    /// Reads the household's order history in the member's own session.
    ///
    /// The token goes into the script and no further: it is not returned, not stored, and not
    /// logged, and the script hands back only recipe ids, public page URLs and weeks.
    private func harvest(with session: MealKitWebSession) async {
        guard !harvesting else { return }
        harvesting = true
        phase = .harvesting
        let arguments: [String: Any] = [
            "token": session.accessToken,
            "tokenType": session.tokenType,
            "subscription": await currentPlanID() ?? "",
            "country": service.country,
            "locale": service.locale,
            "maxPages": MealKitHarvestScript.maxPages,
            "minIntervalMs": MealKitHarvestScript.minIntervalMilliseconds,
            "maxRecipes": MealKitHarvestScript.maxRecipes,
        ]
        do {
            let result = try await webView.callAsyncJavaScript(
                MealKitHarvestScript.body, arguments: arguments, in: nil, contentWorld: .defaultClient)
            guard let json = result as? String else {
                phase = failure(for: .unreadable)
                return
            }
            switch MealKitHarvestResult(json: json, service: service) {
            case .harvested(let harvest):
                Self.logger.info(
                    "Meal-kit order history read: \(harvest.recipes.count, privacy: .public) recipes, \(harvest.weeks, privacy: .public) weeks"
                )
                phase = .done(harvest)
            case .failed(let reason):
                Self.logger.notice("Meal-kit order history could not be read: \(reason.rawValue, privacy: .public)")
                phase = failure(for: reason)
            }
        } catch {
            // Never the error's text: it can quote the page.
            Self.logger.notice("Meal-kit harvest script failed: \((error as NSError).code, privacy: .public)")
            phase = failure(for: .unavailable)
        }
        harvesting = false
    }

    /// What each way of not getting a history says to the member.
    private func failure(for reason: MealKitHarvestFailure) -> Phase {
        switch reason {
        case .forbidden:
            .failed(
                String(
                    localized: """
                        Your \(service.displayName) session ended before we finished reading your orders. \
                        Sign in again to pick up where it stopped.
                        """), canRetry: true)
        case .empty:
            .failed(
                String(
                    localized: """
                        We couldn't find any past deliveries on that \(service.displayName) account. \
                        If you ordered under a different account, sign in with that one.
                        """), canRetry: true)
        case .unreadable:
            .failed(
                String(
                    localized: """
                        \(service.displayName) answered in a way this version of DinnerOS can't read. \
                        Nothing was changed in your recipes — please update the app and try again.
                        """), canRetry: false)
        case .unavailable:
            .failed(
                String(
                    localized: "We couldn't reach \(service.displayName) to read your orders. Check your connection."),
                canRetry: true)
        }
    }

    /// Wipes everything the web view stored. Called when the screen goes away, whichever way it
    /// went: no meal-kit session is left behind inside DinnerOS.
    func forgetEverything() async {
        let store = webView.configuration.websiteDataStore
        await store.removeData(ofTypes: WKWebsiteDataStore.allWebsiteDataTypes(), modifiedSince: .distantPast)
    }

    /// The service's session cookie, when it is there and readable.
    ///
    /// The cookie has to come from the service's own domain: a cookie some other site set in
    /// this web view is not the member's meal-kit session and is never treated as one.
    private func currentSession() async -> MealKitWebSession? {
        guard let match = await cookie(named: service.sessionCookieName) else { return nil }
        return MealKitWebSession(cookieValue: match)
    }

    /// The account's subscription id, when the site already put it in a cookie. It saves the
    /// harvest one request; the script reads the plans endpoint when it is missing.
    private func currentPlanID() async -> String? {
        await cookie(named: service.planCookieName)
    }

    private func cookie(named name: String) async -> String? {
        let cookies = await webView.configuration.websiteDataStore.httpCookieStore.allCookies()
        return cookies.first { cookie in
            cookie.name == name
                && (cookie.domain == service.cookieDomain || cookie.domain.hasSuffix("." + service.cookieDomain))
        }?.value
    }

    // MARK: - WKNavigationDelegate

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        noteLocation(webView.url)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        reportNavigationFailure(error)
    }

    func webView(
        _ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error
    ) {
        reportNavigationFailure(error)
    }

    /// Records which host the member is on and whether they are past the sign-in pages.
    ///
    /// Only the host and path shape are read — never the page, never a query string, which on a
    /// sign-in flow can carry a code.
    private func noteLocation(_ url: URL?) {
        guard let url, let host = url.host() else { return }
        self.host = host
        let path = url.path().lowercased()
        let signingIn = Self.signInPathFragments.contains { path.contains($0) }
        switch (signingIn, leftSignInAt) {
        case (true, _):
            leftSignInAt = nil
            if case .reading = phase { phase = .signingIn }
        case (false, nil):
            // Through the sign-in and somewhere else on their site: the cookie should be along
            // shortly, and if it isn't, the timeout says so instead of spinning forever.
            leftSignInAt = clock.now
            if case .signingIn = phase { phase = .reading }
        case (false, .some):
            break
        }
    }

    private func reportNavigationFailure(_ error: any Error) {
        switch phase {
        case .done, .harvesting:
            return
        default:
            break
        }
        let code = (error as NSError).code
        // -999 is "a newer navigation replaced this one", which is normal in a redirect chain.
        guard code != NSURLErrorCancelled else { return }
        Self.logger.notice("Meal-kit web sign-in navigation failed: \(code, privacy: .public)")
        phase = .failed(
            String(localized: "We couldn't load the \(service.displayName) sign-in page. Check your connection."),
            canRetry: true)
    }
}

/// Hosts the model's web view. It owns nothing: the model made the view and wipes it.
private struct MealKitWebViewHost: UIViewRepresentable {
    let webView: WKWebView

    func makeUIView(context: Context) -> WKWebView { webView }
    func updateUIView(_ uiView: WKWebView, context: Context) {}
}

/// The veil over the page while the sign-in finishes and the history is read.
private struct MealKitWebLoginOverlay: View {
    let message: String

    var body: some View {
        VStack(spacing: 12) {
            ProgressView()
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .padding(24)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 16))
        .accessibilityElement(children: .combine)
    }
}

/// A dead end with a way out of it.
private struct MealKitWebLoginMessage: View {
    let title: String
    let message: String
    /// Nil when trying again cannot help.
    let retry: (() -> Void)?
    let cancel: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundStyle(.orange)
                .accessibilityHidden(true)
            Text(title)
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if let retry {
                Button("Try Again", action: retry)
                    .buttonStyle(.borderedProminent)
            }
            Button("Not Now", action: cancel)
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemGroupedBackground))
    }
}
