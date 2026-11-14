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
