import SwiftUI

/// A recipe on the Menu screen: a big photo, the name, one line of headline, the facts row,
/// and badges. The photo carries a `+` that adds the recipe to the shown week (`✓` once it's
/// there); full-width cards use an Add button under the text instead.
struct MenuRecipeCard: View {
    nonisolated enum Size: Sendable {
        /// In a carousel, a fixed width with the next card peeking.
        case carousel
        /// One per row in All Meals.
        case fullWidth
        /// One line of All Meals while the search keyboard is up: a small photo beside the
        /// name, time and Add, so a whole result fits above the keyboard (Hark's review item 7).
        case searchRow
    }

    let card: MenuCard
    var size: Size = .carousel
    /// Past weeks and members without `plan.edit` see cards without the add control.
    var canAdd = true

    @Environment(MealPlanner.self) private var planner
    @Environment(PlanStore.self) private var plans
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var deleting: RecipeSummary?

    private var isBusy: Bool { planner.busyRecipeIDs.contains(card.recipe.id) }

    var body: some View {
        if size == .searchRow {
            searchRow
        } else {
            cardBody
        }
    }

    private var cardBody: some View {
        VStack(alignment: .leading, spacing: 0) {
            NavigationLink(value: card.recipe) {
                VStack(alignment: .leading, spacing: DS.Space.s) {
                    photo
                    title
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .contextMenu {
                if households.access?.can(.recipesEdit) == true {
                    Button("Delete Recipe", systemImage: "trash", role: .destructive) { deleting = card.recipe }
                }
            }
            if size == .fullWidth, canAdd {
                addButton
                    .padding(.top, DS.Space.sm)
            }
        }
        .frame(width: size == .carousel ? carouselWidth : nil, alignment: .leading)
        .recipeDeletion($deleting, isPlanned: { _ in card.inPlan })
        .accessibilityElement(children: .contain)
    }

    /// The result as one line: photo, name and time open the recipe; Add sits on its own.
    private var searchRow: some View {
        HStack(spacing: DS.Space.m) {
            NavigationLink(value: card.recipe) {
                HStack(spacing: DS.Space.m) {
                    RecipePhoto(
                        url: card.recipe.imageURL, aspectRatio: 1, pointWidth: DS.Size.searchThumbnail,
                        cornerRadius: DS.Radius.medium, title: card.recipe.name, showsTitle: false
                    )
                    .frame(width: DS.Size.searchThumbnail)
                    VStack(alignment: .leading, spacing: DS.Space.hairline) {
                        Text(card.recipe.name)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(Color.primary)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)
                        if let minutes = card.recipe.displayMinutes, minutes > 0 {
                            Label(RecipeFormat.minutes(minutes), systemImage: isQuick ? "bolt.fill" : "clock")
                                .font(.footnote)
                                .foregroundStyle(Color.secondary)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .contentShape(.rect)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(
                    MenuFormat.cardAccessibilityLabel(
                        name: card.recipe.name, minutes: card.recipe.displayMinutes, isQuick: isQuick,
                        badge: contextBadge?.text, isAddOn: card.recipe.isAddon, inPlan: card.inPlan))
            }
            .buttonStyle(.plain)
            if canAdd {
                AddToWeekButton(
                    isInPlan: card.inPlan, isBusy: isBusy, recipeName: card.recipe.name, action: toggle
                )
                .contextMenu { dayMenuItems }
            }
        }
        // A readable line on an iPad, from the leading edge like the chips above it.
        .frame(maxWidth: DS.Size.readableWidth, alignment: .leading)
        .accessibilityElement(children: .contain)
    }

    /// Shared with the carousels that warm the next few photos, so a prefetch asks the CDN for
    /// the same size the card will.
    nonisolated static func carouselWidth(for dynamicTypeSize: DynamicTypeSize) -> CGFloat {
        dynamicTypeSize.isAccessibilitySize ? 300 : 220
    }

    /// What a full-width card's photo is asked for before the layout measures it; `RecipePhoto`
    /// refines it from the real frame.
    nonisolated static let fullWidthEstimate: CGFloat = 400

    private var carouselWidth: CGFloat {
        MenuRecipeCard.carouselWidth(for: dynamicTypeSize)
    }

    private var photo: some View {
        RecipePhoto(
            url: card.recipe.imageURL, aspectRatio: size == .carousel ? 4.0 / 3.0 : 16.0 / 10.0,
            pointWidth: size == .carousel ? carouselWidth : MenuRecipeCard.fullWidthEstimate,
            title: card.recipe.name
        )
        .overlay(alignment: .topTrailing) {
            if size == .carousel, canAdd {
                AddToWeekButton(
                    isInPlan: card.inPlan, isBusy: isBusy, recipeName: card.recipe.name, action: toggle
                )
                .contextMenu { dayMenuItems }
                .padding(DS.Space.xxs)
            }
        }
        .overlay(alignment: .topLeading) {
            if let contextBadge {
                PhotoBadge(
                    text: contextBadge.text, systemImage: contextBadge.code.systemImage,
                    isProminent: contextBadge.code == .autopilotPick
                )
                .padding(DS.Space.s)
                // Spoken as part of the card's label instead.
                .accessibilityHidden(true)
            }
        }
        .overlay(alignment: .bottomLeading) {
            if let minutes = card.recipe.displayMinutes, minutes > 0 {
                TimeBadge(minutes: minutes, isQuick: isQuick)
                    .padding(DS.Space.s)
                    .accessibilityHidden(true)
            }
        }
    }

    /// The name is all a card says now: what it is and how long it takes, and the rest is on
    /// the recipe screen.
    private var title: some View {
        CardTitle(
            name: card.recipe.name,
            reservesHeight: MenuCardMetrics.reservesTitleHeight(hasRowBelow: size == .fullWidth && canAdd)
        )
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            MenuFormat.cardAccessibilityLabel(
                name: card.recipe.name, minutes: card.recipe.displayMinutes, isQuick: isQuick,
                badge: contextBadge?.text, isAddOn: card.recipe.isAddon, inPlan: card.inPlan))
    }

    private var contextBadge: MenuBadge? { MenuCardBadge.context(in: card.badges) }

    private var isQuick: Bool { card.recipe.timeBand == .quick }

    private var addButton: some View {
        HStack(spacing: DS.Space.m) {
            Button(action: toggle) {
                if isBusy {
                    ProgressView()
                } else {
                    Label(
                        card.inPlan ? String(localized: "In Your Week") : String(localized: "Add"),
                        systemImage: card.inPlan ? "checkmark" : "plus"
                    )
                }
            }
            .buttonStyle(.dsSecondary(.small, fullWidth: true))
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
            ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { day in
                Button(day.title(in: plans.week, weekStartsOn: plans.weekStartsOn)) {
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
            HStack(alignment: .top, spacing: DS.Space.m) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                }
            }
            .padding(DS.Space.l)
        }
    }
    .menuPreviewEnvironment()
}

#Preview("Full width, dark") {
    NavigationStack {
        ScrollView {
            VStack(spacing: DS.Space.xxl) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card, size: .fullWidth)
                }
            }
            .padding(DS.Space.l)
        }
    }
    .menuPreviewEnvironment()
    .preferredColorScheme(.dark)
}

