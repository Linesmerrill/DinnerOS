import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the API's global-catalog responses. No real recipes.
nonisolated enum CatalogFixtures {
    static func summary(
        id: String, name: String, inLibrary: Bool = false, libraryRecipeID: String? = nil, reasons: [String] = []
    ) -> String {
        let library = libraryRecipeID.map { #","libraryRecipeId":"\#($0)""# } ?? ""
        let reasonList = reasons.map { #""\#($0)""# }.joined(separator: ",")
        return #"""
            {"id":"\#(id)","catalogKey":"hellofresh:\#(id)","source":"hellofresh","name":"\#(name)",
             "headline":"with Test Sauce","imageUrl":"https://img.example.test/\#(id).jpg",
             "isAddon":false,"totalMinutes":30,"cookMinutes":35,"timeBand":"medium","calories":600,
             "proteinGrams":30,"cuisines":["Thai"],"tags":["Quick"],"inLibrary":\#(inLibrary)\#(library),
             "reasons":[\#(reasonList)]}
            """#
    }

    static func page(_ items: [String], nextCursor: String? = nil, total: Int? = nil) -> Data {
        let cursor = nextCursor.map { #","nextCursor":"\#($0)""# } ?? ""
        let count = total ?? items.count
        return Data(#"{"items":[\#(items.joined(separator: ","))]\#(cursor),"total":\#(count)}"#.utf8)
    }

    /// A catalog entry with every optional field left out, as an older or
    /// thinner server could send it.
    static let minimalPage = Data(#"{"items":[{"id":"c-min","name":"Plain Toast"}],"total":1}"#.utf8)

    static let detail = Data(
        #"""
        {"id":"catalog-1","catalogKey":"hellofresh:catalog-1","source":"hellofresh","name":"Thai Green Curry",
         "headline":"with jasmine rice","description":"A quick curry.","imageUrl":"https://img.example.test/c.jpg",
         "isAddon":false,"servings":[2],"prepMinutes":10,"totalMinutes":35,"cookMinutes":35,
         "cuisines":["Thai"],"tags":["Quick"],"utensils":[],"allergens":[],"nutritionPerServing":[],
         "ingredients":[{"ingredientId":"66e5a1f2c3b4a5d6e7f8ff01","name":"coconut milk","pantryStaple":false,
                         "amounts":[{"servings":2,"quantity":"1","quantityValue":1,"unit":"can",
                                     "sourceUnit":"can","rawText":"1 can"}]}],
         "steps":[{"index":1,"text":"Simmer everything."}],
         "inLibrary":false,"timeBand":"medium","calories":null,"proteinGrams":null,"reasons":[]}
        """#.utf8)

    /// The top of Shared Recipes: two rows, one of them paid, and the chips.
    static func home(_ firstRow: [String]? = nil) -> Data {
        let row = (firstRow ?? [summary(id: "c1", name: "Alpha Bowl"), summary(id: "c9", name: "Nine Bowl")])
            .joined(separator: ",")
        return Data(
            #"""
            {"sections":[
              {"id":"chefs-picks","title":"Chef's Picks This Week","subtitle":"Our favorites right now",
               "type":"editorial","items":[\#(row)],"hasMore":true},
              {"id":"pasta-week","title":"Pasta Week","type":"sponsored","sponsorLabel":"Sponsored by Test Pasta",
               "items":[\#(summary(id: "c1", name: "Alpha Bowl"))],"hasMore":false}],
             "filters":[
              {"id":"cuisine","title":"Cuisine","param":"cuisine","options":[{"value":"thai","label":"Thai"}]},
              {"id":"time","title":"Time","param":"maxMinutes","options":[{"value":"30","label":"30 min or less"}]},
              {"id":"servings","title":"Servings","param":"minServings","options":[{"value":"6","label":"6+"}]}],
             "browseTitle":"Browse All","searchPrompt":"Search shared recipes","seeAllTitle":"See All",
             "emptyTitle":"Nothing New Right Now","emptyText":"Check back later.",
             "noMatchTitle":"No Recipes Match","noMatchText":"Try another word or fewer filters."}
            """#.utf8)
    }

    /// One row in full, for See All.
    static func sectionPage(nextCursor: String? = nil) -> Data {
        let cursor = nextCursor.map { #","nextCursor":"\#($0)""# } ?? ""
        return Data(
            #"{"id":"quick","title":"Quick","type":"algorithmic","items":[\#(summary(id: "c5", name: "Fast Bowl"))],"hasMore":false,"total":1\#(cursor)}"#
                .utf8)
    }

    static func addResult(recipeID: String, created: Bool) -> Data {
        Data(#"{"recipeId":"\#(recipeID)","created":\#(created)}"#.utf8)
    }

    /// A parsed draft, as `POST .../recipes/parse` returns one.
    static let draft = Data(
        #"""
        {"name":"Weeknight Chili","headline":null,"description":"Cosy.","sourceUrl":null,"imageUrl":null,
         "servings":4,"prepMinutes":10,"totalMinutes":45,"cuisines":[],"tags":[],
         "ingredients":[{"name":"kidney beans","quantity":"2","unit":"can","rawText":"2 cans kidney beans",
                         "pantryStaple":false}],
         "steps":["Simmer everything."],"warnings":["Check the amounts."],"fromUrl":false}
        """#.utf8)
}

/// A stateful stand-in for the catalog endpoints. It records what it was asked
/// for, so tests can assert that discovery and search hit different paths.
nonisolated final class FakeCatalogServer: Sendable {
    struct State: Sendable {
        var discoverPages: [Data]
        var searchPages: [Data]
        var detail: Data
        var addResult: Data
        var home: Data = CatalogFixtures.home()
        var failNext: Bool = false
        var failHome: Bool = false
    }

    private let state: Mutex<State>
    private let calls = Mutex<[String]>([])

    init(_ state: State) {
        self.state = Mutex(state)
    }

    /// Paths requested, in order, each as "METHOD path?query".
    var requestedPaths: [String] { calls.withLock { $0 } }

    /// Fails the next list, detail, or add request. The rows at the top have their own switch.
    func failNext() {
        state.withLock { $0.failNext = true }
    }

    /// Fails every request for the rows at the top until turned off.
    func failHome(_ fail: Bool = true) {
        state.withLock { $0.failHome = fail }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url, let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            return (400, Data(#"{"error":{"code":"invalid_request","message":"bad url"}}"#.utf8))
        }
        let method = request.httpMethod ?? "GET"
        let query = components.query.map { "?" + $0 } ?? ""
        calls.withLock { $0.append(method + " " + components.path + query) }
        return state.withLock { state in
            if components.path.hasSuffix("/discover/home") {
                return state.failHome
                    ? (500, Data(#"{"error":{"code":"internal","message":"nope"}}"#.utf8)) : (200, state.home)
            }
            if components.path.contains("/discover/sections/") {
                return (200, CatalogFixtures.sectionPage())
            }
            if state.failNext {
                state.failNext = false
                return (500, Data(#"{"error":{"code":"internal","message":"nope"}}"#.utf8))
            }
            let cursor = components.queryItems?.first { $0.name == "cursor" }?.value
            let index = Int(cursor ?? "0") ?? 0
            switch true {
            case components.path.hasSuffix("/discover"):
                return (200, state.discoverPages.indices.contains(index) ? state.discoverPages[index] : Data("{}".utf8))
            case components.path.hasSuffix("/add"):
                return (201, state.addResult)
            case components.path.contains("/catalog/recipes/"):
                return (200, state.detail)
            case components.path.hasSuffix("/catalog/recipes"):
                return (200, state.searchPages.indices.contains(index) ? state.searchPages[index] : Data("{}".utf8))
            default:
                return (404, Data(#"{"error":{"code":"not_found","message":"no"}}"#.utf8))
            }
        }
    }
}
