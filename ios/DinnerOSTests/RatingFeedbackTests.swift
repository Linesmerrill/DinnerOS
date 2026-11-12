import Foundation
import Testing

@testable import DinnerOS

/// "What could have been better?": picking answers, narrowing to a part, and what gets saved.
struct RatingFeedbackTests {
    private let questions = RatingQuestions(
        title: "What could have been better?",
        categories: [
            .init(
                code: "taste", title: "Taste", symbol: "fork.knife",
                options: [.init(tag: "too-salty", title: "Too salty"), .init(tag: "too-bland", title: "Too bland")]),
            .init(code: "ingredients", title: "An Ingredient", symbol: "carrot", options: []),
        ],
        ingredients: [
            .init(ingredientKey: "k1", name: "Brussels Sprouts", parts: []),
            .init(ingredientKey: "k2", name: "Creamy Sauce", parts: ["Onions", "Cream Cheese"]),
        ],
        reasons: [
            .init(reason: "didnt-like", title: "Didn't like it", detail: ""),
            .init(reason: "too-much", title: "Too much", detail: ""),
            .init(reason: "too-little", title: "Not enough", detail: ""),
            .init(reason: "product", title: "The product I bought", detail: ""),
        ])

    private func rating(tags: [RatingTag], misses: [RatingMiss]? = nil) -> RecipeRating {
        RecipeRating(
            recipeID: "r1", userID: "u1", score: 4, comment: "Good", tags: tags, createdAt: .now, updatedAt: .now,
            misses: misses)
    }

    @Test func startsFromTheSavedAnswersAndKeepsOtherTags() {
        let saved = rating(
            tags: [.makeAgain, .tooSalty],
            misses: [RatingMiss(ingredientKey: "k1", name: "Brussels Sprouts", reason: RatingMiss.product)])
        let feedback = RatingFeedback(rating: saved, questions: questions)
        #expect(feedback.isPicked("too-salty"))
        #expect(feedback.reasons(for: questions.ingredients[0]) == [RatingMiss.product])
        let draft = feedback.draft(score: 3)
        #expect(draft.score == 3)
        #expect(draft.comment == "Good")
        #expect(draft.tags.map(\.rawValue) == ["make-again", "too-salty"])
        #expect(draft.misses?.count == 1)
    }

    @Test func picksAndClearsAnswers() {
        var feedback = RatingFeedback(rating: nil, questions: questions)
        #expect(feedback.isEmpty)
        feedback.toggle("too-bland")
        #expect(feedback.count(in: questions.categories[0], ingredients: questions.ingredients) == 1)
        feedback.toggle("too-bland")
        #expect(feedback.isEmpty)

        let sprouts = questions.ingredients[0]
        feedback.toggleReason("product", for: sprouts)
        #expect(feedback.productKeys == ["k1"])
        feedback.toggleReason("product", for: sprouts)
        #expect(feedback.misses(for: sprouts).isEmpty)
    }

