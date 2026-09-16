import Foundation

/// How many of a week's plan entries are meals, and how many are add-ons.
///
/// A pairing's add-on — garlic bread alongside the pasta — is a plan entry like any other, so
/// counting entries made a week of five dinners and one add-on read "6 meals this week". Every
/// place that says "meals" counts mains only and names the add-ons separately.
nonisolated struct MealCounts: Equatable, Sendable {
    var meals = 0
    var addOns = 0
    /// Main meals Autopilot added. An add-on never counts toward this, even when accepting an
    /// Autopilot proposal is what brought it into the week.
    var mealsFromAutopilot = 0

    init(meals: Int = 0, addOns: Int = 0, mealsFromAutopilot: Int = 0) {
        self.meals = meals
        self.addOns = addOns
        self.mealsFromAutopilot = mealsFromAutopilot
    }

    /// Every entry in the week, add-ons included.
    var total: Int { meals + addOns }

    /// Counts `entries`, asking `isAddOn` about each one.
    ///
    /// The question is asked rather than answered here because whether an entry is an add-on
    /// comes from two places: the entry itself on a current server, and the menu's card for
    /// that recipe on an older one.
    static func of(_ entries: [PlanEntry], isAddOn: (PlanEntry) -> Bool) -> MealCounts {
        var counts = MealCounts()
        for entry in entries {
            if isAddOn(entry) {
                counts.addOns += 1
                continue
            }
            counts.meals += 1
            if entry.isFromAutopilot {
                counts.mealsFromAutopilot += 1
            }
        }
        return counts
    }
}
