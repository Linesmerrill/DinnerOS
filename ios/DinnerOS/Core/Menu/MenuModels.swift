import Foundation

// MARK: - Values

/// Whether a week is before, at, or after this week. Unknown values decode as-is.
nonisolated struct WeekTiming: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let past = WeekTiming(rawValue: "past")
    static let current = WeekTiming(rawValue: "current")
    static let upcoming = WeekTiming(rawValue: "upcoming")

    /// The timing of `week` relative to `current`, for weeks the server hasn't described.
    static func of(_ week: ISOWeek, current: ISOWeek) -> WeekTiming {
        week < current ? .past : week == current ? .current : .upcoming
    }
}

/// A week's plan status in the week list: `draft`, `finalized`, or `none` for an unplanned week.
nonisolated struct WeekStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let draft = WeekStatus(rawValue: "draft")
    static let finalized = WeekStatus(rawValue: "finalized")
    static let none = WeekStatus(rawValue: "none")
}

/// What a card's badge means. Unknown codes decode as-is and still show their text.
nonisolated struct MenuBadgeCode: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let quick = MenuBadgeCode(rawValue: "quick")
    static let makeAgain = MenuBadgeCode(rawValue: "make_again")
    static let kidFavorite = MenuBadgeCode(rawValue: "kid_favorite")
    static let new = MenuBadgeCode(rawValue: "new")
    static let topRated = MenuBadgeCode(rawValue: "top_rated")
    static let autopilotPick = MenuBadgeCode(rawValue: "autopilot_pick")
    static let smokerFriendly = MenuBadgeCode(rawValue: "smoker_friendly")
    static let oftenOrdered = MenuBadgeCode(rawValue: "often_ordered")

    /// A symbol for known codes; `nil` shows the text alone.
    var systemImage: String? {
        switch self {
        case .quick: "bolt.fill"
        case .makeAgain: "arrow.counterclockwise"
        case .kidFavorite: "face.smiling"
        case .new: "sparkle"
        case .topRated: "star.fill"
        case .autopilotPick: "sparkles"
        case .smokerFriendly: "flame"
        case .oftenOrdered: "repeat"
        default: nil
        }
    }
}

/// A short label on a card, such as "Make Again".
nonisolated struct MenuBadge: Decodable, Hashable, Sendable, Identifiable {
    let code: MenuBadgeCode
    let text: String

    var id: String { "\(code.rawValue)|\(text)" }

    private enum CodingKeys: String, CodingKey {
        case code, text
    }

    init(code: MenuBadgeCode, text: String) {
        self.code = code
        self.text = text
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        code = container.decodeLenient(MenuBadgeCode.self, forKey: .code) ?? MenuBadgeCode(rawValue: "")
        text = try container.decode(String.self, forKey: .text)
    }
}

/// A sort order for `GET .../menu/recipes`. Unknown values from the filters response decode as-is.
nonisolated struct MenuSort: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let recommended = MenuSort(rawValue: "recommended")
    static let popular = MenuSort(rawValue: "popular")
    static let recent = MenuSort(rawValue: "recent")
    static let quick = MenuSort(rawValue: "quick")
    static let name = MenuSort(rawValue: "name")

    /// Offered when the filters response isn't available.
    static let known: [MenuSort] = [.recommended, .popular, .recent, .quick, .name]

    var title: String {
        switch self {
        case .recommended: String(localized: "Recommended")
        case .popular: String(localized: "Most Popular")
        case .recent: String(localized: "Recently Ordered")
        case .quick: String(localized: "Quickest")
        case .name: String(localized: "A–Z")
        default: rawValue.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }
}

// MARK: - Cards and sections

