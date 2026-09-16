import Foundation

/// The household settings' meal kit fields: a weekly amount and meals per week.
nonisolated enum MealKitForm {
    /// Why the amount can't be saved, or `nil` when it's empty or valid.
    static func error(_ amountText: String) -> String? {
        let trimmed = amountText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        guard let cents = MoneyText.cents(from: trimmed), MealKitInput.weeklyCentsRange.contains(cents) else {
            return String(
                localized:
                    "Enter a weekly amount from $0.01 to \(MoneyText.format(MealKitInput.weeklyCentsRange.upperBound))."
            )
        }
        return nil
    }

    /// What saving does: nothing when unchanged, `.clear` for an emptied amount, `.set`
    /// otherwise. `nil` while the amount or meal count isn't valid.
    static func change(amountText: String, meals: Int, current: MealKitBaseline?) -> FieldChange<MealKitInput>? {
        let trimmed = amountText.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return current == nil ? .keep : .clear
        }
        guard error(trimmed) == nil, let cents = MoneyText.cents(from: trimmed),
            MealKitInput.mealsRange.contains(meals)
        else { return nil }
        if let current, current.weeklyCents == cents, current.meals == meals {
            return .keep
        }
        return .set(MealKitInput(weeklyCents: cents, meals: meals))
    }
}
