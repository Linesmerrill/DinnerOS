import Foundation
import Testing

@testable import DinnerOS

struct WelcomeTests {
    private let now = Date(timeIntervalSince1970: 1_790_000_000)

    @Test func aNewMemberSeesItOnce() {
        let yesterday = now.addingTimeInterval(-24 * 3600)
        #expect(Welcome.shouldShow(accountCreated: yesterday, seen: false, now: now))
        #expect(!Welcome.shouldShow(accountCreated: yesterday, seen: true, now: now))
    }

    @Test func someoneWhoHasUsedTheAppForWeeksDoesNot() {
        let monthAgo = now.addingTimeInterval(-30 * 24 * 3600)
        #expect(!Welcome.shouldShow(accountCreated: monthAgo, seen: false, now: now))
    }

    @Test func seenIsRememberedPerMemberAndHousehold() throws {
        let defaults = try #require(UserDefaults(suiteName: "WelcomeTests.\(UUID().uuidString)"))
        let storage = UserDefaultsWelcomeStorage(defaults: defaults)
        storage.markSeen(userID: "u1", householdID: "h1")
        #expect(storage.hasSeen(userID: "u1", householdID: "h1"))
        #expect(!storage.hasSeen(userID: "u1", householdID: "h2"))
        #expect(!storage.hasSeen(userID: "u2", householdID: "h1"))
    }
}

struct CatalogSharingSettingTests {
    private let householdJSON =
        #"{"id":"h","name":"H","defaultServings":2,"timeZone":"America/Denver","createdBy":"u","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z""#

    @Test func sharingIsOffUnlessTheServerSaysOtherwise() throws {
        let old = try JSONCoding.makeDecoder().decode(Household.self, from: Data((householdJSON + "}").utf8))
        #expect(old.catalogSharing == .off)
        let chosen = try JSONCoding.makeDecoder().decode(
            Household.self, from: Data((householdJSON + #","catalogSharing":"chosen"}"#).utf8))
        #expect(chosen.catalogSharing == .chosen)
        // A mode this build doesn't know reads as off rather than failing the household.
        let future = try JSONCoding.makeDecoder().decode(
            Household.self, from: Data((householdJSON + #","catalogSharing":"friends"}"#).utf8))
        #expect(future.catalogSharing == .off)
    }

    @Test func changingItSendsOnlyThatField() throws {
        let household = try JSONCoding.makeDecoder().decode(Household.self, from: Data((householdJSON + "}").utf8))
        var draft = HouseholdSettingsDraft(household)
        #expect(draft.changes(against: household).isEmpty)
        draft.catalogSharing = .all
        let changes = draft.changes(against: household)
        #expect(changes.catalogSharing == .all)
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(changes)) as? [String: Any]
        #expect(body?.keys.sorted() == ["catalogSharing"])
        #expect(body?["catalogSharing"] as? String == "all")
    }
}
