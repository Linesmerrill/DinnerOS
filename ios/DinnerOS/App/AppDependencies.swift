import SwiftUI

/// Builds the app's long-lived services from configuration. `DinnerOSApp` is the only
/// place concrete implementations are chosen.
final class AppDependencies {
    let configuration: AppConfiguration
    let session: AuthSession
    let households: HouseholdStore
    let recipes: RecipeLibrary
    let pantry: PantryStore
    /// `nil` when the build has no Google client ID; the Google button is then hidden.
    let googleSignIn: GoogleSignInService?

    init(configuration: AppConfiguration = .main) {
        self.configuration = configuration
        let transport = URLSessionTransport()
        let client = configuration.apiBaseURL.map { APIClient(baseURL: $0, transport: transport) }
        session = AuthSession(api: client.map { AuthAPI(client: $0) }, store: KeychainTokenStore())
        households = HouseholdStore(
            session: session,
            api: client.map { HouseholdsAPI(client: $0) },
            selection: UserDefaultsHouseholdSelection(),
            inviteURLScheme: configuration.urlScheme)
        recipes = RecipeLibrary(session: session, api: client.map { RecipesAPI(client: $0) })
        pantry = PantryStore(
            session: session,
            api: client.map { PantryAPI(client: $0) },
            ingredientsAPI: client.map { IngredientsAPI(client: $0) })
        googleSignIn = GoogleOAuthConfiguration(clientID: configuration.googleIOSClientID).map {
            GoogleSignInService(configuration: $0, transport: transport)
        }
    }
}

extension EnvironmentValues {
    @Entry var googleSignIn: GoogleSignInService? = nil
}
