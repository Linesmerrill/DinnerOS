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
                .foregroundStyle(.white)
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

/// The one safety-critical step: allergens and diets as chips, plus a single ingredient
/// field. No photos — it should read as plainly as possible.
struct AutopilotAvoidStep: View {
    @Binding var settings: AutopilotSettings
    let vocabulary: AutopilotVocabulary

    private var limits: AutopilotLimits { vocabulary.limits }

    var body: some View {
        Form {
            Section {
                ValueChipGroup(
                    options: vocabulary.allergens, selection: $settings.restrictions.allergens,
                    maxCount: vocabulary.allergens.count, order: vocabulary.allergens.map(\.value))
            } header: {
                Text("Allergens")
            }
            Section {
                ValueChipGroup(
                    options: vocabulary.diets, selection: $settings.restrictions.diets,
                    maxCount: vocabulary.diets.count, order: vocabulary.diets.map(\.value))
            } header: {
                Text("Diets")
            }
            Section {
                ValueChipGroup(
                    options: [], selection: $settings.restrictions.excludedIngredients,
                    maxCount: limits.maxExcludedIngredients, maxLength: limits.maxIngredientLength,
                    addPrompt: "Add an ingredient, like cilantro")
            } header: {
                Text("Never Include")
            }
        }
    }
}

// MARK: - Step 3: schedule

/// How many dinners, which nights, and for how many people: one short line per control.
struct AutopilotWeekStep: View {
    @Binding var settings: AutopilotSettings
    let limits: AutopilotLimits
    /// The household's default servings, shown for "Household default".
    var householdServings: Int?

    var body: some View {
        Form {
            Section {
                Stepper(value: $settings.schedule.mealsPerWeek, in: 1...max(settings.schedule.planDays.count, 1)) {
                    LabeledContent("Dinners a Week", value: settings.schedule.mealsPerWeek.formatted())
                }
            } footer: {
                Text("Autopilot fills up to this many of the nights below.")
            }
            Section {
                DayChipRow(selection: settings.schedule.planDays) { day in
                    settings.setPlanDay(day, included: !settings.schedule.planDays.contains(day))
                }
            } header: {
                Text("Nights You Cook")
            }
            Section {
                Picker("Servings", selection: $settings.schedule.defaultServings) {
                    Text(householdServings.map { "Household Default (\($0))" } ?? "Household Default")
                        .tag(Int?.none)
                    ForEach(1...limits.maxServings, id: \.self) { count in
                        Text("\(count)").tag(Int?.some(count))
                    }
                }
            } footer: {
                Text("How many the meals should feed.")
            }
        }
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

#Preview("Avoid, dark") {
    @Previewable @State var settings = AutopilotSettings.defaults
    if let vocabulary = AutopilotPreviewData.vocabulary {
        AutopilotAvoidStep(settings: $settings, vocabulary: vocabulary)
            .preferredColorScheme(.dark)
    }
}

#Preview("Week") {
    @Previewable @State var settings = AutopilotSettings.defaults
    AutopilotWeekStep(settings: $settings, limits: .defaults, householdServings: 4)
}

#Preview("Week, accessibility size") {
    @Previewable @State var settings = AutopilotSettings.defaults
    AutopilotWeekStep(settings: $settings, limits: .defaults, householdServings: 4)
        .environment(\.dynamicTypeSize, .accessibility3)
}
