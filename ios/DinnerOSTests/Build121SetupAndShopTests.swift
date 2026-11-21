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

    // MARK: Review 2

    /// The bar's note and "Choose 14 Products" count the same items: the note says what goes in
    /// the cart now and how many still need a product.
    @Test func theSendNoteCountsTheItemsStillWaitingForAProduct() {
        #expect(
            ShopHandoffText.summary(adding: 1, needsProduct: 14, store: "Walmart", listPage: false, hasCart: false)
                == "Adds 1 item to your Walmart cart. You check out in Walmart. "
                + "Choose products for 14 more to add them too.")
        #expect(
            ShopHandoffText.summary(adding: 3, needsProduct: 1, store: "Walmart", listPage: false, hasCart: false)
                .hasSuffix("Choose a product for 1 more to add it too."))
        #expect(
            ShopHandoffText.summary(adding: 2, needsProduct: 0, store: "Walmart", listPage: false, hasCart: true)
                == "Adds 2 new items to your Walmart cart. What’s already there isn’t added again.")
        #expect(
            ShopHandoffText.summary(adding: 1, needsProduct: 0, store: "Instacart", listPage: true, hasCart: false)
                == "Sends 1 item to Instacart. You pick the store and check out there.")
    }

    /// Which cuisines show follows the counts, but the grid is alphabetical, so it reads the
    /// same in every run and mode.
    @Test func theCuisineGridIsInOneOrderWhateverTheCounts() {
        let tiles = [
            CuisineTile(value: "thai", label: "Thai", recipeCount: 40),
            CuisineTile(value: "indian", label: "Indian", recipeCount: 12),
            CuisineTile(value: "american", label: "American", recipeCount: 90),
        ]
        let shown = CuisineTiles.displayOrder(tiles).map(\.value)
        #expect(shown == ["american", "indian", "thai"])
        #expect(CuisineTiles.displayOrder(tiles.reversed()).map(\.value) == shown)
    }

    /// The renewal is said once, in the terms, not in the price line too.
    @Test func thePriceLineDoesNotRepeatTheRenewal() {
        let plan = PlusPlan(
            productID: "com.linesmerrill.dinneros.plus.yearly", name: "Yearly", priceText: "$39.99 a year", note: nil)
        #expect(PlusText.chargeLine(plan, product: nil, trialDays: 14) == "Free for 14 days, then $39.99 a year.")
        #expect(!PlusText.chargeLine(plan, product: nil, trialDays: 0).contains("Renew"))
    }
}
