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
        }
    }
}
