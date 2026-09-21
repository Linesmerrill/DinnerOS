import SwiftUI

/// Opens "Try Something Else": the global catalog, minus what the household has.
struct DiscoverRoute: Hashable {}

/// Opens one catalog recipe, which the household may not own.
struct CatalogRecipeRoute: Hashable {
    let id: String
    let name: String
}
