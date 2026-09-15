import Foundation
import Synchronization

@testable import DinnerOS

/// Synthetic JSON shaped like the Menu API's responses. Made-up recipes and example image URLs only.
nonisolated enum MenuFixtures {
    static let menuFields = #","calories":690,"proteinGrams":36,"timeBand":"quick""#

    static func summary(id: String, name: String, extra: String = menuFields) -> String {
        #"""
        {"id":"\#(id)","name":"\#(name)","headline":"with Test Sauce",
         "imageUrl":"https://img.example.test/f_auto,q_auto,w_1200/\#(id).jpg","totalMinutes":30,"cookMinutes":30,
         "timesOrdered":0,"isAddon":false,"tags":["Quick"],"householdRating":{"average":null,"count":0},
         "myRating":null\#(extra)}
        """#
    }

    static func card(
        id: String, name: String, badges: String = "[]", inPlan: Bool = false, entryIDs: [String] = [],
        reason: String? = nil
    ) -> String {
        let reasonJSON = reason.map { #""\#($0)""# } ?? "null"
        let ids = entryIDs.map { #""\#($0)""# }.joined(separator: ",")
        return #"""
            {"recipe":\#(summary(id: id, name: name)),"badges":\#(badges),"reason":\#(reasonJSON),
             "inPlan":\#(inPlan),"planEntryIds":[\#(ids)]}
            """#
    }

    static func section(
        id: String, kind: String = "carousel", title: String, subtitle: String? = nil, cards: [String],
        moreQuery: String = "null"
    ) -> String {
        let subtitleJSON = subtitle.map { #""\#($0)""# } ?? "null"
        return #"""
            {"id":"\#(id)","kind":"\#(kind)","title":"\#(title)","subtitle":\#(subtitleJSON),
             "items":[\#(cards.joined(separator: ","))],"moreQuery":\#(moreQuery)}
            """#
    }

    static func menu(
        week: String = "2026-W38", timing: String = "current", plan: String = "null", proposal: String = "null",
        sections: [String]
    ) -> Data {
        let (start, end) = PlanFixtures.dates(for: week)
        return Data(
            #"""
            {"week":"\#(week)","weekStart":"\#(start)","weekEnd":"\#(end)","timing":"\#(timing)",
             "plan":\#(plan),"proposal":\#(proposal),"sections":[\#(sections.joined(separator: ","))]}
            """#.utf8)
    }

    static func page(_ cards: [String], nextCursor: String?) -> Data {
        let cursor = nextCursor.map { #","nextCursor":"\#($0)""# } ?? ""
        return Data(#"{"items":[\#(cards.joined(separator: ","))]\#(cursor)}"#.utf8)
    }

    static let filters = Data(
        #"""
        {"proteins":[{"value":"chicken","label":"Chicken","count":42},{"value":"pork","label":"Pork","count":12}],
         "cuisines":[{"value":"mexican","label":"Mexican","count":8}],
         "tags":[{"value":"kid friendly","label":"Kid Friendly","count":5}],
         "maxMinutes":[15,20,30,45],
         "sorts":[{"value":"recommended","label":"Recommended"},{"value":"quick","label":"Quickest"}]}
        """#.utf8)

    static func weekSummary(
        _ week: String, timing: String, planned: Int = 0, cooked: Int = 0, ordered: Int = 0, status: String = "none"
    ) -> String {
        let (start, end) = PlanFixtures.dates(for: week)
        return #"""
            {"week":"\#(week)","weekStart":"\#(start)","weekEnd":"\#(end)","timing":"\#(timing)",
             "plannedCount":\#(planned),"cookedCount":\#(cooked),"orderedCount":\#(ordered),"status":"\#(status)"}
            """#
    }
}

/// An in-memory stand-in for the Menu endpoints: a menu per week, cursor paging over a recipe list
/// with search, week summaries down to `earliestWeek`, and injectable failures.
nonisolated final class FakeMenuServer: Sendable {
    struct State: Sendable {
        var currentWeek = "2026-W38"
        var earliestWeek: String? = "2026-W20"
        var recipes: [(id: String, name: String)] = [
            ("recipe-1", "Test Kitchen Tacos"), ("recipe-2", "Sample Soup"), ("recipe-3", "Placeholder Pasta"),
            ("recipe-4", "Example Stir Fry"), ("recipe-5", "Synthetic Salad"),
        ]
        var failMenu = false
        var failWeeks = false
        var failFilters = false
        /// Method, path (without `/api/v1/`), and query of every request, in order.
        var log: [String] = []
        var recipeQueries: [[String: String]] = []
        var weekQueries: [[String: String]] = []
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var log: [String] { state.withLock { $0.log } }
    var recipeQueries: [[String: String]] { state.withLock { $0.recipeQueries } }
    var weekQueries: [[String: String]] { state.withLock { $0.weekQueries } }

    func update(_ change: @Sendable (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let query = Dictionary(
            (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []).map {
                ($0.name, $0.value ?? "")
            },
            uniquingKeysWith: { _, last in last })

        return state.withLock { state in
            let queryText = url.query(percentEncoded: false).map { "?\($0)" } ?? ""
            state.log.append("\(request.httpMethod ?? "GET") /\(route.joined(separator: "/"))\(queryText)")
            guard route.count >= 3, route[0] == "households" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            switch Array(route.dropFirst(2)) {
            case ["menu"]:
                guard !state.failMenu else { return (500, Fixtures.errorJSON(code: "internal")) }
                return (200, Self.menu(week: query["week"] ?? state.currentWeek, current: state.currentWeek))
            case ["menu", "recipes"]:
                state.recipeQueries.append(query)
                var matches = state.recipes
                if let search = query["q"], !search.isEmpty {
                    matches = matches.filter { $0.name.localizedCaseInsensitiveContains(search) }
                }
                let start = query["cursor"].flatMap { Int($0) } ?? 0
                let limit = query["limit"].flatMap { Int($0) } ?? 24
                let end = min(start + limit, matches.count)
                let cards = matches[min(start, end)..<end].map { MenuFixtures.card(id: $0.id, name: $0.name) }
                return (200, MenuFixtures.page(Array(cards), nextCursor: end < matches.count ? String(end) : nil))
            case ["menu", "filters"]:
                guard !state.failFilters else { return (500, Fixtures.errorJSON(code: "internal")) }
                return (200, MenuFixtures.filters)
            case ["weeks"]:
                state.weekQueries.append(query)
                guard !state.failWeeks else { return (500, Fixtures.errorJSON(code: "internal")) }
                return (200, Self.weeks(query: query, state: state))
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private static func timing(_ week: ISOWeek, current: ISOWeek) -> String {
        week < current ? "past" : week == current ? "current" : "upcoming"
    }

    private static func menu(week: String, current: String) -> Data {
        guard let parsed = ISOWeek(week), let currentWeek = ISOWeek(current) else { return Data() }
        let timing = timing(parsed, current: currentWeek)
        if timing == "past" {
            return MenuFixtures.menu(
                week: week, timing: timing,
                sections: [
                    MenuFixtures.section(
                        id: "history_planned", kind: "history", title: "What You Planned",
                        cards: [MenuFixtures.card(id: "recipe-3", name: "Placeholder Pasta")])
                ])
        }
        return MenuFixtures.menu(
            week: week, timing: timing,
            sections: [
                MenuFixtures.section(
                    id: "favorites", title: "Your Favorites", subtitle: "Based on what you liked before",
                    cards: [
                        MenuFixtures.card(
                            id: "recipe-1", name: "Test Kitchen Tacos",
                            badges: #"[{"code":"make_again","text":"Make Again"}]"#),
                        MenuFixtures.card(id: "recipe-2", name: "Sample Soup"),
                    ],
                    moreQuery: #"{"sort":"popular"}"#),
                MenuFixtures.section(
                    id: "quick", title: "Quick & Easy",
                    cards: [MenuFixtures.card(id: "recipe-3", name: "Placeholder Pasta")],
                    moreQuery: #"{"maxMinutes":20,"sort":"quick"}"#),
            ])
    }

    private static func weeks(query: [String: String], state: State) -> Data {
        guard let around = query["around"].flatMap({ ISOWeek($0) }), let current = ISOWeek(state.currentWeek) else {
            return Data(#"{"items":[],"earliestWeek":null}"#.utf8)
        }
        let before = query["before"].flatMap { Int($0) } ?? 0
        let after = query["after"].flatMap { Int($0) } ?? 0
        let earliest = state.earliestWeek.flatMap { ISOWeek($0) }
        var items: [String] = []
        for offset in -before...after {
            let week = around.adding(weeks: offset)
            if let earliest, week < earliest { continue }
            items.append(
                MenuFixtures.weekSummary(
                    week.description, timing: timing(week, current: current), planned: week == current ? 2 : 0,
                    cooked: week < current ? 1 : 0))
        }
        let earliestJSON = state.earliestWeek.map { #""\#($0)""# } ?? "null"
        return Data(#"{"items":[\#(items.joined(separator: ","))],"earliestWeek":\#(earliestJSON)}"#.utf8)
    }
}
