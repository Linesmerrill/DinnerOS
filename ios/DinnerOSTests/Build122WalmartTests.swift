import SwiftUI
import Testing

@testable import DinnerOS

/// Build 122 review item 20: Choose in Walmart.
struct Build122WalmartTests {
    /// The banner ad over the results often loaded blank, leaving an empty band above the products.
    @Test func theTidyScriptHidesTheBannerAdOverTheResults() {
        #expect(WalmartPageTidy.css.contains(#"[data-testid="skyline-ad"]"#))
        #expect(WalmartPageTidy.css.contains(#"[id^="SEARCH-SkylineDisplayAd"]"#))
        // The selectors are inside the rule that hides them, so they reach the page's style.
        #expect(WalmartPageTidy.script.contains(#"[data-testid="skyline-ad"]"#))
    }

    /// "Spanish / Español now available!" and "Switch countries" floated over the location bar on a
    /// second search.
    @Test func theTidyScriptHidesTheLanguageAndCountryPopovers() {
        #expect(WalmartPageTidy.phrases.contains("spanish / español"))
        #expect(WalmartPageTidy.phrases.contains("switch countries"))
        #expect(WalmartPageTidy.phrases.allSatisfy { $0 == $0.lowercased() })
    }

    /// Every box it hides is marked, so it can be told apart from Walmart's own hidden parts.
    @Test func hiddenBoxesAreMarked() {
        #expect(WalmartPageTidy.script.contains("data-dinneros-hidden"))
    }

    /// A disabled Use This was all but invisible on the white bar in light mode.
    /// The fill stays the secondary system fill; the text is DisabledText, an opaque gray at
    /// 4.5:1 or better on it (build 122 review 2; SetupGridAndDisabledLookTests measures it).
    @Test func disabledButtonsUseTheReadablePair() {
        #expect(DS.Colors.disabledText == Color("DisabledText"))
        #expect(DS.Colors.disabledFill == Color(.secondarySystemFill))
    }
}
