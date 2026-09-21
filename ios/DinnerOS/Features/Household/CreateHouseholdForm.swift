import SwiftUI

/// Creates a household. Pushed from onboarding, or presented in a sheet from the
/// Household tab (which passes `onCreated` to dismiss).
struct CreateHouseholdForm: View {
    var onCreated: () -> Void = {}

    @Environment(HouseholdStore.self) private var households
    /// Optional so previews and the Household tab's sheet needn't supply one.
    @Environment(MealKitImportStore.self) private var mealKit: MealKitImportStore?
    @State private var name = ""
    /// The household that was just created, which the import offer is for.
    @State private var createdHouseholdID: String?
    @State private var timeZone = TimeZone.current.identifier
    @State private var isSaving = false
    @State private var errorMessage: String?
    @FocusState private var isNameFocused: Bool

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        Form {
            Section {
                TextField("Household name", text: $name)
                    .textInputAutocapitalization(.words)
                    .submitLabel(.done)
                    .focused($isNameFocused)
                    .onSubmit { Task { await create() } }
            } footer: {
                Text("For example, “The Lovelace Kitchen.” You can change it later.")
            }
            Section {
                NavigationLink {
                    TimeZonePicker(selection: $timeZone)
                } label: {
                    LabeledContent("Time Zone", value: TimeZonePicker.summary(for: timeZone))
                }
            } footer: {
                Text("Weekly plans follow this time zone.")
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
        }
        .navigationTitle("New Household")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Create") {
                        Task { await create() }
                    }
                    .disabled(trimmedName.isEmpty)
                }
            }
        }
        .disabled(isSaving)
        .onAppear { isNameFocused = true }
        // Onboarding's second step: bring the recipes you already ordered, or skip and add
        // them by hand. Pushed rather than presented, so Back is never a dead end.
        .navigationDestination(item: $createdHouseholdID) { _ in
            MealKitImportOfferView(onDone: onCreated)
        }
    }

    private func create() async {
        guard !trimmedName.isEmpty, !isSaving else { return }
        isSaving = true
        errorMessage = nil
        defer { isSaving = false }
        do {
            try await households.createHousehold(name: trimmedName, timeZone: timeZone)
            // Offer the import only when there is a store to do it with and the new
            // household is the selected one; otherwise this was the last step.
            if let mealKit, let householdID = households.current?.household.id {
                mealKit.activate(householdID: householdID)
                createdHouseholdID = householdID
            } else {
                onCreated()
            }
        } catch is CancellationError {
            // The screen went away.
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        CreateHouseholdForm()
    }
    .environment(session)
    .environment(HouseholdStore.preview(session: session, phase: .needsHousehold))
}
