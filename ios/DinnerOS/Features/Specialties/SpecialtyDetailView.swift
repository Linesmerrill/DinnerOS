import SwiftUI

/// One specialty ingredient's options: choose one, keep it as is, customize a copy, edit the
/// household's own, or record a house-made batch.
struct SpecialtyDetailView: View {
    let specialtyID: String
    /// Called after an option is chosen here, for example to close the grocery list's picker.
    var onChoose: (() -> Void)?

    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    @State private var sheet: Sheet?
    @State private var isWorking = false
    @State private var actionError: String?
    @State private var loadError: String?
    @State private var confirmsClear = false
    @State private var pendingDeletion: SpecialtyOption?

    private enum Sheet: Identifiable {
        case customize(SpecialtyIngredient, SpecialtyOption)
        case edit(SpecialtyIngredient, SpecialtyOption)
        case madeBatch(SpecialtyIngredient, SpecialtyOption)

        var id: String {
            switch self {
            case .customize(_, let option): "customize-\(option.id)"
            case .edit(_, let option): "edit-\(option.id)"
            case .madeBatch(_, let option): "batch-\(option.id)"
            }
        }
    }

    private var ingredient: SpecialtyIngredient? {
        specialties.ingredient(withID: specialtyID)
    }

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    var body: some View {
        content
            .navigationTitle(ingredient?.name ?? String(localized: "Specialty Ingredient"))
            .navigationBarTitleDisplayMode(.inline)
            .task(id: households.current?.household.id) {
                await load()
            }
            .onChange(of: canEdit) { _, canEdit in
                if !canEdit {
                    sheet = nil
                }
            }
            .sheet(item: $sheet) { sheet in
                switch sheet {
                case .customize(let ingredient, let option):
                    SpecialtyOptionEditor(specialty: ingredient, mode: .customize(option))
                case .edit(let ingredient, let option):
                    SpecialtyOptionEditor(specialty: ingredient, mode: .edit(option))
                case .madeBatch(let ingredient, let option):
                    SpecialtyBatchSheet(ingredient: ingredient, option: option)
                }
            }
            .confirmationDialog(
                "Clear the Choice?", isPresented: $confirmsClear, titleVisibility: .visible
            ) {
                Button("Clear Choice", role: .destructive) { clearChoice() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Grocery lists will ask about it again.")
            }
            .confirmationDialog(
                "Delete This Option?",
                isPresented: Binding(presenting: $pendingDeletion),
                titleVisibility: .visible,
                presenting: pendingDeletion
            ) { option in
                Button("Delete \(option.name)", role: .destructive) { delete(option) }
                Button("Cancel", role: .cancel) {}
            } message: { _ in
                Text("If it's the household's choice, the choice is cleared too and grocery lists ask again.")
            }
            .alert(
                "Couldn't Update \(ingredient?.name ?? String(localized: "Specialty Ingredient"))",
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
        if let ingredient {
            detail(ingredient)
        } else if case .failed(let message) = specialties.phase {
            unavailable(message)
        } else if let loadError {
            unavailable(loadError)
        } else {
            ProgressView("Loading…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func unavailable(_ message: String) -> some View {
        ContentUnavailableView {
            Label("Couldn't Load This Ingredient", systemImage: "exclamationmark.triangle")
        } description: {
            Text(message)
        } actions: {
            Button("Try Again") {
                Task {
                    loadError = nil
                    await specialties.retry()
                    await load()
                }
            }
            .buttonStyle(.borderedProminent)
        }
    }

    private func detail(_ ingredient: SpecialtyIngredient) -> some View {
        List {
            if let refreshError = specialties.refreshError {
                FormErrorLabel(message: refreshError)
            }
            choiceSection(ingredient)
            ForEach(ingredient.options) { option in
                optionSection(option, in: ingredient)
            }
            if canEdit || ingredient.isKeptAsIs {
                asIsSection(ingredient)
            }
        }
        .refreshable {
            _ = try? await specialties.reload(specialtyID: specialtyID)
        }
        .disabled(isWorking)
        .overlay {
            if isWorking {
                ProgressView()
                    .controlSize(.large)
            }
        }
    }

    private func choiceSection(_ ingredient: SpecialtyIngredient) -> some View {
        Section {
            Text(SpecialtyFormat.recipeCount(ingredient.recipeCount))
            LabeledContent("Choice") {
                SpecialtyChoiceBadge(ingredient: ingredient)
            }
            if let optionName = SpecialtyFormat.choiceOptionName(ingredient) {
                LabeledContent("Option", value: optionName)
            }
            // Nobody chose a strategy's pick, so it is never attributed to a member.
            if let note = SpecialtyFormat.strategyNote(ingredient) {
                Label(note, systemImage: "sparkles")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let attribution = SpecialtyFormat.choiceAttribution(
                ingredient, members: households.current?.members, currentUserID: session.currentUser?.id)
            {
                Text(attribution)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let status = SpecialtyFormat.batchStatus(ingredient) {
                LabeledContent("House-Made Batch", value: status)
                if let expiry = PantryExpiry(expiresOn: ingredient.batch?.expiresOn) {
                    Label(expiry.text(), systemImage: expiry.isExpired ? "exclamationmark.circle" : "calendar")
                        .font(.footnote)
                        .foregroundStyle(expiry.isExpired ? Color.red : (expiry.isSoon ? .orange : .secondary))
                }
            }
            if canEdit, ingredient.hasBatchChoice, let option = ingredient.chosenOption {
                Button("Made a Batch", systemImage: "checkmark.seal") {
                    sheet = .madeBatch(ingredient, option)
                }
                .accessibilityHint("Records a batch of \(ingredient.name) in the pantry.")
            }
            // Only a member's own choice can be cleared: a strategy's pick isn't stored, so
            // there would be nothing to un-pick.
            if canEdit, ingredient.hasHouseholdChoice {
                Button("Clear Choice", systemImage: "arrow.uturn.backward", role: .destructive) {
                    confirmsClear = true
                }
            }
        } footer: {
            if !ingredient.aliases.isEmpty {
                Text("Also called \(ingredient.aliases.formatted(.list(type: .or))).")
            } else if !canEdit {
                Text("Your role in this household can see these options but not change them.")
            }
        }
    }

    private func optionSection(_ option: SpecialtyOption, in ingredient: SpecialtyIngredient) -> some View {
        Section {
            SpecialtyOptionCard(
                option: option, specialtyName: ingredient.name, isChosen: ingredient.isHouseholdChoice(option),
                isStrategyPick: ingredient.isResolvedByStrategy && ingredient.isChosen(option))
            if canEdit {
                // A strategy's pick stays offerable, so a member can make it their own choice.
                if !ingredient.isHouseholdChoice(option) {
                    Button("Use This Option", systemImage: "checkmark.circle") {
                        choose(option.id, in: ingredient)
                    }
                }
                if ingredient.canAddHouseholdOption {
                    Button("Customize…", systemImage: "slider.horizontal.3") {
                        sheet = .customize(ingredient, option)
                    }
                    .accessibilityHint("Copies this option so you can change it.")
                }
                if option.isHousehold {
                    Button("Edit…", systemImage: "pencil") {
                        sheet = .edit(ingredient, option)
                    }
                    Button("Delete…", systemImage: "trash", role: .destructive) {
                        pendingDeletion = option
                    }
                }
            }
        } header: {
            Text(option.type.title)
        }
    }

    private func asIsSection(_ ingredient: SpecialtyIngredient) -> some View {
        Section {
            if ingredient.isKeptAsIs {
                Label("Kept as Is", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.tint)
            } else if canEdit {
                Button("Keep as Is", systemImage: "text.badge.checkmark") {
                    choose(SpecialtyChoice.asIsOptionID, in: ingredient)
                }
            }
        } header: {
            Text("Keep as Is")
        } footer: {
            Text(
                "Keeps \(ingredient.name) on grocery lists by its own name and stops asking. Choose this if you buy it somewhere or don't want to be asked."
            )
        }
    }

    // MARK: - Actions

    private func load() async {
        guard let householdID = households.current?.household.id else { return }
        await specialties.activate(householdID: householdID)
        // A grocery list can open an ingredient the list doesn't include.
        guard specialties.phase == .loaded, ingredient == nil else { return }
        do {
            loadError = nil
            try await specialties.reload(specialtyID: specialtyID)
        } catch is CancellationError {
            // The next appearance loads again.
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func choose(_ optionID: String, in ingredient: SpecialtyIngredient) {
        perform {
            try await specialties.choose(optionID: optionID, forSpecialtyWithID: ingredient.id)
            onChoose?()
        }
    }

    private func clearChoice() {
        perform { try await specialties.clearChoice(specialtyID: specialtyID) }
    }

    private func delete(_ option: SpecialtyOption) {
        perform { try await specialties.deleteOption(optionID: option.id, specialtyID: specialtyID) }
    }

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            isWorking = true
            defer { isWorking = false }
            do {
                try await action()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                // After a sign-out the root view replaces this screen, so no error is shown.
                guard session.currentUser != nil else { return }
                actionError = HouseholdStore.message(for: error)
                if (error as? APIError)?.status == 403 {
                    await households.load()
                }
            }
        }
    }
}

/// An option's name, summary, ingredients, and for a batch its steps, yield, and shelf life.
struct SpecialtyOptionCard: View {
    let option: SpecialtyOption
    let specialtyName: String
    /// A member chose this option.
    let isChosen: Bool
    /// The household's standing strategy picked this option; nobody chose it.
    var isStrategyPick = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(option.name)
                    .font(.headline)
                Spacer(minLength: 4)
                if isChosen || isStrategyPick {
                    Image(systemName: isChosen ? "checkmark.circle.fill" : "sparkles")
                        .foregroundStyle(.tint)
                        .imageScale(.large)
                        .accessibilityHidden(true)
                }
            }
            if !tags.isEmpty {
                Text(tags.joined(separator: " · "))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
            }
            Text(option.summary)
                .font(.subheadline)
            if !option.notes.isEmpty {
                Text(option.notes)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            ingredientList
            if option.isBatch {
                batchDetails
            }
        }
        .padding(.vertical, 4)
    }

    private var tags: [String] {
        var tags: [String] = []
        if isChosen { tags.append(String(localized: "Chosen")) }
        if isStrategyPick { tags.append(String(localized: "Your Default")) }
        if option.isDefault { tags.append(String(localized: "Suggested")) }
        if option.isHousehold { tags.append(String(localized: "Your Household's")) }
        return tags
    }

    @ViewBuilder
    private var ingredientList: some View {
        if !option.ingredients.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                if let replaces = SpecialtyFormat.replaces(option, specialtyName: specialtyName) {
                    Text(replaces)
                        .font(.subheadline.weight(.semibold))
                } else {
                    Text("Ingredients")
                        .font(.subheadline.weight(.semibold))
                }
                ForEach(Array(option.ingredients.enumerated()), id: \.offset) { _, ingredient in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(verbatim: "•")
                            .accessibilityHidden(true)
                        Text(ingredient.text)
                    }
                    .font(.subheadline)
                }
            }
        }
    }

    private var batchDetails: some View {
        VStack(alignment: .leading, spacing: 4) {
            if !option.steps.isEmpty {
                Text("Steps")
                    .font(.subheadline.weight(.semibold))
                ForEach(Array(option.steps.enumerated()), id: \.offset) { index, step in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(verbatim: "\(index + 1).")
                            .monospacedDigit()
                        Text(step)
                    }
                    .font(.subheadline)
                    .accessibilityElement(children: .combine)
                }
            }
            if let amount = option.batchYield {
                LabeledContent("Makes", value: amount.text)
                    .font(.subheadline)
            }
            if let days = option.shelfLifeDays {
                LabeledContent("Keeps", value: SpecialtyFormat.days(days))
                    .font(.subheadline)
            }
        }
    }
}