    @Test func narrowsAMadeIngredientToOnePart() {
        var feedback = RatingFeedback(rating: nil, questions: questions)
        let sauce = questions.ingredients[1]
        feedback.setPart("Onions", for: sauce)
        #expect(
            feedback.misses(for: sauce)
                == [RatingMiss(ingredientKey: "k2", name: "Creamy Sauce", part: "Onions", reason: "didnt-like")])
        feedback.toggleReason("product", for: sauce)
        // Every answer about the sauce is about the onions.
        #expect(feedback.misses(for: sauce).map(\.part) == ["Onions", "Onions"])
        feedback.setPart("Onions", for: sauce)
        #expect(feedback.part(for: sauce) == "")
        #expect(feedback.misses(for: sauce).allSatisfy { $0.part.isEmpty })
        #expect(feedback.count(in: questions.categories[1], ingredients: questions.ingredients) == 1)
    }

    /// The noodles were too few and the product was wrong; the sprouts were too much. Each
    /// ingredient keeps all its answers, and "Too much" and "Not enough" replace each other.
    @Test func eachIngredientCanHaveSeveralAnswers() {
        var feedback = RatingFeedback(rating: nil, questions: questions)
        let sprouts = questions.ingredients[0]
        let sauce = questions.ingredients[1]
        feedback.toggleReason("too-little", for: sprouts)
        feedback.toggleReason("product", for: sprouts)
        feedback.toggleReason("too-much", for: sauce)
        #expect(feedback.reasons(for: sprouts) == ["too-little", "product"])
        #expect(feedback.count(in: questions.categories[1], ingredients: questions.ingredients) == 2)

        feedback.toggleReason("too-much", for: sprouts)
        #expect(feedback.reasons(for: sprouts) == ["too-much", "product"])
        #expect(feedback.productKeys == ["k1"])

        let draft = feedback.draft(score: 5)
        #expect(draft.score == 5)
        #expect(
            draft.misses?.map { "\($0.name)|\($0.reason)" } == [
                "Brussels Sprouts|product", "Brussels Sprouts|too-much", "Creamy Sauce|too-much",
            ])

        feedback.clear(sprouts)
        #expect(feedback.misses(for: sprouts).isEmpty)
        #expect(feedback.reasons(for: sauce) == ["too-much"])
    }

    /// "The product I bought" with a note: the note goes on every answer about the ingredient, on
    /// the product flag, and on the recipe for next time.
    @Test func aNoteForNextTimeTravelsWithTheAnswers() {
        var feedback = RatingFeedback(rating: nil, questions: questions)
        let sprouts = questions.ingredients[0]
        feedback.setNote("ignored", for: sprouts)
        #expect(feedback.note(for: sprouts).isEmpty)
        feedback.toggleReason("too-little", for: sprouts)
        feedback.setNote("Get lo mein noodles", for: sprouts)
        feedback.toggleReason("product", for: sprouts)
        #expect(feedback.misses(for: sprouts).map(\.note) == ["Get lo mein noodles", "Get lo mein noodles"])
        #expect(feedback.productChanges.map(\.ingredientKey) == ["k1"])
        #expect(feedback.productChanges.first?.note == "Get lo mein noodles")
        feedback.setNote(String(repeating: "a", count: 300), for: sprouts)
        #expect(feedback.note(for: sprouts).count == RatingFeedback.maxNoteLength)

        feedback.setNote("Get lo mein noodles", for: sprouts)
        var saved = rating(tags: [], misses: feedback.draft(score: 5).misses)
        saved.misses?.append(RatingMiss(ingredientKey: "k2", name: "Creamy Sauce", reason: "too-much"))
        #expect(saved.notesForNextTime.map(\.note) == ["Get lo mein noodles"])
    }

    @Test func aMissNoteDecodesAndOlderOnesHaveNone() throws {
        let json =
            #"[{"ingredientKey":"k1","name":"Ramen Noodles","reason":"product","note":"Get lo mein"},"#
            + #"{"ingredientKey":"k2","name":"Peanuts","reason":"too-much"}]"#
        let misses = try JSONDecoder().decode([RatingMiss].self, from: Data(json.utf8))
        #expect(misses.map(\.note) == ["Get lo mein", ""])
    }

    /// The household ratings list opens a rating into what was said, one line per ingredient.
    @Test func aRatingReadsBackItsIngredientFeedback() {
        let saved = rating(
            tags: [],
            misses: [
                RatingMiss(ingredientKey: "k1", name: "Ramen Noodles", reason: "product", note: "Get lo mein noodles"),
                RatingMiss(ingredientKey: "k2", name: "Peanuts", reason: "too-much"),
                RatingMiss(
                    ingredientKey: "k1", name: "Ramen Noodles", reason: "too-little", note: "Get lo mein noodles"),
                RatingMiss(ingredientKey: "k3", name: "Creamy Sauce", part: "Onions", reason: "didnt-like"),
            ])
        #expect(
            saved.ingredientFeedback == [
                "Creamy Sauce (Onions): didn't like it",
                "Peanuts: too much",
                "Ramen Noodles: not enough, the product I bought. “Get lo mein noodles”",
            ])
        #expect(saved.hasFeedback)
        let starsOnly = RecipeRating(
            recipeID: "r1", userID: "u1", score: 5, comment: "", tags: [], createdAt: .now, updatedAt: .now)
        #expect(!starsOnly.hasFeedback)
        #expect(starsOnly.ingredientFeedback.isEmpty)
    }

    @Test func theRequestSendsMissesOnlyWhenAsked() throws {
        let plain =
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(RatingDraft(score: 4).request))
            as? [String: Any]
        #expect(plain?["misses"] == nil)
        var draft = RatingDraft(score: 4)
        draft.misses = []
        let cleared = try JSONSerialization.jsonObject(with: JSONEncoder().encode(draft.request)) as? [String: Any]
        #expect((cleared?["misses"] as? [Any])?.isEmpty == true)
    }

    @Test func questionsDecode() throws {
        let json = """
            {"title":"What could have been better?","categories":[{"code":"taste","title":"Taste","symbol":"fork.knife",
             "options":[{"tag":"too-salty","title":"Too salty"}]}],
             "ingredients":[{"ingredientKey":"k1","name":"Ground Beef","parts":[]}],
             "reasons":[{"reason":"product","title":"The product I bought","detail":"We'll ask you to pick a different product next time."}]}
            """
        let decoded = try JSONDecoder().decode(RatingQuestions.self, from: Data(json.utf8))
        #expect(decoded.categories.first?.options.first?.tag == "too-salty")
        #expect(decoded.ingredients.first?.name == "Ground Beef")
        #expect(decoded.reasons.first?.detail.isEmpty == false)
    }
}
