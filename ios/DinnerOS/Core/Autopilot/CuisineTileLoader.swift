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

    /// Enough rows to find a photo when a cuisine's first recipes lack one or are already on
    /// another tile, without pulling a whole page per tile.
    static let photoSearchLimit = 8

    /// The per-cuisine lists, kept alive: `MenuStore.makeList` holds only a weak reference.
    @ObservationIgnored private var lists: [MenuRecipeList] = []
    @ObservationIgnored private var hasLoaded = false
    /// The cuisines to show, before photos are handed out.
    @ObservationIgnored private var base: [CuisineTile] = []
    /// Candidate recipes per cuisine, in the server's order.
    @ObservationIgnored private var candidates: [String: [MenuCard]] = [:]

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
        base = CuisineTiles.top(cuisines)
        tiles = base
        guard !base.isEmpty else { return }

        lists = base.map { tile in
            menu.makeList(
                query: MenuRecipeQuery(cuisine: tile.value, sort: .popular), pageSize: Self.photoSearchLimit)
        }
        // The loads run one after another rather than in a task group: everything here is
        // main-actor state, and a handful of small requests isn't worth the isolation dance.
        // Photos are reassigned from scratch after each one, so a tile never shows a recipe an
        // earlier tile already has, and the result doesn't depend on which load finished first.
        for (tile, list) in zip(base, lists) {
            await list.load()
            candidates[tile.value] = list.items
            tiles = CuisineTiles.assignPhotos(base, candidates: candidates)
        }
        // The photos are on the tiles now; the lists themselves aren't needed.
        lists = []
    }
}
