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
        #expect(feedback.miss(for: questions.ingredients[0])?.reason == RatingMiss.product)
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
        feedback.setReason("product", for: sprouts)
        #expect(feedback.productKeys == ["k1"])
        feedback.setReason("product", for: sprouts)
        #expect(feedback.miss(for: sprouts) == nil)
    }

    @Test func narrowsAMadeIngredientToOnePart() {
        var feedback = RatingFeedback(rating: nil, questions: questions)
        let sauce = questions.ingredients[1]
        feedback.setPart("Onions", for: sauce)
        #expect(
            feedback.miss(for: sauce)
                == RatingMiss(ingredientKey: "k2", name: "Creamy Sauce", part: "Onions", reason: "didnt-like"))
        feedback.setReason("product", for: sauce)
        #expect(feedback.miss(for: sauce)?.part == "Onions")
        feedback.setPart("Onions", for: sauce)
        #expect(feedback.miss(for: sauce)?.part == "")
        #expect(feedback.count(in: questions.categories[1], ingredients: questions.ingredients) == 1)
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
