import SwiftUI

/// Opens one specialty ingredient's options.
struct SpecialtyRoute: Hashable {
    let specialtyID: String
}

/// The setup screen in a sheet with Done, as Pantry's More menu and a grocery list open it.
struct SpecialtyIngredientsSheet: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            SpecialtyIngredientsView()
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
    }
}

/// The meal-kit blends, sauces, and concentrates the household's recipes use, most used first,
/// with each one's choice and house-made batch.
///
/// Changing anything is hidden without `pantry.edit`. That's a convenience only: the API checks
/// every change and answers `403` if a hidden rule applies.
struct SpecialtyIngredientsView: View {
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    @State private var confirmsDefaults = false
    @State private var isApplyingDefaults = false
    @State private var defaultsOutcome: SpecialtyDefaultsOutcome?
    @State private var actionError: String?

    private var householdID: String? {
        households.current?.household.id
    }

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    var body: some View {
        content
            .navigationTitle("Specialty Ingredients")
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: SpecialtyRoute.self) { route in
                SpecialtyDetailView(specialtyID: route.specialtyID)
            }
            .task(id: householdID) {
                guard let householdID else { return }
                await specialties.activate(householdID: householdID)
                // The strategy decides every ingredient nobody chose for, so the summary row
                // needs it. A failure leaves the row loading rather than blocking the list.
                try? await specialties.loadSettings()
            }
            .confirmationDialog("Use Suggested Options?", isPresented: $confirmsDefaults, titleVisibility: .visible) {
                Button("Use Suggested for All") { applyDefaults() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    "Every specialty ingredient without a choice gets its suggested option. Choices already made don't change."
                )
            }
            .alert(
                "Suggested Options",
                isPresented: Binding(presenting: $defaultsOutcome),
                presenting: defaultsOutcome
            ) { _ in
                Button("OK") {}
            } message: { outcome in
                Text(outcome.summary)
            }
            .alert(
                "Couldn't Update Specialty Ingredients",
                isPresented: Binding(presenting: $actionError),
                presenting: actionError
            ) { _ in
                Button("OK") {}
            } message: { message in
                Text(message)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch specialties.phase {
        case .idle, .loading:
            ProgressView("Loading specialty ingredients…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Specialty Ingredients", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await specialties.retry() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            if let refreshError = specialties.refreshError {
                FormErrorLabel(message: refreshError)
            }
            if specialties.items.isEmpty {
                ContentUnavailableView(
                    "No Specialty Ingredients", systemImage: "takeoutbag.and.cup.and.straw",
                    description: Text(
                        "None of this household's recipes call for meal-kit blends, sauces, or concentrates.")
                )
                .listRowBackground(Color.clear)
            } else {
                Section {
                    NavigationLink {
                        SpecialtyStrategyView()
                    } label: {
                        SpecialtyStrategySummaryRow(
                            settings: specialties.settings, error: specialties.settingsError)
                    }
                } footer: {
                    Text("What to do about the ones nobody has chosen an option for.")
                }
                Section {
                    Text(
                        "Meal-kit recipes call for blends, sauces, and concentrates that stores don't sell by that name. Choose a store alternative, a house-made batch, or keep one as is."
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    if canEdit && specialties.needsChoiceCount > 0 {
                        Button("Use Suggested for All", systemImage: "wand.and.stars") {
                            confirmsDefaults = true
                        }
                        .disabled(isApplyingDefaults)
                    }
                } footer: {
                    if !canEdit {
                        Text("Your role in this household can see these choices but not change them.")
                    } else if specialties.needsChoiceCount == 1 {
                        Text("1 specialty ingredient doesn't have a choice yet.")
                    } else if specialties.needsChoiceCount > 1 {
                        Text("\(specialties.needsChoiceCount) specialty ingredients don't have a choice yet.")
                    }
                }
                Section("Most Used") {
                    ForEach(specialties.items) { ingredient in
                        NavigationLink(value: SpecialtyRoute(specialtyID: ingredient.id)) {
                            SpecialtyIngredientRow(ingredient: ingredient)
                        }
                    }
                }
            }
        }
        .refreshable {
            await specialties.refresh()
        }
        .disabled(isApplyingDefaults)
        .overlay {
            if isApplyingDefaults {
                ProgressView()
                    .controlSize(.large)
            }
        }
    }

    private func applyDefaults() {
        Task {
            isApplyingDefaults = true
            defer { isApplyingDefaults = false }
            do {
                defaultsOutcome = try await specialties.applyDefaults()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                guard session.currentUser != nil else { return }
                actionError = HouseholdStore.message(for: error)
                if (error as? APIError)?.status == 403 {
                    // The role changed elsewhere; reload it so the screen matches.
                    await households.load()
                }
            }
        }
    }
}

/// One specialty ingredient: name, recipe count, choice, and batch status.
struct SpecialtyIngredientRow: View {
    let ingredient: SpecialtyIngredient

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let isLarge = dynamicTypeSize.isAccessibilitySize
        let layout =
            isLarge
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 3) {
                Text(ingredient.name)
                    .foregroundStyle(Color.primary)
                Text(SpecialtyFormat.recipeCount(ingredient.recipeCount))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if let optionName = SpecialtyFormat.choiceOptionName(ingredient) {
                    Text(optionName)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if let status = SpecialtyFormat.batchStatus(ingredient) {
                    Label(status, systemImage: "house")
                        .font(.footnote)
                        .foregroundStyle(batchColor)
                }
            }
            if !isLarge {
                Spacer(minLength: 8)
            }
            SpecialtyChoiceBadge(ingredient: ingredient)
        }
        // Inside a list link, hierarchical styles resolve against the tint; anchoring them to the
        // primary color keeps secondary text gray.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    private var batchColor: Color {
        switch ingredient.batch?.status {
        case .low: .orange
        case .out: .red
        default: .secondary
        }
    }
}

