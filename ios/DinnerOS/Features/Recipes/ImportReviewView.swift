import SwiftUI

/// What the importer flagged and couldn't map: mostly deliveries whose box may not have
/// matched the recipe page the details came from.
///
/// The screen leads with the divergences worth a second look, folds the ones that are only a
/// different spelling out of the way, and lets the member settle each row: tap it for what
/// happened and what to do, or clear a whole section at once. Reached from the Household tab
/// and from Recipe Import, and hidden without `recipes.import`; the API enforces the
/// permission either way.
///
/// Every link here pushes a view directly rather than a value, so it works in whichever
/// navigation stack the screen was pushed into.
struct ImportReviewView: View {
    @Environment(ImportReviewStore.self) private var reviews
    @Environment(HouseholdStore.self) private var households

    @State private var showsSpellingOnly = false
    @State private var bulk: BulkAction?
    @State private var actionError: String?

    private var householdID: String? {
        households.current?.household.id
    }

    var body: some View {
        content
            .navigationTitle("Import Review")
            .navigationBarTitleDisplayMode(.inline)
            .task(id: householdID) {
                guard let householdID else { return }
                reviews.activate(householdID: householdID)
                await reviews.load()
            }
            .refreshable { await reviews.refresh() }
            .confirmationDialog(
                bulk?.title ?? "", isPresented: showingBulk, titleVisibility: .visible, presenting: bulk
            ) { action in
                Button(action.confirmTitle) { Task { await run(action) } }
                Button("Cancel", role: .cancel) {}
            } message: { action in
                Text(action.message)
            }
    }

    private var showingBulk: Binding<Bool> {
        Binding(get: { bulk != nil }, set: { if !$0 { bulk = nil } })
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
                "Nothing to Review", systemImage: "checkmark.seal",
                description: Text("Every import has been checked."))
        } else {
            List {
                if let refreshError = reviews.refreshError {
                    Section { FormErrorLabel(message: refreshError) }
                }
                if let actionError {
                    Section { FormErrorLabel(message: actionError) }
                }
                Section {
                    ImportReviewCounts(digest: digest)
                } footer: {
                    Text("Every import so far, not only the last one.")
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
            }
            .disabled(reviews.isResolving)
        }
    }

    // MARK: - Sections

    private func differencesSection(_ groups: [ImportVariantGroup]) -> some View {
        Section {
            ForEach(groups) { group in
                NavigationLink {
                    ImportVariantDetailView(group: group)
                } label: {
                    ImportVariantRow(group: group)
                }
            }
            Button("Mark All as Same Recipe", systemImage: "checkmark.circle") {
                bulk = .sameRecipe(groups, spelling: false)
            }
        } header: {
            Text("Box Looked Different")
        } footer: {
            Text("The box had a different name than the recipe in your library. Tap one to decide.")
        }
    }

    private func spellingOnlySection(_ groups: [ImportVariantGroup]) -> some View {
        Section {
            DisclosureGroup(isExpanded: $showsSpellingOnly) {
                ForEach(groups) { group in
                    NavigationLink {
                        ImportVariantDetailView(group: group)
                    } label: {
                        ImportVariantRow(group: group)
                    }
                }
            } label: {
                Label("^[\(groups.count) recipe](inflect: true) with spelling only", systemImage: "textformat.abc")
            }
            Button("Mark All as Same Recipe", systemImage: "checkmark.circle") {
                bulk = .sameRecipe(groups, spelling: true)
            }
        } footer: {
            Text("Only case, spacing, or punctuation differs. That's this screen's guess.")
        }
    }

    private func otherSection(_ items: [ImportReview]) -> some View {
        Section {
            ForEach(items) { item in
                NavigationLink {
                    ImportGapDetailView(item: item)
                } label: {
                    ImportReviewGapRow(item: item)
                }
            }
            Button("Dismiss All", systemImage: "xmark.circle") {
                bulk = .dismissAll(items)
            }
        } header: {
            Text("Other Gaps")
        } footer: {
            Text("Things the recipe page left out. Nothing here can fill them in.")
        }
    }

    private func run(_ action: BulkAction) async {
        actionError = nil
        do {
            try await reviews.resolve(action.items, as: action.resolution)
        } catch is CancellationError {
        } catch {
            actionError = HouseholdStore.message(for: error)
        }
    }
}

