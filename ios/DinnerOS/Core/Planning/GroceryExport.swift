import Foundation
import os

#if canImport(EventKit)
    import EventKit
#endif

/// One reminder to create for a grocery line.
nonisolated struct GroceryReminderDraft: Equatable, Sendable {
    /// For example "2 lb Ground Beef".
    let title: String
    /// The aisle, so the list still says where the item is once Reminders sorts it.
    let notes: String?
}

/// Whether the app may write to Reminders.
nonisolated enum GroceryRemindersAccess: Equatable, Sendable {
    case notDetermined
    case granted
    case denied
}

/// What to do when a list with the week's name is already there.
nonisolated enum GroceryRemindersMerge: Equatable, Sendable {
    /// Empty the list first, so the week's items are the only ones in it.
    case replace
    /// Leave what's there and append.
    case add
}

/// The reminders database the export writes to.
///
/// `EventKitRemindersStore` is the real one. Tests inject their own, so they never touch the
/// member's Reminders.
@MainActor
protocol GroceryRemindersStore: AnyObject, Sendable {
    var access: GroceryRemindersAccess { get }
    /// Asks the member, and reports what they chose.
    func requestAccess() async throws -> GroceryRemindersAccess
    /// The ID of the reminder list titled `title`, or `nil` when there isn't one.
    func listID(titled title: String) throws -> String?
    /// Creates a reminder list and returns its ID.
    func makeList(titled title: String) throws -> String
    /// Removes every reminder in the list, for `replace`.
    func clearList(withID id: String) async throws
    func addReminders(_ drafts: [GroceryReminderDraft], toListWithID id: String) throws
}

/// Why an export couldn't finish.
nonisolated enum GroceryRemindersError: Error, Equatable {
    /// The member said no, or Reminders is restricted. Settings is the only way back.
    case accessDenied
    /// Nothing left to add: every line is checked off.
    case nothingToAdd
}

/// Builds the week's Reminders list: what it's called, and one reminder per unchecked line.
///
/// The titles reuse `GroceryListText.amountText`, so a reminder reads the way the grocery
/// list's own line does.
nonisolated enum GroceryReminderPlan {
    /// For example "DinnerOS · Sep 14–20".
    static func listName(appName: String, week: ISOWeek, locale: Locale = .autoupdatingCurrent) -> String {
        "\(appName) · \(week.rangeLabel(locale: locale))"
    }

    /// The lines still to buy, in the server's aisle order, so Reminders shows them grouped by
    /// aisle without needing sublists.
    static func drafts(for list: GroceryList, checked: Set<String>) -> [GroceryReminderDraft] {
        let layout = GroceryListLayout(list)
        var drafts: [GroceryReminderDraft] = []
        for group in layout.toMake {
            let aisle = String(localized: "Make This Week")
            drafts += group.ingredients.filter { needsBuying($0, checked: checked) }
                .map { draft(for: $0, aisle: aisle) }
        }
        for category in layout.categories {
            drafts += category.items.filter { needsBuying($0, checked: checked) }
                .map { draft(for: $0, aisle: category.title) }
        }
        return drafts
    }

    /// Whether a line belongs in Reminders: not checked off, and not one the pantry already
    /// has.
    ///
    /// A reminder is an instruction to buy something, and it carries no status of its own, so
    /// an `inPantry` line would read as "buy this" even though the list says "in your pantry"
    /// and the shared text says "(in pantry)". A `pantryHint` is only the recipe's guess that
    /// it's a staple the household keeps, so it stays.
    private static func needsBuying(_ item: GroceryItem, checked: Set<String>) -> Bool {
        item.status != .inPantry && !checked.contains(item.ingredientKey)
    }

    private static func draft(for item: GroceryItem, aisle: String) -> GroceryReminderDraft {
        let amount = GroceryListText.amountText(for: item)
        let title = amount.map { "\($0) \(item.name)" } ?? item.name
        return GroceryReminderDraft(title: title, notes: aisle)
    }
}

