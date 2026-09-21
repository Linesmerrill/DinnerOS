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

    /// Reads a recipe page. The phone never loads the page itself.
    func parse(url: String) async {
        let trimmed = url.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        await parse { api, token in
            try await api.parseRecipe(householdID: self.householdID, url: trimmed, accessToken: token)
        }
    }

    /// Starts an empty draft, for typing a recipe from scratch.
    func startBlank() {
        draft = RecipeDraft(ingredients: [RecipeDraftIngredient()], steps: [""])
        errorMessage = nil
        step = .review
    }

    /// Goes back to the entry step, dropping the draft.
    func startOver() {
        draft = RecipeDraft()
        errorMessage = nil
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
            step = .review
        } catch is CancellationError {
            step = .entry
        } catch {
            Self.logger.notice("Parse recipe failed: \(Self.describe(error), privacy: .public)")
            errorMessage = HouseholdStore.message(for: error)
            step = .entry
        }
    }

    /// Status and code only — never a response body, and never what was pasted.
    private static func describe(_ error: any Error) -> String {
        guard let apiError = error as? APIError else { return "\(type(of: error))" }
        return apiError.code ?? "\(apiError.status ?? 0)"
    }
}