/// A whole section settled at once, after a confirmation.
private enum BulkAction {
    case sameRecipe([ImportVariantGroup], spelling: Bool)
    case dismissAll([ImportReview])

    var items: [ImportReview] {
        switch self {
        case .sameRecipe(let groups, _): groups.flatMap(\.items)
        case .dismissAll(let items): items
        }
    }

    var resolution: ImportReviewResolution {
        switch self {
        case .sameRecipe: .sameRecipe
        case .dismissAll: .dismissed
        }
    }

    var title: String {
        switch self {
        case .sameRecipe(let groups, _):
            String(localized: "Mark \(groups.count) recipes as the same recipe?")
        case .dismissAll(let items):
            String(localized: "Dismiss \(items.count) items?")
        }
    }

    var confirmTitle: LocalizedStringKey {
        switch self {
        case .sameRecipe: "Mark All as Same Recipe"
        case .dismissAll: "Dismiss All"
        }
    }

    var message: String {
        switch self {
        case .sameRecipe(_, spelling: true):
            String(localized: "Each delivery counts toward the recipe in your library.")
        case .sameRecipe(_, spelling: false):
            String(localized: "Each delivery counts toward the recipe in your library. Check any protein swaps first.")
        case .dismissAll:
            String(localized: "They leave this list. Your recipes don't change.")
        }
    }
}

// MARK: - Counts

/// Three numbers at the top: what to look at, what was folded away, and what's left over.
/// Their sum is what every Import Review badge shows.
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
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.recipeName)
                    .font(.subheadline.weight(.semibold))
                Text(ImportReviewCopy.summary(for: item))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: item.kind.systemImage)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }
}

extension ImportReview.Kind {
    fileprivate var systemImage: String {
        switch self {
        case .steps: "list.number"
        case .cookTime: "clock"
        case .ingredientUnit: "ruler"
        case .variant, .other: "questionmark.circle"
        }
    }
}

// MARK: - Details

/// One recipe whose box looked different: what happened, and the member's decision.
struct ImportVariantDetailView: View {
    let group: ImportVariantGroup

    @Environment(ImportReviewStore.self) private var reviews
    @Environment(\.dismiss) private var dismiss
    @State private var errorMessage: String?
    @State private var addingName: AddingName?

    private var actions: [ImportReviewAction] { ImportReviewAction.actions(for: group) }

    var body: some View {
        List {
            if let url = group.imageURL {
                Section {
                    RecipeImage(url: url, pointWidth: 360)
                        .listRowInsets(EdgeInsets())
                }
            }
            if let errorMessage {
                Section { FormErrorLabel(message: errorMessage) }
            }
            Section {
                ImportVariantChip(role: .stored, text: group.storedName)
                ForEach(group.deliveredNames, id: \.self) { delivered in
                    ImportVariantChip(role: .delivered, text: delivered)
                }
            } footer: {
                ImportReviewLines(lines: ImportReviewCopy.explanation(for: group))
            }
            Section {
                ForEach(actions, id: \.self) { action in
                    actionRow(action)
                }
            } header: {
                Text("What Was It?")
            }
        }
        .navigationTitle(group.storedName)
        .navigationBarTitleDisplayMode(.inline)
        .disabled(reviews.isResolving)
        .sheet(item: $addingName) { _ in
            AddRecipeView { recipe in
                Task { await resolve(.differentRecipe, linkedRecipeID: recipe.id) }
            }
        }
    }

