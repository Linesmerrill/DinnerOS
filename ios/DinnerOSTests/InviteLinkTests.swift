import Foundation
import Testing

@testable import DinnerOS

struct InviteLinkTests {
    private func parse(_ string: String, scheme: String = InviteLink.defaultScheme) throws -> InviteLink? {
        InviteLink(url: try #require(URL(string: string)), scheme: scheme)
    }

    @Test(arguments: [
        "dinneros://invite?token=abc_DEF-123",
        "DinnerOS://INVITE?token=abc_DEF-123",
        "dinneros://invite/?token=abc_DEF-123",
        "dinneros://invite?utm=1&token=abc_DEF-123",
        "dinneros://invite?token=%20abc_DEF-123%20",
    ])
    func parsesValidLinks(string: String) throws {
        #expect(try parse(string) == .token("abc_DEF-123"))
    }

    @Test(arguments: [
        "dinneros://invite",
        "dinneros://invite?token=",
        "dinneros://invite?token=%20",
        "dinneros://invite?code=ABCDE12345",
    ])
    func reportsMissingToken(string: String) throws {
        #expect(try parse(string) == .missingToken)
    }

    @Test(arguments: [
        "https://invite?token=abc",
        "https://dinneros.example.com/invite?token=abc",
        "otherapp://invite?token=abc",
        "dinneros://join?token=abc",
        "dinneros://invite/extra?token=abc",
        "dinneros:invite?token=abc",
    ])
    func ignoresOtherURLs(string: String) throws {
        #expect(try parse(string) == nil)
    }

    @Test func usesTheConfiguredScheme() throws {
        #expect(try parse("renamed://invite?token=abc", scheme: "renamed") == .token("abc"))
        #expect(try parse("dinneros://invite?token=abc", scheme: "renamed") == nil)
    }
}

struct InviteCodeTests {
    @Test(arguments: [
        ("abcde-12345", "ABCDE12345"),
        (" ABCDE 12345 ", "ABCDE12345"),
        ("abcde\u{2013}12345", "ABCDE12345"),
        ("ab cd\te-12\n345", "ABCDE12345"),
        ("oil", "OIL"),
        ("", ""),
        ("--  --", ""),
    ])
    func normalizes(input: String, expected: String) {
        #expect(InviteCode.normalize(input) == expected)
    }
}

struct AppURLSchemeConfigurationTests {
    @Test func defaultsToDinnerOSScheme() {
        #expect(AppConfiguration(infoDictionary: [:]).urlScheme == "dinneros")
        #expect(AppConfiguration(infoDictionary: ["AppURLScheme": "  "]).urlScheme == "dinneros")
    }

    @Test func readsConfiguredScheme() {
        #expect(AppConfiguration(infoDictionary: ["AppURLScheme": " Renamed "]).urlScheme == "renamed")
    }
}
