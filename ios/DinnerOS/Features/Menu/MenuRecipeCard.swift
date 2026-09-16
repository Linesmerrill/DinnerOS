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
                    title
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

    /// Shared with the carousels that warm the next few photos, so a prefetch asks the CDN for
    /// the same size the card will.
    static func carouselWidth(for dynamicTypeSize: DynamicTypeSize) -> CGFloat {
        dynamicTypeSize.isAccessibilitySize ? 300 : 220
    }

    /// What a full-width card's photo is asked for before the layout measures it; `RecipePhoto`
    /// refines it from the real frame.
    static let fullWidthEstimate: CGFloat = 400

    private var carouselWidth: CGFloat {
        MenuRecipeCard.carouselWidth(for: dynamicTypeSize)
    }

    private var photo: some View {
        RecipePhoto(
            url: card.recipe.imageURL, aspectRatio: size == .carousel ? 4.0 / 3.0 : 16.0 / 10.0,
            pointWidth: size == .carousel ? carouselWidth : MenuRecipeCard.fullWidthEstimate
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
        .overlay(alignment: .topLeading) {
            if let contextBadge {
                PhotoBadge(
                    text: contextBadge.text, systemImage: contextBadge.code.systemImage,
                    isProminent: contextBadge.code == .autopilotPick
                )
                .padding(8)
                // Spoken as part of the card's label instead.
                .accessibilityHidden(true)
            }
        }
        .overlay(alignment: .bottomLeading) {
            if let minutes = card.recipe.displayMinutes, minutes > 0 {
                TimeBadge(minutes: minutes, isQuick: isQuick)
                    .padding(8)
                    .accessibilityHidden(true)
            }
        }
    }

    /// The name is all a card says now: what it is and how long it takes, and the rest is on
    /// the recipe screen.
    private var title: some View {
        CardTitle(name: card.recipe.name)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(
                MenuFormat.cardAccessibilityLabel(
                    name: card.recipe.name, minutes: card.recipe.displayMinutes, isQuick: isQuick,
                    badge: contextBadge?.text, isAddOn: card.recipe.isAddon, inPlan: card.inPlan))
    }

    private var contextBadge: MenuBadge? { MenuCardBadge.context(in: card.badges) }

    private var isQuick: Bool { card.recipe.timeBand == .quick }

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

/// Mixed name lengths, a card with no cook time, and one with no photo — every card in the row
/// has to be the same height.
#Preview("Equal heights") {
    NavigationStack {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                        .border(.red.opacity(0.4))
                }
            }
            .padding()
        }
    }
    .menuPreviewEnvironment()
}

#Preview("Accessibility size") {
    NavigationStack {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                        .border(.red.opacity(0.4))
                }
            }
            .padding()
        }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}
