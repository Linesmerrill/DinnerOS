import Foundation
import Testing

@testable import DinnerOS

struct InviteLinkTests {
    private func parse(
        _ string: String, scheme: String = InviteLink.defaultScheme, webHost: String = InviteLink.defaultWebHost
    ) throws -> InviteLink? {
        InviteLink(url: try #require(URL(string: string)), scheme: scheme, webHost: webHost)
    }

    @Test(arguments: [
        // Universal links, with the token in the fragment.
        "https://api.tlps.dev/invite#token=abc_DEF-123",
        "HTTPS://API.TLPS.DEV/invite#token=abc_DEF-123",
        "https://api.tlps.dev/invite/#token=abc_DEF-123",
        "https://api.tlps.dev/invite#from=email&token=abc_DEF-123",
        "https://api.tlps.dev/invite#token=%20abc_DEF-123%20",
        "https://api.tlps.dev/invite?utm=email#token=abc_DEF-123",
        // A token in the query also works.
        "https://api.tlps.dev/invite?token=abc_DEF-123",
        // Legacy custom-scheme links from emails already sent.
        "dinneros://invite?token=abc_DEF-123",
        "DinnerOS://INVITE?token=abc_DEF-123",
        "dinneros://invite/?token=abc_DEF-123",
        "dinneros://invite?utm=1&token=abc_DEF-123",
        "dinneros://invite?token=%20abc_DEF-123%20",
        "dinneros://invite#token=abc_DEF-123",
    ])
    func parsesValidLinks(string: String) throws {
        #expect(try parse(string) == .token("abc_DEF-123"))
    }

    @Test func fragmentTokenWinsOverQuery() throws {
        #expect(try parse("https://api.tlps.dev/invite?token=old#token=new") == .token("new"))
        #expect(try parse("https://api.tlps.dev/invite?token=old#token=") == .token("old"))
    }

    @Test(arguments: [
        "https://api.tlps.dev/invite",
        "https://api.tlps.dev/invite#",
        "https://api.tlps.dev/invite#token=",
        "https://api.tlps.dev/invite#token",
        "https://api.tlps.dev/invite#code=ABCDE12345",
        "dinneros://invite",
        "dinneros://invite?token=",
        "dinneros://invite?token=%20",
        "dinneros://invite?code=ABCDE12345",
    ])
    func reportsMissingToken(string: String) throws {
        #expect(try parse(string) == .missingToken)
    }

    @Test(arguments: [
        // Wrong host, scheme, port, or path.
        "https://example.com/invite#token=abc",
        "https://api.tlps.dev.example.com/invite#token=abc",
        "https://tlps.dev/invite#token=abc",
        "http://api.tlps.dev/invite#token=abc",
        "https://api.tlps.dev:8443/invite#token=abc",
        "https://api.tlps.dev/join#token=abc",
        "https://api.tlps.dev/invite/extra#token=abc",
        "https://api.tlps.dev/api/v1/invite#token=abc",
        "https://invite?token=abc",
        "otherapp://invite?token=abc",
        "dinneros://join?token=abc",
        "dinneros://invite/extra?token=abc",
        "dinneros:invite?token=abc",
    ])
    func ignoresOtherURLs(string: String) throws {
        #expect(try parse(string) == nil)
    }

    @Test func usesTheConfiguredSchemeAndHost() throws {
        #expect(try parse("renamed://invite?token=abc", scheme: "renamed") == .token("abc"))
        #expect(try parse("dinneros://invite?token=abc", scheme: "renamed") == nil)
        #expect(try parse("https://links.example.com/invite#token=abc", webHost: "links.example.com") == .token("abc"))
        #expect(try parse("https://api.tlps.dev/invite#token=abc", webHost: "links.example.com") == nil)
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

struct InvitationPreviewTests {
    private func preview(inviter: String?, role: HouseholdRole) -> InvitationPreview {
        InvitationPreview(householdName: "Lines", inviterName: inviter, role: role, expiresAt: .now)
    }

    @Test func namesTheHouseholdInviterAndRole() {
        let admin = preview(inviter: "Merrill Lines", role: .admin)
        #expect(admin.joinTitle == "Join Lines?")
        #expect(
            admin.joinMessage
                == "Merrill Lines invited you to join as an admin. Joining shares your name with its members.")
        let member = preview(inviter: "Merrill Lines", role: .member)
        #expect(member.joinMessage.hasPrefix("Merrill Lines invited you to join as a member."))
    }

    @Test(arguments: [nil, "", "  "])
    func withoutAnInviterName(inviter: String?) {
        #expect(
            preview(inviter: inviter, role: .member).joinMessage
                == "You're invited to join as a member. Joining shares your name with its members.")
    }

    @Test func unknownRolesUseTheirName() {
        let shopper = preview(inviter: "Ada", role: HouseholdRole(rawValue: "shopper"))
        #expect(shopper.joinMessage.hasPrefix("Ada invited you to join as shopper."))
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

    @Test func defaultsToTheProductionLinkDomain() {
        #expect(AppConfiguration(infoDictionary: [:]).appLinkDomain == "api.tlps.dev")
        #expect(AppConfiguration(infoDictionary: ["AppLinkDomain": " "]).appLinkDomain == "api.tlps.dev")
    }

    @Test func readsConfiguredLinkDomain() {
        let configuration = AppConfiguration(infoDictionary: ["AppLinkDomain": " Links.Example.com "])
        #expect(configuration.appLinkDomain == "links.example.com")
    }
}
