import Foundation
import LocalAuthentication
import Testing

@testable import DinnerOS

/// Face ID sign-in (decision 760): the lock's states, where the session is kept, and the
/// coordinator that runs the checks.
struct BiometricLockMachineTests {
    private let start = Date(timeIntervalSince1970: 1_800_000_000)

    @Test func startsOpenAndLocksAtLaunchAskingOnce() {
        var machine = BiometricLockMachine()
        #expect(!machine.isLocked)
        machine.lock()
        #expect(machine.phase == .locked)
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .ask)
        }
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        // A second ask while one shows does nothing.
        do {
            let result = machine.beginAsking()
            #expect(!result)
        }
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .none)
        }
    }

    @Test func matchUnlocks() {
        var machine = locked()
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        do {
            let result = machine.finish(.matched)
            #expect(result == .unlock)
        }
        #expect(machine.phase == .open)
    }

    @Test(arguments: [BiometricOutcome.canceled, .failed, .unavailable, .changed, .missing])
    func everythingElseFallsBackToSignInWithoutAskingAgain(_ outcome: BiometricOutcome) {
        var machine = locked()
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        do {
            let result = machine.finish(outcome)
            #expect(result == .signInAnotherWay)
        }
        #expect(machine.phase == .open)
        // No loop: coming back active doesn't ask.
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .none)
        }
    }

    @Test func aSystemInterruptionAsksAgainOnlyWhenBackActive() {
        var machine = locked()
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        do {
            let result = machine.finish(.interrupted)
            #expect(result == .none)
        }
        #expect(machine.phase == .locked)
        do {
            let result = machine.sceneChanged(to: .inactive, at: start, isOn: true)
            #expect(result == .none)
        }
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .ask)
        }
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        // Interrupted again: still one ask per activation.
        do {
            let result = machine.finish(.interrupted)
            #expect(result == .none)
        }
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .ask)
        }
    }

    @Test func finishingWithoutAnAskIsIgnored() {
        var machine = BiometricLockMachine()
        do {
            let result = machine.finish(.matched)
            #expect(result == .none)
        }
        do {
            let result = machine.finish(.canceled)
            #expect(result == .none)
        }
        var lockedOnly = locked()
        do {
            let result = lockedOnly.finish(.canceled)
            #expect(result == .none)
        }
        #expect(lockedOnly.phase == .locked)
    }

    @Test func relocksAfterMoreThanFifteenMinutesAway() {
        var machine = BiometricLockMachine()
        _ = machine.sceneChanged(to: .background, at: start, isOn: true)
        let back = start.addingTimeInterval(15 * 60 + 1)
        // Locked already on the way in, so nothing shows before the lock.
        do {
            let result = machine.sceneChanged(to: .inactive, at: back, isOn: true)
            #expect(result == .none)
        }
        #expect(machine.phase == .locked)
        do {
            let result = machine.sceneChanged(to: .active, at: back, isOn: true)
            #expect(result == .ask)
        }
    }

    @Test func fifteenMinutesOrLessStaysOpen() {
        var machine = BiometricLockMachine()
        _ = machine.sceneChanged(to: .background, at: start, isOn: true)
        do {
            let result = machine.sceneChanged(to: .active, at: start.addingTimeInterval(15 * 60), isOn: true)
            #expect(result == .none)
        }
        #expect(machine.phase == .open)
        // The time away starts again each time it leaves.
        _ = machine.sceneChanged(to: .background, at: start.addingTimeInterval(20 * 60), isOn: true)
        do {
            let result = machine.sceneChanged(to: .active, at: start.addingTimeInterval(30 * 60), isOn: true)
            #expect(result == .none)
        }
        #expect(machine.phase == .open)
    }

    @Test func offNeverLocks() {
        var machine = BiometricLockMachine()
        _ = machine.sceneChanged(to: .background, at: start, isOn: false)
        do {
            let result = machine.sceneChanged(to: .active, at: start.addingTimeInterval(3600), isOn: false)
            #expect(result == .none)
        }
        #expect(machine.phase == .open)
        // Turned off while away: no lock either.
        _ = machine.sceneChanged(to: .background, at: start, isOn: true)
        do {
            let result = machine.sceneChanged(to: .active, at: start.addingTimeInterval(3600), isOn: false)
            #expect(result == .none)
        }
        #expect(machine.phase == .open)
    }

    @Test func signInAnotherWayOnlyFromTheLockScreen() {
        var machine = BiometricLockMachine()
        do {
            let result = machine.chooseAnotherWay()
            #expect(result == .none)
        }
        machine = locked()
        do {
            let result = machine.chooseAnotherWay()
            #expect(result == .signInAnotherWay)
        }
        #expect(machine.phase == .open)
        do {
            let result = machine.sceneChanged(to: .active, at: start, isOn: true)
            #expect(result == .none)
        }
        // Not while the check is showing: its result decides.
        machine = locked()
        do {
            let result = machine.beginAsking()
            #expect(result)
        }
        do {
            let result = machine.chooseAnotherWay()
            #expect(result == .none)
        }
    }

    @Test func laErrorsMapToOutcomes() {
        #expect(BiometricOutcome(LAError(.userCancel)) == .canceled)
        #expect(BiometricOutcome(LAError(.systemCancel)) == .interrupted)
        #expect(BiometricOutcome(LAError(.appCancel)) == .interrupted)
        #expect(BiometricOutcome(LAError(.notInteractive)) == .interrupted)
        #expect(BiometricOutcome(LAError(.biometryNotEnrolled)) == .unavailable)
        #expect(BiometricOutcome(LAError(.passcodeNotSet)) == .unavailable)
        #expect(BiometricOutcome(LAError(.authenticationFailed)) == .failed)
        #expect(BiometricOutcome(LAError(.biometryLockout)) == .failed)
        #expect(BiometricOutcome(URLError(.badURL)) == .failed)
    }

    @Test func namesFollowTheDevice() {
        #expect(BiometryKind(.faceID)?.name == "Face ID")
        #expect(BiometryKind(.touchID)?.name == "Touch ID")
        #expect(BiometryKind(.opticID)?.name == "Optic ID")
        #expect(BiometryKind(.none) == nil)
        #expect(BiometryKind.touchID.offerTitle == "Use Touch ID to sign in next time?")
        #expect(BiometryKind.faceID.toggleTitle == "Sign In with Face ID")
    }

    private func locked() -> BiometricLockMachine {
        var machine = BiometricLockMachine()
        machine.lock()
        return machine
    }
}

