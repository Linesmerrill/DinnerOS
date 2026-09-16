import SwiftUI

/// Renames the household and changes its time zone and default servings. Shown only
/// to members with `household.update`; the server enforces it regardless.
struct HouseholdSettingsForm: View {
    let household: Household

    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    /// Optional so previews needn't supply one.
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @State private var name: String
    @State private var timeZone: String
    @State private var defaultServings: Int
    /// The API's weekday code, or "" for no reminder.
    @State private var orderDay: String
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(household: Household) {
        self.household = household
        _name = State(initialValue: household.name)
        _timeZone = State(initialValue: household.timeZone)
        _defaultServings = State(initialValue: household.defaultServings)
        _orderDay = State(initialValue: household.orderDay ?? "")
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
            defaultServings: defaultServings == household.defaultServings ? nil : defaultServings,
            // "" clears the order day; nil would leave it alone.
            orderDay: orderDay == (household.orderDay ?? "") ? nil : orderDay)
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
            Section {
                Picker("Order Day", selection: $orderDay) {
                    Text("No reminder").tag("")
                    ForEach(OrderDay.codes, id: \.self) { code in
                        Text(OrderDay.name(code)).tag(code)
                    }
                }
            } header: {
                Text("Grocery Order")
            } footer: {
                Text(
                    "Pick the day you usually order. From that day, Shop reminds the household until someone marks the week ordered, and phones that allow notifications get one that morning. Next week starts fresh."
                )
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
        let pickedOrderDay = changes.orderDay?.isEmpty == false
        do {
            try await households.updateHousehold(changes)
            dismiss()
            // Picking an order day is asking to be reminded, so it's the moment to ask
            // whether the reminder may reach a locked phone. Only asked once per device.
            if pickedOrderDay {
                await push?.requestAuthorizationIfNeeded()
            }
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
