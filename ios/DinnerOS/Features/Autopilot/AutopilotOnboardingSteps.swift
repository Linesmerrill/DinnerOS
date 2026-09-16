import SwiftUI

// MARK: - Step 1: taste

/// A grid of real food photos, one per cuisine. Tap to like, tap again for "no thanks", tap
/// once more to clear; long-press picks directly.
struct AutopilotTasteStep: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary
    let tiles: [CuisineTile]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var columns: [GridItem] {
        // One column at accessibility sizes, so the name always fits on its photo.
        let minimum: CGFloat = dynamicTypeSize.isAccessibilitySize ? 260 : 150
        return [GridItem(.adaptive(minimum: minimum), spacing: 12)]
    }

    var body: some View {
        ScrollView {
            LazyVGrid(columns: columns, spacing: 12) {
                ForEach(tiles) { tile in
                    CuisineTileView(
                        tile: tile, preference: settings.preference(for: tile.value, kind: .cuisine),
                        cycle: { cycle(tile) }, set: { set($0, on: tile) })
                }
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
        }
        .scrollBounceBehavior(.basedOnSize)
    }

    private func cycle(_ tile: CuisineTile) {
        set(settings.preference(for: tile.value, kind: .cuisine).next, on: tile)
    }

    private func set(_ preference: AutopilotTastePreference, on tile: CuisineTile) {
        settings.setPreference(preference, for: tile.value, kind: .cuisine, limits: vocabulary.limits)
    }
}

/// One cuisine: its photo, its name, and what the household thinks of it.
private struct CuisineTileView: View {
    let tile: CuisineTile
    let preference: AutopilotTastePreference
    let cycle: () -> Void
    let set: (AutopilotTastePreference) -> Void

    private static let cornerRadius: CGFloat = 18

    var body: some View {
        Button(action: cycle) {
            artwork
                .overlay(alignment: .bottomLeading) { name }
                .overlay(alignment: .topTrailing) { mark }
                .overlay {
                    RoundedRectangle(cornerRadius: Self.cornerRadius)
                        .strokeBorder(preference == .liked ? AnyShapeStyle(.tint) : AnyShapeStyle(.clear), lineWidth: 3)
                }
                .opacity(preference == .disliked ? 0.45 : 1)
                .contentShape(.rect(cornerRadius: Self.cornerRadius))
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button("Like", systemImage: "heart") { set(.liked) }
            Button("No Thanks", systemImage: "hand.thumbsdown") { set(.disliked) }
            if preference != .neutral {
                Button("Clear", systemImage: "arrow.uturn.backward") { set(.neutral) }
            }
        }
        .accessibilityLabel(tile.label)
        .accessibilityValue(CuisineTiles.accessibilityValue(preference))
        .accessibilityHint("Changes between liked, no thanks, and no preference")
        .accessibilityAddTraits(preference == .liked ? .isSelected : [])
    }

    /// The cuisine's photo, or a plain tinted tile when no unused photo was left for it —
    /// never a repeat of another tile's (#335).
    @ViewBuilder
    private var artwork: some View {
        if let url = tile.imageURL {
            RecipePhoto(url: url, aspectRatio: 1, pointWidth: 200, cornerRadius: Self.cornerRadius)
                .overlay {
                    // A scrim under the name, so it stays readable on a light photo.
                    LinearGradient(
                        stops: [
                            .init(color: .black.opacity(0), location: 0),
                            .init(color: .black.opacity(0.25), location: 0.55),
                            .init(color: .black.opacity(0.75), location: 1),
                        ], startPoint: .top, endPoint: .bottom
                    )
                    .clipShape(.rect(cornerRadius: Self.cornerRadius))
                }
        } else {
            RoundedRectangle(cornerRadius: Self.cornerRadius)
                .fill(.tint.opacity(0.18))
                .aspectRatio(1, contentMode: .fit)
                .overlay {
                    RoundedRectangle(cornerRadius: Self.cornerRadius)
                        .strokeBorder(.tint.opacity(0.35))
                }
        }
    }

    private var name: some View {
        Text(tile.label)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(tile.imageURL == nil ? AnyShapeStyle(Color.primary) : AnyShapeStyle(Color.white))
            .lineLimit(2)
            .minimumScaleFactor(0.8)
            .shadow(color: .black.opacity(tile.imageURL == nil ? 0 : 0.6), radius: 3)
            .padding(10)
    }

