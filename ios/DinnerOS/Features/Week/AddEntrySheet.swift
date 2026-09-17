import SwiftUI

/// Adds one recipe to a week with a day, serving size, and note.
///
/// With `fixedWeek` it adds to that week (from the Add Recipes sheet). Without, it
/// offers this week and the next several (from a recipe's screen).
struct AddEntrySheet: View {
    let recipeID: String
    let recipeName: String
    let fixedWeek: ISOWeek?
    var initialDay: PlanDay?
    var onAdded: (() -> Void)?

    @Environment(PlanStore.self) private var plans
    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var week: ISOWeek?
    @State private var day: PlanDay?
    @State private var servings: Int?
    @State private var servingOptions: [Int] = []
    @State private var note = ""
    @State private var summaries: [String: PlanSummary] = [:]
    @State private var loadError: String?
    @State private var isSaving = false
    @State private var saveError: String?

    /// Weeks offered when no week is fixed: this week and the next seven.
    static let upcomingWeekCount = 8

    init(
        recipeID: String, recipeName: String, fixedWeek: ISOWeek?, initialDay: PlanDay? = nil,
        onAdded: (() -> Void)? = nil
    ) {
        self.recipeID = recipeID
        self.recipeName = recipeName
        self.fixedWeek = fixedWeek
        self.initialDay = initialDay
        self.onAdded = onAdded
        _week = State(initialValue: fixedWeek)
        _day = State(initialValue: initialDay)
    }

    private var weekOptions: [ISOWeek] {
        let current = plans.currentWeek
        return (0..<Self.upcomingWeekCount).map { current.adding(weeks: $0) }
    }

    private var canSave: Bool {
        week != nil && servings != nil && !isSaving && note.count <= PlanLimits.maxNoteLength
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(recipeName)
                        .font(.headline)
                }
                Section {
                    if fixedWeek == nil {
                        Picker("Week", selection: $week) {
                            ForEach(weekOptions, id: \.self) { option in
                                Text(weekTitle(option)).tag(ISOWeek?.some(option))
                            }
                        }
                    }
                    if let week {
                        DayPicker(week: week, selection: $day)
                    }
                    servingsPicker
                } footer: {
                    if let week, let summary = summaries[week.description], summary.status == .finalized {
                        Text("That week is finalized. Reopen it from the Week tab before adding recipes.")
                    }
                }
                NoteSection(note: $note)
                if let saveError {
                    Section {
                        FormErrorLabel(message: saveError)
                    }
                }
            }
            .navigationTitle(fixedWeek == nil ? "Add to Week" : "Add Recipe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Add") { save() }
                            .disabled(!canSave)
                    }
                }
            }
            .interactiveDismissDisabled(isSaving)
            .task { await prepare() }
        }
    }

    @ViewBuilder
    private var servingsPicker: some View {
        if let loadError {
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: loadError)
                Button("Try Again") {
                    Task { await loadRecipe() }
                }
            }
        } else if servingOptions.isEmpty {
            LabeledContent("Servings") {
                ProgressView()
            }
        } else {
            Picker("Servings", selection: $servings) {
                ForEach(servingOptions, id: \.self) { size in
                    Text("\(size) servings").tag(Int?.some(size))
                }
            }
        }
    }

    private func weekTitle(_ option: ISOWeek) -> String {
        let current = plans.currentWeek
        let range = option.rangeLabel(weekStartsOn: plans.weekStartsOn)
        let relative: String? =
            switch option {
            case current: String(localized: "This Week")
            case current.next: String(localized: "Next Week")
            default: nil
            }
        var title = relative.map { "\($0), \(range)" } ?? range
        if let summary = summaries[option.description] {
            if summary.status == .finalized {
                title += " · " + summary.status.title
            } else if summary.entryCount > 0 {
                title += " · " + String(localized: "\(summary.entryCount) planned")
            }
        }
        return title
    }

    private func prepare() async {
        if let household = households.current?.household {
            await plans.activate(
                householdID: household.id, timeZone: household.planningTimeZone, weekStartsOn: household.weekStartsOn)
        }
        if week == nil {
            week = plans.currentWeek
        }
        async let recipe: Void = loadRecipe()
        if fixedWeek == nil, let first = weekOptions.first, let last = weekOptions.last {
            // Counts are a hint; the picker works without them.
            let loaded = (try? await plans.summaries(from: first, to: last)) ?? []
            summaries = Dictionary(loaded.map { ($0.week, $0) }, uniquingKeysWith: { _, last in last })
        }
        await recipe
    }

    private func loadRecipe() async {
        loadError = nil
        do {
            let recipe = try await library.recipe(id: recipeID)
            servingOptions = recipe.servingOptions
            if servingOptions.isEmpty {
                loadError = String(localized: "This recipe has no serving sizes, so it can't be planned.")
            } else if servings == nil || !servingOptions.contains(servings ?? 0) {
                servings = recipe.preferredServings(householdDefault: households.current?.household.defaultServings)
            }
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func save() {
        guard let week, let servings else { return }
        let entry = NewPlanEntry(
            recipeID: recipeID, day: day, servings: servings,
            note: note.trimmingCharacters(in: .whitespacesAndNewlines))
        isSaving = true
        saveError = nil
        Task {
            defer { isSaving = false }
            do {
                try await plans.addEntry(entry, to: week)
                onAdded?()
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                saveError = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    AddEntrySheet(recipeID: "recipe-1", recipeName: "Skillet Test Tacos", fixedWeek: nil)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(RecipePreviewData.library(session: session))
        .environment(PlanPreviewData.store(session: session))
}
