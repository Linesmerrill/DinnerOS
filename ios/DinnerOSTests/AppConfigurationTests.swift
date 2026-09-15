import Foundation
import Testing

@testable import DinnerOS

struct AppConfigurationTests {
    @Test func readsValuesFromInfoDictionary() throws {
        let configuration = AppConfiguration(infoDictionary: [
            "CFBundleDisplayName": "Renamed",
            "AppEnvironment": "development",
            "APIBaseURL": "http://localhost:8080",
            "CFBundleShortVersionString": "1.2.3",
            "CFBundleVersion": "42",
            "GoogleIOSClientID": " 123-abc.apps.googleusercontent.com ",
        ])

        #expect(configuration.googleIOSClientID == "123-abc.apps.googleusercontent.com")
        #expect(configuration.displayName == "Renamed")
        #expect(configuration.environment == .development)
        #expect(configuration.apiBaseURL == URL(string: "http://localhost:8080"))
        #expect(configuration.version == "1.2.3")
        #expect(configuration.build == "42")
    }

    @Test func missingValuesFallBackToProductionWithoutURL() {
        let configuration = AppConfiguration(infoDictionary: [:])

        #expect(configuration.environment == .production)
        #expect(configuration.apiBaseURL == nil)
        #expect(configuration.googleIOSClientID == nil)
        #expect(configuration.displayName == "App")
    }

    @Test(arguments: [
        ("https://api.example.com", AppConfiguration.Environment.production, true),
        ("http://api.example.com", .production, false),
        ("http://localhost:8080", .development, true),
        ("https://api.example.com", .development, true),
        ("", .development, false),
        ("   ", .development, false),
        ("localhost:8080", .development, false),
        ("ftp://api.example.com", .development, false),
        ("https://", .production, false),
    ])
    func validatesAPIBaseURL(raw: String, environment: AppConfiguration.Environment, isValid: Bool) {
        #expect((AppConfiguration.parseAPIBaseURL(raw, environment: environment) != nil) == isValid)
    }
}

struct AppTabTests {
    @Test func tabsHaveUniqueIdentifiersAndSymbols() {
        let tabs = AppTab.allCases
        #expect(tabs.first == .recipes)
        #expect(Set(tabs.map(\.id)).count == tabs.count)
        #expect(Set(tabs.map(\.systemImage)).count == tabs.count)
    }
}
