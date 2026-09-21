import SwiftUI
import WebKit
import os

/// The meal-kit sign-in: the service's own login page, in a web view.
///
/// The member types their password into HelloFresh's real page on HelloFresh's real domain,
/// which their password manager can fill, and which they can see in the bar above. Neither this
/// app nor our server ever receives it. When the login succeeds the service writes its session
/// cookie; that cookie's tokens are the only thing taken out of here, and the web view's storage
/// is wiped on the way out so no meal-kit session lingers inside DinnerOS.
///
/// The page is untrusted data throughout: nothing reads its contents, evaluates script in it,
/// screenshots it, or logs anything about it beyond which stage of the flow we are in.
struct MealKitWebLoginView: View {
    let service: MealKitService
    /// Called with the session the login produced. The caller links and dismisses.
    var onSession: (MealKitWebSession) -> Void
    /// Called when the member backs out without signing in.
    var onCancel: () -> Void = {}

    @Environment(\.dismiss) private var dismiss
    @State private var model: MealKitWebLoginModel

    init(
        service: MealKitService, onSession: @escaping (MealKitWebSession) -> Void,
        onCancel: @escaping () -> Void = {}
    ) {
        self.service = service
        self.onSession = onSession
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
            guard case .done(let session) = phase else { return }
            onSession(session)
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
            case .reading, .done:
                MealKitWebLoginOverlay(message: String(localized: "Finishing up…"))
            case .failed(let message):
                MealKitWebLoginMessage(
                    title: String(localized: "That didn't finish"), message: message,
                    retry: { model.retry() }, cancel: cancel)
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

/// The state of the sign-in web view, and the only code that touches its cookie store.
@MainActor
@Observable
final class MealKitWebLoginModel: NSObject, WKNavigationDelegate {
    enum Phase: Equatable {
        /// The member is on the service's pages, signing in.
        case signingIn
        /// They appear to be through; we're waiting for the session cookie.
        case reading
        /// The session is in hand.
        case done(MealKitWebSession)
        /// Something went wrong that trying again might fix.
        case failed(String)
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

    /// Loads the sign-in page and watches for the session it produces.
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
        phase = .signingIn
        webView.load(URLRequest(url: url))
    }

    /// Polls the web view's own cookie store until the session appears, the member gives up, or
    /// the wait after a finished sign-in runs out.
    ///
    /// Cancelled when the screen goes away, because it is driven by the view's `task`.
    func watchForSession() async {
        while !Task.isCancelled {
            if case .done = phase { return }
            if let session = await currentSession() {
                phase = .done(session)
                return
            }
            if let left = leftSignInAt, clock.now - left > Self.cookieTimeout {
                Self.logger.notice("Meal-kit web sign-in produced no session before the timeout")
                phase = .failed(
                    String(
                        localized: """
                            We couldn't pick up your \(service.displayName) sign-in. \
                            Try signing in again, or add your recipes by hand for now.
                            """))
                leftSignInAt = nil
            }
            try? await Task.sleep(for: Self.pollInterval)
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
        let cookies = await webView.configuration.websiteDataStore.httpCookieStore.allCookies()
        let match = cookies.first { cookie in
            cookie.name == service.sessionCookieName
                && (cookie.domain == service.cookieDomain || cookie.domain.hasSuffix("." + service.cookieDomain))
        }
        guard let match else { return nil }
        return MealKitWebSession(cookieValue: match.value)
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
        if case .done = phase { return }
        let code = (error as NSError).code
        // -999 is "a newer navigation replaced this one", which is normal in a redirect chain.
        guard code != NSURLErrorCancelled else { return }
        Self.logger.notice("Meal-kit web sign-in navigation failed: \(code, privacy: .public)")
        phase = .failed(
            String(localized: "We couldn't load the \(service.displayName) sign-in page. Check your connection."))
    }
}

/// Hosts the model's web view. It owns nothing: the model made the view and wipes it.
private struct MealKitWebViewHost: UIViewRepresentable {
    let webView: WKWebView

    func makeUIView(context: Context) -> WKWebView { webView }
    func updateUIView(_ uiView: WKWebView, context: Context) {}
}

/// The "finishing up" veil over the page.
private struct MealKitWebLoginOverlay: View {
    let message: String

    var body: some View {
        VStack(spacing: 12) {
            ProgressView()
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
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