/// "Made a Batch": how many batches were made, recorded as a house-made pantry purchase.
struct SpecialtyBatchSheet: View {
    let ingredient: SpecialtyIngredient
    let option: SpecialtyOption

    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var batches = 1
    /// Created once, so trying again after a failure can't record the batch twice.
    @State private var clientPurchaseID = UUID().uuidString
    @State private var isSaving = false
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    Stepper(value: $batches, in: 1...SpecialtiesAPI.maxBatches) {
                        LabeledContent("Batches", value: batches.formatted())
                    }
                    if let amount = option.batchYield {
                        LabeledContent("Each Makes", value: amount.text)
                    }
                    if let days = option.shelfLifeDays {
                        LabeledContent("Keeps", value: SpecialtyFormat.days(days))
                    }
                } header: {
                    Text(option.name)
                } footer: {
                    Text(
                        "The pantry starts a new estimate for \(ingredient.name) from this amount, and cooking recipes that use it counts against it."
                    )
                }
            }
            .navigationTitle("Made a Batch")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Record") { save() }
                        .disabled(isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            do {
                try await specialties.recordBatch(
                    specialtyID: ingredient.id, optionID: option.id, batches: batches,
                    clientPurchaseID: clientPurchaseID)
                dismiss()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                errorMessage = HouseholdStore.message(for: error)
                if (error as? APIError)?.status == 403 {
                    await households.load()
                }
            }
        }
    }
}

#Preview("Detail") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        SpecialtyDetailView(specialtyID: SpecialtyPreviewData.southwest.id)
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(SpecialtyPreviewData.store(session: session))
}
