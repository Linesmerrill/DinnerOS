import Foundation
import SwiftUI

/// Build-time configuration injected through `ios/Config/*.xcconfig` → `Info.plist`.
///
/// Nothing secret belongs here: everything in the app bundle is readable by anyone
/// holding the binary. Secrets live on the backend.
struct AppConfiguration: Sendable, Equatable {
    enum Environment: String, Sendable {
        case development
        case production
    }

    /// The user-facing product name. "DinnerOS" is a working title, so views read it
    /// from here instead of hardcoding it.
    let displayName: String
    let environment: Environment
    /// `nil` when the build has no valid API URL configured.
    let apiBaseURL: URL?
    /// Google's iOS OAuth client ID (a public identifier). `nil` hides Google sign-in.
    let googleIOSClientID: String?
    /// The custom URL scheme invitation links open (`dinneros://invite?token=...`). It must
    /// match the API's `APP_INVITE_URL_BASE`.
    let urlScheme: String
    let version: String
    let build: String

    init(infoDictionary info: [String: Any]) {
        displayName =
            info["CFBundleDisplayName"] as? String
            ?? info["CFBundleName"] as? String
            ?? "App"
        // Unknown or missing values fall back to the stricter production rules.
        environment = Environment(rawValue: info["AppEnvironment"] as? String ?? "") ?? .production
        apiBaseURL = Self.parseAPIBaseURL(info["APIBaseURL"] as? String, environment: environment)
        let googleClientID = (info["GoogleIOSClientID"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines)
        googleIOSClientID = googleClientID?.isEmpty == false ? googleClientID : nil
        let scheme = (info["AppURLScheme"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        urlScheme = scheme.flatMap { $0.isEmpty ? nil : $0 } ?? InviteLink.defaultScheme
        version = info["CFBundleShortVersionString"] as? String ?? "0"
        build = info["CFBundleVersion"] as? String ?? "0"
    }

    static let main = AppConfiguration(infoDictionary: Bundle.main.infoDictionary ?? [:])

    /// Validates the configured API URL. Production builds only accept https.
    static func parseAPIBaseURL(_ raw: String?, environment: Environment) -> URL? {
        guard
            let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
            !raw.isEmpty,
            let url = URL(string: raw),
            let scheme = url.scheme?.lowercased(),
            let host = url.host(), !host.isEmpty
        else { return nil }

        switch (scheme, environment) {
        case ("https", _), ("http", .development):
            return url
        default:
            return nil
        }
    }
}

extension EnvironmentValues {
    @Entry var appConfiguration: AppConfiguration = .main
}
