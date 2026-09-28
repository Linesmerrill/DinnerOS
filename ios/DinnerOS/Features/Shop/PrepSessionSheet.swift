import SwiftUI

/// "You just got the groceries." One card at a time: weigh out what this week needs, bag the
/// rest one dinner at a time, freeze the bags (docs/shopping-providers.md#the-prep-plan).
///
/// A picture of the counter rather than a paragraph: the ingredient's photo with the amount
/// to keep out, then one frosted card per freezer bag. **Freeze** and **Skip** move to the
/// next card, and a household that closes it half way finds it where it left it. Doing
/// nothing is still fine — nothing is recorded until a card is answered.
struct PrepSessionSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(MealPlanner.self) private var planner
    @Environment(\.dismiss) private var dismiss

    @State private var index = 0
    @State private var bags: Int?
    @State private var isBusy = false
    @State private var errorMessage: String?

    private var cards: [PrepCard] { shopping.prepSession?.cards ?? [] }
    private var card: PrepCard? { index < cards.count ? cards[index] : nil }
    private var canEdit: Bool { shopping.canConfirm }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Put the Groceries Away")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .onAppear { startAtFirstUnanswered() }
    }

    @ViewBuilder
    private var content: some View {
        if let card {
            let presentation = PrepCardPresentation(card: card, chosenBags: bags)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    }
                    PrepCardContent(
                        presentation: presentation, canEdit: canEdit, isBusy: isBusy,
                        setBags: { bags = $0 },
                        plan: { suggestion in Task { await plan(suggestion, on: card) } })
                }
                .padding()
            }
            .background(Color(.systemGroupedBackground))
            .safeAreaInset(edge: .top, spacing: 0) { progress }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if canEdit {
                    PrepActionBar(
                        presentation: presentation, isBusy: isBusy,
                        finish: { Task { await finish(card, bags: presentation.bags) } },
                        skip: { Task { await skip(card) } })
                }
            }
        } else {
            summary
        }
    }

    private var progress: some View {
        VStack(spacing: 4) {
            Text("Item \(index + 1) of \(cards.count)")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
            ProgressView(value: Double(index + 1), total: Double(max(cards.count, 1)))
                .progressViewStyle(.linear)
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .background(.bar)
        .accessibilityElement(children: .combine)
    }

    /// What the member sees when there is nothing left — and when there never was. An empty
    /// week is a good outcome, not a blank screen.
    @ViewBuilder
    private var summary: some View {
        let session = shopping.prepSession
        ContentUnavailableView {
            Label(
                session?.state == .nothingToPrep ? "Nothing to Prep" : "All Put Away",
                systemImage: session?.state == .nothingToPrep ? "checkmark.seal" : "snowflake")
        } description: {
            Text(session?.headline ?? String(localized: "Nothing to prep this week."))
        } actions: {
            if let session, session.skipped > 0 {
                Button("Go Back to the Skipped Ones") { index = firstSkipped(in: session) ?? 0 }
                    .buttonStyle(.bordered)
            }
            Button("Close") { dismiss() }
                .buttonStyle(.borderedProminent)
        }
    }

    // MARK: - Actions

    /// Resumes where the household left off: the first card nobody has answered.
    private func startAtFirstUnanswered() {
        index = cards.firstIndex { !$0.isAnswered } ?? cards.count
        bags = nil
    }

    private func firstSkipped(in session: PrepSession) -> Int? {
        session.cards.firstIndex { $0.status == .skipped }
    }

    private func finish(_ card: PrepCard, bags: Int) async {
        // Zero bags means "nothing to seal"; sending 0 would ask the API for its suggestion.
        await answer {
            try await shopping.completePrepCard(card, portions: card.freezable && bags > 0 ? bags : nil)
        }
    }

    private func skip(_ card: PrepCard) async {
        await answer { try await shopping.skipPrepCard(card) }
    }

    private func answer(_ work: () async throws -> PrepCard) async {
        isBusy = true
        defer { isBusy = false }
        errorMessage = nil
        do {
            _ = try await work()
            advance()
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }

    /// Moves to the next card nobody has answered, or to the summary.
    private func advance() {
        bags = nil
        let next = cards.indices.first { $0 > index && !cards[$0].isAnswered }
        index = next ?? cards.firstIndex { !$0.isAnswered } ?? cards.count
    }

    /// Plans the suggested meal through the ordinary planner, so it behaves exactly like a
    /// meal added from the library — same toast, same undo, same events.
    private func plan(_ suggestion: PrepSuggestion, on card: PrepCard) async {
        isBusy = true
        defer { isBusy = false }
        errorMessage = nil
        let entry = await planner.add(
            recipeID: suggestion.recipeID, name: suggestion.recipeName, to: shopping.week,
            day: PlanDay(rawValue: suggestion.day), servings: suggestion.servings)
        if entry == nil {
            errorMessage = planner.errorMessage ?? String(localized: "That meal couldn't be added. Try again.")
        }
    }
}

// MARK: - The card

/// One card: the header, the "this week" amount, the freezer bags, the leftover line, and the
/// second-meal suggestions. Store-free, so previews can show every state.
struct PrepCardContent: View {
    let presentation: PrepCardPresentation
    let canEdit: Bool
    let isBusy: Bool
    let setBags: (Int) -> Void
    let plan: (PrepSuggestion) -> Void

    private var card: PrepCard { presentation.card }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            header
            PrepThisWeekCard(presentation: presentation)
            if presentation.hasBags {
                PrepFreezerBags(presentation: presentation, canEdit: canEdit, isBusy: isBusy, setBags: setBags)
            }
            if let leftover = presentation.leftoverLine {
                Text(leftover)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel(Text(presentation.leftoverAccessibilityLabel ?? leftover))
            }
            if !card.freezable {
                // Nothing to bag: the one line of guidance is the whole answer.
                Text(card.instruction)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if !card.suggestions.isEmpty {
                PrepSuggestions(suggestions: card.suggestions, isDisabled: isBusy || !canEdit, plan: plan)
            }
            if !card.reminderText.isEmpty, presentation.hasBags {
                Text(card.reminderText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(card.name)
                .font(.title2.weight(.semibold))
            if let pack = presentation.packText {
                Text(pack)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if card.frozen {
                Label("Already in the freezer", systemImage: "snowflake")
                    .font(.footnote)
                    .foregroundStyle(PrepStyle.ice)
                    .padding(.top, 4)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// The amount to weigh out and leave in the fridge, in the app's green.
private struct PrepThisWeekCard: View {
    let presentation: PrepCardPresentation

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .largeTitle) private var photoSize: CGFloat = 88

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 16))
        layout {
            PrepIngredientTile(
                url: presentation.card.imageURL, category: presentation.card.category,
                size: min(photoSize, 140))
            VStack(alignment: .leading, spacing: 2) {
                Text("This week")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(PrepStyle.week)
                Text(presentation.keepText)
                    .font(PrepStyle.readout(.largeTitle))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Text(presentation.keepCaption)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemGroupedBackground), in: PrepStyle.cardShape)
        .overlay(PrepStyle.cardShape.strokeBorder(PrepStyle.week.opacity(0.55), lineWidth: 1.5))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(presentation.keepAccessibilityLabel))
    }
}

/// One frosted card per freezer bag, with the controls that add and remove bags.
private struct PrepFreezerBags: View {
    let presentation: PrepCardPresentation
    let canEdit: Bool
    let isBusy: Bool
    let setBags: (Int) -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            controls
            if dynamicTypeSize.isAccessibilitySize {
                // Large text: one bag per line, full width, so the readout never truncates.
                VStack(alignment: .leading, spacing: 10) { bagCards }
            } else {
                ScrollViewReader { proxy in
                    ScrollView(.horizontal) {
                        HStack(spacing: 10) { bagCards }
                            .padding(.vertical, 2)
                    }
                    .scrollIndicators(.hidden)
                    .onChange(of: presentation.bags) { _, newValue in
                        withAnimation(reduceMotion ? nil : .easeOut(duration: 0.25)) {
                            proxy.scrollTo(newValue, anchor: .trailing)
                        }
                    }
                }
            }
        }
    }

    private var bagCards: some View {
        ForEach(1...presentation.bags, id: \.self) { index in
            PrepBagCard(presentation: presentation, index: index)
                .id(index)
                .transition(
                    reduceMotion
                        ? .opacity
                        : .asymmetric(
                            insertion: .scale(scale: 0.7, anchor: .leading).combined(with: .opacity),
                            removal: .scale(scale: 0.7).combined(with: .opacity)))
        }
    }

    private var controls: some View {
        HStack(spacing: 12) {
            Label("Freezer bags", systemImage: "snowflake")
                .font(.headline)
                .foregroundStyle(PrepStyle.ice)
            Spacer(minLength: 8)
            if canEdit, presentation.bagRange.upperBound > 1 {
                Button {
                    change(to: presentation.bags - 1)
                } label: {
                    Image(systemName: "minus")
                        .frame(minWidth: 20, minHeight: 20)
                }
                .disabled(isBusy || !presentation.canRemoveBag)
                .accessibilityLabel(Text("Remove a bag"))
                .accessibilityValue(Text(presentation.bagCountText))
                Button {
                    change(to: presentation.bags + 1)
                } label: {
                    Image(systemName: "plus")
                        .frame(minWidth: 20, minHeight: 20)
                }
                .disabled(isBusy || !presentation.canAddBag)
                .accessibilityLabel(Text("Add a bag"))
                .accessibilityValue(Text(presentation.bagCountText))
            }
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.circle)
        .tint(PrepStyle.ice)
    }

    /// Adding a bag inserts its card — the direct response to the tap — and says the new
    /// count for VoiceOver.
    private func change(to count: Int) {
        withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8)) {
            setBags(count)
        }
        let next = PrepCardPresentation(card: presentation.card, chosenBags: count, locale: presentation.locale)
        AccessibilityNotification.Announcement(next.bagCountAnnouncement).post()
    }
}

