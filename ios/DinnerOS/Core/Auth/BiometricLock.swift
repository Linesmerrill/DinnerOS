import Foundation
import Observation
import os

/// Face ID (or Touch ID, Optic ID) sign-in: the offer after signing in, the Account toggle, and
/// the lock at launch and after 15 minutes away (decision 760).
///
/// The rules live in `BiometricLockMachine`; this runs its effects. Every way a check can end
/// short of a match ends in normal sign-in, so the lock never hangs or loops.
@Observable
final class BiometricLock {
    private(set) var machine = BiometricLockMachine()
    /// Shows the offer after a sign-in.
    var isOffering = false
    /// Shown when turning it on didn't work.
    var turnOnError: String?
    private(set) var isTurningOn = false
    /// The lock came on over a signed-in app (time away), not at launch.
    private var lockedOverApp = false

    @ObservationIgnored let session: AuthSession
    @ObservationIgnored private let authenticator: any BiometricAuthenticating
    @ObservationIgnored private let now: () -> Date
    /// Whether the app is in front, so an ask isn't started for a background launch.
    @ObservationIgnored var isAppActive: () -> Bool = { true }

    private static let logger = Logger(subsystem: "DinnerOS", category: "auth")

    init(session: AuthSession, authenticator: any BiometricAuthenticating, now: @escaping () -> Date = Date.init) {
        self.session = session
        self.authenticator = authenticator
        self.now = now
        #if DEBUG
            if let seconds = ProcessInfo.processInfo.environment["QA_RELOCK_SECONDS"].flatMap(Double.init) {
                machine.relockAfter = seconds
            }
        #endif
        session.didSignIn = { [weak self] in self?.offerIfNeeded() }
    }

    /// The device's biometrics, or `nil` when none are set up. While it's on, the toggle stays
    /// even if biometrics were removed, so it can be turned off.
    var kind: BiometryKind? {
        authenticator.availableKind() ?? (session.isBiometricSignInOn ? .faceID : nil)
    }

    /// Showing the lock screen: locked at launch, or locked again over a signed-in app.
    var isLocked: Bool {
        session.state == .locked || machine.isLocked
    }

    /// Locked over a signed-in app (after 15 minutes away), as opposed to at launch.
    var coversSignedInApp: Bool {
        lockedOverApp && machine.isLocked
    }

    // MARK: Lock

    func sceneChanged(to scene: BiometricLockMachine.Scene) {
        syncWithSession()
        if scene == .active { session.saveUnsavedSession() }
        let wasLocked = machine.isLocked
        // Signed out with Face ID kept, there's nothing to lock: the sign-in screen shows.
        let effect = machine.sceneChanged(
            to: scene, at: now(), isOn: session.isBiometricSignInOn && session.state != .signedOut)
        if !wasLocked, machine.isLocked { lockedOverApp = session.currentUser != nil }
        if effect == .ask {
            Task { await ask() }
        }
    }

    /// The lock screen appeared: ask once, if the app is in front.
    func askIfReady() async {
        syncWithSession()
        guard machine.phase == .locked, machine.asksWhenActive, isAppActive() else { return }
        await ask()
    }

    /// Shows the Face ID check. Does nothing while one is already showing.
    func ask() async {
        syncWithSession()
        guard machine.beginAsking() else { return }
        let reason = String(localized: "Sign in to DinnerOS")
        var (outcome, proof) = await authenticator.authenticate(reason: reason)
        if outcome == .matched {
            if let saved = session.biometricSetting.enrollmentState, authenticator.enrollmentState() != saved {
                outcome = .changed
            } else if session.state == .locked, !(await session.unlock(with: proof ?? BiometricProof(context: nil))) {
                outcome = .missing
            }
        }
        Self.logger.info("Face ID check: \(String(describing: outcome), privacy: .public)")
        // Faces changed or Face ID is gone: the kept sign-in can't be read again, so it goes.
        // A cancel or a failed match keeps it for the sign-in screen's button.
        if outcome == .changed {
            session.forgetBiometricSignIn(
                notice: String(
                    localized:
                        "\((kind ?? .faceID).name) changed on this device. Sign in another way, then turn it on again in Account."
                ))
        } else if outcome == .unavailable {
            session.forgetBiometricSignIn(
                notice: String(localized: "\((kind ?? .faceID).name) isn’t set up on this device. Sign in another way.")
            )
        }
        // Whatever happens next, the sign-in screen doesn't ask again by itself.
        hasAutoAsked = true
        await run(machine.finish(outcome))
    }

