import Foundation

/// The household settings as the member has them on screen, which may be ahead of the server.
///
/// It is the member's copy: a server answer never replaces a field the member has changed and
/// not yet had confirmed, and never a field that is still on its way to the server
/// (`rebase(from:to:keeping:)`). Only fields someone else changed are brought in.
nonisolated struct HouseholdSettingsDraft: Equatable, Sendable {
    var name: String
    var timeZone: String
    var defaultServings: Int
    /// The API's weekday code, or "" for no order reminder.
    var orderDay: String
    var weekStartsOn: PlanDay
    /// The local hour thaw reminders go out.
    var thawReminderHour: Int
    /// What a week of meal kits cost, as typed; empty turns the comparison off.
    var mealKitAmount: String
    var mealKitMeals: Int

    /// The meal count offered before a household has saved a meal kit.
    static let defaultMealKitMeals = 5

    init(_ household: Household) {
        name = household.name
        timeZone = household.timeZone
        defaultServings = household.defaultServings
        orderDay = household.orderDay ?? ""
        weekStartsOn = household.weekStartsOn
        thawReminderHour = household.thawReminderHour
        mealKitAmount = household.mealKit.map { MoneyText.editingText($0.weeklyCents) } ?? ""
        mealKitMeals = household.mealKit?.meals ?? Self.defaultMealKitMeals
    }

    var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Why the name isn't being saved, or `nil` when it can be.
    var nameError: String? {
        trimmedName.isEmpty
            ? String(localized: "Enter a name to save it. The household keeps its old name until then.") : nil
    }

    /// Why the meal kit amount isn't being saved, or `nil` when it can be.
    var mealKitError: String? {
        MealKitForm.error(mealKitAmount)
    }

    var hasValidationError: Bool {
        nameError != nil || mealKitError != nil
    }

    /// Only the fields that differ from `base`, so a save never overwrites someone else's edit
    /// to a field this member didn't touch. A field that isn't valid yet (an empty name, an
    /// amount that doesn't parse) is left out rather than holding back the others.
    func changes(against base: Household) -> HouseholdChanges {
        HouseholdChanges(
            name: nameError == nil && trimmedName != base.name ? trimmedName : nil,
            timeZone: timeZone == base.timeZone ? nil : timeZone,
            defaultServings: defaultServings == base.defaultServings ? nil : defaultServings,
            // "" clears the order day; nil would leave it alone.
            orderDay: orderDay == (base.orderDay ?? "") ? nil : orderDay,
            mealKit: MealKitForm.change(amountText: mealKitAmount, meals: mealKitMeals, current: base.mealKit) ?? .keep,
            weekStartsOn: weekStartsOn == base.weekStartsOn ? nil : weekStartsOn,
            thawReminderHour: thawReminderHour == base.thawReminderHour ? nil : thawReminderHour)
    }

    /// Brings in what changed on the server between `old` and `new`, field by field, but only
    /// where the member's copy still shows `old`'s value and the field isn't in `inFlight`.
    ///
    /// The `inFlight` exclusion is the stale-response rule for a field the member is still
    /// changing: while a save of servings 3 is out, the member may step back to 2 — which equals
    /// `old` — and the answer carrying 3 must not put 3 back on screen.
    mutating func rebase(from old: Household, to new: Household, keeping inFlight: HouseholdChanges?) {
        let sent = inFlight ?? HouseholdChanges()
        if sent.name == nil, new.name != old.name, trimmedName == old.name {
            name = new.name
        }
        if sent.timeZone == nil, new.timeZone != old.timeZone, timeZone == old.timeZone {
            timeZone = new.timeZone
        }
        if sent.defaultServings == nil, new.defaultServings != old.defaultServings,
            defaultServings == old.defaultServings
        {
            defaultServings = new.defaultServings
        }
        let oldOrderDay = old.orderDay ?? ""
        if sent.orderDay == nil, (new.orderDay ?? "") != oldOrderDay, orderDay == oldOrderDay {
            orderDay = new.orderDay ?? ""
        }
        if sent.weekStartsOn == nil, new.weekStartsOn != old.weekStartsOn, weekStartsOn == old.weekStartsOn {
            weekStartsOn = new.weekStartsOn
        }
        if sent.thawReminderHour == nil, new.thawReminderHour != old.thawReminderHour,
            thawReminderHour == old.thawReminderHour
        {
            thawReminderHour = new.thawReminderHour
        }
        if sent.mealKit == .keep, new.mealKit != old.mealKit,
            MealKitForm.change(amountText: mealKitAmount, meals: mealKitMeals, current: old.mealKit) == .keep
        {
            mealKitAmount = new.mealKit.map { MoneyText.editingText($0.weeklyCents) } ?? ""
            mealKitMeals = new.mealKit?.meals ?? mealKitMeals
        }
    }

    /// The settings `changes` names, for saying what didn't save.
    static func fieldNames(_ changes: HouseholdChanges) -> [String] {
        var names: [String] = []
        if changes.name != nil { names.append(String(localized: "name")) }
        if changes.timeZone != nil { names.append(String(localized: "time zone")) }
        if changes.defaultServings != nil { names.append(String(localized: "default servings")) }
        if changes.weekStartsOn != nil { names.append(String(localized: "week start")) }
        if changes.orderDay != nil { names.append(String(localized: "order day")) }
        if changes.thawReminderHour != nil { names.append(String(localized: "thaw reminder time")) }
        if changes.mealKit != .keep { names.append(String(localized: "meal kit comparison")) }
        return names
    }
}
