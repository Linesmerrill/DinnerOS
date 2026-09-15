import Foundation
import Testing

@testable import DinnerOS

struct AutopilotAPITests {
    private let week = ISOWeek("2026-W38")

    private func makeAPI(_ transport: StubTransport) throws -> AutopilotAPI {
        AutopilotAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func jsonBody(_ request: URLRequest?) throws -> [String: Any] {
        let data = try #require(request?.httpBody)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    // MARK: Profile

    @Test func getProfileDecodesEverySectionAndItsAttribution() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.profile().utf8)) }

        let profile = try await makeAPI(transport).profile(householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/autopilot/profile")
        #expect(request.bearerToken == "token-1")
        #expect(profile.configured)
        #expect(profile.taste.likes.cuisines == ["mexican", "thai"])
        #expect(profile.taste.dislikes.proteins == ["lamb"])
        #expect(profile.restrictions.allergens == ["peanuts"])
        #expect(profile.restrictions.excludedIngredients == ["cilantro"])
        #expect(profile.schedule.planDays == [.mon, .tue, .wed, .thu, .fri, .sun])
        #expect(profile.schedule.defaultServings == nil)
        #expect(profile.schedule.weeknightMaxMinutes == 35)
        #expect(profile.cookTime.maxLongPerWeek == 1)
        #expect(profile.cookTime.minQuickPerWeek == 2)
        #expect(profile.novelty == .balanced)
        #expect(profile.equipment == ["smoker"])
        #expect(profile.weekdayRules.map(\.day) == [.tue, .sun])
        let sunday = try #require(profile.settings.rule(for: .sun))
        #expect(sunday.label == "Sunday smoker night")
        #expect(sunday.proteins == ["chicken", "pork"])
        #expect(sunday.methods == ["smoker"])
        #expect(sunday.timeBand == .long)
        #expect(sunday.frequency == .atMostOnce)
        #expect(profile.settings.rule(for: .tue)?.timeBand == nil)
        #expect(profile.sections[.cookTime]?.updatedBy == AutopilotFixtures.otherMemberID)
        #expect(profile.sections[.taste]?.updatedBy == Fixtures.user.id)
        #expect(profile.effective.defaultServings == 2)
        #expect(profile.updatedAt != nil)
    }

    @Test func unconfiguredProfileDecodesDefaultsWithNullMetadata() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.profile(configured: false).utf8)) }

        let profile = try await makeAPI(transport).profile(householdID: "h", accessToken: "t")

        #expect(!profile.configured)
        #expect(profile.settings == AutopilotSettings.defaults)
        let noAttribution = AutopilotSection.allCases.allSatisfy { profile.sections[$0] == nil }
        #expect(noAttribution)
        #expect(profile.createdBy == nil)
        #expect(profile.updatedAt == nil)
    }

    @Test func replaceProfileSendsEverySectionWithExplicitNulls() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.profile().utf8)) }
        var settings = AutopilotSettings.defaults
        settings.equipment = ["smoker"]
        settings.setRule(.smokerNight(on: .sun), for: .sun)
        settings.setRule(AutopilotWeekdayRule(day: .tue, cuisines: ["mexican"]), for: .tue)

        _ = try await makeAPI(transport).replaceProfile(
            householdID: "household-1", settings: settings, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/autopilot/profile")
        let body = try jsonBody(request)
        #expect(
            Set(body.keys) == ["taste", "restrictions", "schedule", "cookTime", "novelty", "equipment", "weekdayRules"])
        let schedule = try #require(body["schedule"] as? [String: Any])
        #expect(
            Set(schedule.keys) == ["planDays", "weeknights", "mealsPerWeek", "defaultServings", "weeknightMaxMinutes"])
        #expect(schedule["defaultServings"] is NSNull)
        #expect(schedule["planDays"] as? [String] == ["mon", "tue", "wed", "thu", "fri"])
        let cookTime = try #require(body["cookTime"] as? [String: Any])
        #expect(cookTime["avoidConsecutiveLong"] as? Bool == true)
        #expect(cookTime["maxLongPerWeek"] as? Int == 2)
        let rules = try #require(body["weekdayRules"] as? [[String: Any]])
        #expect(rules.map { $0["day"] as? String } == ["tue", "sun"])
        #expect(rules[0]["timeBand"] is NSNull)
        #expect(rules[1]["timeBand"] as? String == "long")
        #expect(rules[1]["methods"] as? [String] == ["smoker"])
        #expect(rules[1]["frequency"] as? String == "every_week")
        #expect(body["novelty"] as? String == "balanced")
    }

    @Test func updateProfileSendsOnlyTheChosenSectionsWhole() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.profile().utf8)) }
        var settings = AutopilotSettings.defaults
        settings.cookTime.maxLongPerWeek = 1

        _ = try await makeAPI(transport).updateProfile(
            householdID: "household-1", update: AutopilotProfileUpdate(settings: settings, sections: [.cookTime]),
            accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PATCH")
        let body = try jsonBody(request)
        #expect(Set(body.keys) == ["cookTime"])
        let cookTime = try #require(body["cookTime"] as? [String: Any])
        #expect(
            Set(cookTime.keys) == [
                "quickMaxMinutes", "mediumMaxMinutes", "maxLongPerWeek", "minQuickPerWeek", "avoidConsecutiveLong",
            ])
        #expect(cookTime["maxLongPerWeek"] as? Int == 1)
    }

    @Test func vocabularyDecodesOptionsCountsAndLimits() async throws {
        let transport = StubTransport { _ in (200, AutopilotFixtures.vocabulary) }

        let vocabulary = try await makeAPI(transport).vocabulary(householdID: "household-1", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/autopilot/vocabulary")
        #expect(vocabulary.cuisines.map(\.value) == ["mexican", "thai"])
        #expect(vocabulary.cuisines[0].recipeCount == 42)
        #expect(vocabulary.diets[0].description == "No meat or fish")
        #expect(vocabulary.allergens[0].recipeCount == nil)
        #expect(vocabulary.catalogRecipeCount == 429)
        #expect(vocabulary.limits.maxRuleValues == 10)
        #expect(vocabulary.limits.maxExcludedIngredients == 50)
        #expect(AutopilotVocabulary.label(for: "tofu", in: vocabulary.proteins) == "Tofu & tempeh")
        #expect(AutopilotVocabulary.label(for: "korean bbq", in: vocabulary.cuisines) == "Korean Bbq")
    }

    @Test func historyDecodesEveryTypeAndClampsTheLimit() async throws {
        let transport = StubTransport { _ in (200, AutopilotFixtures.history) }
        let api = try makeAPI(transport)

        let items = try await api.history(householdID: "household-1", limit: 50, accessToken: "t")
        _ = try await api.history(householdID: "household-1", limit: 500, accessToken: "t")

        #expect(transport.requests[0].url?.path() == "/api/v1/households/household-1/autopilot/profile/history")
        #expect(transport.requests[0].url?.query() == "limit=50")
        #expect(transport.requests[1].url?.query() == "limit=100")
        #expect(items.count == 5)
        #expect(items[0].type == AutopilotHistoryItem.preferencesUpdated)
        #expect(items[0].sections == ["cookTime"])
        #expect(
            items[0].changes?.first
                == AutopilotFieldChange(field: "cookTime.maxLongPerWeek", added: nil, removed: nil, from: "2", to: "1"))
        #expect(items[1].changes?.first?.added == ["thai"])
        #expect(items[2].week == "2026-W38")
        #expect(items[3].cleared == true)
        #expect(items[4].recipeID == "recipe-3")
        #expect(items[4].value == "yes")
        #expect(items[4].previous == "auto")
    }

    // MARK: Recipes

    @Test func attributesDecodeMethodsAndOverride() async throws {
        let transport = StubTransport { _ in (200, AutopilotFixtures.attributes) }

        let attributes = try await makeAPI(transport).attributes(
            householdID: "household-1", recipeID: "recipe-3", accessToken: "t")

        #expect(
            transport.requests.first?.url?.path()
                == "/api/v1/households/household-1/autopilot/recipes/recipe-3/attributes")
        #expect(attributes.cookMinutes == 90)
        #expect(attributes.timeBand == .long)
        #expect(attributes.methods.map(\.method) == ["smoker", "grill"])
        let smoker = try #require(attributes.methods.first)
        #expect(smoker.setting == .no)
        #expect(smoker.heuristicSuits)
        #expect(smoker.evidence == "Pork Shoulder")
        #expect(attributes.methods[1].setting == .automatic)
        #expect(attributes.methods[1].evidence == nil)
        #expect(attributes.override?.methods == ["smoker": false])
    }

    @Test func overrideSendsYesNoOrNullForAutomatic() async throws {
        let transport = StubTransport { _ in (200, AutopilotFixtures.attributes) }
        let api = try makeAPI(transport)

        _ = try await api.setOverride(
            householdID: "household-1", recipeID: "recipe-3",
            methods: ["smoker": AutopilotMethodSetting.yes.overrideValue],
            accessToken: "t")
        _ = try await api.setOverride(
            householdID: "household-1", recipeID: "recipe-3",
            methods: ["smoker": AutopilotMethodSetting.automatic.overrideValue], accessToken: "t")

        #expect(transport.requests[0].httpMethod == "PUT")
        #expect(
            transport.requests[0].url?.path() == "/api/v1/households/household-1/autopilot/recipes/recipe-3/override")
        let yes = try #require(try jsonBody(transport.requests[0])["methods"] as? [String: Any])
        #expect(yes["smoker"] as? Bool == true)
        let automatic = try #require(try jsonBody(transport.requests[1])["methods"] as? [String: Any])
        #expect(Set(automatic.keys) == ["smoker"])
        #expect(automatic["smoker"] is NSNull)
    }

    // MARK: Week context

    @Test func weekContextDecodesDayOverrides() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.context().utf8)) }

        let context = try await makeAPI(transport).weekContext(
            householdID: "household-1", week: try #require(week), accessToken: "t")

        #expect(
            transport.requests.first?.url?.path() == "/api/v1/households/household-1/autopilot/weeks/2026-W38/context")
        #expect(context.configured)
        #expect(context.busy)
        #expect(context.maxMinutes == 20)
        #expect(context.days.map(\.day) == [.fri, .sat])
        #expect(context.draft[.fri].servings == 6)
        #expect(context.draft[.sat].skip)
        #expect(context.draft[.mon].isEmpty)
        #expect(context.note == "Grandparents visiting Friday")
    }

    @Test func saveWeekContextSendsTheWholeContextAndDropsEmptyDays() async throws {
        let transport = StubTransport { _ in (200, Data(AutopilotFixtures.context().utf8)) }
        var draft = AutopilotWeekContextDraft(busy: true, maxMinutes: 20, note: "  Guests Friday  ")
        draft[.sat] = AutopilotDayOverride(day: .sat, skip: true)
        draft[.fri] = AutopilotDayOverride(day: .fri, servings: 6)
        draft[.mon] = AutopilotDayOverride(day: .mon)

        _ = try await makeAPI(transport).saveWeekContext(
            householdID: "household-1", week: try #require(week), draft: draft, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        let body = try jsonBody(request)
        #expect(Set(body.keys) == ["skip", "busy", "mealsPerWeek", "maxMinutes", "servings", "days", "note"])
        #expect(body["busy"] as? Bool == true)
        #expect(body["maxMinutes"] as? Int == 20)
        #expect(body["servings"] is NSNull)
        #expect(body["note"] as? String == "Guests Friday")
        let days = try #require(body["days"] as? [[String: Any]])
        #expect(days.map { $0["day"] as? String } == ["fri", "sat"])
        #expect(days[0]["servings"] as? Int == 6)
        #expect(days[0]["maxMinutes"] is NSNull)
        #expect(days[1]["skip"] as? Bool == true)
        #expect(!draft.isEmpty)
        #expect(AutopilotWeekContextDraft(note: "   ").isEmpty)
    }

    @Test func clearWeekContextDeletes() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await makeAPI(transport).clearWeekContext(
            householdID: "household-1", week: try #require(week), accessToken: "t")

        #expect(transport.requests.first?.httpMethod == "DELETE")
        #expect(
            transport.requests.first?.url?.path() == "/api/v1/households/household-1/autopilot/weeks/2026-W38/context")
    }

    // MARK: Proposals

    @Test func generateDecodesSlotsReasonsMessagesAndUnfilledDays() async throws {
        let body = AutopilotFixtures.proposal(
            unfilled: ["thu"], messages: ["Only 3 quick recipes (≤20 min) match; planned 3 of 4 nights."])
        let transport = StubTransport { _ in (201, Data(body.utf8)) }

        let proposal = try await makeAPI(transport).generate(
            householdID: "household-1", week: try #require(week), accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/autopilot/weeks/2026-W38/generate")
        #expect(try jsonBody(request).isEmpty)
        #expect(proposal.isPending)
        #expect(proposal.version == 1)
        #expect(proposal.slots.map(\.id) == ["mon", "wed", "sun"])
        let sunday = try #require(proposal.slot(id: "sun"))
        #expect(sunday.day == .sun)
        #expect(sunday.date == "2026-09-20")
        #expect(sunday.cookMinutes == 240)
        #expect(sunday.timeBand == .long)
        #expect(sunday.recipe.imageURL == URL(string: "https://img.example.test/recipe-3.jpg"))
        #expect(sunday.reasonText == "Sunday smoker night · Pork · Long cook OK · Rated 4.5★ by your household")
        #expect(proposal.slot(id: "wed")?.cookMinutes == nil)
        #expect(proposal.slot(id: "wed")?.reasonText == "")
        #expect(proposal.unfilled.map(\.day) == [.thu])
        #expect(proposal.messages.first?.code == "not_enough_candidates")
        #expect(proposal.rows.map(\.id) == ["mon", "wed", "thu", "sun"])
        #expect(proposal.decidedBy == nil)
    }

    @Test func swapAcceptAndRejectSendTheVersion() async throws {
        let transport = StubTransport { request in
            if request.url?.path().hasSuffix("/accept") == true {
                let plan = PlanFixtures.plan(entries: [
                    AutopilotFixtures.entry(
                        id: "entry-1", recipeID: "recipe-1", name: "Test Kitchen Tacos", day: "mon", origin: "autopilot"
                    )
                ])
                let added = AutopilotFixtures.entry(
                    id: "entry-1", recipeID: "recipe-1", name: "Test Kitchen Tacos", day: "mon", origin: "autopilot")
                let json = #"""
                    {"proposal":\#(AutopilotFixtures.proposal(status: "accepted", version: 4, excluded: ["wed"])),
                     "plan":\#(plan),"added":[\#(added)],
                     "skipped":[{"slotId":"sun","day":"sun","reason":"dayTaken"},{"slotId":"tue","day":"tue","reason":"alreadyPlanned"}]}
                    """#
                return (200, Data(json.utf8))
            }
            return (200, Data(AutopilotFixtures.proposal(version: 3).utf8))
        }
        let api = try makeAPI(transport)
        let week = try #require(week)

        _ = try await api.swap(householdID: "household-1", week: week, slotID: "sun", version: 2, accessToken: "t")
        let result = try await api.accept(
            householdID: "household-1", week: week, version: 3, excludeSlotIDs: ["wed"], accessToken: "t")
        _ = try await api.reject(householdID: "household-1", week: week, version: 3, accessToken: "t")

        let base = "/api/v1/households/household-1/autopilot/weeks/2026-W38/proposal"
        #expect(
            transport.requests.map { $0.url?.path() } == [base + "/slots/sun/swap", base + "/accept", base + "/reject"])
        let allPosts = transport.requests.allSatisfy { $0.httpMethod == "POST" }
        #expect(allPosts)
        #expect(try jsonBody(transport.requests[0])["version"] as? Int == 2)
        let accept = try jsonBody(transport.requests[1])
        #expect(accept["version"] as? Int == 3)
        #expect(accept["excludeSlotIds"] as? [String] == ["wed"])
        #expect(Set(try jsonBody(transport.requests[2]).keys) == ["version"])

        #expect(result.proposal.status == .accepted)
        #expect(result.proposal.excludedSlotIDs == ["wed"])
        #expect(result.plan.entries.first?.isFromAutopilot == true)
        #expect(result.added.map(\.origin) == [.autopilot])
        #expect(result.skipped.map(\.slotID) == ["sun", "tue"])
        #expect(result.skipped[0].explanation.contains("already has a meal"))
        #expect(result.skipped[1].explanation.contains("already planned"))
    }

    @Test func planEntriesWithoutAnOriginDecodeAsNotAutopilot() async throws {
        let transport = StubTransport { _ in
            (200, Data(PlanFixtures.plan(entries: [PlanFixtures.entry(id: "e1")]).utf8))
        }

        let plan = try await PlansAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        ).plan(householdID: "household-1", week: try #require(week), accessToken: "t")

        #expect(plan.entries.first?.origin == nil)
        #expect(plan.entries.first?.isFromAutopilot == false)
    }

    // MARK: Errors

    @Test(
        arguments: [
            ("proposal_changed", "", AutopilotConflict.proposalChanged, "changed these suggestions"),
            ("proposal_not_pending", "", .proposalNotPending, "already accepted or dismissed"),
            ("plan_finalized", "", .planFinalized, "finalized"),
            ("plan_full", "", .planFull, "50"),
            ("no_alternative", "No other recipe fits Sunday.", .noAlternative, "No other recipe fits Sunday."),
            ("nothing_to_accept", "", .nothingToAccept, "Nothing to add"),
            ("proposal_stale", "", .proposalStale, "Plan the week again"),
            ("conflict", "", .conflict, "at the same time"),
        ] as [(String, String, AutopilotConflict, String)])
    func conflictsMapToCodesAndClearMessages(
        code: String, message: String, expected: AutopilotConflict, fragment: String
    ) async throws {
        let transport = StubTransport { _ in (409, Fixtures.errorJSON(code: code, message: message)) }

        do {
            _ = try await makeAPI(transport).swap(
                householdID: "h", week: try #require(week), slotID: "sun", version: 1, accessToken: "t")
            Issue.record("expected \(code)")
        } catch let error as APIError {
            #expect(AutopilotConflict(error) == expected)
            #expect(error.localizedDescription.contains(fragment))
            #expect(
                AutopilotStore.meansProposalChanged(error)
                    == [.proposalChanged, .proposalNotPending].contains(expected))
        }
    }

    @Test func notFoundMeansTheProposalIsGoneButForbiddenDoesNot() {
        let notFound = APIError.server(status: 404, code: "not_found", message: "", requestID: nil)
        let forbidden = APIError.server(status: 403, code: "forbidden", message: "", requestID: nil)
        #expect(AutopilotStore.meansProposalChanged(notFound))
        #expect(!AutopilotStore.meansProposalChanged(forbidden))
        #expect(AutopilotConflict(forbidden) == nil)
        #expect(forbidden.localizedDescription.contains("role"))
    }
}