/// A Face ID stand-in whose answers are set per test.
final class ScriptedAuthenticator: BiometricAuthenticating {
    var kind: BiometryKind? = .faceID
    var outcomes: [BiometricOutcome]
    var state: Data? = Data([1])
    private(set) var asks = 0

    init(_ outcomes: [BiometricOutcome] = [.matched]) {
        self.outcomes = outcomes
    }

    func availableKind() -> BiometryKind? { kind }

    func authenticate(reason: String) async -> (BiometricOutcome, BiometricProof?) {
        asks += 1
        let outcome = outcomes.isEmpty ? .matched : outcomes.removeFirst()
        return (outcome, outcome == .matched ? BiometricProof(context: nil) : nil)
    }

    func enrollmentState() -> Data? { state }
}

struct BiometricSessionTests {
    private let logoutPath = "/api/v1/auth/logout"
    private let refreshPath = "/api/v1/auth/refresh"

    private struct Setup {
        let session: AuthSession
        let store: InMemoryTokenStore
        let protected: InMemoryProtectedSessionStore
        let setting: BiometricSetting
        let transport: StubTransport
    }

    private func setup(
        stored: StoredSession? = nil, protected: StoredSession? = nil, on: Bool = false,
        handler: @escaping StubTransport.Handler = { _ in (204, Data()) }
    ) throws -> Setup {
        let transport = StubTransport(handler)
        let store = InMemoryTokenStore(session: stored)
        let protectedStore = InMemoryProtectedSessionStore(session: protected)
        let defaults = try #require(UserDefaults(suiteName: "biometric-tests-\(UUID().uuidString)"))
        let setting = BiometricSetting(defaults: defaults)
        if on { setting.turnOn(enrollmentState: Data([1])) }
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        let session = AuthSession(api: api, store: store, protectedStore: protectedStore, biometricSetting: setting)
        return Setup(session: session, store: store, protected: protectedStore, setting: setting, transport: transport)
    }

