import Foundation
import Observation
import os

/// A recipe a companion names (`/companions`).
nonisolated struct CompanionRecipe: Codable, Hashable, Sendable, Identifiable {
    let id: String
    let name: String
}

/// A choice the editor offers, with the words the server chose for it.
nonisolated struct CompanionOption: Decodable, Hashable, Sendable, Identifiable {
    let value: String
    let label: String

    var id: String { value }

    /// Bundled for an older server, or before the first load.
    static let fallbackFrequencies: [CompanionOption] = [
        CompanionOption(value: Companion.everyWeek, label: String(localized: "Every Week")),
        CompanionOption(value: "every_2_weeks", label: String(localized: "Every 2 Weeks")),
        CompanionOption(value: "every_month", label: String(localized: "Once a Month")),
        CompanionOption(value: Companion.onlyWithMeals, label: String(localized: "Only With Meals It Goes With")),
    ]

    static let fallbackModes: [CompanionOption] = [
        CompanionOption(value: "auto", label: String(localized: "Add It for Me")),
        CompanionOption(value: Companion.suggest, label: String(localized: "Just Suggest It")),
        CompanionOption(value: "never", label: String(localized: "Never")),
    ]
}

/// Something that goes with dinner but isn't dinner: a store item ("Tortilla chips") or one
/// of the household's add-on recipes. Decisions 715-719.
nonisolated struct Companion: Decodable, Hashable, Sendable, Identifiable {
    static let everyWeek = "every_week"
    static let onlyWithMeals = "none"
    static let suggest = "suggest"

    let id: String
    let name: String
    /// What goes on the list when it isn't the name.
    var storeItem: String?
    var quantity: Double?
    var unit: String?
    var productURL: String?
    /// The add-on recipe it was made from. Its suggestion plans that recipe.
    var linkedRecipe: CompanionRecipe?
    var cuisines: [String] = []
    var recipes: [CompanionRecipe] = []
    /// Carried over from a pairing rule; sent back unchanged.
    var mealCategories: [String] = []
    var frequency: String
    var mode: String
    /// The line the list shows under the name, from the server.
    var summary: String = ""
    var lastListedWeek: String?

    private enum CodingKeys: String, CodingKey {
        case id, name, storeItem, quantity, unit
        case productURL = "productUrl"
        case linkedRecipe, cuisines, recipes, mealCategories, frequency, mode, summary, lastListedWeek
    }

    init(
        id: String, name: String, storeItem: String? = nil, quantity: Double? = nil, unit: String? = nil,
        productURL: String? = nil, linkedRecipe: CompanionRecipe? = nil, cuisines: [String] = [],
        recipes: [CompanionRecipe] = [], mealCategories: [String] = [], frequency: String = Companion.onlyWithMeals,
        mode: String = Companion.suggest, summary: String = "", lastListedWeek: String? = nil
    ) {
        self.id = id
        self.name = name
        self.storeItem = storeItem
        self.quantity = quantity
        self.unit = unit
        self.productURL = productURL
        self.linkedRecipe = linkedRecipe
        self.cuisines = cuisines
        self.recipes = recipes
        self.mealCategories = mealCategories
        self.frequency = frequency
        self.mode = mode
        self.summary = summary
        self.lastListedWeek = lastListedWeek
    }

    /// Lenient past the ID and name: a field a newer server changed shouldn't hide the row.
    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try c.decode(String.self, forKey: .id),
            name: try c.decode(String.self, forKey: .name),
            storeItem: c.decodeLenient(String.self, forKey: .storeItem),
            quantity: c.decodeLenient(Double.self, forKey: .quantity),
            unit: c.decodeLenient(String.self, forKey: .unit),
            productURL: c.decodeLenient(String.self, forKey: .productURL),
            linkedRecipe: c.decodeLenient(CompanionRecipe.self, forKey: .linkedRecipe),
            cuisines: c.decodeLenient([String].self, forKey: .cuisines) ?? [],
            recipes: c.decodeLenient([CompanionRecipe].self, forKey: .recipes) ?? [],
            mealCategories: c.decodeLenient([String].self, forKey: .mealCategories) ?? [],
            frequency: c.decodeLenient(String.self, forKey: .frequency) ?? Companion.onlyWithMeals,
            mode: c.decodeLenient(String.self, forKey: .mode) ?? Companion.suggest,
            summary: c.decodeLenient(String.self, forKey: .summary) ?? "",
            lastListedWeek: c.decodeLenient(String.self, forKey: .lastListedWeek))
    }
}