/// One bag: the same photo, frosted, with its size and thaw time.
private struct PrepBagCard: View {
    let presentation: PrepCardPresentation
    let index: Int

    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .title) private var photoSize: CGFloat = 56

    var body: some View {
        let wide = dynamicTypeSize.isAccessibilitySize
        let layout =
            wide
            ? AnyLayout(HStackLayout(alignment: .center, spacing: 18))
            : AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
        layout {
            PrepIngredientTile(
                url: presentation.card.imageURL, category: presentation.card.category, size: min(photoSize, 96)
            )
            .overlay(alignment: .topTrailing) {
                Image(systemName: "snowflake")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.white)
                    .padding(4)
                    .background(PrepStyle.ice, in: .circle)
                    .offset(x: 4, y: -4)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(presentation.bagText)
                    .font(PrepStyle.readout(.title))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Text(presentation.thawText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(12)
        .frame(width: wide ? nil : 136, alignment: .leading)
        .frame(maxWidth: wide ? .infinity : nil, alignment: .leading)
        .background {
            PrepStyle.cardShape.fill(Color(.secondarySystemGroupedBackground))
            PrepStyle.cardShape.fill(frost)
        }
        .overlay(PrepStyle.cardShape.strokeBorder(PrepStyle.ice.opacity(0.35), lineWidth: 1))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(presentation.bagAccessibilityLabel(index)))
    }

    /// Ice blue at low opacity over the card background: cold, not a status color.
    private var frost: Color {
        PrepStyle.ice.opacity(colorScheme == .dark ? 0.22 : 0.12)
    }
}

