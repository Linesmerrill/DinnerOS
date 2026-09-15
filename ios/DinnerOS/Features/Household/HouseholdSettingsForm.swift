import SwiftUI

/// Renames the household and changes its time zone and default servings. Shown only
/// to members with `household.update`; the server enforces it regardless.
struct HouseholdSettingsForm: View {
    let household: Household

    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @State private var name: String
    @State private var timeZone: String
    @State private var defaultServings: Int
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(household: Household) {
        self.household = household
        _name = State(initialValue: household.name)
        _timeZone = State(initialValue: household.timeZone)
        _defaultServings = State(initialValue: household.defaultServings)
    }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Only the fields that changed, so a save never overwrites someone else's edit to
    /// a field this screen didn't touch.
    private var changes: HouseholdChanges {
        HouseholdChanges(
            name: trimmedName == household.name ? nil : trimmedName,
            timeZone: timeZone == household.timeZone ? nil : timeZone,
            defaultServings: defaultServings == household.defaultServings ? nil : defaultServings)
    }

    var body: some View {
        Form {
            Section("Name") {
                TextField("Household name", text: $name)
                    .textInputAutocapitalization(.words)
            }
            Section {
                NavigationLink {
                    TimeZonePicker(selection: $timeZone)
                } label: {
                    LabeledContent("Time Zone", value: TimeZonePicker.summary(for: timeZone))
                }
                Stepper(value: $defaultServings, in: 1...12) {
                    LabeledContent("Default Servings", value: defaultServings.formatted())
                }
            } footer: {
                Text("Weekly plans follow the household's time zone and start from its default servings.")
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
        }
        .navigationTitle("Household Settings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(trimmedName.isEmpty || changes.isEmpty)
                }
            }
        }
        .disabled(isSaving)
        .interactiveDismissDisabled(isSaving)
    }

    private func save() async {
        isSaving = true
        errorMessage = nil
        defer { isSaving = false }
        do {
            try await households.updateHousehold(changes)
            dismiss()
        } catch is CancellationError {
            // The sheet went away.
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        HouseholdSettingsForm(household: HouseholdPreviewData.household)
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
}
