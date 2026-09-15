import SwiftUI

/// Synthetic menu data for SwiftUI previews: made-up recipes and example image URLs only.
/// Not DEBUG-only because `#Preview` bodies are type-checked in Release builds too.
enum MenuPreviewData {
    static let week = PlanPreviewData.week

    static func summary(
        id: String, name: String, headline: String?, minutes: Int?, calories: Int?, protein: Int?,
        image: String? = nil, tags: [String] = ["Quick"]
    ) -> RecipeSummary {
        RecipeSummary(
            id: id, name: name, headline: headline,
            imageURLString: image ?? "https://img.example.com/f_auto,q_auto,w_1200/\(id).jpg", totalMinutes: minutes,
            cookMinutes: minutes, timesOrdered: 2, lastOrderedWeek: "2026-W30", isAddon: false, tags: tags,
            householdRating: HouseholdRating(average: 4.5, count: 2), myRating: nil, calories: calories,
            proteinGrams: protein, timeBand: (minutes ?? 60) <= 20 ? .quick : .medium)
    }

    static let cards: [MenuCard] = [
        MenuCard(
            recipe: summary(
                id: "recipe-1", name: "Skillet Test Tacos", headline: "with Lime Crema", minutes: 30, calories: 690,
                protein: 36),
            badges: [MenuBadge(code: .makeAgain, text: "Make Again"), MenuBadge(code: .quick, text: "Quick")],
            reason: "Because you rated it 5 stars", inPlan: true, planEntryIDs: ["entry-1"]),
        MenuCard(
            recipe: summary(
                id: "recipe-3", name: "Placeholder Pasta Bake", headline: "with Roasted Peppers", minutes: 45,
                calories: 820, protein: 28),
            badges: [MenuBadge(code: MenuBadgeCode(rawValue: "chef_special"), text: "Chef's Pick")]),
        MenuCard(
            recipe: summary(
                id: "recipe-2", name: "Sample Garden Salad", headline: nil, minutes: 15, calories: 320, protein: 12),
            badges: [MenuBadge(code: .new, text: "New to You")]),
        MenuCard(
            recipe: summary(
                id: "recipe-4", name: "Example Smoked Pork", headline: "with Charred Corn", minutes: 240,
                calories: 940, protein: 52),
            badges: [MenuBadge(code: .smokerFriendly, text: "Smoker")]),
    ]

    static let sections: [MenuSection] = [
        MenuSection(
            id: "favorites", title: "Your Favorites", subtitle: "Based on what you liked before",
            items: Array(cards.prefix(3)), moreQuery: MenuRecipeQuery(sort: .popular)),
        MenuSection(
            id: "quick", title: "Quick & Easy", subtitle: "Ready in 20 minutes or less",
            items: Array(cards.suffix(2)), moreQuery: MenuRecipeQuery(maxMinutes: 20, sort: .quick)),
        MenuSection(id: "new_to_you", title: "New to You", items: cards.reversed()),
    ]

    static let menu = WeekMenu(
        week: week.description, weekStart: "2026-09-14", weekEnd: "2026-09-20", timing: .current,
        plan: PlanPreviewData.plan, proposal: nil, sections: sections)

    static let pastMenu = WeekMenu(
        week: "2026-W30", weekStart: "2026-07-20", weekEnd: "2026-07-26", timing: .past,
        plan: PlanPreviewData.plan, proposal: nil,
        sections: [
            MenuSection(
                id: "history_planned", kind: .history, title: "What You Planned", items: Array(cards.prefix(2)))
        ])

    static let proposalMenu = WeekMenu(
        week: week.description, weekStart: "2026-09-14", weekEnd: "2026-09-20", timing: .current,
        plan: PlanPreviewData.emptyPlan,
        proposal: MenuProposalSummary(id: "proposal-1", status: .proposed, version: 2, plannedMeals: 4),
        sections: sections)