/// The primary button, named by what it does, and Skip. Pinned to the bottom so the next step
/// is always in reach however long the card is.
private struct PrepActionBar: View {
    let presentation: PrepCardPresentation
    let isBusy: Bool
    let finish: () -> Void
    let skip: () -> Void

    var body: some View {
        VStack(spacing: 6) {
            Button(action: finish) {
                Label(presentation.buttonTitle, systemImage: presentation.hasBags ? "snowflake" : "checkmark")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            // The freezer's color when the button freezes something; the app's otherwise.
            .tint(presentation.hasBags ? PrepStyle.ice : PrepStyle.week)
            .disabled(isBusy)
            Button("Skip This One", action: skip)
                .disabled(isBusy)
        }
        .padding(.horizontal)
        .padding(.vertical, 10)
        .background(.bar)
    }
}

private struct PrepSuggestions: View {
    let suggestions: [PrepSuggestion]
    let isDisabled: Bool
    let plan: (PrepSuggestion) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Or cook it again this week")
                .font(.headline)
            VStack(spacing: 0) {
                ForEach(suggestions) { suggestion in
                    Button {
                        plan(suggestion)
                    } label: {
                        PrepSuggestionLabel(suggestion: suggestion)
                            .padding(.vertical, 10)
                            .padding(.horizontal, 14)
                    }
                    .disabled(isDisabled)
                    if suggestion.id != suggestions.last?.id {
                        Divider().padding(.leading, 14)
                    }
                }
            }
            .background(Color(.secondarySystemGroupedBackground), in: PrepStyle.cardShape)
        }
    }
}

