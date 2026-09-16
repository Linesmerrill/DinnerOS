import Foundation
import Observation
import UIKit
import UserNotifications
import os

/// The system calls push registration needs, behind a protocol so tests don't touch
/// `UNUserNotificationCenter` or `UIApplication`.
protocol PushPlatform: AnyObject {
    func authorizationStatus() async -> PushAuthorization
    /// Shows the system prompt. Returns whether alerts were allowed.
    func requestAuthorization() async throws -> Bool
    /// Asks iOS for a device token; it arrives in the app delegate.
    func registerForRemoteNotifications()
}

final class SystemPushPlatform: PushPlatform {
    func authorizationStatus() async -> PushAuthorization {
        switch await UNUserNotificationCenter.current().notificationSettings().authorizationStatus {
        case .notDetermined: .notDetermined
        case .denied: .denied
        case .authorized, .provisional, .ephemeral: .authorized
        @unknown default: .denied
        }
    }

    func requestAuthorization() async throws -> Bool {
        try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
    }

    func registerForRemoteNotifications() {
        UIApplication.shared.registerForRemoteNotifications()
    }
}

/// The last device token this device registered, and for whom, so sign-out can delete it
/// and a changed token can replace it. A device token isn't a secret.
nonisolated struct StoredPushRegistration: Codable, Equatable, Sendable {
    let token: String
    let userID: String
}

protocol PushRegistrationStorage: AnyObject {
    var registration: StoredPushRegistration? { get set }
}

final class UserDefaultsPushRegistration: PushRegistrationStorage {
    static let key = "pushRegistration"
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    var registration: StoredPushRegistration? {
        get {
            defaults.data(forKey: Self.key).flatMap { try? JSONDecoder().decode(StoredPushRegistration.self, from: $0) }
        }
        set {
            if let newValue, let data = try? JSONEncoder().encode(newValue) {
                defaults.set(data, forKey: Self.key)
            } else {
                defaults.removeObject(forKey: Self.key)
            }
        }
    }
}

final class InMemoryPushRegistration: PushRegistrationStorage {
    var registration: StoredPushRegistration?

    init(registration: StoredPushRegistration? = nil) {
        self.registration = registration
    }
}

/// Push notifications on this device: permission, the device token the API sends to, and
/// the notification a tapped push should open.
///
/// Permission is never asked at launch. It is asked when a member picks the household's
/// order day, or taps "Notify Me" on the order reminder — moments where an alert on a
/// locked phone is obviously the point. A refusal changes nothing else: the bell is the
/// whole product without push. Once allowed, every sign-in and every return to the
/// foreground registers again, so a token iOS rotated reaches the API.
@Observable
final class PushNotificationStore {
    private(set) var authorization: PushAuthorization = .notDetermined
    /// A tapped push waiting for the tab shell to open it.
    private(set) var pendingRoute: PushRoute?

