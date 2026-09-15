import SwiftUI

@main
struct DinnerOSApp: App {
    private let configuration = AppConfiguration.main

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(\.appConfiguration, configuration)
        }
    }
}