    // Built once per test: Fixtures.tokens() reads the clock, so a second ticking over between
    // two reads made them unequal (a flaky CI failure).
    private let saved = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)

    @Test func offByDefaultRestoresAsBefore() async throws {
        let s = try setup(stored: saved)
        #expect(!s.session.isBiometricSignInOn)
        await s.session.restore()
        #expect(s.session.state == .signedIn(Fixtures.user))
        #expect(s.protected.session == nil)
    }

    @Test func turningOnMovesTheSessionBehindFaceID() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        try s.session.turnOnBiometricSignIn(enrollmentState: Data([7]))
        #expect(s.session.isBiometricSignInOn)
        #expect(s.setting.isOn)
        #expect(s.setting.enrollmentState == Data([7]))
        #expect(s.protected.session == saved)
        // Nothing readable without Face ID is left.
        #expect(s.store.session == nil)
    }

    @Test func onAtLaunchLocksWithoutReadingAnything() async throws {
        let s = try setup(protected: saved, on: true)
        await s.session.restore()
        #expect(s.session.state == .locked)
        #expect(s.session.currentUser == nil)
        #expect(s.transport.requests.isEmpty)
        #expect(await s.session.unlock(with: BiometricProof(context: nil)))
        #expect(s.session.state == .signedIn(Fixtures.user))
    }

    @Test func aMissingItemAtLaunchIsNormalSignIn() async throws {
        let s = try setup(protected: nil, on: true)
        await s.session.restore()
        #expect(s.session.state == .signedOut)
        #expect(!s.setting.isOn)
        #expect(!s.session.isBiometricSignInOn)
    }

    @Test func anItemGoneAfterTheCheckSignsOut() async throws {
        let s = try setup(protected: saved, on: true)
        await s.session.restore()
        s.protected.session = nil
        #expect(!(await s.session.unlock(with: BiometricProof(context: nil))))
        #expect(s.session.state == .signedOut)
        #expect(!s.setting.isOn)
    }

    @Test func refreshedTokensStayBehindFaceID() async throws {
        let expired = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let s = try setup(protected: expired, on: true) { _ in
            (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
        }
        await s.session.restore()
        #expect(await s.session.unlock(with: BiometricProof(context: nil)))
        #expect(s.protected.session?.tokens.refreshToken == "refresh-2")
        #expect(s.store.session == nil)
    }

    @Test func aWriteWhileTheDeviceIsLockedIsRetried() async throws {
        let expired = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let s = try setup(protected: expired, on: true) { _ in
            (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
        }
        await s.session.restore()
        s.protected.failsSaving = true
        #expect(await s.session.unlock(with: BiometricProof(context: nil)))
        // The old item stays, the new tokens wait in memory.
        #expect(s.protected.session?.tokens.refreshToken == "refresh-1")
        s.protected.failsSaving = false
        s.session.saveUnsavedSession()
        #expect(s.protected.session?.tokens.refreshToken == "refresh-2")
        #expect(s.store.session == nil)
    }

    @Test func turningOffMovesItBack() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        try s.session.turnOnBiometricSignIn(enrollmentState: nil)
        s.session.turnOffBiometricSignIn()
        #expect(!s.session.isBiometricSignInOn)
        #expect(!s.setting.isOn)
        #expect(s.store.session == saved)
        #expect(s.protected.session == nil)
    }

    @Test func signOutKeepsTheItemForFaceIDSignIn() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        try s.session.turnOnBiometricSignIn(enrollmentState: nil)
        await s.session.signOut()
        #expect(s.session.state == .signedOut)
        #expect(s.session.currentUser == nil)
        // Kept behind Face ID, and not revoked, so the sign-in screen can use it.
        #expect(s.session.isBiometricSignInOn)
        #expect(s.setting.isOn)
        #expect(s.protected.session?.user == Fixtures.user)
        #expect(s.protected.session?.tokens.refreshToken == "refresh-1")
        #expect(s.store.session == nil)
        #expect(s.transport.requests(to: logoutPath).isEmpty)
        #expect(s.session.canSignInWithBiometrics)
        #expect(s.setting.isSignedOut)
    }

    @Test func signedOutWithFaceIDKeptLaunchesToTheSignInScreen() async throws {
        let s = try setup(protected: saved, on: true)
        s.setting.isSignedOut = true
        await s.session.restore()
        #expect(s.session.state == .signedOut)
        #expect(s.session.canSignInWithBiometrics)
        #expect(s.transport.requests.isEmpty)
    }

    @Test func faceIDSignInChecksWithTheServerAndKeepsNewTokensBehindIt() async throws {
        let s = try setup(protected: saved, on: true) { _ in
            (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
        }
        s.setting.isSignedOut = true
        await s.session.restore()
        #expect(await s.session.signInWithBiometrics(proof: BiometricProof(context: nil)) == .signedIn)
        #expect(s.session.state == .signedIn(Fixtures.user))
        #expect(s.transport.requests(to: refreshPath).count == 1)
        #expect(s.protected.session?.tokens.refreshToken == "refresh-2")
        #expect(s.store.session == nil)
        #expect(!s.setting.isSignedOut)
        #expect(s.session.biometricNotice == nil)
    }

    @Test(arguments: [400, 401])
    func aSignInTheServerTurnsDownRemovesItAndSaysSo(_ status: Int) async throws {
        let s = try setup(protected: saved, on: true) { _ in
            (status, Fixtures.errorJSON(code: "invalid_refresh_token"))
        }
        s.setting.isSignedOut = true
        await s.session.restore()
        #expect(await s.session.signInWithBiometrics(proof: BiometricProof(context: nil)) == .rejected)
        #expect(s.session.state == .signedOut)
        #expect(s.protected.session == nil)
        #expect(!s.session.isBiometricSignInOn)
        #expect(!s.setting.isOn)
        #expect(!s.session.canSignInWithBiometrics)
        #expect(s.session.biometricNotice == AuthSession.biometricExpiredNotice)
    }

    @Test func offlineFaceIDSignInUsesTheSavedSession() async throws {
        let s = try setup(protected: saved, on: true) { _ in throw URLError(.notConnectedToInternet) }
        s.setting.isSignedOut = true
        await s.session.restore()
        #expect(await s.session.signInWithBiometrics(proof: BiometricProof(context: nil)) == .signedIn)
        #expect(s.session.state == .signedIn(Fixtures.user))
        // Kept as it was: the refresh waits until the app is back online.
        #expect(s.protected.session?.tokens.refreshToken == "refresh-1")
    }

    @Test func anExpiredKeptSignInIsRejectedWithoutAsking() async throws {
        let old = StoredSession(tokens: Fixtures.tokens(refreshExpiresIn: -60), user: Fixtures.user)
        let s = try setup(protected: old, on: true)
        s.setting.isSignedOut = true
        await s.session.restore()
        #expect(await s.session.signInWithBiometrics(proof: BiometricProof(context: nil)) == .rejected)
        #expect(s.transport.requests.isEmpty)
        #expect(s.protected.session == nil)
        #expect(s.session.biometricNotice != nil)
    }

    @Test func signingInAnotherWayReplacesTheKeptSignIn() async throws {
        let s = try setup(protected: saved, on: true) { _ in
            (200, Fixtures.sessionJSON(access: "a-new", refresh: "r-new"))
        }
        s.setting.isSignedOut = true
        await s.session.restore()
        try await s.session.signIn { api in try await api.signInWithGoogle(idToken: "g", rawNonce: "n") }
        #expect(s.protected.session?.tokens.refreshToken == "r-new")
        #expect(s.store.session == nil)
        #expect(!s.setting.isSignedOut)
        #expect(s.session.isBiometricSignInOn)
    }

    @Test func turningOffThenSigningOutLeavesNoFaceIDSignIn() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        try s.session.turnOnBiometricSignIn(enrollmentState: nil)
        s.session.turnOffBiometricSignIn()
        await s.session.signOut()
        #expect(!s.session.canSignInWithBiometrics)
        #expect(s.protected.session == nil)
        #expect(s.transport.requests(to: logoutPath).count == 1)
    }

    @Test func deletingTheAccountRemovesTheKeptSignIn() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        try s.session.turnOnBiometricSignIn(enrollmentState: nil)
        try await s.session.deleteAccount()
        #expect(s.session.state == .signedOut)
        #expect(s.protected.session == nil)
        #expect(!s.setting.isOn)
        #expect(!s.session.canSignInWithBiometrics)
    }

    @Test func turningOnSignedOutFails() async throws {
        let s = try setup()
        await s.session.restore()
        #expect(throws: AuthSessionError.signedOut) { try s.session.turnOnBiometricSignIn(enrollmentState: nil) }
        #expect(!s.setting.isOn)
    }

    @Test func aFailedWriteLeavesItOff() async throws {
        let s = try setup(stored: saved)
        await s.session.restore()
        s.protected.failsSaving = true
        #expect(throws: (any Error).self) { try s.session.turnOnBiometricSignIn(enrollmentState: nil) }
        #expect(!s.session.isBiometricSignInOn)
        #expect(s.store.session == saved)
    }

    @Test func withoutAProtectedStoreItIsOff() throws {
        let defaults = try #require(UserDefaults(suiteName: "biometric-tests-\(UUID().uuidString)"))
        let setting = BiometricSetting(defaults: defaults)
        setting.turnOn(enrollmentState: nil)
        let session = AuthSession(api: nil, store: InMemoryTokenStore(), biometricSetting: setting)
        #expect(!session.isBiometricSignInOn)
        #expect(!setting.isOn)
    }
}

