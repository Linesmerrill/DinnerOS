import Testing

@testable import DinnerOS

/// Hark's review of build 123 (TestFlight 121), item 2: on an iPad, "Didn't make it?" and "How
/// was it?" sat under the first meal card only, leaving an empty band under the servings control
/// on the cards beside it. And Merrill, Nov 23: the stars show on every meal card again, coming
/// meals included, so the week shows how each was liked last time. "Didn't make it?" stays on
/// past meals, as the cards' last line, on the left; every card in a row is one height.
struct Build124MenuCardsTests {
    private typealias Meal = MealCardRows.Meal

    /// Sunday's pasta is made; Monday's garlic bread and Tuesday's tacos are still ahead. All
    /// three show the stars; only the pasta offers "Didn't make it?", the others keep its line.
    @Test func everyCardShowsTheStarsAndOnlyAPastMealOffersDidntMakeIt() {
        let rows = MealCardRows.of([
            Meal(outcome: .cooked, nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rows == MealCardRows(holdsOutcome: true, holdsRating: true))
        #expect(MealRating.row(canRate: true, weekAsks: rows.holdsRating) == .shown)
        #expect(
            MealOutcomeRow.content(outcome: .cooked, nightHasPassed: true, holdsSpace: rows.holdsOutcome)
                == .didntMakeIt)
        #expect(
            MealOutcomeRow.content(outcome: nil, nightHasPassed: false, holdsSpace: rows.holdsOutcome) == .heldSpace)
    }

    /// The first day of a week: stars on every card, and no line held for "Didn't make it?".
    @Test func aWeekWithNothingOverHoldsNoOutcomeLine() {
        let rows = MealCardRows.of(Array(repeating: Meal(outcome: nil, nightHasPassed: false, canRate: true), count: 3))
        #expect(rows.holdsRating)
        #expect(!rows.holdsOutcome)
        #expect(MealOutcomeRow.content(outcome: nil, nightHasPassed: false, holdsSpace: rows.holdsOutcome) == .nothing)
    }

    /// A meal marked not made offers "I Made It" in the same place.
    @Test func aMealNotMadeOffersIMadeIt() {
        #expect(MealOutcomeRow.content(outcome: .skipped(nil), nightHasPassed: true, holdsSpace: true) == .madeIt)
        #expect(MealOutcomeRow.content(outcome: .skipped(.ateOut), nightHasPassed: false, holdsSpace: false) == .madeIt)
    }

    /// A rated meal shows a follow-up button under its stars, coming or past; the other cards
    /// keep that line so the row stays one height.
    @Test func aRatedMealHoldsTheFollowUpLine() {
        let rated = MealCardRows.of([
            Meal(outcome: nil, nightHasPassed: false, canRate: true, isRated: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rated.holdsFollowUp)
        let unrated = MealCardRows.of([
            Meal(outcome: nil, nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(!unrated.holdsFollowUp)
        // Rated, but its card isn't loaded: no stars, so no follow-up either.
        #expect(
            !MealCardRows.of([Meal(outcome: nil, nightHasPassed: true, canRate: false, isRated: true)]).holdsFollowUp)
    }

    /// The UI tests find the stars and a card's whole frame by these; neither may start like a
    /// card's identifier, or the tests would count them as cards.
    @Test func ratingAndFrameIdentifiersAreNotCards() {
        #expect(MealCard.ratingRowID == "menu.mealRating")
        #expect(QAMarkers.mealCardFrame == "menu.mealCardFrame")
        #expect(!MealCard.ratingRowID.hasPrefix(MealCard.cardIDPrefix))
        #expect(!QAMarkers.mealCardFrame.hasPrefix(MealCard.cardIDPrefix))
    }

    /// The line over the stars: the score you gave, else "How was it?" once the meal's been had,
    /// else that it has no rating yet.
    @Test func theLineOverTheStars() {
        #expect(MealRatingRow.title(score: 4, asks: false) == "Your rating")
        #expect(MealRatingRow.title(score: 4, asks: true) == "Your rating")
        #expect(MealRatingRow.title(score: 0, asks: true) == "How was it?")
        #expect(MealRatingRow.title(score: 0, asks: false) == "Not rated yet")
    }
}
