import Foundation
import Observation
import os

nonisolated enum AuthSessionError: Error, Equatable {
    case notConfigured
    case signedOut
}

extension AuthSessionError: LocalizedError {
    nonisolated var errorDescription: String? {
        switch self {
        case .notConfigured: String(localized: "This build has no API server configured.")
        case .signedOut: String(localized: "You're signed out. Sign in to continue.")
        }
    }
}

/// The app's authentication state and the only owner of DinnerOS tokens.
///
/// All state lives on the main actor, which also serializes token refresh: the API
/// treats two concurrent refreshes with the same refresh token as token theft and
/// revokes the sign-in, so every caller that needs a refresh awaits one shared task.
@Observable
final class AuthSession {
    enum State: Equatable {
        case restoring
        case signedOut
        case signedIn(UserSummary)
        /// Sign-in is behind Face ID (or Touch ID, Optic ID) and no check has passed yet.
        case locked
        case configurationError(String)
    }

    private(set) var state: State = .restoring

    /// Whether this device keeps the session behind Face ID (decision 760). Off by default.
    private(set) var isBiometricSignInOn: Bool

    var currentUser: UserSummary? {
        if case .signedIn(let user) = state { return user }
        return nil
    }

    @ObservationIgnored private let api: AuthAPI?
    @ObservationIgnored private let store: any TokenStore
    @ObservationIgnored private let protectedStore: (any ProtectedSessionStore)?
    @ObservationIgnored let biometricSetting: BiometricSetting
    /// A session that couldn't be written behind Face ID (the device was locked); retried when
    /// the app is next active.
    @ObservationIgnored private var unsavedProtectedSession: StoredSession?
    /// Runs after every successful sign-in, for the Face ID offer.
    @ObservationIgnored var didSignIn: (@MainActor () -> Void)?
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var tokens: AuthTokens?
    @ObservationIgnored private var refreshTask: Task<AuthTokens, any Error>?
    /// Incremented on every sign-in and sign-out so a refresh that finishes afterwards
    /// can't resurrect or overwrite a different session.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var hasRestored = false
    /// Runs at the start of `signOut()`, while requests are still authorized. A sign-out
    /// forced by a rejected refresh token can't make requests, so it doesn't run then.
    @ObservationIgnored var willSignOut: (@MainActor () async -> Void)?

    private static let logger = Logger(subsystem: "DinnerOS", category: "auth")

    /// - Parameter api: `nil` when the build has no valid API URL; the session then
    ///   reports `configurationError` instead of crashing.
    init(
        api: AuthAPI?, store: any TokenStore, protectedStore: (any ProtectedSessionStore)? = nil,
        biometricSetting: BiometricSetting? = nil, now: @escaping () -> Date = Date.init
    ) {
        self.api = api
        self.store = store
        self.protectedStore = protectedStore
        // Without a protected store there's nowhere to keep it, so it's off.
        let setting = biometricSetting ?? BiometricSetting(defaults: UserDefaults(suiteName: "auth.none") ?? .standard)
        if protectedStore == nil, setting.isOn { setting.turnOff(offerAgain: false) }
        self.biometricSetting = setting
        self.isBiometricSignInOn = protectedStore != nil && setting.isOn
        self.now = now
    }

    /// A session frozen in `state`, for SwiftUI previews. Not DEBUG-only because
    /// `#Preview` bodies are type-checked in Release builds too; it has no network access.
    static func preview(_ state: State) -> AuthSession {
        let session = AuthSession(api: nil, store: InMemoryTokenStore())
        session.state = state
        session.hasRestored = true
        return session
    }

    var authAPI: AuthAPI? { api }

    // MARK: - Launch

    /// Restores the stored session once per launch, refreshing an expired access token.
    func restore() async {
        guard !hasRestored else { return }
        hasRestored = true

        guard api != nil else {
            state = .configurationError(
                String(
                    localized: "This build has no valid API server URL. Set API_BASE_URL in the build configuration."))
            return
        }

        if isBiometricSignInOn {
            // Behind Face ID: nothing is read until a check passes. A missing item (faces
            // changed, or the Keychain was reset) means normal sign-in.
            if protectedStore?.hasItem() == true {
                state = .locked
            } else {
                Self.logger.notice("Face ID sign-in item missing; signing out")
                clearLocalSession()
            }
            return
        }

        let stored: StoredSession?
        do {
            stored = try store.load()
        } catch {
            Self.logger.error("Keychain read failed: \(String(describing: error), privacy: .public)")
            stored = nil
        }
        await adopt(stored)
    }

    /// Reads the session behind Face ID after a check passed. `false` when it's gone or expired,
    /// and the session is then signed out.
    func unlock(with proof: BiometricProof) async -> Bool {
        guard state == .locked, let protectedStore else { return currentUser != nil }
        let stored: StoredSession?
        do {
            stored = try protectedStore.load(proof: proof)
        } catch {
            Self.logger.error("Face ID item read failed: \(String(describing: error), privacy: .public)")
            stored = nil
        }
        await adopt(stored)
        return currentUser != nil
    }

