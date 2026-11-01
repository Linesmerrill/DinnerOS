import Foundation
import Observation

/// Fry's products pre-picked from the household's saved Walmart products: loaded a page at a
/// time, each one kept, swapped for a runner-up, or skipped, then saved together.
@MainActor
@Observable
final class MatchProductsModel {
    enum Phase: Equatable {
        case loading
        case loaded
        case failed(String)
    }

    private(set) var items: [ShoppingMatchSuggestion] = []
    /// Every ingredient still to match, across pages.
    private(set) var total = 0
    private(set) var phase = Phase.loading
    /// The product to save per ingredient; missing means skipped.
    private(set) var chosen: [String: ShoppingSearchProduct] = [:]
    private(set) var isSaving = false
    private(set) var savedCount = 0
    private(set) var saveError: String?
    @ObservationIgnored private var next: String?
    @ObservationIgnored private var started = false

    var selectedCount: Int { chosen.count }
    var isLoadingMore: Bool { phase == .loading && !items.isEmpty }

    func product(for item: ShoppingMatchSuggestion) -> ShoppingSearchProduct? { chosen[item.ingredientKey] }

    /// The pick and its runners-up, the pick first, without duplicates.
    func options(for item: ShoppingMatchSuggestion) -> [ShoppingSearchProduct] {
        var seen = Set<String>()
        return ([item.product].compactMap { $0 } + item.alternatives).filter { seen.insert($0.productID).inserted }
    }

    func choose(_ product: ShoppingSearchProduct?, for item: ShoppingMatchSuggestion) {
        chosen[item.ingredientKey] = product
    }

    /// Keeps or skips the item's current product.
    func toggle(_ item: ShoppingMatchSuggestion) {
        if chosen[item.ingredientKey] != nil {
            chosen[item.ingredientKey] = nil
        } else if let first = options(for: item).first(where: { !$0.isOutOfStock }) {
            chosen[item.ingredientKey] = first
        }
    }

    /// Loads every page, showing each as it arrives. Picks start selected; items with no
    /// clear match start skipped.
    func loadAll(
        fetch: (String?) async throws -> ShoppingMatchSuggestionPage, message: (any Error) -> String
    ) async {
        // Loaded to the end already; a failed load picks up where it stopped.
        if started, phase == .loaded { return }
        started = true
        phase = .loading
        repeat {
            do {
                let page = try await fetch(next)
                total = page.total
                for item in page.items where !items.contains(where: { $0.ingredientKey == item.ingredientKey }) {
                    items.append(item)
                    if let product = item.product { chosen[item.ingredientKey] = product }
                }
                next = page.next
            } catch is CancellationError {
                return
            } catch {
                phase = .failed(message(error))
                return
            }
        } while next != nil
        phase = .loaded
    }

    /// Saves every chosen product, one at a time. Stops at the first failure and keeps what
    /// was saved, so trying again only sends the rest.
    func save(
        using saveOne: (ShoppingMatchSuggestion, ShoppingSearchProduct) async throws -> Void,
        message: (any Error) -> String
    ) async -> Bool {
        guard !isSaving else { return false }
        isSaving = true
        defer { isSaving = false }
        saveError = nil
        for item in items {
            guard let product = chosen[item.ingredientKey] else { continue }
            do {
                try await saveOne(item, product)
                chosen[item.ingredientKey] = nil
                items.removeAll { $0.ingredientKey == item.ingredientKey }
                total = max(total - 1, 0)
                savedCount += 1
            } catch is CancellationError {
                return false
            } catch {
                saveError = message(error)
                return false
            }
        }
        return true
    }
}
