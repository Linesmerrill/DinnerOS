import Foundation
import Testing

@testable import DinnerOS

/// The household's standing answer for the specialty ingredients nobody has chosen an option for:
/// the settings round trip, and reading a choice the strategy made rather than a member.
struct SpecialtyStrategyTests {
    private let basePath = "/api/v1/households/household-1/specialty-ingredients"

    private func makeAPI(_ transport: StubTransport) throws -> SpecialtiesAPI {
        SpecialtiesAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func decode<T: Decodable>(_ type: T.Type, from json: String) throws -> T {
        try JSONCoding.makeDecoder().decode(type, from: try #require(json.data(using: .utf8)))
    }

    // MARK: - Settings

    @Test func settingsDecodeTheServersWordsForEveryStrategy() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.settingsJSON(strategy: "closest")) }

        let settings = try await makeAPI(transport).settings(householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == basePath + "/settings")
        #expect(request.bearerToken == "token-1")

        #expect(settings.strategy == .closest)
        #expect(settings.updatedBy == Fixtures.user.id)
        #expect(settings.updatedAt == JSONCoding.parseDate("2026-09-15T18:30:00Z"))
        #expect(settings.wasSet)
        #expect(settings.options.map(\.value) == [.similar, .closest, .ask])
        // The copy is the server's, so the app renders it as sent rather than hardcoding it.
        #expect(settings.currentOption?.label == "As close as possible")
        #expect(
            settings.currentOption?.description
                == "Make a jar you reuse across several meals — more work, closest to the original")
    }

    /// A household that never set one is on the API's default, and nothing may be attributed.
    @Test func settingsNobodyHasEverSetHaveNoAttribution() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.settingsJSON(changed: false)) }

        let settings = try await makeAPI(transport).settings(householdID: "household-1", accessToken: "t")

        #expect(settings.strategy == .similar)
        #expect(settings.updatedBy == nil)
        #expect(settings.updatedAt == nil)
        #expect(!settings.wasSet)
        #expect(
            SpecialtyFormat.strategyAttribution(settings, members: Self.members, currentUserID: nil) == nil)
    }

    @Test func settingTheStrategySendsItAndDecodesWhatComesBack() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.settingsJSON(strategy: "ask")) }

        let updated = try await makeAPI(transport).setSettings(
            householdID: "household-1", strategy: .ask, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == basePath + "/settings")
        #expect(request.jsonBody == ["strategy": "ask"])
        #expect(updated.strategy == .ask)
    }

    @Test func anUnknownStrategyFromANewerServerKeepsItsRawValue() throws {
        let settings = try decode(
            SpecialtySettings.self,
            from: #"{"strategy":"cheapest","updatedBy":null,"updatedAt":null,"options":[]}"#)

        #expect(settings.strategy.rawValue == "cheapest")
        #expect(settings.strategy != .similar)
        #expect(settings.currentOption == nil)
    }

    // MARK: - A choice nobody made

    /// The one breaking change: a strategy-sourced choice has no chooser and no time, so the
    /// whole screen used to fail to decode the first time a strategy applied.
    @Test func aStrategySourcedChoiceDecodesWithNobodyAttributed() throws {
        let ingredient = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.strategySpecialtyJSON)

        #expect(ingredient.choiceSource == .strategy)
        let choice = try #require(ingredient.choice)
        #expect(choice.source == .strategy)
        #expect(choice.strategy == .similar)
        #expect(choice.optionID == "sweet-soy-glaze.store")
        #expect(choice.type == .storeAlternative)
        #expect(choice.chosenBy == nil)
        #expect(choice.chosenAt == nil)

        #expect(choice.isFromStrategy)
        #expect(ingredient.isResolvedByStrategy)
        // Nobody chose it, so it is not the household's choice and cannot be cleared.
        #expect(!ingredient.hasHouseholdChoice)
    }

    @Test func anIngredientNothingAppliesToHasNoChoiceAtAll() throws {
        let ingredient = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.unresolvedSpecialtyJSON)

        #expect(ingredient.choiceSource == .unset)
        #expect(ingredient.choice == nil)
        #expect(!ingredient.isResolvedByStrategy)
        #expect(!ingredient.hasHouseholdChoice)
        #expect(SpecialtyFormat.choiceKind(ingredient) == "Not Set")
    }

    /// A server too old to send `choiceSource` only ever sent a member's own choice, so the
    /// attribution UI stays correct against it.
    @Test func aChoiceWithoutASourceReadsAsTheHouseholdsOwn() throws {
        let json = #"""
            {"id":"fry-seasoning","key":"fry seasoning","name":"Fry Seasoning","aliases":[],"category":"spices",
             "ingredientIds":[],"recipeCount":3,"unitSizes":[],"defaultOptionId":"fry-seasoning.batch",
             "retired":false,
             "choice":{"optionId":"fry-seasoning.batch","type":"house_made_batch","optionName":"Fry seasoning",
               "chosenBy":"user-1","chosenAt":"2026-09-15T18:30:00Z"},
             "options":[],"batch":null}
            """#

        let ingredient = try decode(SpecialtyIngredient.self, from: json)

        #expect(ingredient.choiceSource == .household)
        #expect(ingredient.hasHouseholdChoice)
        #expect(!ingredient.isResolvedByStrategy)
        #expect(ingredient.choice?.chosenBy == "user-1")
    }

    // MARK: - Attribution

    /// Fixed so the formatted dates don't depend on where the tests run.
    private static let enUS = Locale(identifier: "en_US")
    private static let utc = TimeZone(identifier: "UTC") ?? .gmt

    private static let members = [
        HouseholdMember(userID: "user-1", displayName: "Grace Hopper", role: .member, joinedAt: .now),
        HouseholdMember(
            userID: Fixtures.user.id, displayName: "Ada Lovelace", role: .admin, joinedAt: .now),
    ]

    @Test func aMembersChoiceIsAttributedAndAStrategysIsNot() throws {
        let chosen = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.southwestJSON)
        let resolved = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.strategySpecialtyJSON)

        let attribution = try #require(
            SpecialtyFormat.choiceAttribution(
                chosen, members: Self.members, currentUserID: nil, locale: Self.enUS, timeZone: Self.utc))
        #expect(attribution.contains("Grace Hopper"))
        #expect(attribution.contains("Sep 15, 2026"))
        #expect(SpecialtyFormat.strategyNote(chosen) == nil)

        // Nobody chose this one, so no name and no date may appear anywhere.
        #expect(SpecialtyFormat.choiceAttribution(resolved, members: Self.members, currentUserID: nil) == nil)
        #expect(SpecialtyFormat.strategyNote(resolved) != nil)
    }

    @Test func theBadgeSaysWhenTheDefaultPickedTheOption() throws {
        let chosen = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.southwestJSON)
        let resolved = try decode(SpecialtyIngredient.self, from: SpecialtyFixtures.strategySpecialtyJSON)

        #expect(SpecialtyFormat.choiceBadge(chosen) == "House-Made Batch")
        #expect(SpecialtyFormat.choiceBadge(resolved) == "Store Alternative · Default")
    }

    @Test func whoChangedTheStrategyIsShownWithTheDate() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.settingsJSON()) }
        let settings = try await makeAPI(transport).settings(householdID: "household-1", accessToken: "t")

        let attribution = try #require(
            SpecialtyFormat.strategyAttribution(
                settings, members: Self.members, currentUserID: nil, locale: Self.enUS, timeZone: Self.utc))

        #expect(attribution.contains("Ada Lovelace"))
        #expect(attribution.contains("Sep 15, 2026"))
    }

    // MARK: - Grocery lines

    @Test func aGroceryLineTheStrategyProducedSaysSo() throws {
        let via = try decode(GroceryVia.self, from: SpecialtyFixtures.strategyVia)

        #expect(via.strategy == .similar)
        #expect(via.isFromStrategy)
        // The server already words the line; the app doesn't add its own "(your default)".
        #expect(via.text == "Store alternative for Tex-Mex Paste in Smoky Pork Tacos (your default)")
    }

    /// An empty `strategy` means a member chose the option explicitly.
    @Test func aGroceryLineAMemberChoseIsNotADefault() throws {
        let json = #"""
            {"kind":"store_alternative","specialtyId":"tex-mex-paste","specialtyKey":"tex mex paste",
             "specialtyName":"Tex-Mex Paste","optionId":"tex-mex-paste.store","optionName":"Tomato paste",
             "strategy":"","yield":null,"batches":null,"recipes":[],
             "text":"for Tex-Mex Paste in Smoky Pork Tacos"}
            """#

        let via = try decode(GroceryVia.self, from: json)

        #expect(via.strategy == nil)
        #expect(!via.isFromStrategy)
    }
}