    func signInAnotherWay() async {
        syncWithSession()
        await run(machine.chooseAnotherWay())
    }

    // MARK: Sign-in screen

    /// A Face ID check from the sign-in screen is showing.
    private(set) var isSigningIn = false
    /// The sign-in screen asks by itself once per launch, and not after the person signed out,
    /// chose another way, or canceled: those mean they want the other options.
    @ObservationIgnored private(set) var hasAutoAsked = false

    /// The sign-in screen appeared: ask once by itself, when it can.
    func signInIfReady() async {
        guard !hasAutoAsked, !session.wasSignedInThisLaunch, isAppActive(), session.canSignInWithBiometrics
        else { return }
        hasAutoAsked = true
        await signIn()
    }

    /// The sign-in screen's "Sign in with Face ID". A cancel or a failed match leaves the
    /// sign-in screen as it was, with every other way to sign in.
    func signIn() async {
        guard !isSigningIn, session.canSignInWithBiometrics else { return }
        hasAutoAsked = true
        isSigningIn = true
        defer { isSigningIn = false }
        let (outcome, proof) = await authenticator.authenticate(reason: String(localized: "Sign in to DinnerOS"))
        Self.logger.info("Face ID sign-in check: \(String(describing: outcome), privacy: .public)")
        switch outcome {
        case .matched:
            if let saved = session.biometricSetting.enrollmentState, authenticator.enrollmentState() != saved {
                let name = (kind ?? .faceID).name
                session.forgetBiometricSignIn(
                    notice: String(
                        localized:
                            "\(name) changed on this device. Sign in another way, then turn it on again in Account."))
                return
            }
            _ = await session.signInWithBiometrics(proof: proof ?? BiometricProof(context: nil))
        case .unavailable:
            session.forgetBiometricSignIn(
                notice: String(localized: "\((kind ?? .faceID).name) isn’t set up on this device. Sign in another way.")
            )
        case .canceled, .interrupted, .failed, .changed, .missing:
            break
        }
    }

    private func run(_ effect: BiometricLockMachine.Effect) async {
        if !machine.isLocked { lockedOverApp = false }
        switch effect {
        case .signInAnotherWay:
            machine.reset()
            lockedOverApp = false
            await session.signOut()
        case .ask:
            await ask()
        case .unlock, .none:
            break
        }
    }

    /// Locked at launch: the machine locks too, so it asks. Signed out underneath: unlock.
    private func syncWithSession() {
        if session.state == .locked, machine.phase == .open {
            machine.lock()
        } else if session.state == .signedOut, machine.isLocked {
            machine.reset()
            lockedOverApp = false
        }
    }

    // MARK: Offer and toggle

    /// After a sign-in, once per device, when it has biometrics and it isn't on yet.
    func offerIfNeeded() {
        guard !session.isBiometricSignInOn, !session.biometricSetting.wasOffered,
            authenticator.availableKind() != nil
        else { return }
        session.biometricSetting.wasOffered = true
        isOffering = true
    }

    /// Asks for a face first, which also asks permission the first time.
    @discardableResult
    func turnOn() async -> Bool {
        guard let kind = authenticator.availableKind(), !isTurningOn else { return false }
        isTurningOn = true
        defer { isTurningOn = false }
        let (outcome, _) = await authenticator.authenticate(
            reason: String(localized: "Use \(kind.name) to sign in to DinnerOS"))
        switch outcome {
        case .matched:
            do {
                try session.turnOnBiometricSignIn(enrollmentState: authenticator.enrollmentState())
                return true
            } catch {
                Self.logger.error("Face ID sign-in couldn’t turn on: \(String(describing: error), privacy: .public)")
                turnOnError = String(localized: "\(kind.name) couldn’t be turned on. Try again.")
                return false
            }
        case .canceled, .interrupted:
            return false
        case .failed, .unavailable, .changed, .missing:
            turnOnError = String(localized: "\(kind.name) didn’t work. Try again.")
            return false
        }
    }

    func turnOff() {
        session.turnOffBiometricSignIn()
    }
}
