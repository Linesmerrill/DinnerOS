import DeviceCheck
import Foundation

/// A one-time Apple DeviceCheck token for this device, sent with household creation, joining
/// a household, and entering an invite code. The API asks Apple whether this device already
/// belongs to a household, so a throwaway household on the same phone can't earn free months.
/// Deleting and reinstalling the app doesn't change Apple's answer.
nonisolated struct DeviceCheckToken: Encodable, Equatable, Sendable {
    /// Base64, as Apple's API takes it.
    let token: String
    /// Development builds talk to Apple's development DeviceCheck service; TestFlight and the
    /// App Store use production.
    let development: Bool

    /// `nil` where DeviceCheck isn't available (the simulator) or Apple doesn't answer. The
    /// API treats a missing token as "can't verify", so nothing is granted on a guess.
    static func current() async -> DeviceCheckToken? {
        guard DCDevice.current.isSupported else { return nil }
        guard let data = try? await DCDevice.current.generateToken() else { return nil }
        return DeviceCheckToken(token: data.base64EncodedString(), development: isDevelopmentBuild)
    }

    static var isDevelopmentBuild: Bool {
        #if DEBUG
            true
        #else
            false
        #endif
    }
}
