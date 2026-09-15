import Foundation
import Testing

@testable import DinnerOS

private func specialtyList() throws -> GroceryList {
    try JSONCoding.makeDecoder().decode(GroceryList.self, from: SpecialtyFixtures.groceryList)
}

private func item(named name: String, in list: GroceryList) throws -> GroceryItem {
    try #require(list.allItems.first { $0.name == name })
}

struct GrocerySpecialtyDecodingTests {
    @Test func decodesViaSpecialtyDetailAndBatches() throws {
        let list = try specialtyList()

        #expect(list.specialtiesApplied)
        #expect(list.batches.map(\.specialtyID) == ["southwest-spice-blend", "fry-seasoning"])
        let make = list.batches[0]
        #expect(make.status == .make)
        #expect(make.reason == .missing)
        #expect(make.batches == 1)
        #expect(make.batchYield.text == "12 tbsp")
        #expect(make.needed?.text == "2 tbsp")
        #expect(make.remaining == nil)
        #expect(make.pantryItemID == nil)
        #expect(make.recipes.map(\.name) == ["Chili Bowls"])
        #expect(make.text == "Make a batch (makes about 12 tbsp)")
        let made = list.batches[1]
        #expect(made.status == .inPantry)
        #expect(made.reason == .enough)
        #expect(made.remaining?.text == "8 tbsp")
        #expect(made.pantryItemID == "item-fry")

        let tomato = try item(named: "Tomato Paste", in: list)
        #expect(!tomato.specialty)
        #expect(tomato.specialtyDetail == nil)
        let storeVia = try #require(tomato.via.first)
        #expect(storeVia.kind == .storeAlternative)
        #expect(storeVia.specialtyID == "tex-mex-paste")
        #expect(storeVia.batchYield == nil)
        #expect(storeVia.batches == nil)
        #expect(storeVia.text == "for Tex-Mex Paste in Smoky Pork Tacos")

        let cumin = try item(named: "Ground Cumin", in: list)
        #expect(cumin.via.first?.kind == .houseMadeBatch)
        #expect(cumin.via.first?.batchYield?.text == "12 tbsp")
        #expect(cumin.via.first?.batches == 1)

        let glaze = try item(named: "Sweet Soy Glaze", in: list)
        #expect(glaze.needsSpecialtyChoice)
        #expect(!glaze.isHouseMade)
        let detail = try #require(glaze.specialtyDetail)
        #expect(detail.choiceType == nil)
        #expect(detail.suggestedOptions.map(\.id) == ["sweet-soy-glaze.store", "sweet-soy-glaze.batch"])
        #expect(detail.suggestedOptions.first?.isDefault == true)
        #expect(detail.suggestedOptions.last?.type == .houseMadeBatch)
        #expect(detail.text == "Specialty ingredient: choose a store alternative or a house-made batch")

        let fry = try item(named: "Fry Seasoning", in: list)
        #expect(fry.isHouseMade)
        #expect(!fry.needsSpecialtyChoice)
        #expect(fry.specialtyDetail?.choiceType == .houseMadeBatch)
        #expect(fry.status == .inPantry)
    }

    @Test func listsFromBeforeSpecialtiesStillDecode() throws {
        let list = try JSONCoding.makeDecoder().decode(GroceryList.self, from: PlanFixtures.groceryList)

        #expect(!list.specialtiesApplied)
        #expect(list.batches.isEmpty)
        #expect(list.allItems.allSatisfy { !$0.specialty && $0.specialtyDetail == nil && $0.via.isEmpty })
    }