    /// The same filled mark the Menu's cards use once a recipe is in the week.
    @ViewBuilder
    private var mark: some View {
        if preference != .neutral {
            Image(systemName: preference == .liked ? "checkmark" : "hand.thumbsdown.fill")
                .font(.footnote.weight(.bold))
                .foregroundStyle(Color.onAccent)
                .frame(width: 28, height: 28)
                .background {
                    Circle().fill(preference == .liked ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.secondary))
                }
                .padding(8)
                .accessibilityHidden(true)
        }
    }
}

// MARK: - Step 2: restrictions

/// The one safety-critical step. A photo would be noise — there's nothing appetizing to show
/// about peanuts — so allergens and diets are symbol tiles in the same rounded-card shape the
/// Menu uses, rather than two banks of chips a reader skims past (#337). The ingredient field
/// stays, quietly, at the bottom.
struct AutopilotAvoidStep: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var limits: AutopilotLimits { vocabulary.limits }

    private var columns: [GridItem] {
        // One per row at accessibility sizes, where a label needs the whole width.
        let minimum: CGFloat = dynamicTypeSize.isAccessibilitySize ? 260 : 104
        return [GridItem(.adaptive(minimum: minimum), spacing: 10)]
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                group(
                    title: String(localized: "Allergens"), options: vocabulary.allergens,
                    selection: $settings.restrictions.allergens, symbol: AutopilotAvoidSymbol.allergen)
                group(
                    title: String(localized: "Diets"), options: vocabulary.diets,
                    selection: $settings.restrictions.diets, symbol: AutopilotAvoidSymbol.diet)
                ingredients
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
        }
        .scrollBounceBehavior(.basedOnSize)
    }

    @ViewBuilder
    private func group(
        title: String, options: [AutopilotOption], selection: Binding<[String]>,
        symbol: @escaping (String) -> String
    ) -> some View {
        if !options.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text(title)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.secondary)
                LazyVGrid(columns: columns, spacing: 10) {
                    ForEach(options, id: \.value) { option in
                        AvoidTile(
                            label: option.label, systemImage: symbol(option.value),
                            isOn: selection.wrappedValue.contains(option.value)
                        ) {
                            toggle(option.value, in: selection, order: options.map(\.value))
                        }
                    }
                }
            }
        }
    }

    /// Free text, so it keeps the chips-and-a-field control the preference screens use.
    private var ingredients: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Never Include")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
            ValueChipGroup(
                options: [], selection: $settings.restrictions.excludedIngredients,
                maxCount: limits.maxExcludedIngredients, maxLength: limits.maxIngredientLength,
                addPrompt: "Add an ingredient, like cilantro")
        }
    }

    private func toggle(_ value: String, in selection: Binding<[String]>, order: [String]) {
        var list = selection.wrappedValue
        AutopilotInput.set(value, included: !list.contains(value), in: &list, order: order)
        selection.wrappedValue = list
    }
}

/// An allergen or diet: its glyph, its name, and whether it's on. The same corner radius and
/// filled-tint selection as the Menu's cards.
private struct AvoidTile: View {
    let label: String
    let systemImage: String
    let isOn: Bool
    let toggle: () -> Void

    private static let cornerRadius: CGFloat = 18

