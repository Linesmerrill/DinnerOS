import Foundation
import Testing

@testable import DinnerOS

/// The freezer as the app reads it: a pantry item's storage and thaw estimate, the grocery
/// status that keeps a frozen line visible, and the bulk-pack responses behind the Shop
/// sheet.
struct FreezerTests {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONCoding.makeDecoder().decode(type, from: Data(json.utf8))
    }

    // MARK: - Pantry items

    @Test func aPantryItemWithoutStorageReadsAsAShelfItem() throws {
        // Every item written before the freezer existed, and every response from an older
        // API, has to keep working.
        let item = try decode(PantryItem.self, PantryFixtures.oliveOilJSON)
        #expect(item.storage == .pantry)
        #expect(item.frozen == nil)
        #expect(!item.isFrozen)
    }

    @Test func aFrozenItemCarriesItsPortionsAndThawEstimate() throws {
        let item = try decode(PantryItem.self, Self.frozenPorkJSON)
        #expect(item.isFrozen)
        let frozen = try #require(item.frozen)
        #expect(frozen.portions == 3)
        #expect(frozen.frozenOn == "2026-09-15")
        #expect(frozen.thaw.hours == 6)
        #expect(frozen.thaw.measured)
        #expect(frozen.thaw.portionOunces == 18)
    }

    @Test func thawTextNamesThePortionsAndTheEstimate() throws {
        let one = PantryFrozen(
            frozenOn: "2026-09-15", portions: 1,
            thaw: PantryThaw(hours: 5, measured: true, portionOunces: 16, summary: "about 5 hours"))
        #expect(PantryFormat.thawText(one) == "About 5 hours to thaw")

        let split = PantryFrozen(
            frozenOn: "2026-09-15", portions: 4,
            thaw: PantryThaw(hours: 5, measured: true, portionOunces: 16, summary: "about 5 hours"))
        #expect(split.portions == 4)
        #expect(PantryFormat.thawText(split).contains("4 portions"))
        #expect(PantryFormat.thawText(split).contains("about 5 hours to thaw"))
    }

    // MARK: - The grocery line

    @Test func aFrozenGroceryLineStaysOnTheListButIsNotBought() {
        // The whole point: unlike `inPantry`, the line is still shown, so somebody takes it
        // out of the freezer. It just isn't something to buy.
        #expect(!GroceryItemStatus.fromFreezer.needsBuying)
        #expect(!GroceryItemStatus.inPantry.needsBuying)
        #expect(GroceryItemStatus.toBuy.needsBuying)
        #expect(GroceryItemStatus.pantryHint.needsBuying)
    }

    @Test func anUnknownStatusIsStillBought() {
        // A status from a newer server is shown and still exported: leaving something off
        // the shopping list is the damaging mistake, not putting something on it twice.
        let future = GroceryItemStatus(rawValue: "somethingNew")
        #expect(future.needsBuying)
    }

    // MARK: - Thaw reminders

    @Test func thawDueDecodesTodaysItems() throws {
        let due = try decode(ThawDue.self, Self.thawDueJSON)
        #expect(due.date == "2026-09-15")
        #expect(due.reminderHour == 6)
        #expect(due.items.count == 1)
        let item = try #require(due.items.first)
        #expect(item.name == "Pork Loin")
        #expect(item.recipes == ["Sheet-Pan Pork"])
        #expect(item.hours == 5)
        #expect(!item.overnight)
        #expect(item.moveBy == "13:00")
    }

    @Test func moveByTextReadsAsAClock() {
        let item = ThawItem(
            itemID: "1", name: "Pork Loin", recipes: [], hours: 5, measured: true,
            moveBy: "13:00", overnight: false, summary: "")
        // Locale decides the exact shape; what matters is that it stops being 24-hour.
        #expect(item.moveByText != "13:00")
        let unreadable = ThawItem(
            itemID: "1", name: "Pork Loin", recipes: [], hours: 5, measured: true,
            moveBy: "nonsense", overnight: false, summary: "")
        #expect(unreadable.moveByText == "nonsense")
    }

    // MARK: - Bulk packs

    @Test func bulkPacksDecodeWithTheirSuggestions() throws {
        let list = try decode(ShoppingBulkPackList.self, Self.bulkPacksJSON)
        #expect(list.handoffID == "66e5a1f2c3b4a5d6e7f89001")
        #expect(list.packs.count == 2)
        let pork = try #require(list.packs.first)
        #expect(pork.surplus == "54")
        #expect(pork.surplusPercent == 84)
        #expect(pork.freezable)
        #expect(!pork.frozen)
        #expect(pork.suggestions.map(\.recipeName) == ["Pork Fried Rice"])
        #expect(pork.suggestions.first?.day == "sat")
    }

    @Test func packsAlreadySealedAreNotAskedAboutAgain() throws {
        let list = try decode(ShoppingBulkPackList.self, Self.bulkPacksJSON)
        #expect(list.packs.count == 2)
        // The second pack's remainder is already in the freezer, so the sheet has nothing
        // left to offer for it.
        #expect(list.open.map(\.name) == ["Pork Loin"])
    }

    @Test func anExclusionCanSayTheFreezerCoversIt() throws {
        let line = try decode(ShoppingExcludedLine.self, Self.excludedFrozenJSON)
        #expect(line.reason == .inFreezer)
        #expect(line.groceryStatus == .fromFreezer)
        #expect(line.text == "Grab from the freezer")
    }

    // MARK: - Fixtures

    private static let frozenPorkJSON = #"""
        {"id":"66e5a1f2c3b4a5d6e7f84001","householdId":"66e5a1f2c3b4a5d6e7f80a01","ingredientId":null,
         "key":"pork loin","displayName":"Pork Loin","category":"meat-seafood",
         "quantity":"54","quantityValue":54,"unit":"oz","status":"in_stock","isStaple":false,
         "expiresOn":null,"note":"","storage":"freezer",
         "frozen":{"frozenOn":"2026-09-15","portions":3,
                   "thaw":{"hours":6,"measured":true,"portionOunces":18,"summary":"about 6 hours"}},
         "statusSource":"person","lowThresholdPercent":null,"unitSize":null,"estimate":null,
         "updatedBy":"66e5a1f2c3b4a5d6e7f80c01","createdAt":"2026-09-15T18:30:00.000Z",
         "updatedAt":"2026-09-15T18:30:00.000Z"}
        """#

    private static let thawDueJSON = #"""
        {"date":"2026-09-15","reminderHour":6,
         "items":[{"itemId":"66e5a1f2c3b4a5d6e7f84001","name":"Pork Loin","recipes":["Sheet-Pan Pork"],
                   "hours":5,"measured":true,"moveBy":"13:00","overnight":false,
                   "summary":"Pork Loin is for Sheet-Pan Pork tonight. This usually takes about 5 hours in the fridge."}]}
        """#

    private static let bulkPacksJSON = #"""
        {"week":"2026-W38","provider":"walmart","handoffId":"66e5a1f2c3b4a5d6e7f89001",
         "packs":[
           {"lineId":"l1","ingredientKey":"name:pork loin","ingredientId":null,"name":"Pork Loin",
            "category":"meat-seafood","productId":"100000001","productName":"Valley Ridge Pork Loin",
            "packages":1,"unit":"oz","bought":"64","boughtValue":64,"needed":"10","neededValue":10,
            "surplus":"54","surplusValue":54,"surplusPercent":84,
            "surplusText":"This week uses 10 oz of 64 oz","freezable":true,"frozen":false,
            "suggestions":[{"recipeId":"66e5a1f2c3b4a5d6e7f81006","recipeName":"Pork Fried Rice",
                            "imageUrl":null,"day":"sat","servings":2,"cookMinutes":25,
                            "reasons":["Uses what you already bought"]}]},
           {"lineId":"l2","ingredientKey":"name:ground beef","ingredientId":null,"name":"Ground Beef",
            "category":"meat-seafood","productId":"100000002","productName":"Valley Ridge Ground Beef",
            "packages":1,"unit":"oz","bought":"80","boughtValue":80,"needed":"20","neededValue":20,
            "surplus":"60","surplusValue":60,"surplusPercent":75,
            "surplusText":"This week uses 20 oz of 80 oz","freezable":true,"frozen":true,
            "suggestions":[]}]}
        """#

    private static let excludedFrozenJSON = #"""
        {"ingredientKey":"name:pork loin","ingredientId":null,"name":"Pork Loin","category":"meat-seafood",
         "amounts":[],"quantityText":"10 oz","unquantified":false,"groceryStatus":"fromFreezer",
         "recipes":[],"reason":"in_freezer","text":"Grab from the freezer",
         "searchTerms":{"query":"pork loin","qualifiers":[],"avoid":[],"why":""}}
        """#
}
