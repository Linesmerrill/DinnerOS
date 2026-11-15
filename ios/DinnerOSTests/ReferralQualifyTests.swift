import Foundation
import Testing

@testable import DinnerOS

/// Invites count once the new household sets up (decision 698): the app sends a DeviceCheck
/// token, shows what's left, lists each invite's state, and shows the news as a banner.
struct ReferralQualifyTests {
    private func json(_ request: URLRequest?) -> [String: Any]? {
        guard let body = request?.httpBody else { return nil }
        return (try? JSONSerialization.jsonObject(with: body)) as? [String: Any]
    }

    private func client(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    private let token = DeviceCheckToken(token: "ZGV2aWNl", development: true)

    // MARK: DeviceCheck token on create, join and code entry

    @Test func createHouseholdSendsTheDeviceCheckToken() async throws {
        let transport = StubTransport { _ in (201, HouseholdFixtures.created(id: "household-1", name: "Lovelace")) }
        _ = try await HouseholdsAPI(client: try client(transport)).createHousehold(
            name: "Lovelace", timeZone: "America/Denver", defaultServings: nil, deviceCheck: token,
            accessToken: "t")
        let body = try #require(json(transport.requests.first))
        let deviceCheck = try #require(body["deviceCheck"] as? [String: Any])
        #expect(deviceCheck["token"] as? String == "ZGV2aWNl")
        #expect(deviceCheck["development"] as? Bool == true)
    }

    @Test func noTokenIsLeftOutRatherThanSentAsNull() async throws {
        let transport = StubTransport { _ in (201, HouseholdFixtures.created(id: "household-1", name: "Lovelace")) }
        _ = try await HouseholdsAPI(client: try client(transport)).createHousehold(
            name: "Lovelace", timeZone: "America/Denver", defaultServings: nil, accessToken: "t")
        let body = try #require(json(transport.requests.first))
        #expect(body["deviceCheck"] == nil)
    }

    @Test func redeemSendsTheCodeAndTheDeviceCheckToken() async throws {
        let transport = StubTransport { _ in (200, Data(Self.pendingStatus.utf8)) }
        let status = try await PlusAPI(client: try client(transport)).redeem(
            householdID: "household-1", code: "ABCD1234", deviceCheck: token, accessToken: "t")
        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/plus/referral/redeem")
        let body = try #require(json(request))
        #expect(body["code"] as? String == "ABCD1234")
        #expect((body["deviceCheck"] as? [String: Any])?["token"] as? String == "ZGV2aWNl")
        // Pending: no free time yet, and the steps say what's left.
        #expect(status.freeUntil == nil)
        let progress = try #require(status.referralProgress)
        #expect(progress.isPending)
        #expect(progress.message == "Plan a dinner and save 3 recipes to unlock your free month")
        #expect(progress.steps.map(\.done) == [false, false])
        #expect(progress.steps[1].count == 1)
        #expect(progress.steps[1].target == 3)
    }

    // MARK: The sharer's screen

    @Test func referralDecodesHowItWorksAndEachInvite() throws {
        let referral = try JSONCoding.makeDecoder().decode(PlusReferral.self, from: Data(Self.referral.utf8))
        #expect(referral.howItWorks?.title == "How it works")
        #expect(referral.howItWorks?.steps.count == 4)
        let invites = try #require(referral.invites)
        #expect(invites.pending == 1)
        #expect(invites.qualified == 1)
        #expect(invites.expired == 1)
        let items = try #require(invites.items)
        #expect(items.map(\.stateTitle) == ["Pending", "Free month earned", "Expired"])
        #expect(items[0].detailLine == "Needs 2 more recipes")
        // A detail that only repeats the state isn't shown twice.
        #expect(items[1].detailLine == nil)
        #expect(items[2].detailLine == nil)
    }

    @Test func anOlderAPIStillDecodes() throws {
        let old = """
            {"code":"ABCD1234","shareText":"Join me","explainer":"Share it","rewarded":0,"monthsLeft":12,
             "invites":{"joined":1,"monthsEarned":1,"monthsCap":12,"capText":"1 of 12","freeUntil":null,
              "emptyText":"No one yet","joins":[{"at":"2026-11-01T00:00:00Z","counted":true,"status":"Counted"}],
              "usedCode":null}}
            """
        let referral = try JSONCoding.makeDecoder().decode(PlusReferral.self, from: Data(old.utf8))
        #expect(referral.howItWorks == nil)
        #expect(referral.invites?.items == nil)
        #expect(referral.invites?.pending == nil)
    }

    @Test func summaryDecodesCountsAndFooter() throws {
        let json = """
            {"codesUsed":5,"joinedThisWeek":1,"joinedAllTime":5,"topReferrers":[],
             "pending":2,"qualified":2,"expired":1,
             "footerText":"An invite counts once the new household plans a dinner and saves 3 recipes."}
            """
        let summary = try JSONCoding.makeDecoder().decode(PlusReferralSummary.self, from: Data(json.utf8))
        #expect(summary.pending == 2)
        #expect(summary.qualified == 2)
        #expect(summary.expired == 1)
        #expect(summary.footerText?.hasPrefix("An invite counts") == true)
    }

    // MARK: Banners and routing

    @Test func bannersAreFetchedForTheHousehold() async throws {
        let transport = StubTransport { _ in (200, Data(Self.banners.utf8)) }
        let banners = try await NotificationsAPI(client: try client(transport)).banners(
            householdID: "household-1", accessToken: "t")
        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/notifications/banners")
        #expect(banners.count == 1)
        #expect(banners[0].type == .plusReferral)
        #expect(NotificationRouting.destination(for: banners[0]) == .plusInvites)
    }

    @Test func aReferralPushOpensGiveAFreeMonth() {
        let destination = NotificationRouting.destination(
            type: .plusReferral, subject: AppNotificationSubject(kind: "plus", id: "household-1"))
        #expect(destination == .plusInvites)
        #expect(destination.fixtureName == "plus_invites")
    }

    // MARK: Fixtures

    nonisolated static let pendingStatus = """
        {"enabled":true,"active":false,"trial":false,"expiresAt":null,"willRenew":false,"billingIssue":false,
         "freeUntil":null,"offer":null,
         "referralProgress":{"state":"pending",
          "message":"Plan a dinner and save 3 recipes to unlock your free month",
          "steps":[{"id":"dinner","title":"Plan a dinner","done":false,"count":0,"target":1},
                   {"id":"recipes","title":"Save 3 recipes","done":false,"count":1,"target":3}],
          "expiresAt":"2026-11-29T00:00:00Z"}}
        """

    nonisolated static let referral = """
        {"code":"ABCD1234","shareText":"Join me","explainer":"Share it","rewarded":1,"monthsLeft":11,
         "howItWorks":{"title":"How it works","steps":[
          "Share your code with friends, family, even that one aunt.",
          "They download DinnerOS, start a new household, and enter your code.",
          "Once they plan a dinner and save 3 recipes, the invite counts.",
          "Within 24 hours you both get a free month of Plus. We'll send you a notification when it's on."]},
         "invites":{"joined":3,"monthsEarned":1,"monthsCap":12,"capText":"1 of 12","freeUntil":null,
          "emptyText":"No one yet","joins":[],"usedCode":null,
          "pending":1,"qualified":1,"expired":1,
          "items":[
           {"id":"r1","householdName":"The Okafor Table","state":"pending","detail":"Needs 2 more recipes","at":"2026-11-14T00:00:00Z"},
           {"id":"r2","householdName":"Casa Ruiz","state":"qualified","detail":"Free month earned","at":"2026-11-10T00:00:00Z"},
           {"id":"r3","householdName":"Kim House","state":"expired","detail":"Expired","at":"2026-10-01T00:00:00Z"}]}}
        """

    nonisolated static let banners = """
        {"items":[{"id":"n-1","householdId":"household-1","type":"plus.referral",
          "title":"Your free month of Plus is on",
          "body":"Your free month of Plus is on. Thanks for joining from The Lovelace Kitchen's invite.",
          "subject":{"kind":"plus","id":"household-1"},"read":false,"banner":true,
          "createdAt":"2026-11-15T00:00:00Z"}]}
        """
}
