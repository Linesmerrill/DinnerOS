import Foundation
import Testing

@testable import DinnerOS

struct MenuAPITests {
    private func makeAPI(_ transport: StubTransport) throws -> MenuAPI {
        MenuAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func week(_ string: String) throws -> ISOWeek {
        try #require(ISOWeek(string))
    }

    // MARK: Menu

    @Test func menuRequestsTheWeekAndDecodesSectionsInServerOrder() async throws {
        let plan = PlanFixtures.plan(entries: [PlanFixtures.entry(id: "entry-1", recipeID: "recipe-1")])
        let proposal = #"{"id":"proposal-1","status":"proposed","version":3,"plannedMeals":4}"#
        let body = MenuFixtures.menu(
            plan: plan, proposal: proposal,
            sections: [
                MenuFixtures.section(
                    id: "rule_tue", title: "Taco Tuesday",
                    cards: [
                        MenuFixtures.card(
                            id: "recipe-1", name: "Test Kitchen Tacos",
                            badges:
                                #"[{"code":"autopilot_pick","text":"Autopilot Pick"},{"code":"chef_special","text":"Chef's Pick"}]"#,
                            inPlan: true, entryIDs: ["entry-1"], reason: "Taco night")
                    ],
                    moreQuery: #"{"q":"taco","maxMinutes":"30","addons":"false","sort":"quick","unknown":1}"#),
                MenuFixtures.section(id: "favorites", title: "Your Favorites", cards: []),
                MenuFixtures.section(id: "long_cooks", title: "Worth the Wait", cards: []),
            ])
        let transport = StubTransport { _ in (200, body) }

        let menu = try await makeAPI(transport).menu(
            householdID: "household-1", week: try week("2026-W38"), accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/menu")
        #expect(request.url?.query() == "week=2026-W38")
        #expect(request.bearerToken == "token-1")

        #expect(menu.week == "2026-W38")
        #expect(menu.weekStart == "2026-09-14")
        #expect(menu.timing == .current)
        #expect(menu.plan?.entries.map(\.id) == ["entry-1"])
        #expect(menu.proposal == MenuProposalSummary(id: "proposal-1", status: .proposed, version: 3, plannedMeals: 4))
        #expect(menu.sections.map(\.id) == ["rule_tue", "favorites", "long_cooks"])

        let section = try #require(menu.sections.first)
        #expect(section.kind == .carousel)
        #expect(section.moreQuery?.search == "taco")
        #expect(section.moreQuery?.maxMinutes == 30)
        #expect(section.moreQuery?.addons == false)
        #expect(section.moreQuery?.sort == .quick)

        let card = try #require(section.items.first)
        #expect(card.recipe.name == "Test Kitchen Tacos")
        #expect(card.recipe.calories == 690)
        #expect(card.recipe.proteinGrams == 36)
        #expect(card.recipe.timeBand == .quick)
        #expect(card.inPlan)
        #expect(card.planEntryIDs == ["entry-1"])
        #expect(card.reason == "Taco night")
        #expect(card.badges.map(\.code) == [.autopilotPick, MenuBadgeCode(rawValue: "chef_special")])
        // An unknown code still has its text to show, without a symbol.
        #expect(card.badges.last?.text == "Chef's Pick")
        #expect(card.badges.last?.code.systemImage == nil)
    }

    @Test func menuWithMissingOptionalFieldsDecodes() throws {
        let json = Data(
            #"""
            {"week":"2026-W30","timing":"past","sections":[
              {"id":"history_planned","kind":"history","title":"What You Planned",
               "items":[{"recipe":{"id":"r-min","name":"Plain Toast","timesOrdered":0,"isAddon":true,"tags":[],
                                   "householdRating":{"average":null,"count":0},"myRating":null}},
                        {"badges":[]}]},
              {"id":"history_ordered","kind":"brand_new_kind","title":"Delivered"}
            ]}
            """#.utf8)

        let menu = try JSONCoding.makeDecoder().decode(WeekMenu.self, from: json)

        #expect(menu.timing == .past)
        #expect(menu.plan == nil)
        #expect(menu.proposal == nil)
        #expect(menu.weekStart.isEmpty)
        #expect(menu.sections.map(\.id) == ["history_planned", "history_ordered"])
        let history = try #require(menu.sections.first)
        #expect(history.kind == .history)
        #expect(history.subtitle == nil)
        #expect(history.moreQuery == nil)
        // The card without a recipe is skipped.
        #expect(history.items.count == 1)
        let card = try #require(history.items.first)
        #expect(card.badges.isEmpty)
        #expect(!card.inPlan)
        #expect(card.planEntryIDs.isEmpty)
        #expect(card.reason == nil)
        #expect(card.recipe.calories == nil)
        #expect(card.recipe.proteinGrams == nil)
        #expect(card.recipe.timeBand == nil)
        #expect(menu.sections.last?.kind == MenuSectionKind(rawValue: "brand_new_kind"))
        #expect(menu.sections.last?.items.isEmpty == true)
    }

    @Test func unreadableMenuFieldsDontFailTheSummary() throws {
        let json = Data(
            MenuFixtures.summary(
                id: "r1", name: "Odd Fields",
                extra: #","calories":{"kcal":690},"proteinGrams":"36","timeBand":"overnight""#
            ).utf8)

        let summary = try JSONCoding.makeDecoder().decode(RecipeSummary.self, from: json)

        #expect(summary.calories == nil)
        #expect(summary.proteinGrams == 36)
        #expect(summary.timeBand == AutopilotTimeBand(rawValue: "overnight"))
    }

    @Test func cardWithoutInPlanFollowsItsEntryIDs() throws {
        let json = Data(#"{"recipe":\#(MenuFixtures.summary(id: "r1", name: "Tacos")),"planEntryIds":["e1"]}"#.utf8)
        let card = try JSONCoding.makeDecoder().decode(MenuCard.self, from: json)
        #expect(card.inPlan)
    }

    // MARK: All Meals

    @Test func recipesSendEveryFilterInOrderAndEncodeStrictly() async throws {
        let transport = StubTransport { _ in (200, MenuFixtures.page([], nextCursor: nil)) }
        let query = MenuRecipeQuery(
            search: "  salt & pepper+lime ", protein: "chicken", cuisine: "Tex-Mex", maxMinutes: 30,
            tag: "Kid Friendly", addons: false, sort: .quick)

        _ = try await makeAPI(transport).recipes(
            householdID: "household-1", query: query, week: try week("2026-W38"), cursor: "abc=", limit: 24,
            accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/menu/recipes")
        #expect(
            request.url?.query(percentEncoded: true)
                == "sort=quick&q=salt%20%26%20pepper%2Blime&protein=chicken&cuisine=Tex-Mex&maxMinutes=30"
                + "&tag=Kid%20Friendly&addons=false&week=2026-W38&limit=24&cursor=abc%3D")
    }

    @Test func recipesOmitUnsetFilters() {
        let items = MenuAPI.queryItems(query: MenuRecipeQuery(search: "   "), week: nil, cursor: nil, limit: 10)
        #expect(items.map(\.name) == ["sort", "limit"])
        #expect(items.first?.value == "recommended")
    }

    @Test func decodesRecipePageAndSkipsUnreadableCards() throws {
        let body = MenuFixtures.page(
            [MenuFixtures.card(id: "r1", name: "Tacos", inPlan: true, entryIDs: ["e1"]), #"{"recipe":{"id":1}}"#],
            nextCursor: "next-1")

        let page = try JSONCoding.makeDecoder().decode(MenuRecipePage.self, from: body)

        #expect(page.items.map(\.id) == ["r1"])
        #expect(page.nextCursor == "next-1")
        #expect(page.items.first?.recipe.imageURL?.absoluteString.contains("w_1200") == true)
    }

    @Test func decodesFilters() async throws {
        let transport = StubTransport { _ in (200, MenuFixtures.filters) }

        let filters = try await makeAPI(transport).filters(householdID: "h", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/h/menu/filters")
        #expect(filters.proteins.map(\.label) == ["Chicken", "Pork"])
        #expect(filters.proteins.first?.count == 42)
        #expect(filters.cuisines.map(\.value) == ["mexican"])
        #expect(filters.tags.first?.label == "Kid Friendly")
        #expect(filters.maxMinutes == [15, 20, 30, 45])
        #expect(filters.sorts.map(\.value) == [.recommended, .quick])
        #expect(MenuFilterOptions.label(for: "pork", in: filters.proteins) == "Pork")
        #expect(MenuFilterOptions.label(for: "tofu", in: filters.proteins) == "Tofu")
    }

    @Test func filtersFallBackWhenListsAreMissing() throws {
        let filters = try JSONCoding.makeDecoder().decode(
            MenuFilterOptions.self, from: Data(#"{"proteins":[{"value":"beef"}],"maxMinutes":[]}"#.utf8))

        #expect(filters.proteins.first?.label == "beef")
        #expect(filters.proteins.first?.count == nil)
        #expect(filters.cuisines.isEmpty)
        #expect(filters.maxMinutes == MenuFilterOptions.defaultMaxMinutes)
        #expect(filters.sorts.map(\.value) == MenuSort.known)
    }

    // MARK: Weeks

    @Test func weeksRequestTheWindowAndDecode() async throws {
        let body = Data(
            #"""
            {"items":[\#(MenuFixtures.weekSummary("2026-W37", timing: "past", planned: 4, cooked: 3, ordered: 1, status: "finalized")),
                      {"week":"2026-W38","timing":"current","plannedCount":2,"status":"archived"}],
             "earliestWeek":"2026-W20"}
            """#.utf8)
        let transport = StubTransport { _ in (200, body) }

        let response = try await makeAPI(transport).weeks(
            householdID: "household-1", around: try week("2026-W38"), before: 8, after: 4, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/households/household-1/weeks")
        #expect(request.url?.query() == "around=2026-W38&before=8&after=4")
        #expect(response.earliestWeek == "2026-W20")
        #expect(response.items.map(\.week) == ["2026-W37", "2026-W38"])
        let past = try #require(response.items.first)
        #expect(past.timing == .past)
        #expect(past.plannedCount == 4)
        #expect(past.cookedCount == 3)
        #expect(past.orderedCount == 1)
        #expect(past.status == .finalized)
        #expect(past.weekStart == "2026-09-07")
        let current = try #require(response.items.last)
        #expect(current.cookedCount == 0)
        #expect(current.status == WeekStatus(rawValue: "archived"))
    }

    @Test func weeksWithoutHistoryDecode() throws {
        let response = try JSONCoding.makeDecoder().decode(
            WeekListResponse.self, from: Data(#"{"items":[],"earliestWeek":null}"#.utf8))
        #expect(response.items.isEmpty)
        #expect(response.earliestWeek == nil)
    }
}