struct BiometricLockTests {
    private func setup(
        _ outcomes: [BiometricOutcome], locked: Bool = true
    ) async throws -> (BiometricLock, AuthSession, ScriptedAuthenticator, InMemoryProtectedSessionStore) {
        let transport = StubTransport { _ in (204, Data()) }
        let saved = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let protected = InMemoryProtectedSessionStore(session: locked ? saved : nil)
        let defaults = try #require(UserDefaults(suiteName: "biometric-lock-\(UUID().uuidString)"))
        let setting = BiometricSetting(defaults: defaults)
        if locked { setting.turnOn(enrollmentState: Data([1])) }
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        let session = AuthSession(
            api: api, store: InMemoryTokenStore(session: locked ? nil : saved), protectedStore: protected,
            biometricSetting: setting)
        let authenticator = ScriptedAuthenticator(outcomes)
        let lock = BiometricLock(session: session, authenticator: authenticator)
        await session.restore()
        return (lock, session, authenticator, protected)
    }

    @Test func aMatchAtLaunchSignsIn() async throws {
        let (lock, session, auth, _) = try await setup([.matched])
        #expect(lock.isLocked)
        // At launch the lock is the screen itself, not a window over the app.
        #expect(!lock.coversSignedInApp)
        await lock.askIfReady()
        #expect(session.state == .signedIn(Fixtures.user))
        #expect(!lock.isLocked)
        #expect(auth.asks == 1)
    }

