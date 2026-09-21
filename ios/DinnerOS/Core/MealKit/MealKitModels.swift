import Foundation

/// A meal-kit service a household can import its own order history from
/// (`docs/meal-kit-import.md`). Only HelloFresh exists today.
nonisolated enum MealKitService: String, CaseIterable, Sendable {
    case helloFresh = "hellofresh"

    var displayName: String {
        switch self {
        case .helloFresh: "HelloFresh"
        }
    }

    /// The service's own sign-in page, loaded in a web view so the member types their password
    /// into the real site rather than into us. `nil` would mean we can't offer the sign-in at
    /// all, which the screens handle rather than force-unwrapping a constant.
    var loginURL: URL? {
        switch self {
        case .helloFresh: URL(string: "https://www.hellofresh.com/login")
        }
    }

    /// The cookie the service's own login writes. Captured from a signed-in session; see
    /// `docs/meal-kit-import.md`.
    var sessionCookieName: String {
        switch self {
        case .helloFresh: "apiV2Auth"
        }
    }

    /// A cookie that already names the account's subscription, when one exists. It saves the
    /// harvest a request; without it the subscription is read from the account's own plans
    /// endpoint. HelloFresh has none: `hf_plan_id` holds the customer plan's UUID, and the
    /// order history answers 403 to anything but the numeric `legacySubscriptionId`.
    var planCookieName: String? {
        switch self {
        case .helloFresh: nil
        }
    }

    /// The country and locale the account endpoints want. A household outside the US needs these
    /// to change; there is nowhere yet to say so, which is noted in the docs.
    var country: String {
        switch self {
        case .helloFresh: "US"
        }
    }

    var locale: String {
        switch self {
        case .helloFresh: "en-US"
        }
    }

    /// The domain that cookie must come from, so a cookie set by some other site in the web view
    /// is never mistaken for the member's meal-kit session.
    var cookieDomain: String {
        switch self {
        case .helloFresh: "hellofresh.com"
        }
    }
}

/// One recipe of the order history that could not be imported.
nonisolated struct MealKitImportFailure: Decodable, Hashable, Sendable, Identifiable {
    let sourceRecipeID: String
    let name: String
    /// Why, in words written for a person. It never quotes a fetched page.
    let reason: String

    var id: String { sourceRecipeID }

    private enum CodingKeys: String, CodingKey {
        case sourceRecipeID = "sourceRecipeId"
        case name, reason
    }

    init(sourceRecipeID: String, name: String = "", reason: String) {
        self.sourceRecipeID = sourceRecipeID
        self.name = name
        self.reason = reason
    }

    /// `name` is `omitempty` on the server.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            sourceRecipeID: try container.decode(String.self, forKey: .sourceRecipeID),
            name: try container.decodeIfPresent(String.self, forKey: .name) ?? "",
            reason: try container.decode(String.self, forKey: .reason))
    }

    /// What to show: the recipe's name when the importer got that far, else its source ID.
    var title: String { name.isEmpty ? sourceRecipeID : name }
}

/// Why a run stopped, ready to show.
nonisolated struct MealKitImportError: Decodable, Hashable, Sendable {
    /// `auth_expired`, `parse`, `blocked`, `network`, `import`, or `internal`. A code this
    /// build doesn't know still decodes and shows its message.
    let code: String
    let message: String
    let at: Date

}

/// Where the walk that produced one run's order history stopped
/// (`MealKitHarvestSummary` in `api/openapi.yaml`).
nonisolated struct MealKitHarvestSummary: Decodable, Hashable, Sendable {
    let earliestWeek: String
    let latestWeek: String
    let pages: Int
    let weeks: Int
    /// `cap`, `empty`, `end`, or `caught_up`.
    let stopped: String
    /// True when the walk stopped on its own page cap, so history is known to be left unread.
    let moreToFetch: Bool

    init(
        earliestWeek: String = "", latestWeek: String = "", pages: Int = 0, weeks: Int = 0,
        stopped: String = "", moreToFetch: Bool = false
    ) {
        self.earliestWeek = earliestWeek
        self.latestWeek = latestWeek
        self.pages = pages
        self.weeks = weeks
        self.stopped = stopped
        self.moreToFetch = moreToFetch
    }
}