/// Mixed name lengths, a card with no cook time, and one with no photo: every card in the row
/// has to be the same height.
#Preview("Equal heights") {
    NavigationStack {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: DS.Space.m) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                        .border(.red.opacity(0.4))
                }
            }
            .padding(DS.Space.l)
        }
    }
    .menuPreviewEnvironment()
}

#Preview("Accessibility size") {
    NavigationStack {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: DS.Space.m) {
                ForEach(MenuPreviewData.cards) { card in
                    MenuRecipeCard(card: card)
                        .border(.red.opacity(0.4))
                }
            }
            .padding(DS.Space.l)
        }
    }
    .menuPreviewEnvironment()
    .environment(\.dynamicTypeSize, .accessibility3)
}

/// A recipe card as one row of a lazy list or carousel, with its photo warm-up.
///
/// SwiftUI can build a lazy stack's rows on its render thread (`com.apple.SwiftUI.AsyncRenderer`)
/// while it lays out a scroll. A row closure tied to the main actor checks its thread when it
/// starts and stops the app there (decision 701). So the row closure only makes this value,
/// through a `nonisolated` init that touches nothing on the main actor, and SwiftUI runs `body`
/// on the main thread as it does for every view.
struct MenuCardRow: View {
    let index: Int
    let card: MenuCard
    let size: MenuRecipeCard.Size
    let canAdd: Bool
    let photoURLs: [URL?]
    let photoWidth: CGFloat

    nonisolated init(
        index: Int, card: MenuCard, size: MenuRecipeCard.Size, canAdd: Bool, photoURLs: [URL?], photoWidth: CGFloat
    ) {
        self.index = index
        self.card = card
        self.size = size
        self.canAdd = canAdd
        self.photoURLs = photoURLs
        self.photoWidth = photoWidth
    }

    var body: some View {
        MenuRecipeCard(card: card, size: size, canAdd: canAdd)
            .prefetchesPhotos(after: index, in: photoURLs, pointWidth: photoWidth)
    }
}