/// `GET .../companions`: the household's companions and the editor's choices.
nonisolated struct CompanionList: Decodable, Sendable {
    let items: [Companion]
    let frequencies: [CompanionOption]
    let modes: [CompanionOption]

    private enum CodingKeys: String, CodingKey {
        case items, frequencies, modes
    }

    init(items: [Companion], frequencies: [CompanionOption], modes: [CompanionOption]) {
        self.items = items
        self.frequencies = frequencies
        self.modes = modes
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let frequencies = c.decodeLenient([CompanionOption].self, forKey: .frequencies) ?? []
        let modes = c.decodeLenient([CompanionOption].self, forKey: .modes) ?? []
        self.init(
            items: try c.decode(LossyArray<Companion>.self, forKey: .items).elements,
            frequencies: frequencies.isEmpty ? CompanionOption.fallbackFrequencies : frequencies,
            modes: modes.isEmpty ? CompanionOption.fallbackModes : modes)
    }
}

/// A companion as the editor sends it (`POST` and `PUT`).
nonisolated struct CompanionRequest: Encodable, Hashable, Sendable {
    let name: String
    let storeItem: String?
    let quantity: Double?
    let unit: String?
    let productURL: String?
    let cuisines: [String]
    let recipeIDs: [String]
    let mealCategories: [String]
    let frequency: String
    let mode: String

    private enum CodingKeys: String, CodingKey {
        case name, storeItem, quantity, unit
        case productURL = "productUrl"
        case cuisines
        case recipeIDs = "recipeIds"
        case mealCategories, frequency, mode
    }
}

