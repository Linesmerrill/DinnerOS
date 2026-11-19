import Foundation
import LocalAuthentication

/// Which biometric sign-in this device has, named the way Settings names it.
nonisolated enum BiometryKind: String, Equatable, Sendable {
    case faceID
    case touchID
    case opticID

    init?(_ type: LABiometryType) {
        switch type {
        case .faceID: self = .faceID
        case .touchID: self = .touchID
        case .opticID: self = .opticID
        default: return nil
        }
    }

    /// Product names: never translated.
    var name: String {
        switch self {
        case .faceID: "Face ID"
        case .touchID: "Touch ID"
        case .opticID: "Optic ID"
        }
    }

    var systemImage: String {
        switch self {
        case .faceID: "faceid"
        case .touchID: "touchid"
        case .opticID: "opticid"
        }
    }

    /// The offer after a sign-in.
    var offerTitle: String {
        String(localized: "Use \(name) to sign in next time?")
    }

    var toggleTitle: String {
        String(localized: "Sign In with \(name)")
    }
}

/// How a Face ID (or Touch ID, Optic ID) check ended.
nonisolated enum BiometricOutcome: Equatable, Sendable {
    /// The face matched, or the device passcode was entered instead.
    case matched
    /// The person tapped Cancel.
    case canceled
    /// The system stopped the check, for example when the app went to the background. Not the
    /// person's choice, so the lock asks again when the app is back.
    case interrupted
    /// No match, or too many tries.
    case failed
    /// Biometrics or a passcode aren't set up on this device any more.
    case unavailable
    /// Faces or fingers were added or removed since sign-in was turned on.
    case changed
    /// The saved sign-in is gone from the Keychain.
    case missing

    init(_ error: any Error) {
        switch (error as? LAError)?.code {
        case .userCancel: self = .canceled
        case .systemCancel, .appCancel, .notInteractive: self = .interrupted
        case .biometryNotAvailable, .biometryNotEnrolled, .passcodeNotSet: self = .unavailable
        default: self = .failed
        }
    }
}

/// Proof of a passed check, handed to the Keychain so reading the saved sign-in doesn't ask
/// again. `nil` context in tests and the fake.
struct BiometricProof {
    let context: LAContext?
}

/// What the lock needs from LocalAuthentication.
protocol BiometricAuthenticating: AnyObject {
    /// `nil` when the device has no biometrics set up.
    func availableKind() -> BiometryKind?
    /// Asks for a face (or finger), with the device passcode as the fallback.
    func authenticate(reason: String) async -> (BiometricOutcome, BiometricProof?)
    /// Changes whenever faces or fingers are added or removed. `nil` without biometrics.
    func enrollmentState() -> Data?
}

final class LocalBiometricAuthenticator: BiometricAuthenticating {
    func availableKind() -> BiometryKind? {
        let context = LAContext()
        guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil) else { return nil }
        return BiometryKind(context.biometryType)
    }

    func authenticate(reason: String) async -> (BiometricOutcome, BiometricProof?) {
        let context = LAContext()
        do {
            // Owner authentication: biometrics first, then the device passcode.
            guard try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) else {
                return (.failed, nil)
            }
            return (.matched, BiometricProof(context: context))
        } catch {
            return (BiometricOutcome(error), nil)
        }
    }

    func enrollmentState() -> Data? {
        let context = LAContext()
        guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil) else { return nil }
        return context.domainState.biometry.stateHash
    }
}

#if DEBUG
    /// For UI tests: DINNEROS_FAKE_BIOMETRICS is a comma-separated list of answers, used in
    /// order with the last one repeating: match, cancel, nomatch, interrupt, or changed (a
    /// match after a face was added). Never asks the system.
    final class FakeBiometricAuthenticator: BiometricAuthenticating {
        var outcomes: [String]
        var kind: BiometryKind? = .faceID
        /// The enrollment now: different after "changed".
        private var state = Data([1])

        init(outcomes: [String]) {
            self.outcomes = outcomes
        }

        static func fromEnvironment(_ environment: [String: String]) -> FakeBiometricAuthenticator? {
            guard let list = environment["DINNEROS_FAKE_BIOMETRICS"], !list.isEmpty else { return nil }
            return FakeBiometricAuthenticator(outcomes: list.split(separator: ",").map(String.init))
        }

        func availableKind() -> BiometryKind? { kind }

        func authenticate(reason: String) async -> (BiometricOutcome, BiometricProof?) {
            let next = outcomes.count > 1 ? outcomes.removeFirst() : (outcomes.first ?? "match")
            let outcome: BiometricOutcome
            switch next {
            case "cancel": outcome = .canceled
            case "nomatch": outcome = .failed
            case "interrupt": outcome = .interrupted
            case "changed":
                state = Data([2])
                outcome = .matched
            default: outcome = .matched
            }
            return (outcome, outcome == .matched ? BiometricProof(context: nil) : nil)
        }

        func enrollmentState() -> Data? { kind == nil ? nil : state }
    }