    @Test func unreadableSpecialtyFieldsDontHideTheList() throws {
        let json = Data(
            #"""
            {"week":"2026-W38","status":"draft","pantryApplied":false,"specialtiesApplied":true,
             "categories":[{"category":"spices","items":[
               {"ingredientKey":"i-salt","name":"Salt","amounts":[],"quantityText":"","unquantified":true,
                "status":"toBuy","recipes":[],"specialty":true,"specialtyDetail":{"id":7},"via":[{"kind":1}]}]}],
             "batches":[{"specialtyId":5}],"skipped":[]}
            """#.utf8)

        let list = try JSONCoding.makeDecoder().decode(GroceryList.self, from: json)

        #expect(list.batches.isEmpty)
        let salt = try #require(list.allItems.first)
        #expect(salt.specialty)
        #expect(salt.specialtyDetail == nil)
        #expect(salt.via.isEmpty)
        #expect(!salt.needsSpecialtyChoice)
    }
}

struct GroceryListLayoutTests {
    @Test func groupsIngredientsBoughtOnlyForABatch() throws {
        let layout = GroceryListLayout(try specialtyList())

        #expect(layout.toMake.map(\.id) == ["southwest-spice-blend"])
        #expect(layout.toMake.first?.ingredients.map(\.name) == ["Ground Cumin"])
        #expect(layout.alreadyMade.map(\.id) == ["fry-seasoning"])
        #expect(layout.categories.map(\.category) == ["produce", "spices", "condiments"])
        #expect(layout.categories[1].items.map(\.name) == ["Chili Powder", "Smoked Paprika"])
        #expect(layout.categories[2].items.map(\.name) == ["Tomato Paste", "Sweet Soy Glaze", "Fry Seasoning"])
        #expect(layout.needsChoice.map(\.name) == ["Sweet Soy Glaze"])
    }

    @Test func sharedIngredientsStayInTheirAisle() throws {
        let list = try specialtyList()

        #expect(
            GroceryListLayout.batchSpecialtyID(for: try item(named: "Ground Cumin", in: list))
                == "southwest-spice-blend")
        // A recipe also uses it directly.
        #expect(GroceryListLayout.batchSpecialtyID(for: try item(named: "Chili Powder", in: list)) == nil)
        // It's also for a store alternative.
        #expect(GroceryListLayout.batchSpecialtyID(for: try item(named: "Smoked Paprika", in: list)) == nil)
        #expect(GroceryListLayout.batchSpecialtyID(for: try item(named: "Tomato Paste", in: list)) == nil)
        #expect(GroceryListLayout.batchSpecialtyID(for: try item(named: "Yellow Onion", in: list)) == nil)
    }

    @Test func aCategoryLeftEmptyIsOmittedAndOnlyBatchesToMakeGroup() throws {
        let recipe = GroceryRecipe(id: "recipe-1", name: "Chili Bowls")
        let via = GroceryVia(
            kind: .houseMadeBatch, specialtyID: "fajita-spice-blend", specialtyKey: "fajita spice blend",
            specialtyName: "Fajita Spice Blend", optionID: "fajita-spice-blend.batch", optionName: "Fajita blend",
            batchYield: nil, batches: 1, recipes: [recipe], text: "to make Fajita Spice Blend")
        let paprika = GroceryItem(
            ingredientKey: "i-paprika", name: "Paprika", amounts: [], quantityText: "", unquantified: true,
            status: .toBuy, recipes: [recipe], via: [via])
        let amount = GroceryAmount(quantity: "8", quantityValue: 8, unit: "tbsp", text: "8 tbsp")
        let batch = GroceryBatch(
            specialtyID: "fajita-spice-blend", specialtyKey: "fajita spice blend", specialtyName: "Fajita Spice Blend",
            optionID: "fajita-spice-blend.batch", optionName: "Fajita blend", batchYield: amount, status: .make,
            reason: .low, batches: 1, pantryItemID: "item-fajita", remaining: nil, needed: nil, recipes: [recipe],
            text: "Make a batch (makes about 8 tbsp)")
        var list = GroceryList(
            week: "2026-W38", status: .draft, pantryApplied: true,
            categories: [GroceryCategory(category: "spices", items: [paprika])], skipped: [],
            specialtiesApplied: true, batches: [batch])

        let grouped = GroceryListLayout(list)
        #expect(grouped.categories.isEmpty)
        #expect(grouped.toMake.first?.ingredients == [paprika])
        #expect(SpecialtyFormat.batchDetail(batch) == "The pantry batch is running low.")

        list.batches = []
        let ungrouped = GroceryListLayout(list)
        #expect(ungrouped.toMake.isEmpty)
        #expect(ungrouped.categories.first?.items == [paprika])
    }
}

/// Choosing, "Made It", and the purchase card on a list with specialty ingredients.
struct GroceryListSpecialtyActionTests {
    private struct Harness {
        let model: GroceryListModel
        let planServer: FakePlanServer
        let specialtyServer: FakeSpecialtyServer
        let specialties: SpecialtyStore
        let pantry: PantryStore

        var groceryLoads: Int {
            planServer.log.filter { $0.hasSuffix("/grocery") }.count
        }
    }

    private func makeHarness(canEdit: Bool = true) async throws -> Harness {
        let planServer = FakePlanServer(.init(groceryList: SpecialtyFixtures.groceryList))
        let specialtyServer = FakeSpecialtyServer()
        let pantryServer = FakePantryServer(.init(pantries: ["household-1": FakePantryServer.sampleItems]))
        let transport = StubTransport { request in
            let path = request.url?.path() ?? ""
            if path.contains("/specialty-ingredients") { return specialtyServer.handle(request) }
            if path.contains("/pantry") { return pantryServer.handle(request) }
            return planServer.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let pantry = PantryStore(session: session, api: PantryAPI(client: client), ingredientsAPI: nil)
        let specialties = SpecialtyStore(session: session, api: SpecialtiesAPI(client: client))
        specialties.onBatchRecorded = { [pantry] item, householdID in
            pantry.applyChangedItem(item, householdID: householdID)
        }
        let model = GroceryListModel(
            householdID: "household-1", week: try #require(ISOWeek("2026-W38")), session: session,
            api: PlansAPI(client: client), checks: InMemoryGroceryChecks(), purchases: pantry,
            specialties: specialties, canAddToPantry: canEdit)
        await model.load()
        return Harness(
            model: model, planServer: planServer, specialtyServer: specialtyServer, specialties: specialties,
            pantry: pantry)
    }

    @Test func houseMadeItemsDontAskToAddToThePantry() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let list = try #require(model.list)

        let fry = try item(named: "Fry Seasoning", in: list)
        model.toggle(fry)
        #expect(model.isChecked(fry))
        #expect(model.purchasePrompt == nil)

        let cumin = try item(named: "Ground Cumin", in: list)
        model.toggle(cumin)
        #expect(model.purchasePrompt?.item == cumin)
    }

    @Test func choosingFromTheListSavesTheChoiceAndReloadsOnce() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let glaze = try #require(model.layout?.needsChoice.first?.specialtyDetail)
        let loads = harness.groceryLoads

        await model.choose(optionID: "sweet-soy-glaze.store", for: glaze)

        #expect(model.specialtyFailure == nil)
        #expect(harness.specialtyServer.choice(for: "sweet-soy-glaze") == "sweet-soy-glaze.store")
        #expect(harness.groceryLoads == loads + 1)
        #expect(harness.specialties.revision == 1)
        #expect(!model.isChangingSpecialty(withID: "sweet-soy-glaze"))

        // The store's revision change doesn't reload the list a second time.
        await model.specialtiesDidChange()
        #expect(harness.groceryLoads == loads + 1)

        await model.keepAsIs(glaze)
        #expect(harness.specialtyServer.choice(for: "sweet-soy-glaze") == "as_is")
    }

    @Test func aChangeOnTheSetupScreenReloadsAnOpenList() async throws {
        let harness = try await makeHarness()
        let loads = harness.groceryLoads

        try await harness.specialties.choose(
            optionID: "sweet-soy-glaze.batch", forSpecialtyWithID: "sweet-soy-glaze", householdID: "household-1")
        await harness.model.specialtiesDidChange()
        await harness.model.specialtiesDidChange()

        #expect(harness.groceryLoads == loads + 1)
    }

    @Test func madeItRecordsTheBatchReloadsAndRestocksThePantry() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        await harness.pantry.activate(householdID: "household-1")
        let batch = try #require(model.layout?.toMake.first?.batch)
        let loads = harness.groceryLoads

        await model.recordBatch(batch)

        #expect(model.specialtyFailure == nil)
        let recorded = try #require(harness.specialtyServer.batches.first)
        #expect(harness.specialtyServer.batches.count == 1)
        #expect(recorded.optionID == "southwest-spice-blend.batch")
        #expect(recorded.batches == 1)
        #expect(model.lastRecordedBatch?.name == "Southwest Spice Blend")
        #expect(model.lastRecordedBatch?.id == recorded.clientPurchaseID)
        #expect(harness.groceryLoads == loads + 1)
        #expect(harness.pantry.items.contains { $0.id == "item-batch-southwest-spice-blend" })

        model.dismissRecordedBatch(id: "another")
        #expect(model.lastRecordedBatch != nil)
        model.dismissRecordedBatch(id: try #require(recorded.clientPurchaseID))
        #expect(model.lastRecordedBatch == nil)
    }

    @Test func aLostBatchResponseIsRetriedWithTheSameIDAndRecordedOnce() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let batch = try #require(model.layout?.toMake.first?.batch)
        harness.specialtyServer.loseNextBatchResponse()

        await model.recordBatch(batch)

        let failure = try #require(model.specialtyFailure)
        #expect(!failure.isForbidden)
        #expect(failure.title == "Couldn't Record Southwest Spice Blend")
        #expect(model.lastRecordedBatch == nil)

        // An alert clears the failure before its Try Again button runs.
        model.dismissSpecialtyFailure()
        await model.retrySpecialtyAction(failure)

        #expect(model.specialtyFailure == nil)
        #expect(harness.specialtyServer.batches.count == 1)
        let ids = harness.specialtyServer.bodies(endingWith: "/batches").compactMap {
            $0["clientPurchaseId"] as? String
        }
        #expect(ids.count == 2)
        #expect(Set(ids).count == 1)
        #expect(model.lastRecordedBatch?.id == ids.first)

        // Made It again after it was recorded is a new batch.
        await model.recordBatch(batch)
        #expect(harness.specialtyServer.batches.count == 2)
    }

    @Test func membersWithoutPantryEditCantChangeSpecialties() async throws {
        let harness = try await makeHarness(canEdit: false)
        let model = harness.model
        let glaze = try #require(model.layout?.needsChoice.first?.specialtyDetail)
        let batch = try #require(model.layout?.toMake.first?.batch)

        #expect(!model.canChangeSpecialties)
        await model.choose(optionID: "sweet-soy-glaze.store", for: glaze)
        await model.recordBatch(batch)

        #expect(harness.specialtyServer.log.isEmpty)
    }

    @Test func aForbiddenChangeStopsOfferingSpecialtyActions() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let glaze = try #require(model.layout?.needsChoice.first?.specialtyDetail)
        harness.specialtyServer.forbidChanges()

        await model.choose(optionID: "sweet-soy-glaze.store", for: glaze)

        let failure = try #require(model.specialtyFailure)
        #expect(failure.isForbidden)
        #expect(failure.title == "Couldn't Choose an Option for Sweet Soy Glaze")
        #expect(!model.canChangeSpecialties)
        #expect(!model.canAddToPantry)
        let requests = harness.specialtyServer.log.count
        await model.retrySpecialtyAction(failure)
        #expect(harness.specialtyServer.log.count == requests)
    }

    @Test func sharedTextIncludesBatchesAndProvenance() async throws {
        let list = try specialtyList()
        let week = try #require(ISOWeek("2026-W38"))

        let text = GroceryListText.make(list, week: week, checked: [], locale: Locale(identifier: "en_US"))

        #expect(
            text.contains(
                "Make This Week\n- Southwest Spice Blend: Make a batch (makes about 12 tbsp). Needed for Chili Bowls"))
        #expect(text.contains("- [ ] Tomato Paste, 2 ⅓ tbsp\n    for Tex-Mex Paste in Smoky Pork Tacos"))
        #expect(text.contains("- [ ] Fry Seasoning, 1 tbsp (in pantry, house-made)"))
        #expect(!text.contains("Fry Seasoning: In pantry"))
    }
}
