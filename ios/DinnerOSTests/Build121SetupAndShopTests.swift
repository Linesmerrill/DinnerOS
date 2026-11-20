import Foundation
import JavaScriptCore
import Testing

@testable import DinnerOS

/// Build 121 review fixes in Shop and setup: the Walmart chooser's labeled amount and the
/// script that tidies Walmart's page.
@MainActor
struct Build121SetupAndShopTests {
    /// "6 · Tap a product" read as a stray number; it says "Need 6" now.
    @Test func theChooserLabelsTheAmountItNeeds() {
        #expect(ChooseInWalmartView.subtitle(amountText: "6") == "Need 6 · Tap a product to pick it.")
        #expect(ChooseInWalmartView.subtitle(amountText: "2 lb") == "Need 2 lb · Tap a product to pick it.")
        #expect(ChooseInWalmartView.subtitle(amountText: "") == "Tap a product to pick it.")
        #expect(ChooseInWalmartView.subtitle(amountText: "  ") == "Tap a product to pick it.")
    }

    /// The page script names the three things to hide, and it parses and runs without a page:
    /// with no `document` it gives up quietly instead of throwing into Walmart's page.
    @Test func theWalmartTidyScriptRunsQuietlyWithoutItsPage() throws {
        for phrase in ["get the app", "ship to another country"] {
            #expect(WalmartPageTidy.phrases.contains(phrase))
        }
        #expect(WalmartPageTidy.phrases.contains { $0.contains("chat") })
        #expect(WalmartPageTidy.css.contains("display: none !important"))

        let context = try #require(JSContext())
        var thrown: String?
        context.exceptionHandler = { _, exception in thrown = exception?.toString() }
        context.evaluateScript(WalmartPageTidy.script)
        #expect(thrown == nil)
    }

    /// Running it twice on one page does nothing the second time.
    @Test func theWalmartTidyScriptGuardsAgainstRunningTwice() {
        #expect(WalmartPageTidy.script.contains("window.__dinnerosTidy"))
    }
}
