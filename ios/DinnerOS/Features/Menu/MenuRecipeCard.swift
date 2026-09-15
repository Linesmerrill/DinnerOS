import SwiftUI

/// A recipe on the Menu screen: a big photo, the name, one line of headline, the facts row,
/// and badges. The photo carries a `+` that adds the recipe to the shown week (`✓` once it's
/// there); full-width cards use an Add button under the text instead.
struct MenuRecipeCard: View {
    enum Size {
        /// In a carousel, a fixed width with the next card peeking.
        case carousel
        /// One per row in All Meals.
        case fullWidth
    }

    let card: MenuCard
    var size: Size = .carousel
    /// Past weeks and members without `plan.edit` see cards without the add control.
    var canAdd = true

    @Environment(MealPlanner.self) private var planner
    @Environment(PlanStore.self) private var plans
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isBusy: Bool { planner.busyRecipeIDs.contains(card.recipe.id) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            NavigationLink(value: card.recipe) {
                VStack(alignment: .leading, spacing: 8) {
                    photo
                    text
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if size == .fullWidth, canAdd {
                addButton
                    .padding(.top, 10)
            }
        }
        .frame(width: size == .carousel ? carouselWidth : nil, alignment: .leading)
        .accessibilityElement(children: .contain)
    }

    private var carouselWidth: CGFloat {
        dynamicTypeSize.isAccessibilitySize ? 300 : 220
    }

    private var photo: some View {
        RecipePhoto(
            url: card.recipe.imageURL, aspectRatio: size == .carousel ? 4.0 / 3.0 : 16.0 / 10.0,
            pointWidth: size == .carousel ? carouselWidth : 400
        )
        .overlay(alignment: .topTrailing) {
            if size == .carousel, canAdd {
                AddToWeekButton(
                    isInPlan: card.inPlan, isBusy: isBusy, recipeName: card.recipe.name, action: toggle
                )
                .contextMenu { dayMenuItems }
                .padding(4)
            }
        }
    }

    private var text: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(card.recipe.name)
                .font(.headline)
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if let headline = card.recipe.headline, !headline.isEmpty {
                Text(headline)
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                    .lineLimit(1)
            }
            FactsRow(summary: card.recipe)
            if let reason = card.reason {
                Text(reason)
                    .font(.caption)
                    .foregroundStyle(.tint)
                    .lineLimit(2)
            }
            BadgeRow(badges: card.badges)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            MenuFormat.cardAccessibilityLabel(
                name: card.recipe.name, minutes: card.recipe.displayMinutes, calories: card.recipe.calories,
                proteinGrams: card.recipe.proteinGrams, inPlan: card.inPlan))
    }

    private var addButton: some View {
        HStack(spacing: 12) {
            Button(action: toggle) {
                if isBusy {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                } else {
                    Label(
                        card.inPlan ? String(localized: "In Your Week") : String(localized: "Add"),
                        systemImage: card.inPlan ? "checkmark" : "plus"
                    )
                    .frame(maxWidth: .infinity)
                }
            }
            .buttonStyle(.bordered)
            .tint(card.inPlan ? Color.accentColor : Color.accentColor)
            .disabled(isBusy)
            .accessibilityLabel(
                card.inPlan
                    ? Text("Remove \(card.recipe.name) from your week")
                    : Text("Add \(card.recipe.name) to your week")
            )
            .contextMenu { dayMenuItems }
        }
    }

    /// Long-pressing the add control picks a day instead of leaving the meal unscheduled.
    @ViewBuilder
    private var dayMenuItems: some View {
        if !card.inPlan {
            ForEach(PlanDay.allCases) { day in
                Button(day.title(in: plans.week)) {
                    add(day: day)
                }
            }
        }
    }

    private func toggle() {
        if card.inPlan {
            remove()
        } else {
            add(day: nil)
        }
    }

    private func add(day: PlanDay?) {
        Task {
            await planner.add(recipeID: card.recipe.id, name: card.recipe.name, to: plans.week, day: day)
        }
    }

    private func remove() {
        // The card knows its entries; the last one added is the one a second tap removes.
        guard let entry = plans.plan?.entries.last(where: { $0.recipe.id == card.recipe.id }) else { return }
        Task { await planner.remove(entry) }
    }
}

#Preview("Carousel") {
    NavigationStack {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                }
            }
            .padding()
        }
    }
    .menuPreviewEnvironment()
}

#Preview("Full width, dark") {
    NavigationStack {
        ScrollView {
            VStack(spacing: 24) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card, size: .fullWidth)
                }
            }
            .padding()
        }
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}

#Preview("Accessibility size") {
    NavigationStack {
        ScrollView {
            VStack(spacing: 24) {
                MenuRecipeCard(card: MenuPreviewData.cards[0], size: .fullWidth)
                MenuRecipeCard(card: MenuPreviewData.cards[1])
            }
            .padding()
        }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}
