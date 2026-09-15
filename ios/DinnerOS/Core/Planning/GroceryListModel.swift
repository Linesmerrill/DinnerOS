import Foundation
import Observation
import os

/// Which grocery items are checked off, per household and week, on this device.
///
/// Local until saved lists with shared checked state arrive (Phase 7). Items are keyed
/// by `ingredientKey`, which the grocery list aggregates by.
protocol GroceryCheckStorage: AnyObject {
    func checkedItems(householdID: String, week: ISOWeek) -> Set<String>
    func setCheckedItems(_ keys: Set<String>, householdID: String, week: ISOWeek)
}

final class UserDefaultsGroceryChecks: GroceryCheckStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func checkedItems(householdID: String, week: ISOWeek) -> Set<String> {
        Set(defaults.stringArray(forKey: Self.key(householdID: householdID, week: week)) ?? [])
    }

    func setCheckedItems(_ keys: Set<String>, householdID: String, week: ISOWeek) {
        let key = Self.key(householdID: householdID, week: week)
        if keys.isEmpty {
            defaults.removeObject(forKey: key)
        } else {
            defaults.set(keys.sorted(), forKey: key)
        }
    }

    static func key(householdID: String, week: ISOWeek) -> String {
        "groceryChecks.\(householdID).\(week.description)"
    }
}

/// Non-persistent checks for tests and previews.
final class InMemoryGroceryChecks: GroceryCheckStorage {
    private var storage: [String: Set<String>] = [:]

    func checkedItems(householdID: String, week: ISOWeek) -> Set<String> {
        storage["\(householdID).\(week.description)"] ?? []
    }

    func setCheckedItems(_ keys: Set<String>, householdID: String, week: ISOWeek) {
        storage["\(householdID).\(week.description)"] = keys
    }
}

/// One week's grocery list screen: the computed list and local check-off state.
@Observable
final class GroceryListModel {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    let householdID: String
    let week: ISOWeek
    private(set) var phase: Phase = .idle
    private(set) var list: GroceryList?
    /// Set when a reload failed while the list stayed on screen.
    private(set) var refreshError: String?
    private(set) var checked: Set<String>

    /// Items not checked off yet.
    var remainingCount: Int {
        list?.allItems.count { !checked.contains($0.ingredientKey) } ?? 0
    }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PlansAPI?
    @ObservationIgnored private let checks: any GroceryCheckStorage

    private static let logger = Logger(subsystem: "DinnerOS", category: "grocery")

    init(
        householdID: String, week: ISOWeek, session: AuthSession, api: PlansAPI?, checks: any GroceryCheckStorage
    ) {
        self.householdID = householdID
        self.week = week
        self.session = session
        self.api = api
        self.checks = checks
        checked = checks.checkedItems(householdID: householdID, week: week)
    }

    /// A model frozen with `list`, for SwiftUI previews.
    static func preview(session: AuthSession, list: GroceryList, checked: Set<String> = []) -> GroceryListModel {
        let week = ISOWeek(list.week) ?? .current(in: .gmt)
        let checks = InMemoryGroceryChecks()
        checks.setCheckedItems(checked, householdID: "household-preview", week: week)
        let model = GroceryListModel(
            householdID: "household-preview", week: week, session: session, api: nil, checks: checks)
        model.list = list
        model.phase = .loaded
        return model
    }

    func load() async {
        guard let api else { return }
        let householdID = householdID
        let week = week
        if list == nil {
            phase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.groceryList(householdID: householdID, week: week, accessToken: token)
            }
            list = loaded
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if phase == .loading { phase = .idle }
        } catch {
            Self.logger.notice("Grocery list failed")
            let message = HouseholdStore.message(for: error)
            if list != nil {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    func isChecked(_ item: GroceryItem) -> Bool {
        checked.contains(item.ingredientKey)
    }

    func toggle(_ item: GroceryItem) {
        if checked.contains(item.ingredientKey) {
            checked.remove(item.ingredientKey)
        } else {
            checked.insert(item.ingredientKey)
        }
        checks.setCheckedItems(checked, householdID: householdID, week: week)
    }

    func uncheckAll() {
        checked = []
        checks.setCheckedItems([], householdID: householdID, week: week)
    }

    /// The list as plain text for sharing, or `nil` before it loads.
    func plainText(locale: Locale = .autoupdatingCurrent) -> String? {
        list.map { GroceryListText.make($0, week: week, checked: checked, locale: locale) }
    }
}

/// Plain-text export of a grocery list, for Notes, Messages, or a printout.
nonisolated enum GroceryListText {
    /// For example:
    ///
    /// ```text
    /// Grocery List: Sep 14 – 20
    ///
    /// Produce
    /// - [ ] Yellow Onion, 1 ½ + 8 oz
    /// - [x] Garlic, 2 cloves
    ///
    /// Spices
    /// - [ ] Salt (pantry staple)
    /// ```
    ///
    /// Categories keep the server's aisle order. Skipped entries are listed at the end.
    static func make(
        _ list: GroceryList, week: ISOWeek, checked: Set<String>, locale: Locale = .autoupdatingCurrent
    ) -> String {
        var blocks = [String(localized: "Grocery List: \(week.rangeLabel(locale: locale))")]
        for category in list.categories where !category.items.isEmpty {
            let lines = category.items.map { line(for: $0, checked: checked.contains($0.ingredientKey)) }
            blocks.append(([category.title] + lines).joined(separator: "\n"))
        }
        if list.isEmpty {
            blocks.append(String(localized: "Nothing to buy."))
        }
        if !list.skipped.isEmpty {
            let lines = list.skipped.map { "- \($0.recipeName): \($0.reason.explanation)" }
            blocks.append(([String(localized: "Not included")] + lines).joined(separator: "\n"))
        }
        return blocks.joined(separator: "\n\n")
    }

    static func line(for item: GroceryItem, checked: Bool) -> String {
        var text = (checked ? "- [x] " : "- [ ] ") + item.name
        if let amount = amountText(for: item) {
            text += ", " + amount
        }
        switch item.status {
        case .pantryHint: text += " " + String(localized: "(pantry staple)")
        case .inPantry: text += " " + String(localized: "(in pantry)")
        default: break
        }
        return text
    }

    /// The quantity to show, with "as needed" when a recipe gave no amount. `nil` when
    /// there's nothing to show.
    static func amountText(for item: GroceryItem) -> String? {
        switch (item.quantityText.isEmpty, item.unquantified) {
        case (true, _): nil
        case (false, true): String(localized: "\(item.quantityText) + as needed")
        case (false, false): item.quantityText
        }
    }
}