    @ViewBuilder
    private func actionRow(_ action: ImportReviewAction) -> some View {
        switch action {
        case .sameRecipe:
            ImportReviewActionButton(
                title: "Same Recipe", detail: "Keep the recipe in your library. The delivery counts toward it.",
                systemImage: "checkmark.circle"
            ) { Task { await resolve(.sameRecipe) } }
        case .addAsNewRecipe(let name):
            ImportReviewActionButton(
                title: "It's a Different Recipe",
                detail: "Add \u{201C}\(name)\u{201D} yourself. We only know its name, not its ingredients.",
                systemImage: "plus.circle"
            ) { addingName = AddingName(name: name) }
        case .openRecipe:
            if let recipeID = group.recipeID {
                NavigationLink {
                    RecipeDetailView(
                        summary: .placeholder(
                            id: recipeID, name: group.storedName,
                            imageURLString: group.imageURL?.absoluteString))
                } label: {
                    Label("Open Recipe in Your Library", systemImage: "book.closed")
                }
            }
        case .dismiss:
            ImportReviewActionButton(
                title: "Dismiss", detail: "Take it off this list. Nothing changes.", systemImage: "xmark.circle"
            ) { Task { await resolve(.dismissed) } }
        }
    }

    private func resolve(_ resolution: ImportReviewResolution, linkedRecipeID: String? = nil) async {
        errorMessage = nil
        do {
            try await reviews.resolve(group.items, as: resolution, linkedRecipeID: linkedRecipeID)
            dismiss()
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }

    private struct AddingName: Identifiable {
        let name: String
        var id: String { name }
    }
}

/// A missing-steps, cook-time, unit, or unknown item: what it means, and the way off the list.
struct ImportGapDetailView: View {
    let item: ImportReview

    @Environment(ImportReviewStore.self) private var reviews
    @Environment(\.dismiss) private var dismiss
    @State private var errorMessage: String?

    var body: some View {
        List {
            if let url = item.recipeImageURL {
                Section {
                    RecipeImage(url: url, pointWidth: 360)
                        .listRowInsets(EdgeInsets())
                }
            }
            if let errorMessage {
                Section { FormErrorLabel(message: errorMessage) }
            }
            Section {
                Label(item.recipeName, systemImage: item.kind.systemImage)
                    .font(.headline)
            } footer: {
                ImportReviewLines(lines: ImportReviewCopy.explanation(for: item))
            }
            Section {
                ForEach(ImportReviewAction.actions(for: item), id: \.self) { action in
                    switch action {
                    case .openRecipe:
                        if let recipeID = item.recipeID {
                            NavigationLink {
                                RecipeDetailView(
                                    summary: .placeholder(
                                        id: recipeID, name: item.recipeName,
                                        imageURLString: item.recipeImageURLString))
                            } label: {
                                Label("Open Recipe in Your Library", systemImage: "book.closed")
                            }
                        }
                    case .dismiss:
                        ImportReviewActionButton(
                            title: "Dismiss", detail: "Take it off this list. Nothing changes.",
                            systemImage: "xmark.circle"
                        ) { Task { await dismissItem() } }
                    case .sameRecipe, .addAsNewRecipe:
                        EmptyView()
                    }
                }
            }
        }
        .navigationTitle(ImportReviewCopy.summary(for: item))
        .navigationBarTitleDisplayMode(.inline)
        .disabled(reviews.isResolving)
    }

    private func dismissItem() async {
        errorMessage = nil
        do {
            try await reviews.resolve([item], as: .dismissed)
            dismiss()
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

/// A detail screen's explanation: one idea per line.
private struct ImportReviewLines: View {
    let lines: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(lines, id: \.self) { line in
                Text(line)
            }
        }
        .padding(.top, 4)
    }
}

/// A decision: what it does, in one line under its name.
private struct ImportReviewActionButton: View {
    let title: LocalizedStringKey
    let detail: LocalizedStringKey
    let systemImage: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.body.weight(.semibold))
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } icon: {
                Image(systemName: systemImage)
            }
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

#Preview("Box looked different") {
    let session = HouseholdPreviewData.session()
    let digest = ImportReviewDigest(items: ImportReviewPreviewData.items)
    NavigationStack {
        if let group = digest.differences.first {
            ImportVariantDetailView(group: group)
        }
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(ImportReviewPreviewData.store(session: session))
}

#Preview("Nothing to Review") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        ImportReviewView()
            .environment(session)
            .environment(HouseholdPreviewData.store(session: session))
            .environment(ImportReviewPreviewData.store(session: session, items: []))
    }
}