/// A recipe on the Menu screen (`MenuCard`).
nonisolated struct MenuCard: Decodable, Hashable, Sendable, Identifiable {
    var recipe: RecipeSummary
    var badges: [MenuBadge] = []
    /// Why it's suggested, such as "Because you rated Tacos 5★".
    var reason: String?
    /// The recipe is in the week the menu was requested for.
    var inPlan = false
    var planEntryIDs: [String] = []

    var id: String { recipe.id }

    private enum CodingKeys: String, CodingKey {
        case recipe, badges, reason, inPlan
        case planEntryIDs = "planEntryIds"
    }

    init(
        recipe: RecipeSummary, badges: [MenuBadge] = [], reason: String? = nil, inPlan: Bool = false,
        planEntryIDs: [String] = []
    ) {
        self.recipe = recipe
        self.badges = badges
        self.reason = reason
        self.inPlan = inPlan
        self.planEntryIDs = planEntryIDs
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        recipe = try container.decode(RecipeSummary.self, forKey: .recipe)
        badges = container.decodeLossyArray(MenuBadge.self, forKey: .badges)
        reason = container.decodeLenient(String.self, forKey: .reason).flatMap { $0.isEmpty ? nil : $0 }
        planEntryIDs = container.decodeLossyArray(String.self, forKey: .planEntryIDs)
        inPlan = container.decodeLenientBool(forKey: .inPlan) ?? !planEntryIDs.isEmpty
    }

    /// Cards for `recipeID` carrying a rating the member just saved, so a rating given on a card
    /// shows there at once instead of waiting for the next menu load. `household` is left alone
    /// when the new average isn't known yet.
    static func patchingRating(
        _ cards: [MenuCard], recipeID: String, mine: RecipeRating?, household: HouseholdRating?
    ) -> [MenuCard] {
        cards.map { card in
            guard card.recipe.id == recipeID else { return card }
            var patched = card
            patched.recipe.myRating = mine
            if let household {
                patched.recipe.householdRating = household
            }
            return patched
        }
    }

    /// Cards with `inPlan` and `planEntryIDs` recomputed from `plan`, the source of truth after a change.
    static func patching(_ cards: [MenuCard], with plan: Plan) -> [MenuCard] {
        var entryIDs: [String: [String]] = [:]
        for entry in plan.entries {
            entryIDs[entry.recipe.id, default: []].append(entry.id)
        }
        return cards.map { card in
            var card = card
            card.planEntryIDs = entryIDs[card.recipe.id] ?? []
            card.inPlan = !card.planEntryIDs.isEmpty
            return card
        }
    }
}

nonisolated struct MenuSectionKind: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let carousel = MenuSectionKind(rawValue: "carousel")
    /// Read-only recipes from a past week.
    static let history = MenuSectionKind(rawValue: "history")
}

/// A titled row of cards, shown in the server's order (`MenuSection`).
nonisolated struct MenuSection: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    var kind: MenuSectionKind
    var title: String
    var subtitle: String?
    var items: [MenuCard]
    /// Filters for "Show More"; `nil` hides the link.
    var moreQuery: MenuRecipeQuery?

    private enum CodingKeys: String, CodingKey {
        case id, kind, title, subtitle, items, moreQuery
    }

    init(
        id: String, kind: MenuSectionKind = .carousel, title: String, subtitle: String? = nil, items: [MenuCard],
        moreQuery: MenuRecipeQuery? = nil
    ) {
        self.id = id
        self.kind = kind
        self.title = title
        self.subtitle = subtitle
        self.items = items
        self.moreQuery = moreQuery
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        kind = container.decodeLenient(MenuSectionKind.self, forKey: .kind) ?? .carousel
        title = try container.decode(String.self, forKey: .title)
        subtitle = container.decodeLenient(String.self, forKey: .subtitle).flatMap { $0.isEmpty ? nil : $0 }
        items = container.decodeLossyArray(MenuCard.self, forKey: .items)
        moreQuery = container.decodeLenient(MenuRecipeQuery.self, forKey: .moreQuery)
    }
}

