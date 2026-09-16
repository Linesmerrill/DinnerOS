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

/// One week's grocery list screen: the computed list, local check-off state, the "Add to
/// pantry?" prompt for lines just checked off, and specialty ingredient actions (choosing an
/// option for a line, and "Made It" for a house-made batch).
///
/// The prompt never blocks shopping: confirming closes it at once and records the purchase
/// in the background, and checking off another line replaces an unanswered prompt. A failed
/// purchase is kept to try again with the same `clientPurchaseId`. "Don't ask during this
/// trip" lasts as long as this model, which is one visit to the list.
@Observable
final class GroceryListModel {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    /// "Add to pantry?" for a line just checked off.
    struct PurchasePrompt: Equatable, Identifiable {
        let item: GroceryItem
        var draft: PantryPurchaseDraft

        var id: String { draft.clientPurchaseID }
    }

    /// A purchase that couldn't be recorded.
    struct PurchaseFailure: Equatable, Identifiable {
        let prompt: PurchasePrompt
        let message: String
        /// The member's role doesn't allow pantry changes, so trying again won't help.
        let isForbidden: Bool

        var id: String { prompt.id }
    }

    /// A purchase just recorded, for a brief confirmation.
    struct RecordedPurchase: Equatable, Identifiable {
        let id: String
        let name: String
    }

    /// A specialty ingredient change made from the list.
    enum SpecialtyAction: Equatable {
        case choose(optionID: String, specialtyID: String, name: String)
        /// Keeps its `clientPurchaseID`, so trying again records one batch.
        case recordBatch(GroceryBatch, clientPurchaseID: String)

        var specialtyID: String {
            switch self {
            case .choose(_, let specialtyID, _): specialtyID
            case .recordBatch(let batch, _): batch.specialtyID
            }
        }

        var name: String {
            switch self {
            case .choose(_, _, let name): name
            case .recordBatch(let batch, _): batch.specialtyName
            }
        }
    }

    /// A specialty ingredient change that failed.
    struct SpecialtyFailure: Equatable, Identifiable {
        let id: String
        let action: SpecialtyAction
        let message: String
        /// The member's role doesn't allow the change, so trying again won't help.
        let isForbidden: Bool

        var title: String {
            switch action {
            case .choose(_, _, let name): String(localized: "Couldn't Choose an Option for \(name)")
            case .recordBatch(let batch, _): String(localized: "Couldn't Record \(batch.specialtyName)")
            }
        }
    }

    let householdID: String
    let week: ISOWeek
    private(set) var phase: Phase = .idle
    private(set) var list: GroceryList?
    /// Set when a reload failed while the list stayed on screen.
    private(set) var refreshError: String?
    private(set) var checked: Set<String>

    private(set) var purchasePrompt: PurchasePrompt?
    private(set) var purchaseFailure: PurchaseFailure?
    private(set) var lastRecordedPurchase: RecordedPurchase?
    private(set) var purchasesInFlight = 0
    private(set) var isSkippingPurchasePrompts = false
    /// Whether the member may change the pantry (`pantry.edit`), which also covers specialty
    /// ingredient choices and batches. The screen keeps it current; a `403` turns it off.
    private(set) var canAddToPantry: Bool

    /// Specialty ingredients with a change in flight, by ID.
    private(set) var specialtyActionsInFlight: Set<String> = []
    private(set) var specialtyFailure: SpecialtyFailure?
    /// A batch just recorded, for a brief confirmation. Its ID is the `clientPurchaseId`.
    private(set) var lastRecordedBatch: RecordedPurchase?

    /// The prompt's editable amount, for bindings. Writes for a replaced prompt are ignored.
    var purchaseDraft: PantryPurchaseDraft {
        get { purchasePrompt?.draft ?? PantryPurchaseDraft(clientPurchaseID: "") }
        set {
            guard purchasePrompt?.id == newValue.clientPurchaseID else { return }
            purchasePrompt?.draft = newValue
        }
    }

    /// Items not checked off yet.
    var remainingCount: Int {
        list?.allItems.count { !checked.contains($0.ingredientKey) } ?? 0
    }

    /// The list arranged for the screen: batches to make with their ingredients, then aisles.
    var layout: GroceryListLayout? {
        list.map(GroceryListLayout.init)
    }

    /// Whether Choose and Made It are offered. Hiding them is a convenience; the API enforces
    /// `pantry.edit`.
    var canChangeSpecialties: Bool {
        specialties != nil && canAddToPantry
    }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PlansAPI?
    @ObservationIgnored private let checks: any GroceryCheckStorage
    @ObservationIgnored private let purchases: (any PantryPurchaseRecording)?
    @ObservationIgnored private let specialties: (any SpecialtyChoosing)?
    /// A batch's `clientPurchaseId` until it's recorded, by specialty ID, so tapping Made It again
    /// after a failure can't record twice.
    @ObservationIgnored private var batchPurchaseIDs: [String: String] = [:]
    /// The specialty revision this list already reloaded for.
    @ObservationIgnored private var handledSpecialtyRevision: Int?

