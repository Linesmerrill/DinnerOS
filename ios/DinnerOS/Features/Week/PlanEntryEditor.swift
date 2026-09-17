import SwiftUI

/// Changes a planned entry's day, servings, and note, or removes it.
struct PlanEntryEditor: View {
    let entry: PlanEntry
    let week: ISOWeek

    @Environment(PlanStore.self) private var plans
    @Environment(RecipeLibrary.self) private var library
    @Environment(\.dismiss) private var dismiss

    @State private var day: PlanDay?
    @State private var servings: Int
    @State private var note: String
    /// The recipe's sizes; only the entry's own size until the recipe loads.
    @State private var servingOptions: [Int]
    @State private var optionsError: String?
    @State private var isSaving = false
    @State private var saveError: String?
    @State private var confirmingRemoval = false

    init(entry: PlanEntry, week: ISOWeek) {
        self.entry = entry
        self.week = week
        _day = State(initialValue: entry.day)
        _servings = State(initialValue: entry.servings)
        _note = State(initialValue: entry.note)
        _servingOptions = State(initialValue: [entry.servings])
    }

    private var changes: PlanEntryChanges {
        let trimmedNote = note.trimmingCharacters(in: .whitespacesAndNewlines)
        return PlanEntryChanges(
            day: day == entry.day ? nil : .some(day),
            servings: servings == entry.servings ? nil : servings,
            note: trimmedNote == entry.note ? nil : trimmedNote)
    }

    private var noteTooLong: Bool {
        note.count > PlanLimits.maxNoteLength
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(entry.recipe.name)
                        .font(.headline)
                }
                Section {
                    DayPicker(week: week, selection: $day)
                    Picker("Servings", selection: $servings) {
                        ForEach(servingOptions, id: \.self) { size in
                            Text("\(size) servings").tag(size)
                        }
                    }
                } footer: {
                    if let optionsError {
                        Text(optionsError)
                    }
                }
                NoteSection(note: $note)
                if let saveError {
                    Section {
                        FormErrorLabel(message: saveError)
                    }
                }
                Section {
                    Button("Remove from Week", role: .destructive) {
                        confirmingRemoval = true
                    }
                    .disabled(isSaving)
                }
            }
            .navigationTitle("Edit Recipe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save") { save() }
                            .disabled(changes.isEmpty || noteTooLong)
                    }
                }
            }
            .confirmationDialog(
                "Remove \(entry.recipe.name) from this week?", isPresented: $confirmingRemoval,
                titleVisibility: .visible
            ) {
                Button("Remove", role: .destructive) { remove() }
            }
            .interactiveDismissDisabled(isSaving)
            .task { await loadServingOptions() }
        }
    }

    private func loadServingOptions() async {
        do {
            let options = try await library.recipe(id: entry.recipe.id).servingOptions
            // Keep the entry's size visible even if the recipe no longer offers it.
            servingOptions = options.contains(entry.servings) ? options : (options + [entry.servings]).sorted()
            optionsError =
                options.contains(entry.servings)
                ? nil : String(localized: "This recipe no longer offers \(entry.servings) servings.")
        } catch is CancellationError {
            return
        } catch {
            optionsError = String(localized: "Couldn't load this recipe's serving sizes.")
        }
    }

    private func save() {
        run { try await plans.updateEntry(id: entry.id, changes: changes) }
    }

    private func remove() {
        run { try await plans.deleteEntry(id: entry.id) }
    }

    private func run(_ change: @escaping () async throws -> Void) {
        isSaving = true
        saveError = nil
        Task {
            defer { isSaving = false }
            do {
                try await change()
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                saveError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// A day of `week`, or no day.
struct DayPicker: View {
    let week: ISOWeek
    @Binding var selection: PlanDay?

    @Environment(PlanStore.self) private var plans

    var body: some View {
        Picker("Day", selection: $selection) {
            Text("No Day").tag(PlanDay?.none)
            ForEach(PlanDay.week(startingOn: plans.weekStartsOn)) { day in
                Text(day.title(in: week, weekStartsOn: plans.weekStartsOn)).tag(PlanDay?.some(day))
            }
        }
    }
}

/// A note field with the API's length limit.
struct NoteSection: View {
    @Binding var note: String

    var body: some View {
        Section {
            TextField("Note", text: $note, prompt: Text("Optional, like “double the lime”"), axis: .vertical)
                .lineLimit(1...4)
        } footer: {
            if note.count > PlanLimits.maxNoteLength {
                Text("Notes can be up to \(PlanLimits.maxNoteLength) characters.")
                    .foregroundStyle(.red)
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    PlanEntryEditor(entry: PlanPreviewData.plan.entries[0], week: PlanPreviewData.week)
        .environment(RecipePreviewData.library(session: session))
        .environment(PlanPreviewData.store(session: session))
}