    static let weeks: [WeekSummary] = {
        let current = week
        return (-4...4).map { offset in
            let value = current.adding(weeks: offset)
            return WeekSummary(
                week: value.description, timing: WeekTiming.of(value, current: current),
                plannedCount: offset <= 0 ? 4 : offset == 1 ? 2 : 0, cookedCount: offset < 0 ? 3 : 0,
                orderedCount: offset < 0 ? 1 : 0, status: offset < 0 ? .finalized : .draft)
        }
    }()

    static let filters = MenuFilterOptions(
        proteins: [
            MenuFilterOption(value: "chicken", label: "Chicken", count: 42),
            MenuFilterOption(value: "pork", label: "Pork", count: 18),
        ],
        cuisines: [
            MenuFilterOption(value: "mexican", label: "Mexican", count: 12),
            MenuFilterOption(value: "italian", label: "Italian", count: 9),
        ],
        tags: [MenuFilterOption(value: "kid friendly", label: "Kid Friendly", count: 7)])

    static let customizations = [
        CustomizationGroup(
            ingredientKey: "i-pork", ingredientName: "Ground Pork", amountText: "10 ounce",
            choices: [
                CustomizationChoice(
                    id: "original", label: "Ground Pork", ingredientName: "Ground Pork", amountText: "10 ounce",
                    kind: .original),
                CustomizationChoice(
                    id: "double", label: "2x Ground Pork", ingredientName: "Ground Pork", amountText: "20 ounce",
                    kind: .double, badge: "Double portion"),
                CustomizationChoice(
                    id: "chicken", label: "Chopped Chicken Breast", ingredientName: "Chicken Breast",
                    amountText: "10 ounce", kind: .swap),
                CustomizationChoice(
                    id: "beef", label: "Ground Beef", ingredientName: "Ground Beef", amountText: "10 ounce",
                    kind: .swap),
            ])
    ]

    static let pairings = [
        RecipePairing(
            target: .recipe(
                summary(
                    id: "addon-1", name: "Sample Garlic Bread", headline: "with Herb Butter", minutes: 10,
                    calories: 260, protein: 6)), reason: "Goes with pasta", inPlan: true),
        RecipePairing(
            target: .groceryItem(PairingGroceryItem(name: "Example Club Crackers", quantity: "1", unit: "box")),
            inPlan: false),
    ]
}

extension View {
    /// Every store the Menu and recipe screens read, with synthetic data.
    func menuPreviewEnvironment(
        plan: Plan? = PlanPreviewData.plan, menu: WeekMenu? = MenuPreviewData.menu, withProposal: Bool = false
    ) -> some View {
        modifier(MenuPreviewEnvironment(plan: plan, menu: menu, withProposal: withProposal))
    }
}

/// Builds the preview stores once per rendering and injects them.
struct MenuPreviewEnvironment: ViewModifier {
    let plan: Plan?
    let menu: WeekMenu?
    var withProposal = false

    func body(content: Content) -> some View {
        let session = HouseholdPreviewData.session()
        let households = HouseholdPreviewData.store(session: session)
        let library = RecipePreviewData.library(session: session)
        let plans = PlanStore.preview(session: session, plan: plan)
        let menuStore = MenuStore.preview(
            session: session, menu: menu, weeks: MenuPreviewData.weeks, allMeals: MenuPreviewData.cards,
            filters: MenuPreviewData.filters)
        return
            content
            .environment(session)
            .environment(households)
            .environment(library)
            .environment(plans)
            .environment(menuStore)
            .environment(MealPlanner(plans: plans, library: library, households: households))
            .environment(AutopilotPreviewData.store(session: session, withProposal: withProposal))
            .environment(EventReporter.preview(session: session))
            .environment(PantryPreviewData.store(session: session))
            .environment(NotificationPreviewData.store(session: session))
            .environment(SpecialtyPreviewData.store(session: session))
            .environment(ShopPreviewData.store(session: session))
    }
}
