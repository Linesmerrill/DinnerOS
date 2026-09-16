import Foundation

/// The event types the app may send (`ClientEvent.type` in `api/openapi.yaml`). The
/// server records every other type itself.
nonisolated enum ClientEventType: String, Codable, Sendable {
    case recipeViewed = "recipe.viewed"
    case recipeCooked = "recipe.cooked"
    case recipeSkipped = "recipe.skipped"
    case groceryItemChecked = "grocery.item_checked"
}

/// Where a recipe was seen.
nonisolated enum RecipeViewSurface: String, Codable, Sendable {
    case detail, plan, search, recommendation
}

/// Why a planned meal wasn't cooked.
nonisolated enum SkipReason: String, Codable, CaseIterable, Sendable, Identifiable {
    case noTime = "no-time"
    case ateOut = "ate-out"
    case missingIngredients = "missing-ingredients"
    case notInTheMood = "not-in-the-mood"
    case other

    var id: String { rawValue }

    var title: String {
        switch self {
        case .noTime: String(localized: "No Time")
        case .ateOut: String(localized: "Ate Out")
        case .missingIngredients: String(localized: "Missing Ingredients")
        case .notInTheMood: String(localized: "Not in the Mood")
        case .other: String(localized: "Other Reason")
        }
    }
}

extension ClientEventType {
    /// Whether this event carries an answer about a planned meal, rather than something the
    /// app merely observed.
    var isOutcome: Bool {
        self == .recipeCooked || self == .recipeSkipped
    }
}

/// What the household said happened to a planned meal.
///
/// Only ever set from an answer someone gave. Nothing infers it: a week going by doesn't cook a
/// meal, and a rating says the food was good, not that this household made it that night.
nonisolated enum MealOutcome: Equatable, Sendable {
    case cooked
    case skipped(SkipReason?)
}

extension MealOutcome: Codable {
    private enum CodingKeys: String, CodingKey {
        case kind, reason
    }

    private enum Kind: String, Codable {
        case cooked, skipped
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        switch try container.decode(Kind.self, forKey: .kind) {
        case .cooked:
            self = .cooked
        case .skipped:
            self = .skipped(try container.decodeIfPresent(SkipReason.self, forKey: .reason))
        }
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .cooked:
            try container.encode(Kind.cooked, forKey: .kind)
        case .skipped(let reason):
            try container.encode(Kind.skipped, forKey: .kind)
            try container.encodeIfPresent(reason, forKey: .reason)
        }
    }
}

/// An outcome and when it was answered, so old ones can be dropped.
nonisolated struct RecordedOutcome: Codable, Equatable, Sendable {
    var outcome: MealOutcome
    var recordedAt: Date
}

/// The API's limits on client event fields.
nonisolated enum EventLimits {
    static let maxBatchSize = 100
    static let maxIDLength = 64
    static let maxNameLength = 100
    static let servings = 1...12
}

nonisolated struct RecipeViewedPayload: Codable, Equatable, Sendable {
    var surface: RecipeViewSurface?
}

nonisolated struct RecipeCookedPayload: Codable, Equatable, Sendable {
    var entryID: String?
    /// `YYYY-MM-DD`.
    var date: String?
    var servings: Int?

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case date, servings
    }
}

nonisolated struct RecipeSkippedPayload: Codable, Equatable, Sendable {
    var entryID: String?
    var date: String?
    var reason: SkipReason?

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case date, reason
    }
}

nonisolated struct GroceryItemCheckedPayload: Codable, Equatable, Sendable {
    /// The catalog ingredient, when the line has one.
    var ingredientID: String?
    var name: String?
    /// `false` when the item was unchecked again.
    var checked: Bool

    private enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, checked
    }
}

/// A typed payload; each client event type has exactly one shape, and the API rejects
/// unknown fields.
nonisolated enum ClientEventPayload: Equatable, Sendable {
    case recipeViewed(RecipeViewedPayload)
    case recipeCooked(RecipeCookedPayload)
    case recipeSkipped(RecipeSkippedPayload)
    case groceryItemChecked(GroceryItemCheckedPayload)

    var type: ClientEventType {
        switch self {
        case .recipeViewed: .recipeViewed
        case .recipeCooked: .recipeCooked
        case .recipeSkipped: .recipeSkipped
        case .groceryItemChecked: .groceryItemChecked
        }
    }
}

