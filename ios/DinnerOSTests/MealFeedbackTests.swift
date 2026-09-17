import Foundation
import Testing

@testable import DinnerOS

/// When the Menu asks how a meal went, and what happens to the cards when it's answered.
struct MealFeedbackTests {

    // MARK: - When the question is asked

    @Test func everyMealInAPastWeekHasHadItsNight() {
        for day in PlanDay.allCases {
            #expect(MealFeedback.hasHappened(day: day, timing: .past, today: nil, weekStartsOn: .mon))
        }
        // A past week's meal with no day still happened, or didn't, a week ago either way.
        #expect(MealFeedback.hasHappened(day: nil, timing: .past, today: nil, weekStartsOn: .mon))
    }

    @Test func thisWeekAsksOnlyAboutTodayAndTheDaysBefore() {
        #expect(MealFeedback.hasHappened(day: .mon, timing: .current, today: .wed, weekStartsOn: .mon))
        #expect(
            MealFeedback.hasHappened(day: .wed, timing: .current, today: .wed, weekStartsOn: .mon),
            "tonight's meal is fair to ask about")
        #expect(!MealFeedback.hasHappened(day: .thu, timing: .current, today: .wed, weekStartsOn: .mon))
        #expect(!MealFeedback.hasHappened(day: .sun, timing: .current, today: .mon, weekStartsOn: .mon))
    }

    /// With Sunday weeks, Sunday comes first: by Monday its meal has happened, and Saturday's hasn't.
    @Test func thisWeekFollowsTheHouseholdsWeekStartDay() {
        #expect(MealFeedback.hasHappened(day: .sun, timing: .current, today: .mon, weekStartsOn: .sun))
        #expect(!MealFeedback.hasHappened(day: .sat, timing: .current, today: .mon, weekStartsOn: .sun))
        #expect(!MealFeedback.hasHappened(day: .sun, timing: .current, today: .mon, weekStartsOn: .mon))
        // Saturday weeks: Friday is the last night.
        #expect(MealFeedback.hasHappened(day: .sat, timing: .current, today: .fri, weekStartsOn: .sat))
        #expect(!MealFeedback.hasHappened(day: .fri, timing: .current, today: .sat, weekStartsOn: .sat))
    }

    /// Asking about a meal planned for the week without a night would be asking about nothing.
    @Test func aMealWithNoDayIsNeverAskedAboutInTheCurrentWeek() {
        #expect(!MealFeedback.hasHappened(day: nil, timing: .current, today: .wed, weekStartsOn: .mon))
    }

    @Test func anUpcomingWeekIsNeverAskedAbout() {
        for day in PlanDay.allCases {
            #expect(!MealFeedback.hasHappened(day: day, timing: .upcoming, today: .wed, weekStartsOn: .mon))
        }
    }

    /// A week the app can't place — no "today" for the current week — asks nothing rather than
    /// asking about every meal in it.
    @Test func withoutATodayTheCurrentWeekAsksNothing() {
        #expect(!MealFeedback.hasHappened(day: .mon, timing: .current, today: nil, weekStartsOn: .mon))
    }

    // MARK: - Showing a saved rating on the cards

    private static let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    private func activatedStore() async throws -> MenuStore {
        let server = FakeMenuServer()
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate("2026-09-16T12:00:00Z"))
        let store = MenuStore(session: session, api: MenuAPI(client: client), now: { instant })
        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        return store
    }

    private func rating(_ score: Int, recipeID: String = "recipe-1") -> RecipeRating {
        RecipeRating(
            recipeID: recipeID, userID: Fixtures.user.id, score: score, comment: "", tags: [],
            createdAt: .now, updatedAt: .now)
    }

    @MainActor
    @Test func aSavedRatingShowsOnEveryCardForThatRecipe() async throws {
        let store = try await activatedStore()
        #expect(store.card(forRecipeID: "recipe-1")?.recipe.myRating == nil, "the fixture starts unrated")

        store.applyRating(
            recipeID: "recipe-1", mine: rating(5), household: HouseholdRating(average: 5, count: 1))

        let card = try #require(store.card(forRecipeID: "recipe-1"))
        #expect(card.recipe.myRating?.score == 5)
        #expect(card.recipe.householdRating == HouseholdRating(average: 5, count: 1))
    }

    @MainActor
    @Test func ratingOneRecipeLeavesTheOtherCardsAlone() async throws {
        let store = try await activatedStore()

        store.applyRating(
            recipeID: "recipe-1", mine: rating(4), household: HouseholdRating(average: 4, count: 1))

        let other = try #require(store.card(forRecipeID: "recipe-2"))
        #expect(other.recipe.myRating == nil)
        #expect(other.recipe.householdRating == .unrated)
    }

    /// The save reloads the recipe to learn the new average; when that reload fails the rating is
    /// still saved, so the stars have to change even though the average isn't known yet.
    @MainActor
    @Test func aRatingWithoutANewAverageKeepsTheAverageItHad() async throws {
        let store = try await activatedStore()
        store.applyRating(
            recipeID: "recipe-1", mine: rating(3), household: HouseholdRating(average: 3, count: 2))

        store.applyRating(recipeID: "recipe-1", mine: rating(5), household: nil)

        let card = try #require(store.card(forRecipeID: "recipe-1"))
        #expect(card.recipe.myRating?.score == 5)
        #expect(card.recipe.householdRating == HouseholdRating(average: 3, count: 2))
    }

    @MainActor
    @Test func removingARatingClearsItFromTheCard() async throws {
        let store = try await activatedStore()
        store.applyRating(
            recipeID: "recipe-1", mine: rating(5), household: HouseholdRating(average: 5, count: 1))

        store.applyRating(recipeID: "recipe-1", mine: nil, household: .unrated)

        let card = try #require(store.card(forRecipeID: "recipe-1"))
        #expect(card.recipe.myRating == nil)
        #expect(card.recipe.householdRating == .unrated)
    }
}