    var body: some View {
        Button(action: toggle) {
            VStack(spacing: 8) {
                Image(systemName: systemImage)
                    .font(.title2)
                    .symbolVariant(.none)
                    .foregroundStyle(isOn ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.accentColor))
                Text(label)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(isOn ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 14)
            .padding(.horizontal, 8)
            .background {
                RoundedRectangle(cornerRadius: Self.cornerRadius)
                    .fill(isOn ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(Color(.secondarySystemBackground)))
            }
            .overlay {
                RoundedRectangle(cornerRadius: Self.cornerRadius)
                    .strokeBorder(isOn ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.clear), lineWidth: 2)
            }
            .contentShape(.rect(cornerRadius: Self.cornerRadius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .accessibilityHint("Autopilot never suggests a recipe with this")
    }
}

// MARK: - Step 3: schedule

/// The one thing about a week that has to be asked: which nights the household cooks. The
/// dinners Autopilot plans follow the nights picked, and everything else about the schedule —
/// planning fewer dinners than nights, servings, a weeknight time cap — keeps the API's
/// defaults until someone changes them in Preferences (#337).
struct AutopilotWeekStep: View {
    @Binding var settings: AutopilotSettings

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var columns: [GridItem] {
        let minimum: CGFloat = dynamicTypeSize.isAccessibilitySize ? 260 : 96
        return [GridItem(.adaptive(minimum: minimum), spacing: 10)]
    }

    private var nights: Int { settings.schedule.planDays.count }

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                LazyVGrid(columns: columns, spacing: 10) {
                    ForEach(PlanDay.allCases) { day in
                        NightTile(day: day, isOn: settings.schedule.planDays.contains(day)) {
                            settings.setPlanNight(day, included: !settings.schedule.planDays.contains(day))
                        }
                    }
                }
                Text("^[\(nights) dinner](inflect: true) a week")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel(Text("Autopilot plans ^[\(nights) dinner](inflect: true) a week"))
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
        }
        .scrollBounceBehavior(.basedOnSize)
    }
}

/// One night of the week, filled in when the household cooks it.
private struct NightTile: View {
    let day: PlanDay
    let isOn: Bool
    let toggle: () -> Void

    private static let cornerRadius: CGFloat = 18

    var body: some View {
        Button(action: toggle) {
            VStack(spacing: 6) {
                Text(DayChipRow.shortName(day))
                    .font(.headline)
                    .foregroundStyle(isOn ? AnyShapeStyle(Color.onAccent) : AnyShapeStyle(Color.primary))
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Image(systemName: isOn ? "fork.knife" : "moon.zzz")
                    .font(.subheadline)
                    .foregroundStyle(
                        isOn ? AnyShapeStyle(Color.onAccent.opacity(0.9)) : AnyShapeStyle(Color.secondary))
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
            .padding(.horizontal, 6)
            .background {
                RoundedRectangle(cornerRadius: Self.cornerRadius)
                    .fill(isOn ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(Color(.secondarySystemBackground)))
            }
            .contentShape(.rect(cornerRadius: Self.cornerRadius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(day.name())
        .accessibilityValue(isOn ? Text("Cooking") : Text("Not cooking"))
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }
}

// MARK: - Previews

#Preview("Likes") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotTasteStep(
            settings: $settings, vocabulary: vocabulary, tiles: AutopilotPreviewData.cuisineTiles)
    }
}

#Preview("Likes, dark") {
    @Previewable @State var settings = AutopilotPreviewData.likedSettings
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotTasteStep(
            settings: $settings, vocabulary: vocabulary, tiles: AutopilotPreviewData.cuisineTiles)
    }
}

#Preview("Likes, accessibility size") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotTasteStep(
            settings: $settings, vocabulary: vocabulary, tiles: AutopilotPreviewData.cuisineTiles)
    }
}

#Preview("Avoid") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotAvoidStep(settings: $settings, vocabulary: vocabulary)
    }
}

#Preview("Avoid, chosen") {
    @Previewable @State var settings = AutopilotPreviewData.likedSettings
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotAvoidStep(settings: $settings, vocabulary: vocabulary)
    }
}

#Preview("Avoid, dark") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotAvoidStep(settings: $settings, vocabulary: vocabulary)
            .preferredColorScheme(.dark)
    }
}

#Preview("Avoid, accessibility size") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotAvoidStep(settings: $settings, vocabulary: vocabulary)
            .environment(\.dynamicTypeSize, .accessibility3)
    }
}

#Preview("Nights") {
    @Previewable @State var settings = AutopilotSettings.defaults
    AutopilotWeekStep(settings: $settings)
}

#Preview("Nights, dark") {
    @Previewable @State var settings = AutopilotSettings.defaults
    AutopilotWeekStep(settings: $settings)
        .preferredColorScheme(.dark)
}

#Preview("Nights, accessibility size") {
    @Previewable @State var settings = AutopilotSettings.defaults
    AutopilotWeekStep(settings: $settings)
        .environment(\.dynamicTypeSize, .accessibility3)
}
