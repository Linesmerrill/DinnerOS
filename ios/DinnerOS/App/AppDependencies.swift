import SwiftUI

/// Builds the app's long-lived services from configuration. `DinnerOSApp` is the only
/// place concrete implementations are chosen.
final class AppDependencies {
    let configuration: AppConfiguration
    let session: AuthSession
    /// `nil` when the build has no Google client ID; the Google button is then hidden.
    let googleSignIn: GoogleSignInService?

    init(configuration: AppConfiguration = .main) {
        self.configuration = configuration
        let transport = URLSessionTransport()
        let api = configuration.apiBaseURL.map { AuthAPI(client: APIClient(baseURL: $0, transport: transport)) }
        session = AuthSession(api: api, store: KeychainTokenStore())
        googleSignIn = GoogleOAuthConfiguration(clientID: configuration.googleIOSClientID).map {
            GoogleSignInService(configuration: $0, transport: transport)
        }
    }
}

extension EnvironmentValues {
    @Entry var googleSignIn: GoogleSignInService? = nil
}