/// What the editor holds while a member types.
nonisolated struct CompanionDraft: Hashable, Sendable {
    var name = ""
    var storeItem = ""
    /// 0 means no amount.
    var quantity = 0
    var unit: String?
    var productLink = ""
    var cuisines: [String] = []
    var recipes: [CompanionRecipe] = []
    var mealCategories: [String] = []
    var frequency = Companion.onlyWithMeals
    var mode = Companion.suggest
    /// The add-on recipe it was made from; it plans that recipe, so it has no store item.
    var linkedRecipe: CompanionRecipe?

    init() {}

    init(_ companion: Companion) {
        name = companion.name
        storeItem = companion.storeItem ?? ""
        quantity = companion.quantity.map { Int($0.rounded()) } ?? 0
        unit = companion.unit
        productLink = companion.productURL ?? ""
        cuisines = companion.cuisines
        recipes = companion.recipes
        mealCategories = companion.mealCategories
        frequency = companion.frequency
        mode = companion.mode
        linkedRecipe = companion.linkedRecipe
    }

    private static func trimmed(_ s: String) -> String {
        s.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var canSave: Bool { !Self.trimmed(name).isEmpty }

    /// Whether it goes with anything, so "Only With Meals It Goes With" means something.
    var goesWithSomething: Bool { !cuisines.isEmpty || !recipes.isEmpty || !mealCategories.isEmpty }

    mutating func toggleCuisine(_ value: String) {
        if let i = cuisines.firstIndex(of: value) {
            cuisines.remove(at: i)
        } else {
            cuisines.append(value)
        }
    }

    mutating func toggleRecipe(_ recipe: CompanionRecipe) {
        if let i = recipes.firstIndex(where: { $0.id == recipe.id }) {
            recipes.remove(at: i)
        } else {
            recipes.append(recipe)
        }
    }

    var request: CompanionRequest {
        let item = Self.trimmed(storeItem)
        let link = Self.trimmed(productLink)
        return CompanionRequest(
            name: Self.trimmed(name), storeItem: item.isEmpty ? nil : item,
            quantity: quantity > 0 ? Double(quantity) : nil, unit: quantity > 0 ? unit : nil,
            productURL: link.isEmpty ? nil : link, cuisines: cuisines, recipeIDs: recipes.map(\.id),
            mealCategories: mealCategories, frequency: frequency, mode: mode)
    }
}

/// Typed wrappers for `/api/v1/households/{householdId}/companions` (docs/api.md). Reads need
/// `household.view`, changes `recipes.edit`. Use them through `AuthSession.authorized`.
nonisolated struct CompanionsAPI: Sendable {
    let client: APIClient

    func list(householdID: String, accessToken: String) async throws -> CompanionList {
        try await client.send(APIRequest.get(Self.path(householdID)).authorized(with: accessToken))
    }

    func create(householdID: String, request body: CompanionRequest, accessToken: String) async throws -> Companion {
        let request = try APIRequest.post(Self.path(householdID), body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    func update(
        householdID: String, id: String, request body: CompanionRequest, accessToken: String
    ) async throws -> Companion {
        let request = try APIRequest.put(Self.path(householdID) + "/\(id)", body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    func delete(householdID: String, id: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID) + "/\(id)").authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/companions"
    }
}

/// The household's companions, for the Companions screen and the editor.
///
/// The list is a `GET`, so it is replayed offline from the saved answer like every other
/// screen; changes need the network. Every change calls `onChange`, so the week's
/// suggestions follow.
@Observable
final class CompanionsStore {
    enum Phase: Equatable {
        case idle, loading, loaded
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var items: [Companion] = []
    private(set) var frequencies = CompanionOption.fallbackFrequencies
    private(set) var modes = CompanionOption.fallbackModes
    private(set) var phase: Phase = .idle

    /// Told after a companion is saved or deleted.
    @ObservationIgnored var onChange: (() -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: CompanionsAPI?

    private static let logger = Logger(subsystem: "DinnerOS", category: "companions")

    init(session: AuthSession, api: CompanionsAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with `items`, for previews.
    static func preview(session: AuthSession, items: [Companion] = []) -> CompanionsStore {
        let store = CompanionsStore(session: session, api: nil)
        store.householdID = "household-preview"
        store.items = items
        store.phase = .loaded
        return store
    }

    func load(householdID: String) async {
        if householdID != self.householdID {
            items = []
            self.householdID = householdID
        }
        guard let api else { return }
        if items.isEmpty { phase = .loading }
        do {
            let list = try await session.authorized { token in
                try await api.list(householdID: householdID, accessToken: token)
            }
            guard householdID == self.householdID else { return }
            apply(list)
        } catch is CancellationError {
            return
        } catch {
            guard householdID == self.householdID else { return }
            phase = .failed(HouseholdStore.message(for: error))
        }
    }

    func apply(_ list: CompanionList) {
        items = list.items
        frequencies = list.frequencies
        modes = list.modes
        phase = .loaded
    }

    /// Saves a new companion, or changes the one with `id`.
    @discardableResult
    func save(_ draft: CompanionDraft, id: String?, householdID: String) async throws -> Companion {
        guard let api else { throw AuthSessionError.notConfigured }
        let request = draft.request
        let saved = try await session.authorized { token in
            if let id {
                try await api.update(householdID: householdID, id: id, request: request, accessToken: token)
            } else {
                try await api.create(householdID: householdID, request: request, accessToken: token)
            }
        }
        if householdID == self.householdID {
            if let i = items.firstIndex(where: { $0.id == saved.id }) {
                items[i] = saved
            } else {
                items.append(saved)
            }
        }
        Self.logger.info("Companion saved")
        onChange?()
        return saved
    }

    func delete(id: String, householdID: String) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            try await session.authorized { token in
                try await api.delete(householdID: householdID, id: id, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            // Someone else deleted it already.
        }
        items.removeAll { $0.id == id }
        Self.logger.info("Companion deleted")
        onChange?()
    }

    /// Forgets everything, for sign-out.
    func reset() {
        householdID = nil
        items = []
        frequencies = CompanionOption.fallbackFrequencies
        modes = CompanionOption.fallbackModes
        phase = .idle
    }
}