    @Test(arguments: [BiometricOutcome.canceled, .failed])
    func aCancelOrFailureAtLaunchIsNormalSignIn(_ outcome: BiometricOutcome) async throws {
        let (lock, session, auth, protected) = try await setup([outcome])
        await lock.askIfReady()
        #expect(session.state == .signedOut)
        #expect(!lock.isLocked)
        // Kept: the sign-in screen offers Face ID again, with the other ways.
        #expect(session.isBiometricSignInOn)
        #expect(protected.session != nil)
        #expect(session.canSignInWithBiometrics)
        // Back active: no second ask, from the lock or the sign-in screen.
        lock.sceneChanged(to: .active)
        await lock.askIfReady()
        await lock.signInIfReady()
        #expect(auth.asks == 1)
    }

    @Test func noFaceIDAtLaunchRemovesTheKeptSignIn() async throws {
        let (lock, session, _, protected) = try await setup([.unavailable])
        await lock.askIfReady()
        #expect(session.state == .signedOut)
        #expect(!session.isBiometricSignInOn)
        #expect(protected.session == nil)
        #expect(session.biometricNotice != nil)
    }

    @Test func changedFacesAreNormalSignIn() async throws {
        let (lock, session, auth, _) = try await setup([.matched])
        auth.state = Data([2])
        await lock.askIfReady()
        #expect(session.state == .signedOut)
        #expect(!session.isBiometricSignInOn)
    }

    @Test func aMissingItemAfterTheMatchIsNormalSignIn() async throws {
        let (lock, session, _, protected) = try await setup([.matched])
        protected.session = nil
        await lock.askIfReady()
        #expect(session.state == .signedOut)
        #expect(!lock.isLocked)
    }

    @Test func anInterruptionWaitsForTheNextActivation() async throws {
        let (lock, session, auth, _) = try await setup([.interrupted, .matched])
        await lock.askIfReady()
        #expect(session.state == .locked)
        #expect(auth.asks == 1)
        // Back in front, it asks once more.
        await lock.askIfReady()
        #expect(auth.asks == 2)
        #expect(session.state == .signedIn(Fixtures.user))
    }