/// How far back this household's order history has been read, and where the next harvest
/// resumes (`MealKitImportHistory` in `api/openapi.yaml`).
///
/// It is the whole of what the server remembers about a household's meal-kit account: two ISO
/// weeks and a flag. Not a token, not a cookie, not an email address.
nonisolated struct MealKitImportHistory: Decodable, Hashable, Sendable {
    /// The oldest delivered week any harvest has reached.
    let earliestWeek: String
    /// The newest one seen. A catch-up pass walks back only to here.
    let latestWeek: String
    /// The week the next harvest should walk back from, or empty for "start at today".
    let resumeFromWeek: String
    /// True once a harvest reached the start of the account's history.
    let complete: Bool
    /// True when history is known to be left unread.
    let moreToFetch: Bool
    /// Set when the service refused us and a new run will be refused until then.
    let blockedUntil: Date?

    init(
        earliestWeek: String = "", latestWeek: String = "", resumeFromWeek: String = "",
        complete: Bool = false, moreToFetch: Bool = false, blockedUntil: Date? = nil
    ) {
        self.earliestWeek = earliestWeek
        self.latestWeek = latestWeek
        self.resumeFromWeek = resumeFromWeek
        self.complete = complete
        self.moreToFetch = moreToFetch
        self.blockedUntil = blockedUntil
    }

    /// Nothing has ever been read.
    var isEmpty: Bool { earliestWeek.isEmpty && latestWeek.isEmpty }

    /// The week a resumed walk starts below, or `nil` when there is nothing older to read.
    var resumeWeek: String? { resumeFromWeek.isEmpty ? nil : resumeFromWeek }
}

/// One import run (`MealKitImportJob` in `api/openapi.yaml`).
nonisolated struct MealKitImportJob: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// `queued`, `running`, `succeeded`, `dead`, or `canceled`.
    let status: String
    /// `recipes`, `import`, or `done`.
    let phase: String
    /// Recipes on the account's order history; 0 until it has been read.
    let recipesFound: Int
    let recipesDone: Int
    let imported: Int
    let updated: Int
    let unchanged: Int
    /// Things the importer could not map confidently. They are read on the Import Review
    /// screen, not here.
    let reviewItems: Int
    let failures: [MealKitImportFailure]
    /// Where the walk that produced this run's order history stopped. Absent from a server
    /// older than the cursor, which reads as "it did not say".
    let harvest: MealKitHarvestSummary?
    let attempts: Int
    let maxAttempts: Int
    let lastError: MealKitImportError?
    let createdAt: Date
    let updatedAt: Date
    let finishedAt: Date?

    init(
        id: String, status: String, phase: String = "orders", recipesFound: Int = 0, recipesDone: Int = 0,
        imported: Int = 0, updated: Int = 0, unchanged: Int = 0, reviewItems: Int = 0,
        failures: [MealKitImportFailure] = [], harvest: MealKitHarvestSummary? = nil,
        attempts: Int = 0, maxAttempts: Int = 5,
        lastError: MealKitImportError? = nil, createdAt: Date = .distantPast,
        updatedAt: Date = .distantPast, finishedAt: Date? = nil
    ) {
        self.id = id
        self.status = status
        self.phase = phase
        self.recipesFound = recipesFound
        self.recipesDone = recipesDone
        self.imported = imported
        self.updated = updated
        self.unchanged = unchanged
        self.reviewItems = reviewItems
        self.failures = failures
        self.harvest = harvest
        self.attempts = attempts
        self.maxAttempts = maxAttempts
        self.lastError = lastError
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.finishedAt = finishedAt
    }

    /// What the screen shows. A status this build doesn't know is treated as still working,
    /// which is the safe direction: it never claims a run finished that hasn't.
    enum State: Hashable, Sendable {
        /// Waiting for the importer to pick it up.
        case queued
        /// Being imported right now.
        case running
        /// Everything on the order history was handled.
        case finished
        /// It gave up. `lastError` says why.
        case failed
        /// Someone stopped it while it was in flight.
        case canceled
    }

    var state: State {
        switch status {
        case "queued": .queued
        case "succeeded": .finished
        case "dead": .failed
        case "canceled": .canceled
        default: .running
        }
    }

    /// True while the run is still going to do something without anyone's help.
    var isWorking: Bool { state == .queued || state == .running }

    /// Recipes that reached the library, new or refreshed.
    var recipesAdded: Int { imported + updated }

    /// Whether the walk that produced this run left history unread.
    var moreHistoryToFetch: Bool { harvest?.moreToFetch ?? false }

    /// How far along, or `nil` before the order history has been read (there is no total to
    /// measure against yet, and a made-up bar would be a lie).
    var progress: Double? {
        guard recipesFound > 0 else { return nil }
        return min(1, Double(recipesDone) / Double(recipesFound))
    }
}