/// Sends a week's grocery list to Reminders.
nonisolated struct GroceryRemindersExport: Sendable {
    let store: any GroceryRemindersStore

    private static let logger = Logger(subsystem: "DinnerOS", category: "grocery-export")

    /// Whether a list named `name` is already there, so the caller can offer Replace or Add.
    /// Returns `false` without asking for access, because that answer needs it.
    @MainActor
    func hasExistingList(named name: String) throws -> Bool {
        guard store.access == .granted else { return false }
        return try store.listID(titled: name) != nil
    }

    /// Asks for access when it hasn't been asked for yet.
    @MainActor
    func requestAccess() async throws -> GroceryRemindersAccess {
        guard store.access != .granted else { return .granted }
        return try await store.requestAccess()
    }

    /// Adds `drafts` to the list named `name`, creating it when it isn't there. Returns how
    /// many reminders were added.
    @MainActor
    @discardableResult
    func export(_ drafts: [GroceryReminderDraft], to name: String, merge: GroceryRemindersMerge) async throws -> Int {
        guard !drafts.isEmpty else { throw GroceryRemindersError.nothingToAdd }
        guard try await requestAccess() == .granted else { throw GroceryRemindersError.accessDenied }

        let listID: String
        if let existing = try store.listID(titled: name) {
            listID = existing
            if merge == .replace {
                try await store.clearList(withID: existing)
            }
        } else {
            listID = try store.makeList(titled: name)
        }
        try store.addReminders(drafts, toListWithID: listID)
        Self.logger.info("Exported \(drafts.count, privacy: .public) grocery lines to Reminders")
        return drafts.count
    }
}

#if canImport(EventKit)
    /// The real Reminders database, through EventKit.
    @MainActor
    final class EventKitRemindersStore: GroceryRemindersStore {
        private let eventStore: EKEventStore

        init(eventStore: EKEventStore = EKEventStore()) {
            self.eventStore = eventStore
        }

        var access: GroceryRemindersAccess {
            switch EKEventStore.authorizationStatus(for: .reminder) {
            case .fullAccess: .granted
            case .notDetermined: .notDetermined
            default: .denied
            }
        }

        func requestAccess() async throws -> GroceryRemindersAccess {
            let granted = try await eventStore.requestFullAccessToReminders()
            return granted ? .granted : .denied
        }

        func listID(titled title: String) throws -> String? {
            eventStore.calendars(for: .reminder)
                .first { $0.title == title && !$0.isImmutable }?
                .calendarIdentifier
        }

        func makeList(titled title: String) throws -> String {
            let calendar = EKCalendar(for: .reminder, eventStore: eventStore)
            calendar.title = title
            // A new list needs a writable source; the one new reminders already go to is the
            // member's own choice, so it's the least surprising home for ours.
            guard
                let source = eventStore.defaultCalendarForNewReminders()?.source
                    ?? eventStore.sources.first(where: { $0.sourceType == .local })
            else { throw GroceryRemindersError.accessDenied }
            calendar.source = source
            try eventStore.saveCalendar(calendar, commit: true)
            return calendar.calendarIdentifier
        }

        func clearList(withID id: String) async throws {
            guard let calendar = eventStore.calendar(withIdentifier: id) else { return }
            let predicate = eventStore.predicateForReminders(in: [calendar])
            // Only the identifiers cross the callback boundary: `EKReminder` isn't `Sendable`,
            // so handing the objects themselves back would be a data race.
            let identifiers: [String] = await withCheckedContinuation { continuation in
                eventStore.fetchReminders(matching: predicate) { reminders in
                    continuation.resume(returning: (reminders ?? []).map(\.calendarItemIdentifier))
                }
            }
            for identifier in identifiers {
                guard let reminder = eventStore.calendarItem(withIdentifier: identifier) as? EKReminder else {
                    continue
                }
                try eventStore.remove(reminder, commit: false)
            }
            try eventStore.commit()
        }

        func addReminders(_ drafts: [GroceryReminderDraft], toListWithID id: String) throws {
            guard let calendar = eventStore.calendar(withIdentifier: id) else {
                throw GroceryRemindersError.accessDenied
            }
            for draft in drafts {
                let reminder = EKReminder(eventStore: eventStore)
                reminder.title = draft.title
                reminder.notes = draft.notes
                reminder.calendar = calendar
                try eventStore.save(reminder, commit: false)
            }
            try eventStore.commit()
        }
    }
#endif
