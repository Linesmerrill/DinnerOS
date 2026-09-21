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

    /// The domain that cookie must come from, so a cookie set by some other site in the web view
    /// is never mistaken for the member's meal-kit session.
    var cookieDomain: String {
        switch self {
        case .helloFresh: "hellofresh.com"
        }
    }
}

/// The stored connection to a meal-kit account (`MealKitLink` in `api/openapi.yaml`).
///
/// It deliberately carries no email address and nothing derived from the stored tokens:
/// the API never returns either, and the app never needs them.
nonisolated struct MealKitLink: Decodable, Hashable, Sendable {
    /// `active`, or `needs_reauth` when the stored session expired.
    let status: String
    /// A display string such as "HelloFresh account".
    let accountLabel: String
    let linkedAt: Date
    let updatedAt: Date
    /// When the importer last used the connection, or `nil` before the first run.
    let lastUsedAt: Date?

    /// The member has to sign in again before anything more can be imported.
    var needsSignIn: Bool { status == "needs_reauth" }

    private enum CodingKeys: String, CodingKey {
        case status, accountLabel, linkedAt, updatedAt, lastUsedAt
    }

    init(status: String, accountLabel: String, linkedAt: Date, updatedAt: Date, lastUsedAt: Date? = nil) {
        self.status = status
        self.accountLabel = accountLabel
        self.linkedAt = linkedAt
        self.updatedAt = updatedAt
        self.lastUsedAt = lastUsedAt
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

    /// The member's session expired, so signing in again is the fix.
    var needsSignIn: Bool { code == "auth_expired" }
}

/// One import run (`MealKitImportJob` in `api/openapi.yaml`).
nonisolated struct MealKitImportJob: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// `queued`, `running`, `paused_auth`, `succeeded`, `dead`, or `canceled`.
    let status: String
    /// `orders`, `recipes`, `import`, or `done`.
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
    let attempts: Int
    let maxAttempts: Int
    let lastError: MealKitImportError?
    let createdAt: Date
    let updatedAt: Date
    let finishedAt: Date?

    init(
        id: String, status: String, phase: String = "orders", recipesFound: Int = 0, recipesDone: Int = 0,
        imported: Int = 0, updated: Int = 0, unchanged: Int = 0, reviewItems: Int = 0,
        failures: [MealKitImportFailure] = [], attempts: Int = 0, maxAttempts: Int = 5,
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
        /// Waiting for the member to sign in to the meal kit again.
        case needsSignIn
        /// Everything on the order history was handled.
        case finished
        /// It gave up. `lastError` says why.
        case failed
        /// The account was unlinked while it was in flight.
        case canceled
    }

    var state: State {
        switch status {
        case "queued": .queued
        case "paused_auth": .needsSignIn
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

    /// How far along, or `nil` before the order history has been read (there is no total to
    /// measure against yet, and a made-up bar would be a lie).
    var progress: Double? {
        guard recipesFound > 0 else { return nil }
        return min(1, Double(recipesDone) / Double(recipesFound))
    }
}

/// Response to `GET /api/v1/households/{householdId}/meal-kit/{source}`.
nonisolated struct MealKitStatus: Decodable, Hashable, Sendable {
    /// False when the server has no import encryption key: the app then offers adding
    /// recipes by hand rather than a sign-in it can't honour.
    let enabled: Bool
    let link: MealKitLink?
    let latestJob: MealKitImportJob?

    init(enabled: Bool, link: MealKitLink? = nil, latestJob: MealKitImportJob? = nil) {
        self.enabled = enabled
        self.link = link
        self.latestJob = latestJob
    }

    /// Nothing linked, and nothing to show.
    static let unavailable = MealKitStatus(enabled: false)

    /// Whether there is anything worth polling for.
    var isWorking: Bool { latestJob?.isWorking ?? false }
}

/// Response to `GET .../meal-kit/{source}/imports`.
nonisolated struct MealKitImportJobList: Decodable, Hashable, Sendable {
    let items: [MealKitImportJob]
}

/// Body of `PUT .../meal-kit/{source}/link`.
///
/// It carries the session the member's own sign-in on the meal kit's website produced — never a
/// password, because they never type one into this app. The server stores it encrypted; nothing
/// on this device keeps it, and nothing logs it.
nonisolated struct MealKitLinkRequest: Encodable, Sendable {
    let accessToken: String
    /// Omitted when the sign-in produced none; the session then simply expires sooner.
    let refreshToken: String?
    /// Omitted when the cookie didn't say enough to work it out.
    let expiresAt: Date?
    let startImport: Bool

    init(session: MealKitWebSession, startImport: Bool) {
        accessToken = session.accessToken
        refreshToken = session.refreshToken.isEmpty ? nil : session.refreshToken
        expiresAt = session.expiresAt
        self.startImport = startImport
    }
}