/// One event the app observed. The same JSON is sent to the API and kept in the
/// on-device queue, so a queued event is sent exactly as it was recorded.
nonisolated struct ClientEvent: Codable, Equatable, Sendable, Identifiable {
    /// A UUID that makes retries idempotent: the API stores an ID once.
    let clientEventID: String
    /// Required for `recipe.*` events, absent otherwise.
    let recipeID: String?
    /// An ISO week such as `2026-W38`, when the event concerns one.
    let week: String?
    let occurredAt: Date
    let payload: ClientEventPayload

    var id: String { clientEventID }
    var type: ClientEventType { payload.type }

    /// The plan entry this event is about, for the `recipe.cooked` and `recipe.skipped`
    /// events that name one.
    var entryID: String? {
        switch payload {
        case .recipeCooked(let value): value.entryID
        case .recipeSkipped(let value): value.entryID
        case .recipeViewed, .groceryItemChecked: nil
        }
    }

    init(
        clientEventID: String = UUID().uuidString, recipeID: String?, week: String?, occurredAt: Date,
        payload: ClientEventPayload
    ) {
        self.clientEventID = clientEventID
        self.recipeID = recipeID
        self.week = week
        self.occurredAt = occurredAt
        self.payload = payload
    }

    private enum CodingKeys: String, CodingKey {
        case clientEventID = "clientEventId"
        case type
        case recipeID = "recipeId"
        case week, occurredAt, payload
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        clientEventID = try container.decode(String.self, forKey: .clientEventID)
        recipeID = try container.decodeIfPresent(String.self, forKey: .recipeID)
        week = try container.decodeIfPresent(String.self, forKey: .week)
        occurredAt = try container.decode(Date.self, forKey: .occurredAt)
        switch try container.decode(ClientEventType.self, forKey: .type) {
        case .recipeViewed:
            payload = .recipeViewed(try container.decode(RecipeViewedPayload.self, forKey: .payload))
        case .recipeCooked:
            payload = .recipeCooked(try container.decode(RecipeCookedPayload.self, forKey: .payload))
        case .recipeSkipped:
            payload = .recipeSkipped(try container.decode(RecipeSkippedPayload.self, forKey: .payload))
        case .groceryItemChecked:
            payload = .groceryItemChecked(try container.decode(GroceryItemCheckedPayload.self, forKey: .payload))
        }
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(clientEventID, forKey: .clientEventID)
        try container.encode(type, forKey: .type)
        try container.encodeIfPresent(recipeID, forKey: .recipeID)
        try container.encodeIfPresent(week, forKey: .week)
        try container.encode(occurredAt, forKey: .occurredAt)
        switch payload {
        case .recipeViewed(let value): try container.encode(value, forKey: .payload)
        case .recipeCooked(let value): try container.encode(value, forKey: .payload)
        case .recipeSkipped(let value): try container.encode(value, forKey: .payload)
        case .groceryItemChecked(let value): try container.encode(value, forKey: .payload)
        }
    }
}

// MARK: - Payloads from app models

extension RecipeCookedPayload {
    /// The entry's ID, planned date, and servings, leaving out values the API would reject.
    nonisolated init(entry: PlanEntry) {
        self.init(
            entryID: ClientEventFields.id(entry.id), date: entry.date,
            servings: EventLimits.servings.contains(entry.servings) ? entry.servings : nil)
    }
}

extension RecipeSkippedPayload {
    nonisolated init(entry: PlanEntry, reason: SkipReason?) {
        self.init(entryID: ClientEventFields.id(entry.id), date: entry.date, reason: reason)
    }
}

extension GroceryItemCheckedPayload {
    /// A catalog line sends its ingredient ID and name; an uncatalogued line (keyed
    /// `name:<normalized name>`) sends only the name.
    nonisolated init(item: GroceryItem, checked: Bool) {
        let isCatalogued = !item.ingredientKey.hasPrefix("name:")
        self.init(
            ingredientID: isCatalogued ? ClientEventFields.id(item.ingredientKey) : nil,
            name: ClientEventFields.name(item.name), checked: checked)
    }
}

nonisolated enum ClientEventFields {
    /// `nil` for an empty ID or one over the API's limit.
    static func id(_ value: String) -> String? {
        value.isEmpty || value.count > EventLimits.maxIDLength ? nil : value
    }

    /// Trimmed and cut to the API's limit, counted in Unicode scalars; `nil` when empty.
    static func name(_ value: String) -> String? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return String(String.UnicodeScalarView(trimmed.unicodeScalars.prefix(EventLimits.maxNameLength)))
    }
}

// MARK: - Ingestion

/// Body of `POST /api/v1/households/{householdId}/events`.
nonisolated struct IngestEventsRequest: Encodable, Sendable {
    let events: [ClientEvent]
}

/// Every event in a batch is counted once: accepted, duplicate, or rejected.
nonisolated struct IngestEventsResponse: Decodable, Equatable, Sendable {
    let accepted: Int
    /// Events whose `clientEventId` was already stored; they count as delivered.
    let duplicates: Int
    /// Invalid events. Retrying them can't succeed.
    let rejected: [EventRejection]
}

nonisolated struct EventRejection: Decodable, Equatable, Sendable {
    /// Position in the batch.
    let index: Int
    let message: String
}

/// Typed wrapper for event ingestion. Use it through `AuthSession.authorized`.
nonisolated struct EventsAPI: Sendable {
    let client: APIClient

    func send(_ events: [ClientEvent], householdID: String, accessToken: String) async throws
        -> IngestEventsResponse
    {
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/events", body: IngestEventsRequest(events: events))
        return try await client.send(request.authorized(with: accessToken))
    }
}
