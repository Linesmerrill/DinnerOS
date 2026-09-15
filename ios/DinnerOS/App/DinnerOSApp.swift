import SwiftUI

@main
struct DinnerOSApp: App {
    @State private var dependencies = AppDependencies()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(\.appConfiguration, dependencies.configuration)
                .environment(\.googleSignIn, dependencies.googleSignIn)
                .environment(dependencies.session)
                .environment(dependencies.households)
                .environment(dependencies.recipes)
                .environment(dependencies.pantry)
                .onOpenURL { url in
                    // Never log the URL: invitation links carry a secret token.
                    dependencies.households.handleOpenURL(url)
                }
        }
    }
}