    @Test func notAskedWhileInTheBackground() async throws {
        let (lock, session, auth, _) = try await setup([.matched])
        lock.isAppActive = { false }
        await lock.askIfReady()
        #expect(auth.asks == 0)
        #expect(session.state == .locked)
    }

    @Test func signInAnotherWaySignsOut() async throws {
        let (lock, session, auth, protected) = try await setup([.matched])
        lock.isAppActive = { false }
        await lock.askIfReady()
        await lock.signInAnotherWay()
        #expect(session.state == .signedOut)
        // The sign-in screen keeps its Face ID button but doesn't ask by itself.
        #expect(protected.session != nil)
        await lock.signInIfReady()
        #expect(auth.asks == 0)
    }

    @Test func offerShowsOnceAfterSignInWhenTheDeviceHasBiometrics() async throws {
        let (lock, session, _, _) = try await setup([], locked: false)
        #expect(!lock.isOffering)
        lock.offerIfNeeded()
        #expect(lock.isOffering)
        lock.isOffering = false
        lock.offerIfNeeded()
        #expect(!lock.isOffering)
        #expect(session.biometricSetting.wasOffered)
    }

    @Test func noOfferWithoutBiometrics() async throws {
        let (lock, _, auth, _) = try await setup([], locked: false)
        auth.kind = nil
        lock.offerIfNeeded()
        #expect(!lock.isOffering)
        #expect(lock.kind == nil)
    }

    @Test func signInTriggersTheOffer() async throws {
        let transport = StubTransport { _ in (200, Fixtures.sessionJSON(access: "a", refresh: "r")) }
        let defaults = try #require(UserDefaults(suiteName: "biometric-lock-\(UUID().uuidString)"))
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        let session = AuthSession(
            api: api, store: InMemoryTokenStore(), protectedStore: InMemoryProtectedSessionStore(),
            biometricSetting: BiometricSetting(defaults: defaults))
        let lock = BiometricLock(session: session, authenticator: ScriptedAuthenticator())
        await session.restore()
        try await session.signIn { api in try await api.signInWithGoogle(idToken: "g", rawNonce: "n") }
        #expect(lock.isOffering)
    }

    @Test func turnOnAsksFirst() async throws {
        let (lock, session, auth, protected) = try await setup([.canceled, .matched], locked: false)
        #expect(!(await lock.turnOn()))
        #expect(!session.isBiometricSignInOn)
        #expect(lock.turnOnError == nil)
        #expect(await lock.turnOn())
        #expect(session.isBiometricSignInOn)
        #expect(protected.session != nil)
        #expect(auth.asks == 2)
        lock.turnOff()
        #expect(!session.isBiometricSignInOn)
        #expect(protected.session == nil)
    }

    @Test func turnOnFailureSaysSo() async throws {
        let (lock, session, _, _) = try await setup([.failed], locked: false)
        #expect(!(await lock.turnOn()))
        #expect(lock.turnOnError == "Face ID didn’t work. Try again.")
        #expect(!session.isBiometricSignInOn)
    }

    @Test func relocksAfterFifteenMinutesAndAMatchOpensItAgain() async throws {
        var clock = Date(timeIntervalSince1970: 1_800_000_000)
        let (_, session, _, _) = try await setup([], locked: false)
        let auth = ScriptedAuthenticator([.matched, .matched])
        let lock = BiometricLock(session: session, authenticator: auth, now: { clock })
        #expect(await lock.turnOn())
        lock.sceneChanged(to: .background)
        clock = clock.addingTimeInterval(16 * 60)
        lock.sceneChanged(to: .inactive)
        #expect(lock.coversSignedInApp)
        await lock.askIfReady()
        #expect(!lock.isLocked)
        // Signed in all along: nothing was reset.
        #expect(session.state == .signedIn(Fixtures.user))
    }

    @Test func aRelockCancelSignsOutFully() async throws {
        var clock = Date(timeIntervalSince1970: 1_800_000_000)
        let (_, session, _, _) = try await setup([], locked: false)
        let auth = ScriptedAuthenticator([.matched, .canceled])
        let lock = BiometricLock(session: session, authenticator: auth, now: { clock })
        #expect(await lock.turnOn())
        lock.sceneChanged(to: .background)
        clock = clock.addingTimeInterval(16 * 60)
        lock.sceneChanged(to: .inactive)
        await lock.askIfReady()
        #expect(session.state == .signedOut)
        #expect(!lock.isLocked)
        #expect(session.canSignInWithBiometrics)
    }

