import Foundation

/// The APNs gateway this build's device tokens belong to. Xcode debug builds are signed
/// with the development `aps-environment` and get sandbox tokens; TestFlight and App Store
/// builds get production tokens. The API sends each token to its own gateway.
nonisolated enum PushEnvironment: String, Codable, Sendable {
    case sandbox
    case production

    static var current: PushEnvironment {
        #if DEBUG
            .sandbox
        #else
            .production
        #endif
    }
}

/// Whether this device may show alerts, as far as DinnerOS cares.
nonisolated enum PushAuthorization: Equatable, Sendable {
    /// Never asked. The app asks at a moment the member understands (an order day).
    case notDetermined
    /// Refused, or turned off in Settings. The bell keeps working.
    case denied
    /// Authorized, provisional, or ephemeral: alerts can arrive.
    case authorized
}

/// The body of `PUT /api/v1/me/device-tokens`.
nonisolated struct RegisterDeviceTokenRequest: Encodable, Equatable, Sendable {
    let token: String
    let environment: PushEnvironment
    var platform = "ios"
}

/// The body of `DELETE /api/v1/me/device-tokens`.
nonisolated struct DeleteDeviceTokenRequest: Encodable, Equatable, Sendable {
    let token: String
}

/// The response to `PUT /api/v1/me/device-tokens`.
nonisolated struct DeviceTokenRegistration: Decodable, Equatable, Sendable {
    let token: String
    let environment: PushEnvironment
    let platform: String
}

/// Where a tapped push opens: the same place its row on the bell opens.
nonisolated struct PushRoute: Equatable, Sendable, Identifiable {
    let notificationID: String
    let householdID: String
    let type: AppNotificationType
    let subject: AppNotificationSubject?

    var id: String { notificationID }

    /// Reads the custom keys the API adds beside `aps` (`internal/push`). `nil` when the
    /// payload isn't a DinnerOS notification.
    init?(userInfo: [AnyHashable: Any]) {
        guard
            let notificationID = userInfo["notificationId"] as? String, !notificationID.isEmpty,
            let householdID = userInfo["householdId"] as? String, !householdID.isEmpty
        else { return nil }
        self.notificationID = notificationID
        self.householdID = householdID
        type = AppNotificationType(rawValue: userInfo["type"] as? String ?? "")
        if let subject = userInfo["subject"] as? [String: Any],
            let kind = subject["kind"] as? String, let id = subject["id"] as? String
        {
            self.subject = AppNotificationSubject(kind: kind, id: id)
        } else {
            subject = nil
        }
    }

    init(notificationID: String, householdID: String, type: AppNotificationType, subject: AppNotificationSubject?) {
        self.notificationID = notificationID
        self.householdID = householdID
        self.type = type
        self.subject = subject
    }
}

extension Data {
    /// A device token as the lowercase hex APNs addresses it by.
    nonisolated var pushTokenHex: String {
        map { String(format: "%02x", $0) }.joined()
    }
}
