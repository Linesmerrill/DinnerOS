import Foundation
import Testing

@testable import DinnerOS

/// Every way into recipes other households shared uses one name.
struct SharedRecipesTests {
    @Test func oneNameEverywhere() {
        #expect(SharedRecipes.title == "Shared Recipes")
        #expect(SharedRecipes.subtitle == "Recipes other households shared")
        #expect(SharedRecipes.systemImage == "books.vertical")
    }

    @Test func oldNamesAreGone() {
        for old in ["Try Something Else", "Browse Recipes", "Browse Shared Recipes"] {
            #expect(SharedRecipes.title != old)
        }
    }
}

/// Shared Recipes pages carry the server's empty words and, later, partner placements.
struct SharedRecipesPageTests {
    @Test func decodesEmptyWordsAndPromotion() throws {
        let json = Data(
            #"""
            {"items":[{"id":"a","name":"Tacos","promotion":{"label":"Sponsored","partner":"Kitchen Co"}},
                      {"id":"b","name":"Soup"}],
             "total":2,"emptyTitle":"Nothing New Right Now","emptyText":"Check back later."}
            """#.utf8)
        let page = try JSONDecoder().decode(CatalogListPage.self, from: json)
        #expect(page.emptyTitle == "Nothing New Right Now")
        #expect(page.emptyText == "Check back later.")
        #expect(page.items[0].promotion == CatalogPromotion(label: "Sponsored", partner: "Kitchen Co"))
        #expect(page.items[1].promotion == nil)
    }

    @Test func olderServerPageStillDecodes() throws {
        let json = Data(#"{"items":[{"id":"a","name":"Tacos","promotion":"bad"}],"total":1}"#.utf8)
        let page = try JSONDecoder().decode(CatalogListPage.self, from: json)
        #expect(page.emptyTitle == nil && page.emptyText == nil)
        #expect(page.items.first?.promotion == nil)
    }
}