    /// Called when a push arrives while the app is open, so the bell's count follows.
    @ObservationIgnored var onNotificationReceived: (() async -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: DeviceTokensAPI?
    @ObservationIgnored private let platform: any PushPlatform
    @ObservationIgnored private let storage: any PushRegistrationStorage
    @ObservationIgnored private let environment: PushEnvironment
    /// Registered with the API during this launch; iOS hands the token over on every
    /// `registerForRemoteNotifications`, and one upload per launch is enough.
    @ObservationIgnored private var uploaded: StoredPushRegistration?

    /// How long sign-out waits for the server to forget this device.
    static let signOutTimeout: Duration = .seconds(3)

    private static let logger = Logger(subsystem: "DinnerOS", category: "push")

    init(
        session: AuthSession, api: DeviceTokensAPI?, platform: any PushPlatform = SystemPushPlatform(),
        storage: any PushRegistrationStorage = UserDefaultsPushRegistration(), environment: PushEnvironment = .current
    ) {
        self.session = session
        self.api = api
        self.platform = platform
        self.storage = storage
        self.environment = environment
    }

    /// A store with no system access, for SwiftUI previews.
    static func preview(session: AuthSession, authorization: PushAuthorization = .notDetermined)
        -> PushNotificationStore
    {
        let store = PushNotificationStore(
            session: session, api: nil, platform: PreviewPushPlatform(), storage: InMemoryPushRegistration())
        store.authorization = authorization
        return store
    }

    // MARK: - Permission and registration

    /// Reads the current permission and, when alerts are allowed and someone is signed in,
    /// registers for a device token. Runs at sign-in and whenever the app becomes active,
    /// which also notices a change made in Settings.
    func refresh() async {
        authorization = await platform.authorizationStatus()
        if authorization == .authorized, session.currentUser != nil, api != nil {
            platform.registerForRemoteNotifications()
        }
    }

    /// Asks for permission if this device has never been asked, then registers. Returns
    /// whether alerts are allowed. A member who already refused isn't asked again; iOS
    /// wouldn't show the prompt anyway.
    @discardableResult
    func requestAuthorizationIfNeeded() async -> Bool {
        authorization = await platform.authorizationStatus()
        if authorization == .notDetermined {
            do {
                authorization = try await platform.requestAuthorization() ? .authorized : .denied
            } catch {
                Self.logger.notice(
                    "Notification permission request failed: \(String(describing: type(of: error)), privacy: .public)")
                authorization = await platform.authorizationStatus()
            }
        }
        if authorization == .authorized, session.currentUser != nil, api != nil {
            platform.registerForRemoteNotifications()
        }
        return authorization == .authorized
    }

    /// iOS issued a device token: send it to the API for the signed-in user.
    func didRegister(deviceToken: Data) async {
        await register(token: deviceToken.pushTokenHex)
    }

    func didFailToRegister(_ error: any Error) {
        // Simulators without an APNs connection and builds without the entitlement land here.
        Self.logger.notice("Remote notification registration failed: \((error as NSError).code, privacy: .public)")
    }

    func register(token: String) async {
        guard let api, let userID = session.currentUser?.id, !token.isEmpty else { return }
        let registration = StoredPushRegistration(token: token, userID: userID)
        guard uploaded != registration else { return }
        let previous = storage.registration
        do {
            _ = try await session.authorized { accessToken in
                try await api.register(token: token, environment: environment, accessToken: accessToken)
            }
        } catch is CancellationError {
            return
        } catch {
            Self.logger.notice("Device token registration failed: \(Self.describe(error), privacy: .public)")
            return
        }
        // The signed-in user may have changed while the request ran.
        guard session.currentUser?.id == userID else { return }
        uploaded = registration
        storage.registration = registration
        // iOS rotated the token: the old one will never deliver again.
        if let previous, previous.userID == userID, previous.token != token {
            try? await session.authorized { accessToken in
                try await api.delete(token: previous.token, accessToken: accessToken)
            }
        }
    }

    /// Forgets this device on the server before the session's tokens are cleared, so a
    /// signed-out phone stops getting the household's alerts. Bounded by `signOutTimeout`:
    /// sign-out never waits on a slow network. The local record is cleared either way.
    func unregisterForSignOut() async {
        let registration = storage.registration ?? uploaded
        storage.registration = nil
        uploaded = nil
        guard let api, let registration, registration.userID == session.currentUser?.id else { return }
        let session = self.session
        let deletion = Task {
            try await session.authorized { accessToken in
                try await api.delete(token: registration.token, accessToken: accessToken)
            }
        }
        let timeout = Task {
            try await Task.sleep(for: Self.signOutTimeout)
            deletion.cancel()
        }
        if case .failure(let error) = await deletion.result {
            Self.logger.notice("Device token delete failed: \(Self.describe(error), privacy: .public)")
        }
        timeout.cancel()
    }

    // MARK: - Opening pushes

    /// A push was tapped. The tab shell opens it (`MainTabView`).
    func open(_ route: PushRoute) {
        pendingRoute = route
    }

    /// The tab shell handled `route`.
    func finishOpening(_ route: PushRoute) {
        if pendingRoute == route {
            pendingRoute = nil
        }
    }

    /// A push arrived while the app was in the foreground.
    func notificationReceived() async {
        await onNotificationReceived?()
    }

    /// Forgets in-memory state after sign-out. The permission is the device's, so it stays.
    func reset() {
        uploaded = nil
        pendingRoute = nil
    }

    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}

/// A platform that never prompts, for previews.
private final class PreviewPushPlatform: PushPlatform {
    func authorizationStatus() async -> PushAuthorization { .notDetermined }
    func requestAuthorization() async throws -> Bool { false }
    func registerForRemoteNotifications() {}
}
