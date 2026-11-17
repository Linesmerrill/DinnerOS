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

    /// A store page's scripts put the recipe on the page after it loads: the web view waits
    /// for it, rather than reading the empty page at once or sleeping a fixed time.
    /// Waits for the recipe the page's script adds, and sends the page as soon as it shows,
    /// not after the full wait. Judged by what the page held, not by the clock, so a busy
    /// machine doesn't fail it.
    @Test @MainActor func theWebViewWaitsForTheRecipeToRender() async throws {
        let page = """
            <html><body><div id="root"></div><script>
            setTimeout(() => {
              document.getElementById('root').innerHTML =
                '<div itemscope itemtype="http://schema.org/Recipe"><h1>Quick Slaw Recipe</h1><ul>' +
                '<li data-testid="RecipeDetail-ingredients">2 cups shredded cabbage</li></ul></div>';
            }, 1500);
            setTimeout(() => { document.body.insertAdjacentHTML('beforeend', '<p>late-' + 'marker</p>'); }, 12000);
            </script></body></html>
            """
        let html = try await WebPageReader.read(
            html: page, baseURL: try #require(URL(string: "https://shop.example.test/r/slaw")))
        #expect(html.contains("RecipeDetail-ingredients"), "read the page before its recipe showed: \(html.prefix(300))")
        #expect(!html.contains("late-marker</p>"), "kept waiting after the recipe showed")
    }

    @Test @MainActor func theReadyCheckKnowsARecipeFromAnEmptyShell() async throws {
        let shell = try await WebPageReader.read(
            html:
                #"<html><body><div id="root"></div><script type="application/ld+json">{"@type":"Recipe","name":"Soup"}</script></body></html>"#,
            baseURL: try #require(URL(string: "https://example.test/")))
        #expect(shell.contains("Soup"), "a page with JSON-LD is ready at once")
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
