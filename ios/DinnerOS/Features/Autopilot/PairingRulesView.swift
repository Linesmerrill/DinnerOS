import SwiftUI

/// The household's add-on pairing rules: "Pasta → Garlic Bread", "Soup → Club Crackers".
///
/// The whole list is saved with one `PATCH .../autopilot/profile` carrying the `pairings`
/// array, keeping each saved rule's `id` so the server doesn't recreate it.
struct PairingRulesView: View {
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var rules: [PairingRule]?
    @State private var editing: PairingRule?
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var limits: AutopilotLimits { autopilot.limits }

    private var saved: [PairingRule] { autopilot.profile?.pairings ?? [] }

    private var hasChanges: Bool {
        guard let rules else { return false }
        return rules != saved
    }

    private var isFull: Bool {
        (rules?.count ?? 0) >= limits.maxPairingRules
    }

    var body: some View {
        List {
            if let rules {
                Section {
                    if rules.isEmpty {
                        Text("No rules yet. Autopilot still suggests add-ons it has learned from your weeks.")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(Array(rules.enumerated()), id: \.element.identity) { _, rule in
                            row(rule)
                        }
                        .onDelete(perform: delete)
                        .deleteDisabled(!canEdit)
                    }
                } header: {
                    Text("Rules")
                } footer: {
                    Text(
                        "A rule adds something to every matching meal. “Always” includes it with Autopilot's picks; “Suggest” offers it."
                    )
                }
                if canEdit {
                    Section {
                        Button("Add a Rule", systemImage: "plus") {
                            editing = .new()
                        }
                        .disabled(isFull)
                    } footer: {
                        if isFull {
                            Text("You can have up to \(limits.maxPairingRules) rules.")
                        }
                    }
                }
                Section {
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        SectionAttributionText(change: autopilot.profile?.sections[.pairings])
                        if !canEdit {
                            Text("Your role can see pairing rules but not change them.")
                        }
                    }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
            }
        }
        .navigationTitle("Pairings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
        .disabled(isSaving)
        .onAppear {
            if rules == nil {
                rules = saved
            }
        }
        .sheet(item: $editing) { rule in
            PairingRuleEditor(rule: rule, vocabulary: autopilot.vocabulary, limits: limits) { edited in
                apply(edited)
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if canEdit {
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Save", action: save)
                        .disabled(!hasChanges)
                }
            }
        }
    }

    private func row(_ rule: PairingRule) -> some View {
        Button {
            guard canEdit else { return }
            editing = rule
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(PairingFormat.ruleTitle(rule, vocabulary: autopilot.vocabulary))
                    .foregroundStyle(Color.primary)
                Text(PairingFormat.ruleSummary(rule, vocabulary: autopilot.vocabulary))
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(canEdit ? .isButton : [])
    }

    // MARK: Editing

    /// Replaces an edited rule, or appends a new one. New rules are matched on `identity`,
    /// which is their client id until the server assigns one.
    private func apply(_ edited: PairingRule) {
        var updated = rules ?? []
        if let index = updated.firstIndex(where: { $0.identity == edited.identity }) {
            updated[index] = edited
        } else {
            updated.append(edited)
        }
        rules = updated
        errorMessage = nil
    }

    private func delete(at offsets: IndexSet) {
        guard canEdit else { return }
        rules?.remove(atOffsets: offsets)
        errorMessage = nil
    }

    private func save() {
        guard let rules, var settings = autopilot.profile?.settings else { return }
        if let message = PairingInput.validationMessage(for: rules, limits: limits) {
            errorMessage = message
            return
        }
        settings.pairings = rules
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                try await autopilot.saveSections([.pairings], from: settings)
                self.rules = autopilot.profile?.pairings
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Adds or edits one rule: what it matches, what it adds, and how often.
struct PairingRuleEditor: View {
    let vocabulary: AutopilotVocabulary?
    let limits: AutopilotLimits
    let save: (PairingRule) -> Void

    @Environment(\.dismiss) private var dismiss

    @State private var rule: PairingRule
    @State private var quantityText: String
    @State private var isPickingRecipe = false

    init(
        rule: PairingRule, vocabulary: AutopilotVocabulary?, limits: AutopilotLimits,
        save: @escaping (PairingRule) -> Void
    ) {
        self.vocabulary = vocabulary
        self.limits = limits
        self.save = save
        _rule = State(initialValue: rule)
        _quantityText = State(
            initialValue: rule.add.groceryItem?.quantity.map {
                $0.formatted(.number.precision(.fractionLength(0...2)))
            } ?? "")
    }

    private var validationMessage: String? {
        PairingInput.validationMessage(for: rule, limits: limits)
    }

    private var addKind: PairingKind {
        rule.add.kind ?? .groceryItem
    }

    var body: some View {
        NavigationStack {
            Form {
                whenSection
                addSection
                frequencySection
                Section {
                    if let validationMessage {
                        FormErrorLabel(message: validationMessage)
                    }
                }
            }
            .navigationTitle(rule.id == nil ? "New Rule" : "Edit Rule")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        save(normalized())
                        dismiss()
                    }
                    .disabled(validationMessage != nil)
                }
            }
            .navigationDestination(isPresented: $isPickingRecipe) {
                AddonRecipePicker { summary in
                    rule.add = .recipe(id: summary.id, name: summary.name)
                    isPickingRecipe = false
                }
            }
        }
    }

    // MARK: When

    private var whenSection: some View {
        Group {
            Section {
                let options = vocabulary?.mealCategoryOptions ?? []
                ChipFlowLayout {
                    ForEach(options) { option in
                        let category = MealCategory(rawValue: option.value)
                        let isSelected = rule.when.mealCategories.contains(category)
                        ChoiceChip(title: option.label, count: option.recipeCount, state: isSelected ? .on : .off) {
                            rule.setMealCategory(
                                category, included: !isSelected, order: options.map(\.value))
                        }
                    }
                }
                .padding(.vertical, 4)
            } header: {
                Text("When You Plan")
            } footer: {
                Text("The kind of dinner this goes with. Numbers are your recipes in each.")
            }
            Section("Cuisines (Optional)") {
                ValueChipGroup(
                    options: vocabulary?.cuisines ?? [], selection: $rule.when.cuisines,
                    maxCount: limits.maxRuleValues, maxLength: limits.maxValueLength, addPrompt: "Add a cuisine",
                    initialCount: 8)
            }
            Section("Food Types (Optional)") {
                ValueChipGroup(
                    options: vocabulary?.tags ?? [], selection: $rule.when.tags, maxCount: limits.maxRuleValues,
                    maxLength: limits.maxValueLength, addPrompt: "Add a food type", initialCount: 8)
            }
            Section {
                ValueChipGroup(
                    options: vocabulary?.proteins ?? [], selection: $rule.when.proteins,
                    maxCount: limits.maxRuleValues, order: (vocabulary?.proteins ?? []).map(\.value))
            } header: {
                Text("Proteins (Optional)")
            } footer: {
                Text("A meal has to match every group you set.")
            }
        }
    }

    // MARK: Add

    private var addSection: some View {
        Section {
            Picker(
                "Add",
                selection: Binding(get: { addKind }, set: { rule.setAddKind($0) })
            ) {
                Text("Add-on Recipe").tag(PairingKind.recipe)
                Text("Grocery Item").tag(PairingKind.groceryItem)
            }
            .pickerStyle(.segmented)

            switch addKind {
            case .recipe:
                Button {
                    isPickingRecipe = true
                } label: {
                    LabeledContent("Recipe") {
                        Text(rule.add.recipeName ?? String(localized: "Choose…"))
                            .foregroundStyle(rule.add.recipeID == nil ? Color.secondary : Color.primary)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            case .groceryItem:
                TextField("Name, like Club Crackers", text: groceryName)
                HStack {
                    TextField("Amount", text: $quantityText)
                        .keyboardType(.decimalPad)
                        .onChange(of: quantityText) { _, text in
                            setQuantity(text)
                        }
                    TextField("Unit, like package", text: groceryUnit)
                        .textInputAutocapitalization(.never)
                }
            }
        } header: {
            Text("What to Add")
        } footer: {
            Text(
                addKind == .recipe
                    ? String(localized: "An add-on from your recipes, planned on the same day as the meal.")
                    : String(localized: "A plain line on the week's grocery list. An amount is optional.")
            )
        }
    }

    private var frequencySection: some View {
        Section {
            Picker("How Often", selection: $rule.frequency) {
                ForEach(vocabulary?.pairingFrequencyOptions ?? [], id: \.value) { option in
                    Text(option.label).tag(PairingFrequency(rawValue: option.value))
                }
            }
        } footer: {
            Text("“Always” is added with Autopilot's picks unless you take it out. “Suggest” is offered.")
        }
    }

    // MARK: Bindings

    private var groceryName: Binding<String> {
        Binding(
            get: { rule.add.groceryItem?.name ?? "" },
            set: { name in
                var item = rule.add.groceryItem ?? PairingGroceryItem(name: "")
                item = PairingGroceryItem(
                    name: String(name.prefix(limits.maxGroceryItemNameLength)), quantity: item.quantity,
                    unit: item.unit)
                rule.add = .groceryItem(item)
            })
    }

    private var groceryUnit: Binding<String> {
        Binding(
            get: { rule.add.groceryItem?.unit ?? "" },
            set: { unit in
                var item = rule.add.groceryItem ?? PairingGroceryItem(name: "")
                item.unit = unit.isEmpty ? nil : unit
                rule.add = .groceryItem(item)
            })
    }

    private func setQuantity(_ text: String) {
        var item = rule.add.groceryItem ?? PairingGroceryItem(name: "")
        item.quantity = text.isEmpty ? nil : Double(text)
        rule.add = .groceryItem(item)
    }

    /// Trims the grocery name before saving, the way the API stores it.
    private func normalized() -> PairingRule {
        var copy = rule
        if var item = copy.add.groceryItem {
            item = PairingGroceryItem(
                name: PairingInput.normalizedName(item.name), quantity: item.quantity, unit: item.unit)
            copy.add = .groceryItem(item)
        }
        copy.label = PairingInput.normalizedName(copy.label)
        return copy
    }
}

/// Picks an add-on recipe for a rule, over its own copy of the library so the Menu's search
/// doesn't change. Only add-ons can be paired, so the list is filtered to them.
struct AddonRecipePicker: View {
    let choose: (RecipeSummary) -> Void

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households

    @State private var picker: RecipeLibrary?
    @State private var searchText = ""

    private static let searchDebounce = Duration.milliseconds(350)

    var body: some View {
        Group {
            if let picker {
                content(picker)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Choose an Add-On")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $searchText, prompt: "Search add-ons")
        .task {
            guard let householdID = households.current?.household.id else { return }
            let picker = picker ?? library.makeIndependentCopy()
            self.picker = picker
            await picker.activate(householdID: householdID)
        }
        .task(id: searchText) {
            guard let picker, searchText != picker.filters.search else { return }
            do {
                try await Task.sleep(for: Self.searchDebounce)
            } catch {
                return
            }
            await picker.setSearch(searchText)
        }
    }

    @ViewBuilder
    private func content(_ picker: RecipeLibrary) -> some View {
        // The list endpoint has no add-on filter, so they're picked out here; paging keeps
        // loading until the add-ons appear.
        let addons = picker.items.filter(\.isAddon)
        List {
            ForEach(addons) { summary in
                Button {
                    choose(summary)
                } label: {
                    RecipeRow(summary: summary)
                }
                .buttonStyle(.plain)
            }
            if picker.hasMore {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .task(id: picker.nextCursor) {
                        await picker.loadMore()
                    }
            }
        }
        .overlay {
            if addons.isEmpty, !picker.hasMore, picker.phase == .loaded {
                ContentUnavailableView(
                    "No Add-Ons", systemImage: "takeoutbag.and.cup.and.straw",
                    description: Text("Add-ons like garlic bread come from your imported orders."))
            }
        }
    }
}

#Preview("Rules") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        PairingRulesView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}

#Preview("Rule editor") {
    PairingRuleEditor(
        rule: AutopilotPreviewData.pairingRules[0], vocabulary: AutopilotPreviewData.vocabulary,
        limits: .defaults
    ) { _ in }
    .environment(HouseholdPreviewData.store(session: HouseholdPreviewData.session()))
}

#Preview("New rule") {
    PairingRuleEditor(rule: .new(), vocabulary: AutopilotPreviewData.vocabulary, limits: .defaults) { _ in }
        .environment(HouseholdPreviewData.store(session: HouseholdPreviewData.session()))
}