/// Response to `GET /api/v1/households/{householdId}/meal-kit/{source}`.
///
/// There is no linked account to report: the server stores nothing about one. What a household
/// has is import runs.
nonisolated struct MealKitStatus: Decodable, Hashable, Sendable {
    /// False when the server has meal-kit import turned off: the app then offers adding recipes
    /// by hand rather than a sign-in it can't honour.
    let enabled: Bool
    let latestJob: MealKitImportJob?
    /// How far back the household's order history has been read. The harvest resumes from here
    /// rather than walking four years back from today every time.
    let history: MealKitImportHistory

    init(
        enabled: Bool, latestJob: MealKitImportJob? = nil,
        history: MealKitImportHistory = MealKitImportHistory()
    ) {
        self.enabled = enabled
        self.latestJob = latestJob
        self.history = history
    }

    /// A server from before the cursor existed sends no `history`, which reads as "nothing has
    /// been harvested" — the safe direction: the harvest then starts at today, as it used to.
    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            enabled: try c.decode(Bool.self, forKey: .enabled),
            latestJob: try c.decodeIfPresent(MealKitImportJob.self, forKey: .latestJob),
            history: try c.decodeIfPresent(MealKitImportHistory.self, forKey: .history)
                ?? MealKitImportHistory())
    }

    private enum CodingKeys: String, CodingKey {
        case enabled, latestJob, history
    }

    /// Nothing to show.
    static let unavailable = MealKitStatus(enabled: false)

    /// Whether there is anything worth polling for.
    var isWorking: Bool { latestJob?.isWorking ?? false }
}

/// Response to `GET .../meal-kit/{source}/imports`.
nonisolated struct MealKitImportJobList: Decodable, Hashable, Sendable {
    let items: [MealKitImportJob]
}

/// Body of `POST .../meal-kit/{source}/imports`.
///
/// The order history and nothing else. There is deliberately no field for a token, a cookie, or
/// an account: the sign-in stayed in the web view, and the server has nowhere to put one.
nonisolated struct MealKitStartImportRequest: Encodable, Sendable {
    let recipes: [MealKitOrderedRecipe]
    /// Where the walk stopped, so the server knows whether to resume next time and what to tell
    /// the member. It is weeks and counts — there is nothing in it about the account.
    let harvest: Report

    init(harvest: MealKitHarvest) {
        recipes = harvest.recipes
        self.harvest = Report(
            earliestWeek: harvest.earliestWeek, latestWeek: harvest.latestWeek,
            pages: harvest.pages, weeks: harvest.weeks,
            // A reason a newer script invented is left out rather than sent: the server refuses
            // what it cannot read, and a harvest must not be lost over a label.
            stopped: harvest.stopped == .unknown ? nil : harvest.stopped.rawValue)
    }

    struct Report: Encodable, Sendable {
        let earliestWeek: String
        let latestWeek: String
        let pages: Int
        let weeks: Int
        let stopped: String?
    }
}
