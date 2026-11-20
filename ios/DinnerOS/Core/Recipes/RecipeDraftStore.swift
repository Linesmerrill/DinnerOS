import Foundation
import Observation
import os

/// Adding a recipe by hand: paste text or a link, review what came back, save.
///
/// It is created per screen rather than for the app's lifetime, because a draft
/// belongs to the sheet the member opened and should not survive it.
@Observable
final class RecipeDraftStore {
    enum Step: Equatable {
        /// Choosing between pasting text and pasting a link.
        case entry
        case parsing
        /// A draft came back and is being reviewed.
        case review
        case saving
    }

    private(set) var step: Step = .entry
    /// The draft being reviewed. Edited in place by the review form.
    var draft = RecipeDraft()
    /// Set when parsing or saving failed. The screen stays where it was.
    var errorMessage: String?
    /// The saved recipe, once there is one.
    private(set) var saved: Recipe?

    var isBusy: Bool { step == .parsing || step == .saving }
    var canSave: Bool { step == .review && draft.isSaveable }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: RecipesAPI?
    @ObservationIgnored private let householdID: String
    /// The link that couldn't be read, so Write Your Own can keep it as the recipe's source.
    private(set) var failedLink: String?
    /// Fetches a page on this phone with a browser's headers; replaced in tests.
    @ObservationIgnored var pageLoader: @Sendable (String) async throws -> String = RecipePageLoader.fetch
    /// Opens a page in the hidden web view and waits for it to render; replaced in tests.
    @ObservationIgnored var webPageLoader: @Sendable (String) async throws -> String = RecipePageLoader.render

    private static let logger = Logger(subsystem: "DinnerOS", category: "recipes")

    init(session: AuthSession, api: RecipesAPI?, householdID: String) {
        self.session = session
        self.api = api
        self.householdID = householdID
    }

    /// A store fixed at `step` with `draft`, for SwiftUI previews.
    static func preview(session: AuthSession, draft: RecipeDraft, step: Step = .review) -> RecipeDraftStore {
        let store = RecipeDraftStore(session: session, api: nil, householdID: "household-preview")
        store.draft = draft
        store.step = step
        return store
    }

    /// Parses pasted text. The server does the reading; this only moves the screen along.
    func parse(text: String) async {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        await parse { api, token in
            try await api.parseRecipe(householdID: self.householdID, text: trimmed, accessToken: token)
        }
    }

    /// Reads a recipe page. The server reads it, always; this phone only loads the page for it
    /// when the server can't (decision 728):
    ///
    /// 1. The server fetches the link.
    /// 2. `fetch_failed` (the site turned it away): this phone fetches the page like Safari and
    ///    sends it.
    /// 3. Still no recipe, or the phone's fetch failed, or `page_needs_phone` (a store site like
    ///    Kroger's that builds its recipe in the browser): the hidden web view opens the page,
    ///    waits for the recipe to show, and sends that.
    ///
    /// If nothing works, the screen shows what went wrong and offers Write Your Own with the
    /// link kept, and Paste Text.
    func parse(url: String) async {
        let trimmed = RecipePageLoader.link(in: url)
        guard !trimmed.isEmpty else { return }
        failedLink = nil
        let fetch = pageLoader
        let render = webPageLoader
        let householdID = householdID
        await parse { api, token in
            try await Self.read(
                trimmed, householdID: householdID, api: api, token: token, fetch: fetch, render: render)
        }
        if step == .entry, errorMessage != nil {
            failedLink = trimmed
        }
    }

    private static func read(
        _ link: String, householdID: String, api: RecipesAPI, token: String,
        fetch: @Sendable (String) async throws -> String, render: @Sendable (String) async throws -> String
    ) async throws -> RecipeDraft {
        let refused: APIError
        do {
            return try await api.parseRecipe(householdID: householdID, url: link, accessToken: token)
        } catch let error as APIError where error.code == "fetch_failed" || error.code == "page_needs_phone" {
            refused = error
        }
        var lastError: any Error = refused
        if refused.code == "fetch_failed" {
            let page: String?
            do {
                page = try await fetch(link)
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                page = nil  // The phone couldn't fetch it either; the web view is next.
            }
            if let page {
                do {
                    return try await api.parseRecipe(
                        householdID: householdID, url: link, html: page, accessToken: token)
                } catch let error as APIError where error.code == "fetch_failed" {
                    lastError = error
                }
            }
        }
        let rendered: String
        do {
            rendered = try await render(link)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            // "That site only shows its recipes in a web browser" says nothing useful once the
            // browser couldn't load it either.
            if (lastError as? APIError)?.code == "page_needs_phone" {
                throw RecipeImportError.pageDidNotLoad
            }
            throw lastError
        }
        return try await api.parseRecipe(householdID: householdID, url: link, html: rendered, accessToken: token)
    }

