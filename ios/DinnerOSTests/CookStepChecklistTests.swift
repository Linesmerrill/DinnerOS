import Testing

@testable import DinnerOS

/// The prep words and parts the by-step list reads out of a step's own words.
struct CookStepChecklistTests {
    @Test func prepWordsReadAsDone() {
        #expect(CookChecklist.prepWords(in: "Trim and slice ") == "sliced")
        #expect(CookChecklist.prepWords(in: "Quarter ") == "quartered")
        #expect(CookChecklist.prepWords(in: "Peel, core, and dice ") == "peeled, cored, and diced")
        #expect(CookChecklist.prepWords(in: "Halve, core, and thinly slice ") == "halved, cored, and thinly sliced")
        #expect(CookChecklist.prepWords(in: "In a medium bowl, combine ") == nil)
        // A word that belongs to something earlier in the sentence never lands here.
        #expect(CookChecklist.prepWords(in: "Stir drained rigatoni, half the Parmesan, and ") == nil)
        #expect(CookChecklist.prepWords(in: "Peel and mince or grate ") == "peeled, minced, or grated")
        #expect(CookChecklist.prepWords(in: "Add diced ") == "diced")
        #expect(CookChecklist.prepWords(in: "juice from half ") == "juiced")
        #expect(CookChecklist.prepWords(in: "Dice the onion, then add the ") == nil)
    }
}

struct CookAheadTests {
    @Test func butterAndCreamCheeseGetReadyAhead() {
        #expect(CookChecklist.aheadNote("Butter", step: 4) == "cut into pieces, for step 4")
        #expect(CookChecklist.aheadNote("Unsalted Butter", step: 3) == "cut into pieces, for step 3")
        #expect(CookChecklist.aheadNote("Peanut Butter", step: 3) == nil)
        #expect(CookChecklist.aheadNote("Cream Cheese", step: 3) == "let soften, for step 3")
        #expect(CookChecklist.aheadNote("Shallot", step: 2) == nil)
    }
}
