import SwiftUI

/// Creates a household. Pushed from onboarding, or presented in a sheet from the
/// Household tab (which passes `onCreated` to dismiss).
struct CreateHouseholdForm: View {
    var onCreated: () -> Void = {}

    @Environment(HouseholdStore.self) private var households
    @State private var name = ""
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
    }

    private func create() async {
        guard !trimmedName.isEmpty, !isSaving else { return }
        isSaving = true
        errorMessage = nil
        defer { isSaving = false }
        do {
            try await households.createHousehold(name: trimmedName, timeZone: timeZone)
            onCreated()
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
