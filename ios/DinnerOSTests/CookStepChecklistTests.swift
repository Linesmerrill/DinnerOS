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
    }
}
