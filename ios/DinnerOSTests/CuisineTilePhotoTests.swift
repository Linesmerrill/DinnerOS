import Foundation
import Testing

@testable import DinnerOS

/// Build 122, Hark's review item 24: a tile on "What do you like?" was a gray placeholder. A
/// server that picks a photo for each cuisine offers only cuisines it has a photo for.
struct CuisineTilePhotoTests {
    private func option(_ value: String, _ count: Int, _ image: String? = nil) -> AutopilotOption {
        AutopilotOption(
            value: value, label: value.capitalized, description: nil, recipeCount: count,
            imageURL: image.flatMap(URL.init(string:)))
    }

    @Test func aCuisineWithoutAPhotoIsLeftOutWhenTheServerSendsPhotos() {
        let tiles = CuisineTiles.top([
            option("indian", 9),
            option("italian", 7, "https://img.example.test/italian.jpg"),
            option("mexican", 5, "https://img.example.test/mexican.jpg"),
        ])
        #expect(tiles.map(\.value) == ["italian", "mexican"])
        #expect(tiles.allSatisfy { $0.imageURL != nil })
        #expect(tiles.first?.imageURL?.absoluteString == "https://img.example.test/italian.jpg")
    }

    @Test func anOlderServerWithoutPhotosStillGetsTiles() {
        let tiles = CuisineTiles.top([option("indian", 9), option("italian", 7)])
        #expect(tiles.map(\.value) == ["indian", "italian"])
        #expect(tiles.allSatisfy { $0.imageURL == nil })
    }

    @Test func theVocabularyPhotoDecodes() throws {
        let json = Data(
            #"{"value":"thai","label":"Thai","recipeCount":4,"imageUrl":"https://img.example.test/thai.jpg"}"#.utf8)
        let decoded = try JSONDecoder().decode(AutopilotOption.self, from: json)
        #expect(decoded.imageURL?.absoluteString == "https://img.example.test/thai.jpg")
        let old = try JSONDecoder().decode(
            AutopilotOption.self, from: Data(#"{"value":"thai","label":"Thai"}"#.utf8))
        #expect(old.imageURL == nil)
    }

    /// The grid reads in one order everywhere: by name, whatever the counts.
    @Test func theGridOrderIsTheSameWhateverTheCounts() {
        let light = CuisineTiles.displayOrder(
            CuisineTiles.top([option("thai", 9, "https://a.test/1"), option("cuban", 3, "https://a.test/2")]))
        let dark = CuisineTiles.displayOrder(
            CuisineTiles.top([option("cuban", 9, "https://a.test/2"), option("thai", 3, "https://a.test/1")]))
        #expect(light.map(\.value) == ["cuban", "thai"])
        #expect(light.map(\.value) == dark.map(\.value))
    }
}
