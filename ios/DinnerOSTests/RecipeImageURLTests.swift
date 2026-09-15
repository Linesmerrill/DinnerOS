import Foundation
import Testing

@testable import DinnerOS

struct RecipeImageURLTests {
    private static let original =
        "https://img.example.test/f_auto,fl_lossy,q_auto,w_1200/recipes/image/sample-tacos-1a2b.jpg"

    private func url(_ string: String) throws -> URL {
        try #require(URL(string: string))
    }

    @Test func swapsTheWidthTokenForTheCardSize() throws {
        let sized = RecipeImageURL.sized(try url(Self.original), pointWidth: 240, scale: 2)
        #expect(
            sized?.absoluteString
                == "https://img.example.test/f_auto,fl_lossy,q_auto,w_480/recipes/image/sample-tacos-1a2b.jpg")
    }

    @Test(arguments: [(1, 160), (160, 160), (161, 320), (700, 800), (1_000, 1_080), (5_000, 1_200)])
    func roundsUpToABucket(pixels: Int, bucket: Int) {
        #expect(RecipeImageURL.bucket(for: pixels) == bucket)
    }

    @Test func neverRequestsMoreThanTheOriginalWidth() throws {
        let small = try url("https://img.example.test/q_auto,w_300/a.jpg")
        #expect(RecipeImageURL.sized(small, pixelWidth: 1_000) == small)
        #expect(
            RecipeImageURL.sized(small, pixelWidth: 100).absoluteString == "https://img.example.test/q_auto,w_160/a.jpg"
        )
    }

    @Test func leavesURLsWithoutAWidthTokenAlone() throws {
        let plain = try url("https://images.example.test/recipes/w_tacos_1200.jpg?w=1200")
        #expect(RecipeImageURL.sized(plain, pixelWidth: 320) == plain)
        let lookalike = try url("https://img.example.test/w_12x/a.jpg")
        #expect(RecipeImageURL.sized(lookalike, pixelWidth: 320) == lookalike)
    }

    @Test func keepsQueryAndEncodedCharacters() throws {
        let encoded = try url("https://img.example.test/w_1200/Sample%20Tacos.jpg?v=2")
        #expect(
            RecipeImageURL.sized(encoded, pixelWidth: 300).absoluteString
                == "https://img.example.test/w_320/Sample%20Tacos.jpg?v=2")
    }

    @Test func handlesMissingAndDegenerateSizes() throws {
        #expect(RecipeImageURL.sized(nil, pointWidth: 200, scale: 3) == nil)
        let original = try url(Self.original)
        #expect(RecipeImageURL.sized(original, pointWidth: 0, scale: 3) == original)
        #expect(RecipeImageURL.sized(original, pointWidth: .infinity, scale: 3) == original)
        // A scale below 1 counts as 1.
        #expect(RecipeImageURL.sized(original, pointWidth: 100, scale: 0)?.absoluteString.contains("w_160") == true)
    }
}
