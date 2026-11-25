import Testing

@testable import DinnerOS

/// An ingredient without a photo shows a sage tile with its first letter (Hark's build 121
/// round 3: Pine Nuts showed a gray fork and knife that read as a missing image).
struct IngredientFallbackTests {
    @Test func theLetterIsTheFoodsFirstLetter() {
        #expect(IngredientFallbackGlyph.initial(for: "Pine Nuts") == "P")
        #expect(IngredientFallbackGlyph.initial(for: "grape tomatoes") == "G")
        #expect(IngredientFallbackGlyph.initial(for: "Ćevapi") == "Ć")
    }

    @Test func wordsThatSayHowItsCutAreSkipped() {
        #expect(IngredientFallbackGlyph.initial(for: "Fresh Basil") == "B")
        #expect(IngredientFallbackGlyph.initial(for: "Boneless Skinless Chicken Thighs") == "C")
        #expect(IngredientFallbackGlyph.initial(for: "baby spinach") == "S")
        #expect(IngredientFallbackGlyph.initial(for: "Diced Chicken Thighs") == "C")
        // A name that is only a descriptor still gets its own letter.
        #expect(IngredientFallbackGlyph.initial(for: "Baby") == "B")
    }

    @Test func numbersAndMarksBeforeTheNameAreSkipped() {
        #expect(IngredientFallbackGlyph.initial(for: "  2% Milk") == "M")
        #expect(IngredientFallbackGlyph.initial(for: "\u{201C}Everything\u{201D} Seasoning") == "E")
    }

    @Test func aNameWithNoLettersHasNoLetter() {
        #expect(IngredientFallbackGlyph.initial(for: "") == nil)
        #expect(IngredientFallbackGlyph.initial(for: "123") == nil)
    }

    @Test func theUITestsMarkerNamesTheIngredientAndItsLetter() {
        #expect(IngredientFallbackGlyph.markerLabel(for: "Pine Nuts") == "Pine Nuts: P")
        #expect(IngredientFallbackGlyph.markerLabel(for: "") == ": no letter")
        #expect(QAMarkers.ingredientFallback == "ingredient.fallback")
    }
}
