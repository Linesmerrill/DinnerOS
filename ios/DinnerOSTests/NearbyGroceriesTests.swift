import Foundation
import Testing

@testable import DinnerOS

/// Request a Store's nearby list: closest first, grouped by chain the way the API files
/// requests, and a picked store sent with its location.
struct NearbyGroceriesTests {
    private let catalog = [
        ShoppingCatalogItem(key: "frys", name: "Fry's", kind: .grocer, status: .available, aliases: ["frys food"]),
        ShoppingCatalogItem(key: "giant", name: "Giant", kind: .grocer, status: .unsupported),
        ShoppingCatalogItem(key: "giant-eagle", name: "Giant Eagle", kind: .grocer, status: .unsupported),
        ShoppingCatalogItem(
            key: "sprouts", name: "Sprouts Farmers Market", kind: .grocer, status: .unsupported, aliases: ["sprouts"]),
    ]

    @Test func closestFirstWithUnplacedStoresLastAndNoDuplicates() {
        let stores = [
            NearbyGroceryStore(name: "B Market", address: "2 Test St", distance: 3000),
            NearbyGroceryStore(name: "Unplaced", address: "", distance: nil),
            NearbyGroceryStore(name: "A Market", address: "1 Test St", distance: 800),
            NearbyGroceryStore(name: "A Market", address: "1 Test St", distance: 800),
        ]
        #expect(NearbyGroceries.sorted(stores).map(\.name) == ["A Market", "B Market", "Unplaced"])
    }

    @Test func aStoreBelongsToTheLongestMatchingChain() {
        #expect(NearbyGroceries.catalogItem(for: "Fry's Food Store #622", in: catalog)?.key == "frys")
        #expect(NearbyGroceries.catalogItem(for: "Giant Eagle Market District", in: catalog)?.key == "giant-eagle")
        #expect(NearbyGroceries.catalogItem(for: "Giant Food", in: catalog)?.key == "giant")
        #expect(NearbyGroceries.catalogItem(for: "Sprouts Farmers Market - Tempe", in: catalog)?.key == "sprouts")
        #expect(NearbyGroceries.catalogItem(for: "Giantess Foods", in: catalog) == nil)
        #expect(NearbyGroceries.catalogItem(for: "Corner Market", in: catalog) == nil)
    }

    @Test func storesTheCatalogLacksAreFiledByChainName() {
        #expect(NearbyGroceries.requestKey(for: "Corner Market #3") == "corner-market")
        #expect(NearbyGroceries.requestKey(for: "Corner Market - Downtown") == "corner-market")
        #expect(NearbyGroceries.requestKey(for: "Ma & Pa's Grocery") == "ma-and-pas-grocery")
    }

    @Test func chainNamesMatchTheAPI() {
        let cases = [
            "Fry's Food Store #622": "Fry's Food Store",
            "Bashas' - Chandler (Ray Rd)": "Bashas'",
            "Safeway Store 1234": "Safeway",
            "Sprouts Farmers Market No. 412": "Sprouts Farmers Market",
            "Food City, Mesa": "Food City",
            "Trader Joe's # 59": "Trader Joe's",
            "99 Ranch Market": "99 Ranch Market",
            "Store 7": "Store 7",
        ]
        for (name, chain) in cases {
            #expect(StoreNameText.chain(name) == chain, "\(name)")
        }
        #expect(StoreNameText.normalized("Stop & Shop") == "stop and shop")
        #expect(StoreNameText.normalized("H-E-B") == "h e b")
        #expect(StoreNameText.normalized("Fry’s") == "frys")
    }

    @Test func distanceIsInMilesOrKilometersNeverFeet() {
        let us = Locale(identifier: "en_US")
        #expect(NearbyGroceryStore(name: "A", address: "", distance: 8368).distanceText(locale: us) == "5.2 mi")
        #expect(NearbyGroceryStore(name: "A", address: "", distance: 136).distanceText(locale: us) == "0.1 mi")
        #expect(
            NearbyGroceryStore(name: "A", address: "", distance: 8368).distanceText(locale: Locale(identifier: "en_CA"))
                == "8.4 km")
        #expect(NearbyGroceryStore(name: "A", address: "", distance: nil).distanceText == nil)
    }

    @Test func aNearbyPickSendsTheChainSourceAndLocation() throws {
        let store = NearbyGroceryStore(name: "Corner Market #3", address: "1 Test St, Phoenix", distance: 100)
        let body =
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(CreateShoppingStoreRequest.nearby(store)))
            as? [String: Any]
        #expect(body?["name"] as? String == "Corner Market #3")
        #expect(body?["source"] as? String == "nearby")
        let place = body?["place"] as? [String: Any]
        #expect(place?["name"] as? String == "Corner Market #3" && place?["address"] as? String == "1 Test St, Phoenix")
        #expect(body?["key"] == nil)

        let typed =
            try JSONSerialization.jsonObject(
                with: JSONEncoder().encode(CreateShoppingStoreRequest.name("Corner Market")))
            as? [String: Any]
        #expect(typed?["source"] as? String == "typed" && typed?["place"] == nil)
    }

    @Test func aRequestCarriesItsPlace() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let json = #"""
            {"id":"r1","key":"sprouts","name":"Sprouts Farmers Market","status":"unsupported","note":null,"custom":false,
             "place":{"name":"Sprouts Farmers Market #412","address":"1 Test St"},
             "requestedBy":"u1","requestedAt":"2026-01-01T00:00:00Z"}
            """#
        let request = try decoder.decode(ShoppingStoreRequest.self, from: Data(json.utf8))
        #expect(request.place?.address == "1 Test St")
    }
}
