import Testing

@testable import DinnerOS

/// Build 123 (Merrill, build 122 on Nov 22): a week with nothing to ask about yet showed an empty
/// band under its meal cards, because every card kept room for "Didn't make it?" and the stars.
/// A card now keeps room for a row only when another card in the week shows it.
struct Build123MenuCardsTests {
    private typealias Meal = MealCardRows.Meal

    /// Merrill's week on its first day: three dinners and garlic bread, every night still ahead.
    /// No room for "Didn't make it?"; the stars show on every card (Nov 23), nothing is held.
    @Test func aWeekWithNothingToAskKeepsNoRoom() {
        let week = Array(repeating: Meal(outcome: nil, nightHasPassed: false, canRate: true), count: 4)
        #expect(MealCardRows.of(week) == MealCardRows(holdsOutcome: false, holdsRating: true))
        #expect(MealRating.row(canRate: true, weekAsks: true) == .shown)
    }

    /// A night that's over shows "Didn't make it?" on its card, and the others keep its line.
    @Test func onePassedNightHoldsRoomOnEveryCard() {
        let week = [
            Meal(outcome: nil, nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ]
        let rows = MealCardRows.of(week)
        #expect(rows == MealCardRows(holdsOutcome: true, holdsRating: true))
        #expect(
            MealOutcomeRow.content(outcome: nil, nightHasPassed: false, holdsSpace: rows.holdsOutcome) == .heldSpace)
    }

    /// Marked made ahead of its night: it asks how it was, so the week keeps room.
    @Test func aMealMarkedMadeAsksEvenBeforeItsNight() {
        let rows = MealCardRows.of([
            Meal(outcome: .cooked, nightHasPassed: false, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rows.holdsRating)
        #expect(rows.holdsOutcome)
    }

    /// Not made: "I Made It" shows in the outcome row; the stars show as on every card.
    @Test func aMealNotMadeHoldsTheOutcomeRow() {
        let rows = MealCardRows.of([
            Meal(outcome: .skipped(nil), nightHasPassed: true, canRate: true),
            Meal(outcome: nil, nightHasPassed: false, canRate: true),
        ])
        #expect(rows == MealCardRows(holdsOutcome: true, holdsRating: true))
        #expect(MealOutcomeRow.content(outcome: .skipped(nil), nightHasPassed: true, holdsSpace: true) == .madeIt)
    }

    /// Without its menu card a meal can't show the stars (they'd ask again about a meal already
    /// rated), so it holds their room beside a card that shows them, and none on its own.
    @Test func aMealWithoutItsCardDoesNotAsk() {
        let rows = MealCardRows.of([Meal(outcome: nil, nightHasPassed: true, canRate: false)])
        #expect(rows == MealCardRows(holdsOutcome: true, holdsRating: false))
        #expect(MealRating.row(canRate: false, weekAsks: false) == .none)
        #expect(MealRating.row(canRate: false, weekAsks: true) == .heldSpace)
    }

    @Test func anEmptyWeekHoldsNothing() {
        #expect(MealCardRows.of([]) == .none)
    }

    @Test func theOutcomeRowShowsForAnAnswerOrAPassedNight() {
        #expect(MealOutcomeRow.shows(outcome: nil, nightHasPassed: true))
        #expect(MealOutcomeRow.shows(outcome: .cooked, nightHasPassed: false))
        #expect(MealOutcomeRow.shows(outcome: .skipped(.ateOut), nightHasPassed: false))
        #expect(!MealOutcomeRow.shows(outcome: nil, nightHasPassed: false))
    }

    /// The UI tests count cards, add-ons and stars by these.
    @Test func cardIdentifiersTellAddOnsApart() {
        #expect(MealCard.cardID(entryID: "e1", isAddOn: false, asks: false) == "menu.mealCard.e1")
        #expect(MealCard.cardID(entryID: "e1", isAddOn: false, asks: true) == "menu.mealCard.e1.asks")
        #expect(MealCard.cardID(entryID: "e2", isAddOn: true, asks: false) == "menu.mealCard.addOn.e2")
        #expect(MealCard.cardID(entryID: "e2", isAddOn: true, asks: true).hasPrefix(MealCard.cardIDPrefix))
        #expect(MealCard.ratingRowID == "menu.mealRating")
    }

    /// Markers are for the UI tests only: off unless the app is launched with QA_MARKERS=1.
    @Test func qaMarkersAreOffByDefault() {
        #expect(!QAMarkers.isOn)
        #expect(QAMarkers.photoPlaceholder == "photo.placeholder")
    }
}