/// The week's pending Autopilot proposal, as the menu summarizes it.
nonisolated struct MenuProposalSummary: Decodable, Hashable, Sendable {
    let id: String
    let status: AutopilotProposalStatus
    let version: Int
    let plannedMeals: Int

    var isPending: Bool { status == .proposed }

    private enum CodingKeys: String, CodingKey {
        case id, status, version, plannedMeals
    }

    init(id: String, status: AutopilotProposalStatus, version: Int, plannedMeals: Int) {
        self.id = id
        self.status = status
        self.version = version
        self.plannedMeals = plannedMeals
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        status = try container.decode(AutopilotProposalStatus.self, forKey: .status)
        version = container.decodeLenientInt(forKey: .version) ?? 0
        plannedMeals = container.decodeLenientInt(forKey: .plannedMeals) ?? 0
    }
}

/// Response to `GET /api/v1/households/{householdId}/menu?week=`.
nonisolated struct WeekMenu: Decodable, Equatable, Sendable {
    let week: String
    let weekStart: String
    let weekEnd: String
    let timing: WeekTiming
    /// `nil` for a week nobody has planned.
    var plan: Plan?
    var proposal: MenuProposalSummary?
    var sections: [MenuSection]

    private enum CodingKeys: String, CodingKey {
        case week, weekStart, weekEnd, timing, plan, proposal, sections
    }

    init(
        week: String, weekStart: String, weekEnd: String, timing: WeekTiming, plan: Plan?,
        proposal: MenuProposalSummary?, sections: [MenuSection]
    ) {
        self.week = week
        self.weekStart = weekStart
        self.weekEnd = weekEnd
        self.timing = timing
        self.plan = plan
        self.proposal = proposal
        self.sections = sections
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        week = try container.decode(String.self, forKey: .week)
        weekStart = container.decodeLenient(String.self, forKey: .weekStart) ?? ""
        weekEnd = container.decodeLenient(String.self, forKey: .weekEnd) ?? ""
        timing = try container.decode(WeekTiming.self, forKey: .timing)
        plan = container.decodeLenient(Plan.self, forKey: .plan)
        proposal = container.decodeLenient(MenuProposalSummary.self, forKey: .proposal)
        sections = container.decodeLossyArray(MenuSection.self, forKey: .sections)
    }

    /// Shows `updated` as the week's plan and recomputes every card's plan state.
    mutating func apply(_ updated: Plan) {
        plan = updated
        for index in sections.indices {
            sections[index].items = MenuCard.patching(sections[index].items, with: updated)
        }
    }

    /// Shows a rating the member just saved on every card for that recipe.
    mutating func applyRating(recipeID: String, mine: RecipeRating?, household: HouseholdRating?) {
        for index in sections.indices {
            sections[index].items = MenuCard.patchingRating(
                sections[index].items, recipeID: recipeID, mine: mine, household: household)
        }
    }
}

// MARK: - All Meals

/// Filters for `GET .../menu/recipes`, and a section's `moreQuery`. Changing any of them
/// starts a new cursor sequence.
nonisolated struct MenuRecipeQuery: Decodable, Hashable, Sendable {
    /// The API accepts at most this many characters of search text.
    static let maxSearchLength = 100

    var search = ""
    var protein: String?
    var cuisine: String?
    var maxMinutes: Int?
    var tag: String?
    /// `true` for add-ons only, `false` for mains only, `nil` for both.
    var addons: Bool?
    var sort: MenuSort = .recommended

    init(
        search: String = "", protein: String? = nil, cuisine: String? = nil, maxMinutes: Int? = nil,
        tag: String? = nil, addons: Bool? = nil, sort: MenuSort = .recommended
    ) {
        self.search = search
        self.protein = protein
        self.cuisine = cuisine
        self.maxMinutes = maxMinutes
        self.tag = tag
        self.addons = addons
        self.sort = sort
    }

    private enum CodingKeys: String, CodingKey {
        case search = "q"
        case protein, cuisine, maxMinutes, tag, addons, sort
    }

    /// Reads a server-built query leniently: numbers and Booleans may arrive as strings, and
    /// unknown keys are ignored.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        search = container.decodeLenient(String.self, forKey: .search) ?? ""
        protein = Self.nonEmpty(container.decodeLenient(String.self, forKey: .protein))
        cuisine = Self.nonEmpty(container.decodeLenient(String.self, forKey: .cuisine))
        maxMinutes = container.decodeLenientInt(forKey: .maxMinutes)
        tag = Self.nonEmpty(container.decodeLenient(String.self, forKey: .tag))
        addons = container.decodeLenientBool(forKey: .addons)
        sort = Self.nonEmpty(container.decodeLenient(String.self, forKey: .sort)).map(MenuSort.init) ?? .recommended
    }

    /// Trimmed and cut to the API's limit.
    var normalizedSearch: String {
        String(search.trimmingCharacters(in: .whitespacesAndNewlines).prefix(Self.maxSearchLength))
    }

    /// Filters other than search and sort, for the filter button's count.
    var filterCount: Int {
        [protein != nil, cuisine != nil, maxMinutes != nil, tag != nil, addons != nil].filter { $0 }.count
    }

    /// Whether anything narrows the list, so an empty result isn't "no recipes at all".
    var isNarrowed: Bool {
        !normalizedSearch.isEmpty || filterCount > 0
    }

    /// Whether both send the same request, for example "taco" and "taco ".
    func isSameRequest(as other: MenuRecipeQuery) -> Bool {
        var lhs = self
        var rhs = other
        lhs.search = normalizedSearch
        rhs.search = other.normalizedSearch
        return lhs == rhs
    }

    private static func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        return value
    }
}

