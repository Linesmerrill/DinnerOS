import Testing

@testable import DinnerOS

/// Hark's review of build 123 (TestFlight 121), item 2: on an iPad, "Didn't make it?" and "How
/// was it?" sat under the first meal card only, leaving an empty band under the servings control
/// on the cards beside it. When one card offers "Didn't make it?", every card does, in the same
/// place; the stars stay on past and made meals only, and the cards keep one height.
struct Build124MenuCardsTests {
    private typealias Meal = MealCardRows.Meal

    /// Sunday's pasta is over; Monday's garlic bread and Tuesday's tacos are still ahead. All
    /// three offer "Didn't make it?", only the pasta asks how it was.
    @Test func oneNightOverOffersDidntMakeItOnEveryCard() {
        let rows = MealCardRows.of([
            Meal(outcome: .cooked, nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rows.holdsOutcome)
        #expect(
            MealOutcomeRow.content(outcome: .cooked, nightHasPassed: true, weekOffers: rows.holdsOutcome)
                == .didntMakeIt)
        #expect(
            MealOutcomeRow.content(outcome: nil, nightHasPassed: false, weekOffers: rows.holdsOutcome) == .didntMakeIt)
        // Stars stay where they were: the meals still ahead only hold their room.
        #expect(MealRating.row(outcome: .cooked, nightHasPassed: true, weekAsks: rows.holdsRating) == .shown)
        #expect(MealRating.row(outcome: nil, nightHasPassed: false, weekAsks: rows.holdsRating) == .heldSpace)
    }

    /// The first day of a week: nothing to answer yet, so no card offers it.
    @Test func aWeekWithNothingOverOffersNothing() {
        let rows = MealCardRows.of(Array(repeating: Meal(outcome: nil, nightHasPassed: false, canRate: true), count: 3))
        #expect(MealOutcomeRow.content(outcome: nil, nightHasPassed: false, weekOffers: rows.holdsOutcome) == .nothing)
    }

    /// A meal marked not made offers "I Made It" in the same place, whatever its neighbours show.
    @Test func aMealNotMadeOffersIMadeIt() {
        #expect(MealOutcomeRow.content(outcome: .skipped(nil), nightHasPassed: true, weekOffers: true) == .madeIt)
        #expect(MealOutcomeRow.content(outcome: .skipped(.ateOut), nightHasPassed: false, weekOffers: false) == .madeIt)
    }

    /// A meal answered on its own still offers the row, without a neighbour.
    @Test func aPassedNightOffersItAlone() {
        #expect(MealOutcomeRow.content(outcome: nil, nightHasPassed: true, weekOffers: false) == .didntMakeIt)
    }

    /// A rated meal shows a follow-up button under its stars; the other cards keep that line so
    /// the row stays one height.
    @Test func aRatedMealHoldsTheFollowUpLine() {
        let rated = MealCardRows.of([
            Meal(outcome: nil, nightHasPassed: true, canRate: true, isRated: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rated.holdsFollowUp)
        let unrated = MealCardRows.of([
            Meal(outcome: nil, nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(!unrated.holdsFollowUp)
        // Rated some earlier week but not asking now (night ahead): no follow-up shows, none held.
        let ahead = MealCardRows.of([Meal(outcome: nil, nightHasPassed: false, canRate: true, isRated: true)])
        #expect(!ahead.holdsFollowUp)
    }
}
