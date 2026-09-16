import SwiftUI

/// Opens the import review backlog from the Household tab.
struct ImportReviewRoute: Hashable {}

/// What the importer flagged and couldn't map: mostly deliveries whose box may not have
/// matched the recipe page the details came from.
///
/// The screen leads with the divergences worth a second look, folds the ones that are only a
/// different spelling out of the way, and says plainly that nothing here can be resolved from
/// the app. Reached from the Household tab and hidden without `recipes.import`; the API
/// enforces the permission either way.
struct ImportReviewView: View {
    @Environment(ImportReviewStore.self) private var reviews
    @Environment(HouseholdStore.self) private var households

    @State private var showsSpellingOnly = false

    private var householdID: String? {
        households.current?.household.id
    }

    var body: some View {
        content
            .navigationTitle("Import Review")
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: RecipeSummary.self) { summary in
                RecipeDetailView(summary: summary)
            }
            .task(id: householdID) {
                guard let householdID else { return }
                reviews.activate(householdID: householdID)
                await reviews.load()
            }
            .refreshable { await reviews.refresh() }
    }

    @ViewBuilder
    private var content: some View {
        switch reviews.phase {
        case .idle, .loading:
            ProgressView("Loading import review…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Import Review", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await reviews.load() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            loaded(reviews.digest)
        }
    }

    @ViewBuilder
    private func loaded(_ digest: ImportReviewDigest) -> some View {
        if digest.isEmpty {
            ContentUnavailableView(
                "Nothing Flagged", systemImage: "checkmark.seal",
                description: Text("The last import mapped everything it read."))
        } else {
            List {
                if let refreshError = reviews.refreshError {
                    Section {
                        FormErrorLabel(message: refreshError)
                    }
                }
                Section {
                    ImportReviewCounts(digest: digest)
                }
                .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))

                if !digest.differences.isEmpty {
                    differencesSection(digest.differences)
                }
                if !digest.spellingOnly.isEmpty {
                    spellingOnlySection(digest.spellingOnly)
                }
                if !digest.otherItems.isEmpty {
                    otherSection(digest.otherItems)
                }
                resolvingSection
            }
        }
    }

    // MARK: - Sections

    private func differencesSection(_ groups: [ImportVariantGroup]) -> some View {
        Section {
            ForEach(groups) { group in
                ImportVariantRow(group: group)
            }
        } header: {
            Text("Box Looked Different")
        } footer: {
            Text(
                "On at least one delivered week the box carried the name on the right, while the app stores the one "
                    + "on the left. The grocery list, allergens, and Autopilot all read the stored one."
            )
        }
    }

    private func spellingOnlySection(_ groups: [ImportVariantGroup]) -> some View {
        Section {
            DisclosureGroup(isExpanded: $showsSpellingOnly) {
                ForEach(groups) { group in
                    ImportVariantRow(group: group)
                }
            } label: {
                Label(
                    "^[\(groups.count) recipe](inflect: true) — spelling only",
                    systemImage: "textformat.abc")
            }
        } footer: {
            Text(
                "Folded away because the delivered name matches the stored one once case, spacing, and punctuation "
                    + "are ignored. That's this screen's guess, not the API's: anything it can't match stays above."
            )
        }
    }

    private func otherSection(_ items: [ImportReview]) -> some View {
        Section("Other Gaps") {
            ForEach(items) { item in
                ImportReviewGapRow(item: item)
            }
        }
    }

    private var resolvingSection: some View {
        Section("Fixing These") {
            Label {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Nothing here can be resolved from the app.")
                        .font(.subheadline.weight(.semibold))
                    Text(
                        "Each one needs a capture of the delivered page and a re-import — `go run . variants` and "
                            + "`capture` in importers/hellofresh. The list clears itself when the import runs again."
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
            } icon: {
                Image(systemName: "arrow.clockwise.circle")
                    .foregroundStyle(.secondary)
            }
            Label {
                Text("The backlog doesn't record which weeks a box differed on, only how many deliveries were flagged.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } icon: {
                Image(systemName: "calendar.badge.exclamationmark")
                    .foregroundStyle(.secondary)
            }
        }
    }
}

// MARK: - Counts

/// Three numbers at the top: what to look at, what was folded away, and what's left over.
private struct ImportReviewCounts: View {
    let digest: ImportReviewDigest

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            tile(value: digest.differences.count, label: "Look Again", tint: .orange)
            tile(value: digest.spellingOnly.count, label: "Spelling", tint: .secondary)
            tile(value: digest.otherItems.count, label: "Other", tint: .secondary)
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
    }

    private func tile(value: Int, label: LocalizedStringKey, tint: some ShapeStyle) -> some View {
        VStack(spacing: 2) {
            Text(value.formatted())
                .font(.title2.weight(.semibold))
                .foregroundStyle(tint)
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
    }
}

// MARK: - Rows

/// One recipe's divergence: the stored name over the delivered one, so the eye judges.
private struct ImportVariantRow: View {
    let group: ImportVariantGroup

    var body: some View {
        if let recipeID = group.recipeID {
            NavigationLink(
                value: RecipeSummary.placeholder(id: recipeID, name: group.storedName, imageURLString: nil)
            ) {
                comparison
            }
        } else {
            comparison
        }
    }

    private var comparison: some View {
        VStack(alignment: .leading, spacing: 8) {
            ImportVariantChip(role: .stored, text: group.storedName)
            ForEach(group.deliveredNames, id: \.self) { delivered in
                ImportVariantChip(role: .delivered, text: delivered)
            }
            if group.flaggedDeliveries > 1 {
                Text("^[\(group.flaggedDeliveries) flagged delivery](inflect: true)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if group.recipeID == nil {
                Text("No stored recipe carries this ID any more.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

/// A stored or delivered name, labelled so the pair reads without a legend.
private struct ImportVariantChip: View {
    enum Role {
        case stored
        case delivered

        var title: LocalizedStringKey {
            switch self {
            case .stored: "Stored"
            case .delivered: "Delivered"
            }
        }

        var systemImage: String {
            switch self {
            case .stored: "book.closed"
            case .delivered: "shippingbox"
            }
        }
    }

    let role: Role
    let text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Label(role.title, systemImage: role.systemImage)
                .font(.caption2.weight(.bold))
                .labelStyle(.titleAndIcon)
                .foregroundStyle(.secondary)
            Text(text)
                .font(.subheadline)
                .foregroundStyle(.primary)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(background, in: .rect(cornerRadius: 10))
        .accessibilityElement(children: .combine)
    }

    private var background: AnyShapeStyle {
        switch role {
        case .stored: AnyShapeStyle(.quaternary)
        case .delivered: AnyShapeStyle(Color.orange.opacity(0.18))
        }
    }
}

/// A missing-steps or unknown-unit item: what it's about, on which recipe.
private struct ImportReviewGapRow: View {
    let item: ImportReview

    var body: some View {
        if let recipeID = item.recipeID {
            NavigationLink(
                value: RecipeSummary.placeholder(id: recipeID, name: item.recipeName, imageURLString: nil)
            ) {
                label
            }
        } else {
            label
        }
    }

    private var label: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.recipeName)
                    .font(.subheadline.weight(.semibold))
                Text(summary)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: systemImage)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private var systemImage: String {
        switch item.kind {
        case .steps: "list.number"
        case .ingredientUnit: "ruler"
        case .variant, .other: "questionmark.circle"
        }
    }

    private var summary: String {
        switch item.kind {
        case .steps:
            String(localized: "No instructions on the page")
        case .ingredientUnit(let ingredient):
            String(localized: "Unknown unit on \(ingredient)")
        case .variant, .other:
            item.reason
        }
    }
}

// MARK: - Previews

enum ImportReviewPreviewData {
    static let items: [ImportReview] = [
        ImportReview(
            recipeID: "recipe-1", recipeName: "Chicken Sausage Cavatappi Bolognese", source: "hellofresh",
            sourceRecipeID: "src-1", field: "variant", value: "Pork Sausage Cavatappi Bolognese",
            reason: "delivered menu variant differed from the canonical page",
            createdAt: .now.addingTimeInterval(-9 * 86_400)),
        ImportReview(
            recipeID: "recipe-1", recipeName: "Chicken Sausage Cavatappi Bolognese", source: "hellofresh",
            sourceRecipeID: "src-1b", field: "variant", value: "Pork Sausage Cavatappi Bolognese",
            reason: "delivered menu variant differed from the canonical page",
            createdAt: .now.addingTimeInterval(-8 * 86_400)),
        ImportReview(
            recipeID: "recipe-2", recipeName: "Honey Butter Corn Bread", source: "hellofresh",
            sourceRecipeID: "src-2", field: "variant", value: "honey butter cornbread",
            reason: "delivered menu variant differed from the canonical page",
            createdAt: .now.addingTimeInterval(-7 * 86_400)),
        ImportReview(
            recipeID: "recipe-3", recipeName: "Sheet Pan Test Bake", source: "hellofresh", sourceRecipeID: "src-3",
            field: "steps", reason: "the page had no instructions",
            createdAt: .now.addingTimeInterval(-6 * 86_400)),
        ImportReview(
            recipeID: nil, recipeName: "Pickled Onion Bowls", source: "hellofresh", sourceRecipeID: "src-4",
            field: "ingredients.Red Onion.unit", value: "pick", reason: "unknown unit",
            createdAt: .now.addingTimeInterval(-5 * 86_400)),
    ]

    static func store(session: AuthSession, items: [ImportReview] = items) -> ImportReviewStore {
        .preview(session: session, items: items, householdID: HouseholdPreviewData.household.id)
    }
}

#Preview("Backlog") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ImportReviewView()
            .environment(session)
            .environment(HouseholdPreviewData.store(session: session))
            .environment(ImportReviewPreviewData.store(session: session))
    }
}

#Preview("Nothing Flagged") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ImportReviewView()
            .environment(session)
            .environment(HouseholdPreviewData.store(session: session))
            .environment(ImportReviewPreviewData.store(session: session, items: []))
    }
}