/// Response to `GET .../menu/recipes`.
nonisolated struct MenuRecipePage: Decodable, Equatable, Sendable {
    let items: [MenuCard]
    /// Send back as `cursor` for the next page; `nil` on the last page.
    let nextCursor: String?

    private enum CodingKeys: String, CodingKey {
        case items, nextCursor
    }

    init(items: [MenuCard], nextCursor: String?) {
        self.items = items
        self.nextCursor = nextCursor
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = container.decodeLossyArray(MenuCard.self, forKey: .items)
        nextCursor = container.decodeLenient(String.self, forKey: .nextCursor).flatMap { $0.isEmpty ? nil : $0 }
    }
}

nonisolated struct MenuFilterOption: Decodable, Hashable, Sendable, Identifiable {
    let value: String
    let label: String
    /// Matching recipes; `nil` when the server doesn't count.
    var count: Int?

    var id: String { value }

    private enum CodingKeys: String, CodingKey {
        case value, label, count
    }

    init(value: String, label: String, count: Int? = nil) {
        self.value = value
        self.label = label
        self.count = count
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        value = try container.decode(String.self, forKey: .value)
        label = container.decodeLenient(String.self, forKey: .label) ?? value
        count = container.decodeLenientInt(forKey: .count)
    }
}

nonisolated struct MenuSortOption: Decodable, Hashable, Sendable, Identifiable {
    let value: MenuSort
    let label: String

    var id: String { value.rawValue }
}

/// Response to `GET .../menu/filters`. Missing lists fall back to what the app knows.
nonisolated struct MenuFilterOptions: Decodable, Equatable, Sendable {
    var proteins: [MenuFilterOption] = []
    var cuisines: [MenuFilterOption] = []
    var tags: [MenuFilterOption] = []
    var maxMinutes: [Int] = MenuFilterOptions.defaultMaxMinutes
    var sorts: [MenuSortOption] = MenuFilterOptions.defaultSorts

    static let defaultMaxMinutes = [15, 20, 30, 45]
    static let defaultSorts = MenuSort.known.map { MenuSortOption(value: $0, label: $0.title) }
    /// Used before the filters load or when they can't.
    static let fallback = MenuFilterOptions()

    private enum CodingKeys: String, CodingKey {
        case proteins, cuisines, tags, maxMinutes, sorts
    }

    init(
        proteins: [MenuFilterOption] = [], cuisines: [MenuFilterOption] = [], tags: [MenuFilterOption] = [],
        maxMinutes: [Int] = MenuFilterOptions.defaultMaxMinutes,
        sorts: [MenuSortOption] = MenuFilterOptions.defaultSorts
    ) {
        self.proteins = proteins
        self.cuisines = cuisines
        self.tags = tags
        self.maxMinutes = maxMinutes
        self.sorts = sorts
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        proteins = container.decodeLossyArray(MenuFilterOption.self, forKey: .proteins)
        cuisines = container.decodeLossyArray(MenuFilterOption.self, forKey: .cuisines)
        tags = container.decodeLossyArray(MenuFilterOption.self, forKey: .tags)
        let minutes = container.decodeLossyArray(Int.self, forKey: .maxMinutes).filter { $0 > 0 }
        maxMinutes = minutes.isEmpty ? Self.defaultMaxMinutes : minutes
        let sorts = container.decodeLossyArray(MenuSortOption.self, forKey: .sorts)
        self.sorts = sorts.isEmpty ? Self.defaultSorts : sorts
    }

    /// The label for a filter value, falling back to the value itself.
    static func label(for value: String, in options: [MenuFilterOption]) -> String {
        options.first { $0.value == value }?.label ?? value.capitalized
    }
}

