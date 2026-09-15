import Foundation
import Observation

/// Catalog suggestions for an ingredient name being typed, fetched once typing pauses.
@Observable
final class IngredientSuggestions {
    /// Shorter text clears the suggestions without a request.
    static let minimumQueryLength = 2
    static let resultLimit = 8
    /// Long enough to skip requests while typing, short enough to feel live.
    static let defaultDebounce = Duration.milliseconds(300)

    /// The query `results` answer.
    private(set) var query = ""
    private(set) var results: [CatalogIngredient] = []
    private(set) var isSearching = false
    private(set) var errorMessage: String?

    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private let search: @MainActor (String) async throws -> [CatalogIngredient]
    @ObservationIgnored private var latestRequest = 0

    init(
        debounce: Duration = IngredientSuggestions.defaultDebounce,
        search: @escaping @MainActor (String) async throws -> [CatalogIngredient]
    ) {
        self.debounce = debounce
        self.search = search
    }

    /// Call for every edit from a task the next edit cancels, such as `.task(id:)`. Waits
    /// for `debounce`, then searches for the trimmed text.
    func update(for text: String) async {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count >= Self.minimumQueryLength else {
            latestRequest += 1
            query = ""
            results = []
            errorMessage = nil
            isSearching = false
            return
        }
        guard trimmed != query || errorMessage != nil else { return }
        do {
            try await Task.sleep(for: debounce)
        } catch {
            // A newer edit cancelled this one.
            return
        }

        latestRequest += 1
        let request = latestRequest
        isSearching = true
        defer {
            if request == latestRequest { isSearching = false }
        }
        do {
            let found = try await search(trimmed)
            guard request == latestRequest, !Task.isCancelled else { return }
            query = trimmed
            results = found
            errorMessage = nil
        } catch {
            guard request == latestRequest, !Task.isCancelled, !(error is CancellationError) else { return }
            query = trimmed
            results = []
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}
