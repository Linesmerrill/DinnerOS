import Foundation
import Testing

@testable import DinnerOS

/// Recipes from a web page: the link pasted, the page trimmed before it's sent, and what the
/// review screen shows.
struct RecipePageImportTests {
    @Test func findsTheLinkInWhatWasPasted() {
        #expect(RecipePageLoader.link(in: "  https://example.test/a  ") == "https://example.test/a")
        #expect(
            RecipePageLoader.link(in: "Made this last night! https://blog.example.test/chili?x=1 so good")
                == "https://blog.example.test/chili?x=1")
        #expect(RecipePageLoader.link(in: "no link here") == "no link here")
    }

    @Test func aPageIsTrimmedButKeepsItsRecipeData() {
        let page = """
            <html><head><script>var ads = 1;</script>
            <script type="application/ld+json">{"@type":"Recipe","name":"Soup"}</script>
            <style>body { color: red }</style></head>
            <body><!-- comment --><svg><path d="M0"/></svg><h2>Ingredients</h2><ul><li>1 cup rice</li></ul></body></html>
            """
        let trimmed = RecipePageLoader.trimmed(page)
        #expect(trimmed.contains(#"{"@type":"Recipe","name":"Soup"}"#))
        #expect(trimmed.contains("<li>1 cup rice</li>"))
        #expect(!trimmed.contains("var ads"))
        #expect(!trimmed.contains("color: red"))
        #expect(!trimmed.contains("comment"))
        #expect(!trimmed.contains("<svg"))
        let huge = String(repeating: "a", count: RecipePageLoader.maxBytes + 10)
        #expect(RecipePageLoader.trimmed(huge).utf8.count == RecipePageLoader.maxBytes)
    }

    @Test func aDraftFromAPageCarriesNotesGroupsAndTheReminder() throws {
        let json = """
            {"name":"Tacos","servings":4,"sourceUrl":"https://example.test/tacos","fromUrl":true,
             "cuisines":[],"tags":[],"steps":[],
             "ingredients":[{"name":"garlic","quantity":"3","unit":"clove","rawText":"2-3 cloves garlic, minced",
                             "note":"minced","group":"Salsa","pantryStaple":false}],
             "warnings":["Some lines have no amount. Add one so the list buys enough.",
                         "Imported recipes can have mistakes. Please check the ingredients before you shop."]}
            """
        let draft = try JSONDecoder().decode(RecipeDraft.self, from: Data(json.utf8))
        #expect(draft.fromURL)
        #expect(draft.ingredients.first?.note == "minced")
        #expect(draft.ingredients.first?.group == "Salsa")
        // The reminder has its own place on the screen, so it isn't listed twice.
        #expect(draft.listedWarnings == ["Some lines have no amount. Add one so the list buys enough."])
        #expect(
            ImportedRecipeText.note
                == "Imported recipes can have mistakes. Please check the ingredients before you shop.")
    }
}