private struct PrepSuggestionLabel: View {
    let suggestion: PrepSuggestion

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(suggestion.recipeName)
                    .foregroundStyle(.tint)
                if let detail {
                    Text(detail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "plus.circle")
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }

    private var detail: String? {
        var parts: [String] = []
        if let day = PlanDay(rawValue: suggestion.day) {
            parts.append(day.name())
        }
        if let minutes = suggestion.cookMinutes {
            parts.append(String(localized: "\(minutes) min"))
        }
        parts += suggestion.reasons.prefix(1)
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

// MARK: - Shared pieces

/// The prep plan's few deliberate choices, in one place.
enum PrepStyle {
    /// The freezer's ice blue — the same family as the pantry's Freezer tag (`FrozenBadge`).
    static let ice = Color(.systemBlue)
    /// The app's green, named explicitly: `Color.accentColor` follows whatever tint a host
    /// view sets, and "this week" must never drift toward the freezer's blue.
    static let week = Color("AccentColor")
    static let cardShape = RoundedRectangle(cornerRadius: 16, style: .continuous)

    /// A kitchen-scale readout: rounded, heavy, digits that don't shift as the count changes.
    static func readout(_ style: Font.TextStyle) -> Font {
        .system(style, design: .rounded, weight: .heavy).monospacedDigit()
    }
}

/// The ingredient's photo on a rounded square, or a clean category glyph on the same tile when
/// there is no photo or it can't be loaded — never a broken image.
struct PrepIngredientTile: View {
    let url: URL?
    let category: String
    var size: CGFloat = 88

    @Environment(\.displayScale) private var displayScale

    var body: some View {
        RoundedRectangle(cornerRadius: size * 0.2, style: .continuous)
            .fill(Color(.tertiarySystemFill))
            .frame(width: size, height: size)
            .overlay {
                CachedImage(key: ImageKey(url: url, pointWidth: size, scale: displayScale)) {
                    glyph
                } fallback: {
                    glyph
                }
            }
            .clipShape(.rect(cornerRadius: size * 0.2, style: .continuous))
            .accessibilityHidden(true)
    }

    private var glyph: some View {
        Image(systemName: Self.symbol(for: category))
            .font(.system(size: size * 0.38, weight: .medium))
            .foregroundStyle(.secondary)
    }

    /// A food glyph for the ingredient's grocery category.
    static func symbol(for category: String) -> String {
        switch category {
        case "produce": "carrot"
        case "dairy-eggs": "cup.and.saucer"
        case "frozen": "snowflake"
        case "beverages": "waterbottle"
        case "spices": "leaf"
        case "bakery", "pantry", "deli", "condiments": "basket"
        default: "fork.knife"
        }
    }
}

// MARK: - Banner

/// The week's prep, at a glance: the first item's photo, what this week keeps out, and what
/// goes in the freezer. Opens the checklist. Shown on the Shop tab and in the Pantry, because
/// putting the groceries away belongs to both.
struct PrepSessionBanner: View {
    let session: PrepSession
    let open: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .headline) private var photoSize: CGFloat = 48
    @ScaledMetric(relativeTo: .subheadline) private var rowPhotoSize: CGFloat = 32

    private var presentation: PrepBannerPresentation { PrepBannerPresentation(session: session) }

    var body: some View {
        if presentation.listsItems {
            itemList
        } else {
            singleItem
        }
    }

    /// Several things to put away: a row for each, so the member sees which at a glance.
    private var itemList: some View {
        Section {
            HStack(spacing: 12) {
                Text(presentation.countTitle)
                    .font(.headline)
                Spacer(minLength: 0)
                Button("Start", action: open)
                    .buttonStyle(.borderedProminent)
                    .accessibilityHint(Text("Opens the checklist for putting the groceries away"))
            }
            VStack(alignment: .leading, spacing: 10) {
                ForEach(presentation.rows, id: \.card.id) { row in
                    itemRow(row)
                }
                if let more = presentation.moreText {
                    Text(more)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.vertical, 4)
            .contentShape(Rectangle())
            .onTapGesture(perform: open)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(presentation.accessibilityLabel))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction(named: Text("Start"), open)
        }
    }

    private func itemRow(_ row: PrepCardPresentation) -> some View {
        HStack(spacing: 10) {
            PrepIngredientTile(url: row.card.imageURL, category: row.card.category, size: min(rowPhotoSize, 52))
            Text(row.card.name)
                .font(.subheadline)
                .lineLimit(2)
            Spacer(minLength: 8)
            Text(row.glanceText)
                .font(PrepStyle.readout(.subheadline))
                .foregroundStyle(row.hasBags ? PrepStyle.ice : .secondary)
                .multilineTextAlignment(.trailing)
        }
    }

    private var singleItem: some View {
        Section {
            let layout =
                dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
                : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
            layout {
                HStack(spacing: 12) {
                    PrepIngredientTile(
                        url: presentation.card?.imageURL, category: presentation.card?.category ?? "",
                        size: min(photoSize, 72))
                    summary
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text(presentation.accessibilityLabel))
                Spacer(minLength: 0)
                Button("Start", action: open)
                    .buttonStyle(.borderedProminent)
                    .accessibilityHint(Text("Opens the checklist for putting the groceries away"))
            }
            .padding(.vertical, 2)
        }
    }

    @ViewBuilder
    private var summary: some View {
        VStack(alignment: .leading, spacing: 1) {
            if let amount = presentation.amountText {
                Text(presentation.card?.name ?? "")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(amount)
                    .font(PrepStyle.readout(.headline))
                if let detail = presentation.detailText {
                    Text(detail)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(presentation.detailIsFreezer ? PrepStyle.ice : .secondary)
                }
            } else {
                Text(session.headline)
                    .font(.subheadline)
            }
        }
    }
}

// MARK: - Previews

#Preview("One bag") {
    PrepCardPreview(card: PrepPreviewData.oneBag)
}

