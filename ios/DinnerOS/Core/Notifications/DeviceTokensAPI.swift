import Foundation

/// Typed wrappers for `/api/v1/me/device-tokens`, the signed-in user's push registrations.
/// The token travels in the body, never the path, so it stays out of request logs. Use
/// them through `AuthSession.authorized`.
nonisolated struct DeviceTokensAPI: Sendable {
    static let path = "/api/v1/me/device-tokens"

    let client: APIClient

    /// Registers (or refreshes) this device for push. Idempotent.
    func register(token: String, environment: PushEnvironment, accessToken: String) async throws
        -> DeviceTokenRegistration
    {
        try await client.send(
            try APIRequest.put(Self.path, body: RegisterDeviceTokenRequest(token: token, environment: environment))
                .authorized(with: accessToken))
    }

    /// Removes this device's registration, for sign-out. Idempotent.
    func delete(token: String, accessToken: String) async throws {
        let body = try JSONCoding.makeEncoder().encode(DeleteDeviceTokenRequest(token: token))
        try await client.sendIgnoringBody(
            APIRequest(method: .delete, path: Self.path, body: body, bearerToken: nil).authorized(with: accessToken))
    }
}
