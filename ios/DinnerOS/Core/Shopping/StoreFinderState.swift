import Foundation
import Observation

/// The store finder's ZIP code, results, and whether the list is open: typed or filled in from
/// where the member is, searched, then folded down to the picked store until tapped again.
@MainActor
@Observable
final class StoreFinderState {
    /// What the member typed, kept to five digits.
    var zip = "" {
        didSet {
            let filtered = ZIPCode.filtered(zip)
            if filtered != zip { zip = filtered }
        }
    }
    private(set) var stores: [ShoppingStoreLocation] = []
    private(set) var isSearching = false
    private(set) var isLocating = false
    var errorMessage: String?
    /// Location is off for the app, so the message offers Settings.
    private(set) var locationDenied = false
    /// The ZIP field and results are showing. Picking a store folds them away.
    var isExpanded = true
    /// A search has run since the screen opened.
    private(set) var hasSearched = false

    var isValidZIP: Bool { ZIPCode.isValid(zip) }
    var isBusy: Bool { isSearching || isLocating }

    /// Starts with the household's saved ZIP code, folded to the saved store when there is one.
    func start(savedZIP: String?, hasStore: Bool) {
        if zip.isEmpty, let saved = ZIPCode.normalized(savedZIP) { zip = saved }
        isExpanded = !hasStore
    }

    /// Folds the list down to the picked store.
    func didPick() {
        isExpanded = false
        errorMessage = nil
    }

    /// Searches near the typed ZIP code. `remember` saves it to the household after a search
    /// that found stores.
    func find(
        using search: (String) async throws -> [ShoppingStoreLocation],
        remember: (String) async -> Void,
        storeName: String,
        message: (any Error) -> String
    ) async {
        guard isValidZIP, !isSearching else { return }
        isSearching = true
        defer { isSearching = false }
        errorMessage = nil
        locationDenied = false
        let zip = zip
        do {
            let found = try await search(zip)
            stores = found
            hasSearched = true
            isExpanded = true
            if found.isEmpty {
                errorMessage = String(localized: "No \(storeName) stores near \(zip).")
            } else {
                await remember(zip)
            }
        } catch is CancellationError {
            return
        } catch {
            errorMessage = message(error)
        }
    }

    /// Fills in the ZIP code from where the member is. False, with a message, when location is
    /// off or can't be found; typing still works.
    func fillZIP(with locator: any ZIPLocating) async -> Bool {
        guard !isBusy else { return false }
        isLocating = true
        defer { isLocating = false }
        errorMessage = nil
        locationDenied = false
        do {
            zip = try await locator.currentZIP()
            return true
        } catch ZIPLocatorError.denied {
            locationDenied = true
            errorMessage = String(
                localized: "Location is off for this app. Turn it on in Settings, or type your ZIP code.")
        } catch is CancellationError {
            return false
        } catch {
            errorMessage = String(localized: "Couldn't find your ZIP code. Type it instead.")
        }
        return false
    }
}