/// A colored capsule naming the choice: Not Set, Keep as Is, Store Alternative, or House-Made Batch.
struct SpecialtyChoiceBadge: View {
    let ingredient: SpecialtyIngredient

    var body: some View {
        Text(SpecialtyFormat.choiceBadge(ingredient))
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
    }

    private var color: Color {
        guard let type = ingredient.choice?.type else { return .orange }
        if type == .asIs { return .gray }
        if type == .houseMadeBatch { return .green }
        return .blue
    }
}

/// Sample data for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are type-checked
/// in Release builds too.
enum SpecialtyPreviewData {
    static let blendStore = option(
        id: "southwest-spice-blend.store", specialtyID: "southwest-spice-blend", type: .storeAlternative,
        name: "Southwest blend from the spice rack",
        per: amount("1", "tbsp"),
        ingredients: [("Chili Powder", "3/2", "tsp", "1 ½ tsp"), ("Ground Cumin", "3/4", "tsp", "¾ tsp")],
        summary: "1 tbsp = 1 ½ tsp Chili Powder + ¾ tsp Ground Cumin")

    static let blendBatch = option(
        id: "southwest-spice-blend.batch", specialtyID: "southwest-spice-blend", type: .houseMadeBatch,
        name: "Southwest spice blend (house blend)", isDefault: true,
        ingredients: [("Chili Powder", "3", "tbsp", "3 tbsp"), ("Ground Cumin", "2", "tbsp", "2 tbsp")],
        steps: ["Stir everything together in a bowl.", "Store in an airtight jar."], yield: amount("12", "tbsp"),
        shelfLifeDays: 180, summary: "Makes about 12 tbsp and keeps 180 days.")

    static let southwest = SpecialtyIngredient(
        id: "southwest-spice-blend", key: "southwest spice blend", name: "Southwest Spice Blend", aliases: [],
        category: "spices", ingredientIDs: [], recipeCount: 12, unitSizes: [],
        defaultOptionID: "southwest-spice-blend.batch", retired: false,
        choice: SpecialtyChoice(
            optionID: blendBatch.id, type: .houseMadeBatch, optionName: blendBatch.name,
            chosenBy: HouseholdPreviewData.user.id, chosenAt: .now),
        options: [blendStore, blendBatch],
        batch: SpecialtyBatchStock(
            pantryItemID: "p9", status: .low, remaining: amount("2", "tbsp"), percentRemaining: 17,
            expiresOn: nil))

    static let glaze = SpecialtyIngredient(
        id: "sweet-soy-glaze", key: "sweet soy glaze", name: "Sweet Soy Glaze", aliases: [], category: "condiments",
        note: "Bottled sweet soy glaze is usually in the international aisle.",
        ingredientIDs: [], recipeCount: 8, unitSizes: [], defaultOptionID: "sweet-soy-glaze.store", retired: false,
        choice: nil,
        options: [
            option(
                id: "sweet-soy-glaze.store", specialtyID: "sweet-soy-glaze", type: .storeAlternative,
                name: "Soy and honey", isDefault: true, per: amount("1", "tbsp"),
                ingredients: [("Soy Sauce", "2", "tsp", "2 tsp"), ("Honey", "1", "tsp", "1 tsp")],
                summary: "1 tbsp = 2 tsp Soy Sauce + 1 tsp Honey")
        ],
        batch: nil)