    private static let logger = Logger(subsystem: "DinnerOS", category: "grocery")

    init(
        householdID: String, week: ISOWeek, session: AuthSession, api: PlansAPI?, checks: any GroceryCheckStorage,
        purchases: (any PantryPurchaseRecording)? = nil, specialties: (any SpecialtyChoosing)? = nil,
        canAddToPantry: Bool = false
    ) {
        self.householdID = householdID
        self.week = week
        self.session = session
        self.api = api
        self.checks = checks
        self.purchases = purchases
        self.specialties = specialties
        self.canAddToPantry = canAddToPantry
        handledSpecialtyRevision = specialties?.revision
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

    /// Reads this device's check-offs again, for lines checked off elsewhere, such as the ones
    /// an order confirmed on the Shop tab covered.
    func reloadChecks() {
        checked = checks.checkedItems(householdID: householdID, week: week)
    }

    /// Checks or unchecks a line. Checking one off asks "Add to pantry?" when the member may
    /// change the pantry and hasn't turned the prompt off for this trip. A house-made item is
    /// already in the pantry, so it never asks.
    func toggle(_ item: GroceryItem) {
        if checked.contains(item.ingredientKey) {
            checked.remove(item.ingredientKey)
            if purchasePrompt?.item.ingredientKey == item.ingredientKey {
                purchasePrompt = nil
            }
        } else {
            checked.insert(item.ingredientKey)
            offerPurchase(for: item)
        }
        checks.setCheckedItems(checked, householdID: householdID, week: week)
    }

    func uncheckAll() {
        checked = []
        purchasePrompt = nil
        checks.setCheckedItems([], householdID: householdID, week: week)
    }

    // MARK: - Add to pantry

    func setCanAddToPantry(_ canAdd: Bool) {
        canAddToPantry = canAdd
        if !canAdd {
            purchasePrompt = nil
        }
    }

    /// Declines the prompt. Nothing is recorded.
    func skipPurchase() {
        purchasePrompt = nil
    }

    /// Declines this prompt and every later one until the list is left.
    func stopAskingThisTrip() {
        isSkippingPurchasePrompts = true
        purchasePrompt = nil
    }

    /// Closes the prompt and records its purchase. Returns when the request finishes.
    func confirmPurchase() async {
        guard let prompt = purchasePrompt, prompt.draft.isValid else { return }
        purchasePrompt = nil
        await submit(prompt)
    }

    /// Sends a failed purchase again with the same `clientPurchaseId`, so it's recorded once
    /// even if the first attempt reached the server.
    func retryPurchase() async {
        guard let failure = purchaseFailure, !failure.isForbidden else { return }
        purchaseFailure = nil
        await submit(failure.prompt)
    }

    func dismissPurchaseFailure() {
        purchaseFailure = nil
    }

    /// Hides the confirmation for `id`, unless a newer one replaced it.
    func dismissRecordedPurchase(id: String) {
        if lastRecordedPurchase?.id == id {
            lastRecordedPurchase = nil
        }
    }

    private func offerPurchase(for item: GroceryItem) {
        guard purchases != nil, canAddToPantry, !isSkippingPurchasePrompts, !item.isHouseMade else { return }
        purchasePrompt = PurchasePrompt(item: item, draft: PantryPurchaseDraft(groceryItem: item))
    }

    private func submit(_ prompt: PurchasePrompt) async {
        guard let purchases else { return }
        purchasesInFlight += 1
        defer { purchasesInFlight -= 1 }
        do {
            let purchase = try prompt.draft.groceryPurchase(for: prompt.item, week: week)
            try await purchases.recordPurchase(purchase, householdID: householdID)
            lastRecordedPurchase = RecordedPurchase(id: prompt.id, name: prompt.item.name)
            Self.logger.info("Grocery line added to the pantry")
        } catch is CancellationError {
            return
        } catch let error as APIError where error.status == 403 {
            Self.logger.notice("Grocery purchase forbidden")
            setCanAddToPantry(false)
            purchaseFailure = PurchaseFailure(
                prompt: prompt, message: HouseholdStore.message(for: error), isForbidden: true)
        } catch {
            Self.logger.notice("Grocery purchase failed")
            purchaseFailure = PurchaseFailure(
                prompt: prompt, message: HouseholdStore.message(for: error), isForbidden: false)
        }
    }

    // MARK: - Specialty ingredients

    func isChangingSpecialty(withID specialtyID: String) -> Bool {
        specialtyActionsInFlight.contains(specialtyID)
    }

    /// Chooses an option (or `SpecialtyChoice.asIsOptionID`) for a line, then reloads the list.
    func choose(optionID: String, for specialty: GrocerySpecialty) async {
        await perform(.choose(optionID: optionID, specialtyID: specialty.id, name: specialty.name))
    }

    /// Keeps a line by its own name and stops asking.
    func keepAsIs(_ specialty: GrocerySpecialty) async {
        await choose(optionID: SpecialtyChoice.asIsOptionID, for: specialty)
    }

    /// Records the batches the list asks for, then reloads the list.
    func recordBatch(_ batch: GroceryBatch) async {
        let clientPurchaseID = batchPurchaseIDs[batch.specialtyID] ?? UUID().uuidString
        batchPurchaseIDs[batch.specialtyID] = clientPurchaseID
        await perform(.recordBatch(batch, clientPurchaseID: clientPurchaseID))
    }

    /// Sends a failed change again; a batch keeps its `clientPurchaseId`. Takes the failure itself,
    /// because an alert's dismissal can clear `specialtyFailure` before its button's task runs.
    func retrySpecialtyAction(_ failure: SpecialtyFailure) async {
        guard !failure.isForbidden else { return }
        if specialtyFailure?.id == failure.id {
            specialtyFailure = nil
        }
        await perform(failure.action)
    }

    func dismissSpecialtyFailure() {
        specialtyFailure = nil
    }

    /// Hides the batch confirmation for `id`, unless a newer one replaced it.
    func dismissRecordedBatch(id: String) {
        if lastRecordedBatch?.id == id {
            lastRecordedBatch = nil
        }
    }

    /// Reloads after specialty ingredients changed elsewhere, such as the setup screen. A change
    /// this list made already reloaded it.
    func specialtiesDidChange() async {
        guard let specialties, specialties.revision != handledSpecialtyRevision else { return }
        handledSpecialtyRevision = specialties.revision
        await load()
    }

    private func perform(_ action: SpecialtyAction) async {
        guard let specialties, canAddToPantry else { return }
        let specialtyID = action.specialtyID
        guard !specialtyActionsInFlight.contains(specialtyID) else { return }
        specialtyActionsInFlight.insert(specialtyID)
        defer { specialtyActionsInFlight.remove(specialtyID) }
        do {
            switch action {
            case .choose(let optionID, let specialtyID, _):
                try await specialties.choose(
                    optionID: optionID, forSpecialtyWithID: specialtyID, householdID: householdID)
                Self.logger.info("Specialty chosen from the grocery list")
            case .recordBatch(let batch, let clientPurchaseID):
                try await specialties.recordBatch(
                    specialtyID: batch.specialtyID, optionID: batch.optionID, batches: max(batch.batches, 1),
                    clientPurchaseID: clientPurchaseID, householdID: householdID)
                batchPurchaseIDs[batch.specialtyID] = nil
                lastRecordedBatch = RecordedPurchase(id: clientPurchaseID, name: batch.specialtyName)
                Self.logger.info("Batch recorded from the grocery list")
            }
            handledSpecialtyRevision = specialties.revision
            await load()
        } catch is CancellationError {
            return
        } catch let error as APIError where error.status == 403 {
            Self.logger.notice("Specialty change forbidden")
            setCanAddToPantry(false)
            specialtyFailure = SpecialtyFailure(
                id: UUID().uuidString, action: action, message: HouseholdStore.message(for: error), isForbidden: true)
        } catch {
            Self.logger.notice("Specialty change failed")
            specialtyFailure = SpecialtyFailure(
                id: UUID().uuidString, action: action, message: HouseholdStore.message(for: error), isForbidden: false)
        }
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
    /// Make This Week
    /// - Southwest Spice Blend: Make a batch (makes about 12 tbsp). Needed for Chili Bowls
    ///
    /// Produce
    /// - [ ] Yellow Onion, 1 ½ + 8 oz
    /// - [x] Garlic, 2 cloves
    ///
    /// Spices
    /// - [ ] Salt (pantry staple)
    /// - [ ] Ground Cumin, 2 tbsp
    ///     to make Southwest Spice Blend (makes about 12 tbsp)
    /// ```
    ///
    /// Categories keep the server's aisle order. Skipped entries are listed at the end.
    static func make(
        _ list: GroceryList, week: ISOWeek, checked: Set<String>, locale: Locale = .autoupdatingCurrent
    ) -> String {
        var blocks = [String(localized: "Grocery List: \(week.rangeLabel(locale: locale))")]
        let toMake = list.batches.filter { $0.status == .make }
        if !toMake.isEmpty {
            let lines = toMake.map { batch in
                var line = "- \(batch.specialtyName): \(batch.text)"
                if let recipes = SpecialtyFormat.neededFor(batch.recipes, locale: locale) {
                    line += ". " + recipes
                }
                return line
            }
            blocks.append(([String(localized: "Make This Week")] + lines).joined(separator: "\n"))
        }
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
        case .inPantry where item.isHouseMade: text += " " + String(localized: "(in pantry, house-made)")
        case .inPantry: text += " " + String(localized: "(in pantry)")
        default: break
        }
        for via in item.via {
            text += "\n    " + via.text
        }
        for extra in item.extras {
            text += "\n    " + extra.text
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
