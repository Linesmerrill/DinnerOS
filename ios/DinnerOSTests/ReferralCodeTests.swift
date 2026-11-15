import Foundation
import Testing

@testable import DinnerOS

/// A household can pick its own invite code or get a new random one (decision 699).
struct ReferralCodeTests {
    private func client(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    private func json(_ request: URLRequest?) -> [String: Any]? {
        guard let body = request?.httpBody else { return nil }
        return (try? JSONSerialization.jsonObject(with: body)) as? [String: Any]
    }

    @Test func availabilityAsksWithTheTypedCode() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"available":false,"message":"That code is taken."}"#.utf8)) }
        let result = try await PlusAPI(client: try client(transport)).codeAvailability(
            householdID: "household-1", code: "lines-fam", accessToken: "t")
        let url = try #require(transport.requests.first?.url)
        #expect(url.path() == "/api/v1/households/household-1/plus/referral/code-availability")
        #expect(url.query() == "code=lines-fam")
        #expect(!result.available)
        #expect(result.message == "That code is taken.")
    }

    @Test func aCustomCodeIsSentAsCustom() async throws {
        let transport = StubTransport { _ in (200, Data(Self.changed.utf8)) }
        let referral = try await PlusAPI(client: try client(transport)).changeCode(
            householdID: "household-1", custom: "lines-fam", accessToken: "t")
        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/plus/referral/code")
        let body = try #require(json(request))
        #expect(body["custom"] as? String == "lines-fam")
        #expect(body["regenerate"] == nil)
        #expect(referral.code == "lines-fam")
        #expect(referral.codeChangesLeft == 4)
        #expect(referral.codeChangesText == "You can change your code 4 more times in the next 30 days.")
        #expect(referral.canChangeCode)
    }

    @Test func aRandomCodeIsAskedForWithRegenerate() async throws {
        let transport = StubTransport { _ in (200, Data(Self.changed.utf8)) }
        _ = try await PlusAPI(client: try client(transport)).changeCode(
            householdID: "household-1", custom: nil, accessToken: "t")
        let body = try #require(json(transport.requests.first))
        #expect(body["regenerate"] as? Bool == true)
        #expect(body["custom"] == nil)
    }

    @Test func theServersMessageShowsWhenACodeIsTaken() async throws {
        let transport = StubTransport { _ in
            (409, Data(#"{"error":{"code":"code_taken","message":"That code is taken.","requestId":"r"}}"#.utf8))
        }
        await #expect {
            _ = try await PlusAPI(client: try client(transport)).changeCode(
                householdID: "household-1", custom: "lines-fam", accessToken: "t")
        } throws: { error in
            HouseholdStore.message(for: error) == "That code is taken."
        }
    }

    @Test func anOlderAPICantChangeCodes() throws {
        let old = #"{"code":"ABCD2345","shareText":"Join me","explainer":"Share it","rewarded":0,"monthsLeft":12}"#
        let referral = try JSONCoding.makeDecoder().decode(PlusReferral.self, from: Data(old.utf8))
        #expect(!referral.canChangeCode)
        #expect(referral.codeHelp == nil)
    }

    @Test func noChangesLeftSaysWhenItCanChangeAgain() throws {
        let json = """
            {"code":"lines-fam","shareText":"Join me","explainer":"Share it","rewarded":0,"monthsLeft":12,
             "codeChangesLeft":0,"codeChangesLimit":5,"codeChangesNext":"2026-12-13T00:00:00Z",
             "codeChangesText":"You can change your code again on December 13."}
            """
        let referral = try JSONCoding.makeDecoder().decode(PlusReferral.self, from: Data(json.utf8))
        #expect(referral.codeChangesLeft == 0)
        #expect(referral.codeChangesNext != nil)
    }

    @Test func aCustomCodeShowsAsTyped() {
        #expect(PlusInviteView.spaced("lines-fam") == "lines-fam")
        #expect(PlusInviteView.spaced("linesfam") == "linesfam")
        #expect(PlusInviteView.spaced("ABCD2345") == "ABCD 2345")
        #expect(PlusText.copyableCode("lines-fam") == "lines-fam")
    }

    nonisolated static let changed = """
        {"code":"lines-fam","shareText":"Join me: https://dinneros.app/invite/lines-fam","explainer":"Share it",
         "rewarded":0,"monthsLeft":12,"shareMessage":"Join me","shareUrl":"https://dinneros.app/invite/lines-fam",
         "codeChangesLeft":4,"codeChangesLimit":5,
         "codeChangesText":"You can change your code 4 more times in the next 30 days.",
         "codeHelp":"Use 3 to 20 letters, numbers or hyphens."}
        """
}