    /// The member picked the cuisine, even None. No guess replaces it after this.
    func pickCuisines(_ values: [String]) {
        draft.cuisines = values
        draft.cuisinesChosen = true
    }

    /// The cuisine last filled in from the title, so a new title can replace it.
    @ObservationIgnored private var guessedCuisines: [String] = []

    /// Fills in the cuisine the title names, until the member picks one. A guess only
    /// replaces nothing or an earlier guess, never a page's own cuisine.
    func guessCuisine(using guess: (String) async -> [String]?) async {
        let name = draft.name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard draft.cuisinesChosen != true, !name.isEmpty else { return }
        guard draft.cuisines.isEmpty || draft.cuisines == guessedCuisines else { return }
        guard let found = await guess(name), draft.cuisinesChosen != true,
            draft.name.trimmingCharacters(in: .whitespacesAndNewlines) == name,
            draft.cuisines.isEmpty || draft.cuisines == guessedCuisines
        else { return }
        guessedCuisines = found
        draft.cuisines = found
    }

    /// Starts an empty draft, for typing a recipe from scratch.
    func startBlank() {
        draft = RecipeDraft(steps: [""])
        errorMessage = nil
        failedLink = nil
        step = .review
    }

    /// Write Your Own after a link couldn't be read: the blank form, with the link saved as
    /// where the recipe came from.
    func writeOwnKeepingLink() {
        let link = failedLink
        startBlank()
        draft.sourceURL = link
    }

    /// Sends the member's photo and returns its link. The draft takes it when they save.
    func uploadPhoto(_ jpeg: Data) async throws -> String {
        guard let api else { throw AuthSessionError.notConfigured }
        let householdID = householdID
        return try await session.authorized { token in
            try await api.uploadPhoto(householdID: householdID, jpeg: jpeg, accessToken: token)
        }
    }

    /// Photos on the web for a search, a page at a time.
    func searchPhotos(_ query: String, page: Int) async throws -> RecipePhotoSearchPage {
        guard let api else { throw AuthSessionError.notConfigured }
        let householdID = householdID
        return try await session.authorized { token in
            try await api.searchPhotos(householdID: householdID, query: query, page: page, accessToken: token)
        }
    }

    /// Picks a photo found with photo search: where to show it from and who to credit.
    func useFoundPhoto(_ photoID: String) async throws -> RecipePhotoPick {
        guard let api else { throw AuthSessionError.notConfigured }
        let householdID = householdID
        return try await session.authorized { token in
            try await api.useFoundPhoto(householdID: householdID, photoID: photoID, accessToken: token)
        }
    }

    /// Goes back to the entry step, dropping the draft.
    func startOver() {
        draft = RecipeDraft()
        errorMessage = nil
        failedLink = nil
        step = .entry
    }

    /// Saves the reviewed draft into the household's library.
    @discardableResult
    func save() async -> Recipe? {
        guard let api, canSave else { return nil }
        step = .saving
        errorMessage = nil
        var outgoing = draft
        // Blank rows a member left behind are not ingredients or steps.
        outgoing.ingredients = draft.ingredients.filter {
            !$0.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
        outgoing.steps = draft.steps.filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        // Warnings are advice for the person reviewing; the server ignores them.
        outgoing.warnings = []
        let nutrition = draft.nutrition.entries
        outgoing.nutritionPerServing = nutrition.isEmpty ? nil : nutrition
        do {
            let recipe = try await session.authorized { token in
                try await api.createRecipe(householdID: self.householdID, draft: outgoing, accessToken: token)
            }
            saved = recipe
            return recipe
        } catch is CancellationError {
            step = .review
            return nil
        } catch {
            Self.logger.notice("Create recipe failed: \(Self.describe(error), privacy: .public)")
            errorMessage = HouseholdStore.message(for: error)
            step = .review
            return nil
        }
    }

    private func parse(
        _ request: @escaping @Sendable (RecipesAPI, String) async throws -> RecipeDraft
    ) async {
        guard let api else { return }
        step = .parsing
        errorMessage = nil
        do {
            let parsed = try await session.authorized { token in
                try await request(api, token)
            }
            draft = parsed
            draft.nutrition = NutritionValues(parsed.nutritionPerServing ?? [])
            step = .review
        } catch is CancellationError {
            step = .entry
        } catch {
            Self.logger.notice("Parse recipe failed: \(Self.describe(error), privacy: .public)")
            errorMessage = HouseholdStore.message(for: error)
            step = .entry
        }
    }

    /// Status and code only, never a response body, and never what was pasted.
    private static func describe(_ error: any Error) -> String {
        guard let apiError = error as? APIError else { return "\(type(of: error))" }
        return apiError.code ?? "\(apiError.status ?? 0)"
    }
}

/// A link the phone couldn't load either, after the server sent it to the phone.
enum RecipeImportError: LocalizedError {
    case pageDidNotLoad

    var errorDescription: String? {
        String(localized: "That page didn’t load. Check the link and try again.")
    }
}