#endif

/// Whether biometric sign-in is on for this device, kept in UserDefaults (no secrets: the
/// session itself is in the Keychain).
final class BiometricSetting {
    private let defaults: UserDefaults
    private let onKey = "biometricSignIn.on"
    private let offeredKey = "biometricSignIn.offered"
    private let stateKey = "biometricSignIn.enrollment"

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    var isOn: Bool { defaults.bool(forKey: onKey) }

    /// The offer after sign-in shows once; turning it off by signing out offers it again.
    var wasOffered: Bool {
        get { defaults.bool(forKey: offeredKey) }
        set { defaults.set(newValue, forKey: offeredKey) }
    }

    /// The enrollment state when it was turned on, to notice new faces or fingers.
    var enrollmentState: Data? { defaults.data(forKey: stateKey) }

    func turnOn(enrollmentState: Data?) {
        defaults.set(true, forKey: onKey)
        defaults.set(enrollmentState, forKey: stateKey)
        wasOffered = true
    }

    func turnOff(offerAgain: Bool) {
        defaults.removeObject(forKey: onKey)
        defaults.removeObject(forKey: stateKey)
        if offerAgain { wasOffered = false }
    }
}

/// The lock's states, with no UI or system calls, so every path is a unit test.
///
/// - Locked at launch when sign-in is behind Face ID, and again after more than 15 minutes in
///   the background.
/// - Asks once each time the app becomes active while locked. A cancel, a failed match, changed
///   faces or a missing Keychain item all end in normal sign-in, never another automatic ask, so
///   it can't loop. Only a system interruption (a call, the app leaving) asks again.
nonisolated struct BiometricLockMachine: Equatable, Sendable {
    enum Phase: Equatable, Sendable {
        case open
        case locked
        case asking
    }

    enum Scene: Equatable, Sendable {
        case active
        case inactive
        case background
    }

    enum Effect: Equatable, Sendable {
        case none
        /// Show the Face ID check.
        case ask
        /// Matched: show the app.
        case unlock
        /// Sign out and show the sign-in screen.
        case signInAnotherWay
    }

    static let fifteenMinutes: TimeInterval = 15 * 60

    /// Time away before it locks again. Shorter only in UI tests.
    var relockAfter: TimeInterval = Self.fifteenMinutes
    private(set) var phase: Phase = .open
    private(set) var backgroundedAt: Date?
    /// Set when locking; cleared when an ask starts, so each lock asks by itself only once.
    private(set) var asksWhenActive = false

    var isLocked: Bool { phase != .open }

    mutating func lock() {
        guard phase == .open else { return }
        phase = .locked
        asksWhenActive = true
        backgroundedAt = nil
    }

    mutating func sceneChanged(to scene: Scene, at date: Date, isOn: Bool) -> Effect {
        switch scene {
        case .background:
            if phase == .open, isOn { backgroundedAt = date }
            return .none
        case .inactive, .active:
            if phase == .open, let since = backgroundedAt {
                backgroundedAt = nil
                if isOn, date.timeIntervalSince(since) > relockAfter { lock() }
            }
            guard scene == .active, phase == .locked, asksWhenActive else { return .none }
            return .ask
        }
    }

    /// Starts an ask. `false` while one is already showing or when unlocked.
    mutating func beginAsking() -> Bool {
        guard phase == .locked else { return false }
        phase = .asking
        asksWhenActive = false
        return true
    }

    mutating func finish(_ outcome: BiometricOutcome) -> Effect {
        guard phase == .asking else { return .none }
        switch outcome {
        case .matched:
            phase = .open
            return .unlock
        case .interrupted:
            phase = .locked
            asksWhenActive = true
            return .none
        case .canceled, .failed, .unavailable, .changed, .missing:
            phase = .open
            return .signInAnotherWay
        }
    }

    /// The lock screen's "Sign in another way".
    mutating func chooseAnotherWay() -> Effect {
        guard phase == .locked else { return .none }
        phase = .open
        asksWhenActive = false
        return .signInAnotherWay
    }

    mutating func reset() {
        let relockAfter = relockAfter
        self = BiometricLockMachine()
        self.relockAfter = relockAfter
    }
}