    private func adopt(_ stored: StoredSession?) async {
        guard let stored, !stored.tokens.isRefreshTokenExpired(at: now()) else {
            clearLocalSession()
            return
        }

        tokens = stored.tokens
        state = .signedIn(stored.user)
        let restoredGeneration = generation

        if stored.tokens.isAccessTokenExpired(at: now()) {
            do {
                _ = try await refreshTokens(replacing: stored.tokens.accessToken)
            } catch {
                // A rejected refresh token already signed us out inside refreshTokens.
                // Anything else (offline, server error) keeps the cached session; the
                // next authorized request retries the refresh.
                if generation == restoredGeneration {
                    Self.logger.notice("Launch refresh deferred: \(Self.describe(error), privacy: .public)")
                }
            }
        }
    }

    // MARK: - Sign in and out

    /// Runs a sign-in call and, on success, stores and publishes the new session.
    func signIn(_ operation: (AuthAPI) async throws -> SessionResponse) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        let response = try await operation(api)
        generation += 1
        refreshTask?.cancel()
        refreshTask = nil
        tokens = response.tokens
        persist(StoredSession(tokens: response.tokens, user: response.user))
        state = .signedIn(response.user)
        Self.logger.info("Signed in (new user: \(response.isNewUser, privacy: .public))")
        didSignIn?()
    }

    // MARK: - Face ID

    /// Moves the session behind Face ID. Call after a check passed, with the enrollment state
    /// then, so a later change of faces is noticed.
    func turnOnBiometricSignIn(enrollmentState: Data?) throws {
        guard let protectedStore else { throw AuthSessionError.notConfigured }
        guard let tokens, let user = currentUser else { throw AuthSessionError.signedOut }
        try protectedStore.save(StoredSession(tokens: tokens, user: user))
        biometricSetting.turnOn(enrollmentState: enrollmentState)
        isBiometricSignInOn = true
        do {
            try store.clear()
        } catch {
            Self.logger.error("Keychain clear failed: \(String(describing: error), privacy: .public)")
        }
        Self.logger.info("Face ID sign-in on")
    }

    /// Moves the session back to the normal Keychain item.
    func turnOffBiometricSignIn() {
        guard isBiometricSignInOn else { return }
        isBiometricSignInOn = false
        biometricSetting.turnOff(offerAgain: false)
        unsavedProtectedSession = nil
        if let tokens, let user = currentUser {
            persist(StoredSession(tokens: tokens, user: user))
        }
        protectedStore?.clear()
        Self.logger.info("Face ID sign-in off")
    }

    /// Writes a session that couldn't be saved behind Face ID while the device was locked.
    func saveUnsavedSession() {
        guard let session = unsavedProtectedSession else { return }
        unsavedProtectedSession = nil
        persist(session)
    }

    /// What the sign-in screen offers besides Apple and Google. Offline, or with no server,
    /// nothing extra shows.
    func signInOptions() async -> SignInOptions {
        guard let api else { return .none }
        return (try? await api.signInOptions()) ?? .none
    }

    /// Runs `willSignOut`, clears local credentials, then revokes the sign-in on the server
    /// on a best-effort basis. The Keychain is cleared even if the server call fails.
    func signOut() async {
        // Still signed in here, so the hook can make authorized calls (the push store
        // deletes this device's token). It bounds its own wait.
        if currentUser != nil, let willSignOut {
            await willSignOut()
        }
        let refreshToken = tokens?.refreshToken
        clearLocalSession()
        guard let api, let refreshToken else { return }
        do {
            try await api.logout(refreshToken: refreshToken)
        } catch {
            Self.logger.notice("Server logout failed: \(Self.describe(error), privacy: .public)")
        }
    }

    /// Permanently deletes the account on the server, then clears the local session.
    ///
    /// `willSignOut` runs first, while requests are still authorized, so this device's push
    /// registration is forgotten the same way as at sign-out. If the deletion fails the user
    /// stays signed in and the error is thrown. On success there is no logout call: the
    /// server already deleted every session.
    func deleteAccount(appleAuthorizationCode: String? = nil) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        guard currentUser != nil else { throw AuthSessionError.signedOut }
        if let willSignOut {
            await willSignOut()
        }
        try await authorized { token in
            try await api.deleteAccount(accessToken: token, appleAuthorizationCode: appleAuthorizationCode)
        }
        Self.logger.info("Account deleted")
        clearLocalSession()
    }

    // MARK: - Authorized requests

    /// Runs `operation` with a valid access token.
    ///
    /// An expired token is refreshed first; offline, the old one is used so saved answers can
    /// stand in. If the server still answers `401`, the session refreshes once and retries once.
    /// A rejected refresh, or a second `401`, signs out.
    func authorized<Response: Sendable>(
        _ operation: (String) async throws -> Response
    ) async throws -> Response {
        let accessToken: String
        do {
            accessToken = try await validAccessToken()
        } catch let error as APIError where ResponseCache.shouldReplay(error) {
            // Offline with an expired token: the refresh can't reach the server, so the request
            // goes with the old token. It can't reach the server either, and the saved answer
            // is shown instead of an error. Nothing here signs anyone out.
            guard let stale = tokens?.accessToken else { throw error }
            accessToken = stale
        }
        do {
            return try await operation(accessToken)
        } catch let error as APIError where error.isUnauthorized {
            let refreshed = try await refreshTokens(replacing: accessToken)
            do {
                return try await operation(refreshed.accessToken)
            } catch let retryError as APIError where retryError.isUnauthorized {
                Self.logger.notice("Request rejected after refresh; signing out")
                clearLocalSession()
                throw retryError
            }
        }
    }

    /// Loads `GET /me` and updates the stored user summary.
    func loadCurrentUser() async throws -> MeResponse {
        guard let api else { throw AuthSessionError.notConfigured }
        let me = try await authorized { token in try await api.me(accessToken: token) }
        if let tokens, currentUser != me.user {
            persist(StoredSession(tokens: tokens, user: me.user))
            state = .signedIn(me.user)
        }
        return me
    }

    private func validAccessToken() async throws -> String {
        guard let tokens else { throw AuthSessionError.signedOut }
        if tokens.isAccessTokenExpired(at: now()) {
            return try await refreshTokens(replacing: tokens.accessToken).accessToken
        }
        return tokens.accessToken
    }

    /// Returns tokens newer than `staleAccessToken`, starting at most one refresh.
    ///
    /// - If a refresh is already running, waits for it.
    /// - If another caller already replaced `staleAccessToken`, returns the current
    ///   tokens without refreshing again.
    private func refreshTokens(replacing staleAccessToken: String) async throws -> AuthTokens {
        if let refreshTask {
            return try await refreshTask.value
        }
        guard let api, let current = tokens else { throw AuthSessionError.signedOut }
        if current.accessToken != staleAccessToken {
            return current
        }

        let refreshToken = current.refreshToken
        let startedGeneration = generation
        let task = Task { try await api.refresh(refreshToken: refreshToken) }
        refreshTask = task
        defer {
            if refreshTask == task { refreshTask = nil }
        }

        do {
            let refreshed = try await task.value
            guard generation == startedGeneration else { throw AuthSessionError.signedOut }
            tokens = refreshed
            if let user = currentUser {
                persist(StoredSession(tokens: refreshed, user: user))
            }
            return refreshed
        } catch {
            if generation == startedGeneration, Self.isRejectedCredential(error) {
                Self.logger.notice("Refresh rejected; signing out")
                clearLocalSession()
            }
            throw error
        }
    }

    // MARK: - Helpers

    private func clearLocalSession() {
        generation += 1
        refreshTask?.cancel()
        refreshTask = nil
        tokens = nil
        do {
            try store.clear()
        } catch {
            Self.logger.error("Keychain clear failed: \(String(describing: error), privacy: .public)")
        }
        // Signing out turns Face ID sign-in off; it's offered again at the next sign-in.
        protectedStore?.clear()
        unsavedProtectedSession = nil
        if isBiometricSignInOn || biometricSetting.isOn {
            biometricSetting.turnOff(offerAgain: true)
            isBiometricSignInOn = false
        }
        state = .signedOut
    }

    private func persist(_ session: StoredSession) {
        if isBiometricSignInOn, let protectedStore {
            do {
                try protectedStore.save(session)
                unsavedProtectedSession = nil
            } catch {
                // Kept in memory and written when the app is next active and unlocked.
                unsavedProtectedSession = session
                Self.logger.error("Face ID item write failed: \(String(describing: error), privacy: .public)")
            }
            return
        }
        do {
            try store.save(session)
        } catch {
            // The session still works for this launch; it just won't survive a relaunch.
            Self.logger.error("Keychain write failed: \(String(describing: error), privacy: .public)")
        }
    }

    /// The server rejected the refresh token itself (as opposed to a network or
    /// server failure, where the token may still be valid).
    private static func isRejectedCredential(_ error: any Error) -> Bool {
        guard let status = (error as? APIError)?.status else { return false }
        return status == 400 || status == 401
    }

    /// Error codes only; never tokens or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error {
        case let apiError as APIError:
            switch apiError {
            case .server(let status, let code, _, let requestID):
                "\(status) \(code) request=\(requestID ?? "-")"
            case .transport(let code):
                "transport \(code.rawValue)"
            case .invalidResponse:
                "invalid response"
            case .decoding(let type):
                "decoding \(type)"
            }
        default:
            String(describing: type(of: error))
        }
    }
}