// MARK: - Weeks

/// One week in `GET .../weeks` (`WeekSummary`).
nonisolated struct WeekSummary: Decodable, Hashable, Sendable, Identifiable {
    let week: String
    let weekStart: String
    let weekEnd: String
    let timing: WeekTiming
    /// Main meals, not counting add-ons.
    var plannedCount: Int
    /// Add-ons planned alongside those meals; `0` from a server that doesn't send it.
    var addOnCount: Int
    var cookedCount: Int
    var orderedCount: Int
    var status: WeekStatus

    var id: String { week }
    var isoWeek: ISOWeek? { ISOWeek(week) }

    private enum CodingKeys: String, CodingKey {
        case week, weekStart, weekEnd, timing, plannedCount, addOnCount, cookedCount, orderedCount, status
    }

    init(
        week: String, weekStart: String = "", weekEnd: String = "", timing: WeekTiming, plannedCount: Int = 0,
        addOnCount: Int = 0, cookedCount: Int = 0, orderedCount: Int = 0, status: WeekStatus = .none
    ) {
        self.week = week
        self.weekStart = weekStart
        self.weekEnd = weekEnd
        self.timing = timing
        self.plannedCount = plannedCount
        self.addOnCount = addOnCount
        self.cookedCount = cookedCount
        self.orderedCount = orderedCount
        self.status = status
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        week = try container.decode(String.self, forKey: .week)
        weekStart = container.decodeLenient(String.self, forKey: .weekStart) ?? ""
        weekEnd = container.decodeLenient(String.self, forKey: .weekEnd) ?? ""
        timing = container.decodeLenient(WeekTiming.self, forKey: .timing) ?? .upcoming
        plannedCount = container.decodeLenientInt(forKey: .plannedCount) ?? 0
        addOnCount = container.decodeLenientInt(forKey: .addOnCount) ?? 0
        cookedCount = container.decodeLenientInt(forKey: .cookedCount) ?? 0
        orderedCount = container.decodeLenientInt(forKey: .orderedCount) ?? 0
        status = container.decodeLenient(WeekStatus.self, forKey: .status) ?? .none
    }
}

/// Response to `GET .../weeks?around=&before=&after=`.
nonisolated struct WeekListResponse: Decodable, Equatable, Sendable {
    let items: [WeekSummary]
    /// The household's first week with any history; `nil` when it has none.
    let earliestWeek: String?

    private enum CodingKeys: String, CodingKey {
        case items, earliestWeek
    }

    init(items: [WeekSummary], earliestWeek: String?) {
        self.items = items
        self.earliestWeek = earliestWeek
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = container.decodeLossyArray(WeekSummary.self, forKey: .items)
        earliestWeek = container.decodeLenient(String.self, forKey: .earliestWeek)
    }
}

/// A pill in the week strip: a week and what the server said about it, if anything.
nonisolated struct WeekStripItem: Hashable, Sendable, Identifiable {
    let week: ISOWeek
    let summary: WeekSummary?
    let timing: WeekTiming

    var id: String { week.description }
}