    // MARK: The sign-in screen

    /// Signed out with Face ID kept, as after a sign-out on a past launch.
    private func signedOut(
        _ outcomes: [BiometricOutcome],
        refresh: @escaping StubTransport.Handler = { _ in
            (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
        }
    ) async throws -> (BiometricLock, AuthSession, ScriptedAuthenticator, InMemoryProtectedSessionStore) {
        let saved = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let protected = InMemoryProtectedSessionStore(session: saved)
        let defaults = try #require(UserDefaults(suiteName: "biometric-signin-\(UUID().uuidString)"))
        let setting = BiometricSetting(defaults: defaults)
        setting.turnOn(enrollmentState: Data([1]))
        setting.isSignedOut = true
        let api = AuthAPI(
            client: APIClient(
                baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: StubTransport(refresh)))
        let session = AuthSession(
            api: api, store: InMemoryTokenStore(), protectedStore: protected, biometricSetting: setting)
        let authenticator = ScriptedAuthenticator(outcomes)
        let lock = BiometricLock(session: session, authenticator: authenticator)
        await session.restore()
        return (lock, session, authenticator, protected)
    }

    @Test func theSignInScreenAsksOnceByItselfAndAMatchSignsIn() async throws {
        let (lock, session, auth, _) = try await signedOut([.matched])
        #expect(!lock.isLocked)
        await lock.signInIfReady()
        #expect(session.state == .signedIn(Fixtures.user))
        #expect(auth.asks == 1)
    }

    @Test(arguments: [BiometricOutcome.canceled, .failed, .interrupted])
    func aCancelOrNoMatchKeepsEveryWayToSignIn(_ outcome: BiometricOutcome) async throws {
        let (lock, session, auth, protected) = try await signedOut([outcome, .matched])
        await lock.signInIfReady()
        #expect(session.state == .signedOut)
        #expect(session.canSignInWithBiometrics)
        #expect(protected.session != nil)
        #expect(session.biometricNotice == nil)
        // Only once by itself; the button still works.
        await lock.signInIfReady()
        #expect(auth.asks == 1)
        await lock.signIn()
        #expect(session.state == .signedIn(Fixtures.user))
        #expect(auth.asks == 2)
    }

    @Test func changedFacesOnTheSignInScreenRemoveItAndSaySo() async throws {
        let (lock, session, auth, protected) = try await signedOut([.matched])
        auth.state = Data([2])
        await lock.signIn()
        #expect(session.state == .signedOut)
        #expect(protected.session == nil)
        #expect(!session.canSignInWithBiometrics)
        #expect(
            session.biometricNotice
                == "Face ID changed on this device. Sign in another way, then turn it on again in Account.")
    }

    @Test func aServerRejectionOnTheSignInScreenFallsBack() async throws {
        let (lock, session, _, protected) = try await signedOut([.matched]) { _ in
            (401, Fixtures.errorJSON(code: "invalid_refresh_token"))
        }
        await lock.signIn()
        #expect(session.state == .signedOut)
        #expect(protected.session == nil)
        #expect(!session.canSignInWithBiometrics)
        #expect(session.biometricNotice == AuthSession.biometricExpiredNotice)
    }

    @Test func noAskByItselfAfterSigningOutThisLaunch() async throws {
        let (lock, session, auth, _) = try await signedOut([.matched])
        await lock.signIn()
        await session.signOut()
        #expect(session.canSignInWithBiometrics)
        await lock.signInIfReady()
        #expect(auth.asks == 1)
    }

    @Test func signedOutWithFaceIDKeptNeverLocks() async throws {
        var clock = Date(timeIntervalSince1970: 1_800_000_000)
        let (_, session, _, _) = try await signedOut([])
        let lock = BiometricLock(session: session, authenticator: ScriptedAuthenticator(), now: { clock })
        lock.sceneChanged(to: .background)
        clock = clock.addingTimeInterval(16 * 60)
        lock.sceneChanged(to: .active)
        #expect(!lock.isLocked)
        #expect(session.state == .signedOut)
    }

    @Test func noButtonWithoutAKeptSignIn() async throws {
        let (lock, session, auth, _) = try await setup([], locked: false)
        await session.signOut()
        #expect(!session.canSignInWithBiometrics)
        await lock.signInIfReady()
        await lock.signIn()
        #expect(auth.asks == 0)
    }
}