    /// Nobody chose this one: the household's `similar` default picked the store alternative.
    static let paste = SpecialtyIngredient(
        id: "tex-mex-paste", key: "tex mex paste", name: "Tex-Mex Paste", aliases: [], category: "condiments",
        ingredientIDs: [], recipeCount: 6, unitSizes: [], defaultOptionID: "tex-mex-paste.store", retired: false,
        choice: SpecialtyChoice(
            optionID: "tex-mex-paste.store", type: .storeAlternative, optionName: "Tomato paste and chili spices",
            source: .strategy, strategy: .similar),
        options: [
            option(
                id: "tex-mex-paste.store", specialtyID: "tex-mex-paste", type: .storeAlternative,
                name: "Tomato paste and chili spices", isDefault: true, per: amount("1", "tbsp"),
                ingredients: [("Tomato Paste", "2", "tsp", "2 tsp"), ("Chili Powder", "1/2", "tsp", "½ tsp")],
                summary: "1 tbsp = 2 tsp Tomato Paste + ½ tsp Chili Powder")
        ],
        batch: nil,
        choiceSource: .strategy)

    static let items = [southwest, paste, glaze]

    /// The three strategies as the server words them. Sample data standing in for a response:
    /// the app itself never hardcodes this copy.
    static let strategyOptions = [
        SpecialtyStrategyOption(
            value: .similar, label: "Something similar",
            description: "Buy something close from the store — quicker, tastes a little different"),
        SpecialtyStrategyOption(
            value: .closest, label: "As close as possible",
            description: "Make a jar you reuse across several meals — more work, closest to the original"),
        SpecialtyStrategyOption(
            value: .ask, label: "Ask me each time",
            description: "Leave each specialty ingredient on the list until someone picks an option"),
    ]

    static func settings(strategy: SpecialtyStrategy = .similar, wasSet: Bool = true) -> SpecialtySettings {
        SpecialtySettings(
            strategy: strategy,
            updatedBy: wasSet ? HouseholdPreviewData.user.id : nil,
            updatedAt: wasSet ? .now : nil,
            options: strategyOptions)
    }

    static func store(session: AuthSession, settings: SpecialtySettings? = nil) -> SpecialtyStore {
        .preview(
            session: session, items: items, householdID: HouseholdPreviewData.household.id, settings: settings)
    }

    /// A household store whose role may lack `pantry.edit`, for the read-only previews.
    static func householdStore(session: AuthSession, canEdit: Bool = true) -> HouseholdStore {
        guard !canEdit else { return HouseholdPreviewData.store(session: session) }
        let detail = HouseholdDetail(
            household: HouseholdPreviewData.household,
            members: HouseholdPreviewData.detail.members,
            role: .member,
            permissions: [.householdView, .membersView])
        return .preview(
            session: session, phase: .ready, current: detail,
            households: [
                HouseholdListItem(
                    household: HouseholdPreviewData.household, role: .member, permissions: detail.permissions)
            ],
            invitations: [])
    }

    private static func amount(_ quantity: String, _ unit: String) -> SpecialtyAmount {
        SpecialtyAmount(
            quantity: quantity, quantityValue: Double(quantity) ?? 1, unit: unit, text: "\(quantity) \(unit)")
    }

    private static func option(
        id: String, specialtyID: String, type: SpecialtyOptionType, name: String, isDefault: Bool = false,
        per: SpecialtyAmount? = nil, ingredients: [(String, String, String, String)], steps: [String] = [],
        yield batchYield: SpecialtyAmount? = nil, shelfLifeDays: Int? = nil, summary: String
    ) -> SpecialtyOption {
        SpecialtyOption(
            id: id, specialtyID: specialtyID, source: .curated, type: type, name: name, notes: "",
            isDefault: isDefault, per: per,
            ingredients: ingredients.map { name, quantity, unit, text in
                SpecialtyOptionIngredient(
                    name: name, quantity: quantity, quantityValue: nil, unit: unit, text: "\(text) \(name)",
                    category: nil)
            },
            steps: steps, batchYield: batchYield, shelfLifeDays: shelfLifeDays, basedOnOptionID: nil,
            summary: summary, createdBy: nil, updatedBy: nil, createdAt: nil, updatedAt: nil)
    }
}

#Preview("List") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        SpecialtyIngredientsView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(SpecialtyPreviewData.store(session: session))
}
