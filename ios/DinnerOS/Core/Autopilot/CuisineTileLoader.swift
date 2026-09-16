import Foundation
import Observation

/// Fills the onboarding grid's tiles with real photos from the catalog.
///
/// It asks `MenuStore` for one small `menu/recipes?cuisine=…` list per cuisine, which reuses
/// the Menu's paging and needs no new endpoint. Tiles appear as soon as the cuisines are
/// known, so the step is usable while the photos are still arriving; a cuisine whose recipes
/// have no photo simply keeps the fallback glyph (#331).
@Observable
@MainActor
final class CuisineTileLoader {
    /// The tiles to show, in order. Empty until `load` is called.
    private(set) var tiles: [CuisineTile] = []

    /// Enough rows to find a photo when the first recipe of a cuisine happens to lack one,
    /// without pulling a whole page per tile.
    static let photoSearchLimit = 6

    /// The per-cuisine lists, kept alive: `MenuStore.makeList` holds only a weak reference.
    @ObservationIgnored private var lists: [MenuRecipeList] = []
    @ObservationIgnored private var hasLoaded = false

    init() {}

    /// A loader frozen with `tiles`, for SwiftUI previews. It makes no requests.
    static func preview(tiles: [CuisineTile]) -> CuisineTileLoader {
        let loader = CuisineTileLoader()
        loader.tiles = tiles
        loader.hasLoaded = true
        return loader
    }

    /// Shows the top cuisines from `vocabulary`, then fetches a photo for each. Does nothing
    /// after the first call, so returning to the step doesn't refetch.
    func load(cuisines: [AutopilotOption], menu: MenuStore) async {
        guard !hasLoaded else { return }
        hasLoaded = true
        tiles = CuisineTiles.top(cuisines)
        guard !tiles.isEmpty else { return }

        // One list per cuisine, loaded together; each fills its own tile as it arrives.
        lists = tiles.map { tile in
            menu.makeList(query: MenuRecipeQuery(cuisine: tile.value, sort: .popular))
        }
        // Each list fills its own tile as it arrives. The loads run one after another rather
        // than in a task group: everything here is main-actor state, and a handful of small
        // requests isn't worth the isolation dance.
        for (index, list) in lists.enumerated() {
            await list.load()
            guard index < tiles.count, let url = CuisineTiles.photo(in: list.items) else { continue }
            tiles[index].imageURL = url
        }
        // The photos are on the tiles now; the lists themselves aren't needed.
        lists = []
    }
}
