import Foundation
import Testing

@testable import DinnerOS

/// A recipe link shared from Safari: kept for the app, taken once, and dropped when stale.
@Suite(.serialized)
struct SharedRecipeLinkTests {
    /// The test build has no keychain, so the link is kept in memory.
    init() {
        let box = MemoryBox()
        SharedRecipeLink.storage = .init(
            read: { box.data },
            write: {
                box.data = $0; return true
            }, delete: { box.data = nil })
    }

    private nonisolated final class MemoryBox: @unchecked Sendable {
        var data: Data?
    }

    @Test func findsTheLinkInSharedText() {
        #expect(
            SharedRecipeLink.webLink(in: "Look at this one https://cook.example.test/soup?x=1 so good")?.absoluteString
                == "https://cook.example.test/soup?x=1")
        #expect(SharedRecipeLink.webLink(in: "no link here") == nil)
        #expect(SharedRecipeLink.webLink(in: "mailto:a@example.test") == nil)
    }

    @Test func aLinkIsTakenOnce() throws {
        SharedRecipeLink.clear()
        let url = try #require(URL(string: "https://cook.example.test/soup"))
        #expect(SharedRecipeLink.save(url))
        #expect(SharedRecipeLink.take() == "https://cook.example.test/soup")
        #expect(SharedRecipeLink.take() == nil)
    }

    @Test func theNewestLinkWinsAndAStaleOneIsDropped() throws {
        SharedRecipeLink.clear()
        let now = Date()
        #expect(SharedRecipeLink.save(try #require(URL(string: "https://a.example.test/1")), now: now))
        #expect(SharedRecipeLink.save(try #require(URL(string: "https://a.example.test/2")), now: now))
        #expect(SharedRecipeLink.take(now: now) == "https://a.example.test/2")

        #expect(SharedRecipeLink.save(try #require(URL(string: "https://a.example.test/3")), now: now))
        #expect(SharedRecipeLink.take(now: now.addingTimeInterval(SharedRecipeLink.maxAge + 1)) == nil)
        #expect(!SharedRecipeLink.save(try #require(URL(string: "file:///etc/hosts"))))
    }
}