#Preview("Several bags") {
    PrepCardPreview(card: PrepPreviewData.severalBags)
}

#Preview("No bags, leftover only") {
    PrepCardPreview(card: PrepPreviewData.leftoverOnly)
}

#Preview("No photo") {
    PrepCardPreview(card: PrepPreviewData.noPhoto)
}

#Preview("Dark mode") {
    PrepCardPreview(card: PrepPreviewData.severalBags)
        .preferredColorScheme(.dark)
}

#Preview("Large text") {
    PrepCardPreview(card: PrepPreviewData.oneBag)
        .dynamicTypeSize(.accessibility3)
}

#Preview("Banner") {
    List {
        if let one = PrepPreviewData.session(PrepPreviewData.oneBagJSON) {
            PrepSessionBanner(session: one) {}
        }
        if let several = PrepPreviewData.session(
            PrepPreviewData.severalBagsJSON, PrepPreviewData.noPhotoJSON, PrepPreviewData.leftoverOnlyJSON)
        {
            PrepSessionBanner(session: several) {}
        }
    }
    .environment(\.imageLoader, PrepPreviewData.imageLoader)
}

#Preview("Banner, dark, large text") {
    List {
        if let one = PrepPreviewData.session(PrepPreviewData.noPhotoJSON) {
            PrepSessionBanner(session: one) {}
        }
    }
    .environment(\.imageLoader, PrepPreviewData.imageLoader)
    .preferredColorScheme(.dark)
    .dynamicTypeSize(.accessibility2)
}

/// A card with its bag count held locally, the way the sheet holds it.
private struct PrepCardPreview: View {
    let card: PrepCard?
    @State private var bags: Int?

    var body: some View {
        if let card {
            let presentation = PrepCardPresentation(card: card, chosenBags: bags)
            ScrollView {
                PrepCardContent(
                    presentation: presentation, canEdit: true, isBusy: false, setBags: { bags = $0 }, plan: { _ in }
                )
                .padding()
            }
            .background(Color(.systemGroupedBackground))
            .safeAreaInset(edge: .bottom, spacing: 0) {
                PrepActionBar(presentation: presentation, isBusy: false, finish: {}, skip: {})
            }
            .environment(\.imageLoader, PrepPreviewData.imageLoader)
        } else {
            Text("Preview data didn't decode.")
        }
    }
}
