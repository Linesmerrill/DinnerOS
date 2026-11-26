import Foundation
import Testing

@testable import DinnerOS

/// Build 123 fixes from Hark's build 122 round 5 review.
@MainActor
struct Build123ReviewTests {
    /// The Shop bar said "Everything is already in your Walmart cart" above "Choose 2 Products
    /// in Walmart" while Jasmine Rice still had no product. It says that only when nothing is
    /// left to choose; otherwise it says what's in the cart and counts the rest.
    @Test func everythingInTheCartOnlyWhenNothingIsLeftToChoose() {
        #expect(
            ShopHandoffText.nothingNewText(notice: nil, needsProduct: 0, store: "Walmart", listPage: false)
                == "Everything is already in your Walmart cart")
        #expect(
            ShopHandoffText.nothingNewText(notice: nil, needsProduct: 2, store: "Walmart", listPage: false)
                == "What you picked is in your Walmart cart. Choose products for 2 more to add them too.")
        #expect(
            ShopHandoffText.nothingNewText(notice: nil, needsProduct: 1, store: "Walmart", listPage: false)
                == "What you picked is in your Walmart cart. Choose a product for 1 more to add it too.")
        // The store's own "everything" notice, set by a send that added nothing, is no truer.
        let everything = ShoppingText.everythingInCart(in: "Walmart")
        let afterSend = ShopHandoffText.nothingNewText(
            notice: everything, needsProduct: 2, store: "Walmart", listPage: false)
        #expect(!afterSend.contains("Everything"))
        #expect(
            ShopHandoffText.nothingNewText(notice: everything, needsProduct: 0, store: "Walmart", listPage: false)
                == everything)
        // Instacart is a list, not a cart.
        #expect(
            ShopHandoffText.nothingNewText(notice: nil, needsProduct: 0, store: "Instacart", listPage: true)
                == "Everything is already on your Instacart list")
        #expect(
            ShopHandoffText.nothingNewText(notice: nil, needsProduct: 3, store: "Instacart", listPage: true)
                == "What you picked is on your Instacart list. Choose products for 3 more to add them too.")
        // Another notice is shown as it is.
        let skipped = "Nothing to add to your cart. You skipped every item that has a product."
        #expect(
            ShopHandoffText.nothingNewText(notice: skipped, needsProduct: 2, store: "Walmart", listPage: false)
                == skipped)
    }

    /// "Autopilot suggested 1 meal" said "add them" (Hark's build 123 review): one meal is "it".
    @Test func autopilotsPicksSayItForOneMeal() {
        #expect(AutopilotPicksText.title(meals: 1) == "Autopilot suggested 1 meal")
        #expect(AutopilotPicksText.subtitle(meals: 1) == "Review, swap, and add it to your week.")
        #expect(AutopilotPicksText.title(meals: 4) == "Autopilot suggested 4 meals")
        #expect(AutopilotPicksText.subtitle(meals: 4) == "Review, swap, and add them to your week.")
    }
}
