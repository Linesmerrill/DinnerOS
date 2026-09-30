import SwiftUI

/// Every household setting, shown in place and saved as it changes — there is no Edit mode
/// and no Save button (`HouseholdSettingsAutosave`).
///
/// Members without `household.update` see the same values as plain text. The server enforces
/// the permission regardless.
struct HouseholdSettingsSections: View {
    let settings: HouseholdSettingsAutosave
    /// Shown in the Household section; `nil` leaves the row out.
    var role: HouseholdRole?

    @FocusState private var focus: TextSetting?
    /// A week start picked and not yet confirmed; it moves meals, so it's asked about first.
    @State private var proposedWeekStart: PlanDay?
    /// The Custom bag size alert, and what's typed in it.
    @State private var asksCustomBag = false
    @State private var customBagText = ""

    private enum TextSetting: Hashable {
        case name, mealKitAmount
    }

    private var draft: HouseholdSettingsDraft { settings.draft }
    private var saved: Household { settings.confirmed }

    var body: some View {
        Group {
            if case .failed(let message) = settings.status {
                Section {
                    FormErrorLabel(message: message)
                    if settings.canEdit {
                        Button("Try Again", systemImage: "arrow.clockwise") { settings.flush() }
                    }
                }
            }
            householdSection
            weekSection
            orderSection
            thawSection
            sharingSection
            mealKitSection
        }
        // Modifiers here would be copied onto every section; the screen-wide ones are in
        // `householdSettingsAutosave(_:)` on the list itself.
    }

    // MARK: - Sections

    private var householdSection: some View {
        Section {
            if settings.canEdit {
                LabeledContent("Name") {
                    TextField(
                        "Household name", text: binding(\.name, delay: HouseholdSettingsAutosave.typingDelay)
                    )
                    .multilineTextAlignment(.trailing)
                    .textInputAutocapitalization(.words)
                    .submitLabel(.done)
                    .focused($focus, equals: .name)
                    .onSubmit { settings.flush() }
                    // Leaving the field sends the name now rather than after the pause.
                    .onChange(of: focus) { old, _ in
                        if old == .name { settings.flush() }
                    }
                }
                if let error = draft.nameError {
                    FormErrorLabel(message: error)
                }
                NavigationLink {
                    TimeZonePicker(selection: binding(\.timeZone))
                } label: {
                    LabeledContent("Time Zone", value: TimeZonePicker.summary(for: draft.timeZone))
                }
                Stepper(value: binding(\.defaultServings), in: 1...12) {
                    LabeledContent("Default Servings", value: draft.defaultServings.formatted())
                }
            } else {
                LabeledContent("Name", value: saved.name)
                LabeledContent("Time Zone", value: TimeZonePicker.summary(for: saved.timeZone))
                LabeledContent("Default Servings", value: saved.defaultServings.formatted())
            }
            if let role {
                LabeledContent("Your Role", value: role.displayName)
            }
        } header: {
            HStack {
                Text("Household")
                Spacer()
                AutosaveStatusLabel(settings: settings)
            }
        } footer: {
            if settings.canEdit {
                Text(
                    "Weekly plans follow the household's time zone and start from its default servings. Changes save as you make them."
                )
            } else {
                Text("Only a household admin can change these settings.")
            }
        }
    }

    private var weekSection: some View {
        Section {
            if settings.canEdit {
                Picker("Week Starts On", selection: weekStartBinding) {
                    ForEach(WeekStartChoices.days, id: \.self) { day in
                        Text(day.name()).tag(day)
                    }
                }
                // Attached to the picker, not the list: on iOS 26 the dialog is a popover
                // that points at whatever view it's attached to.
                .confirmationDialog(
                    Text("Start weeks on \(proposedWeekStart?.name() ?? "")?"),
                    isPresented: Binding(presenting: $proposedWeekStart),
                    titleVisibility: .visible,
                    presenting: proposedWeekStart
                ) { day in
                    Button("Start Weeks on \(day.name())") {
                        // Confirmed is the member's "done": it goes now, not after a pause.
                        settings.edit(\.weekStartsOn, to: day, delay: .zero)
                    }
                    Button("Cancel", role: .cancel) {}
                } message: { _ in
                    Text(
                        "Meals keep their dates. A meal that now falls in a different week moves to that week's plan and grocery list, for everyone in the household."
                    )
                }
            } else {
                LabeledContent("Week Starts On", value: saved.weekStartsOn.name())
            }
        } header: {
            Text("Week")
        } footer: {
            Text("Meals keep their dates; a meal that now falls in another week moves to that week's list.")
        }
    }

    private var orderSection: some View {
        Section {
            if settings.canEdit {
                Picker("Order Day", selection: binding(\.orderDay)) {
                    Text("No reminder").tag("")
                    ForEach(OrderDay.codes, id: \.self) { code in
                        Text(OrderDay.name(code)).tag(code)
                    }
                }
            } else {
                LabeledContent(
                    "Order Day",
                    value: saved.orderDay.map { OrderDay.name($0) } ?? String(localized: "No reminder"))
            }
        } header: {
            Text("Grocery Order")
        } footer: {
            Text(
                "Pick the day you usually order. From that day, Shop reminds the household until someone marks the week ordered, and phones that allow notifications get one that morning. Next week starts fresh."
            )
        }
    }

    /// The offered sizes, plus the household's own if it picked one the list doesn't have.
    private var freezeBagChoices: [Int] {
        let current = draft.freezeBagOunces
        return Household.freezeBagOunces.contains(current)
            ? Household.freezeBagOunces : (Household.freezeBagOunces + [current]).sorted()
    }

    /// The picker's tag for Custom, which asks for a number instead of saving.
    private static let customBagTag = -1

    private var freezeBagBinding: Binding<Int> {
        Binding(
            get: { settings.draft.freezeBagOunces },
            set: { ounces in
                if ounces == Self.customBagTag {
                    let current = settings.draft.freezeBagOunces
                    customBagText = current > 0 ? String(current) : ""
                    asksCustomBag = true
                } else {
                    settings.edit(\.freezeBagOunces, to: ounces, delay: HouseholdSettingsAutosave.controlDelay)
                }
            })
    }

    private var thawSection: some View {
        Section {
            if settings.canEdit {
                Picker("Reminder Time", selection: binding(\.thawReminderHour)) {
                    ForEach(Household.thawReminderHours, id: \.self) { hour in
                        Text(Household.thawHourText(hour)).tag(hour)
                    }
                }
            } else {
                LabeledContent("Reminder Time", value: Household.thawHourText(saved.thawReminderHour))
            }
            if settings.canEdit {
                Picker("Freezer Bag", selection: freezeBagBinding) {
                    ForEach(freezeBagChoices, id: \.self) { ounces in
                        Text(Household.freezeBagText(ounces)).tag(ounces)
                    }
                    Text("Custom…").tag(Self.customBagTag)
                }
                .alert("Freezer Bag", isPresented: $asksCustomBag) {
                    TextField("Ounces", text: $customBagText)
                        .keyboardType(.numberPad)
                    Button("Save") {
                        if let ounces = Household.customBagOunces(customBagText) {
                            settings.edit(\.freezeBagOunces, to: ounces, delay: .zero)
                        }
                    }
                    .disabled(Household.customBagOunces(customBagText) == nil)
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text("How many ounces in each bag? 1 to \(Household.maxFreezeBagOunces).")
                }
            } else {
                LabeledContent("Freezer Bag", value: Household.freezeBagText(saved.freezeMinOunces ?? 0))
            }
            if settings.canEdit {
                Picker("How You Freeze Meat", selection: binding(\.freezerWrap)) {
                    ForEach(FreezerWrap.allCases) { wrap in
                        Text(wrap.title).tag(wrap)
                    }
                }
            } else {
                LabeledContent("How You Freeze Meat", value: saved.freezerWrap.title)
            }
        } header: {
            Text("Freezer")
        } footer: {
            Text(
                "We remind you the morning a frozen item needs to thaw.\nLeftover meat gets frozen in bags this size. Less than a bag goes into the meal or gets tossed.\nFrozen raw meat gets a best-by date from the FDA's freezer times. Vacuum sealed keeps longest."
            )
        }
    }

    private var sharingSection: some View {
        Section {
            if settings.canEdit {
                Picker("Share With Other Households", selection: binding(\.catalogSharing)) {
                    ForEach(CatalogSharing.allCases) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
            } else {
                LabeledContent("Share With Other Households", value: saved.catalogSharing.title)
            }
        } header: {
            Text("Recipe Sharing")
        } footer: {
            Text(sharingFooter)
        }
    }

    private var sharingFooter: String {
        switch draft.catalogSharing {
        case .off:
            String(localized: "Your recipes stay yours. You can still browse and add recipes others share.")
        case .all:
            String(
                localized:
                    "Other households can find your recipes in Shared Recipes and add a copy. Your notes, ratings, and plans are never shared."
            )
        case .chosen:
            String(
                localized: "Only recipes you mark as shared can be found by others. Turn it on from a recipe's page.")
        }
    }

    private var mealKitSection: some View {
        Section {
            if settings.canEdit {
                LabeledContent("Weekly Amount") {
                    TextField(
                        "What did you spend on meal kits?",
                        text: binding(\.mealKitAmount, delay: HouseholdSettingsAutosave.typingDelay),
                        prompt: Text("For example 130")
                    )
                    .multilineTextAlignment(.trailing)
                    .keyboardType(.decimalPad)
                    .focused($focus, equals: .mealKitAmount)
                    .onChange(of: focus) { old, _ in
                        if old == .mealKitAmount { settings.flush() }
                    }
                }
                Stepper(value: binding(\.mealKitMeals), in: MealKitInput.mealsRange) {
                    LabeledContent("Meals per Week", value: draft.mealKitMeals.formatted())
                }
                if let error = draft.mealKitError {
                    FormErrorLabel(message: error)
                }
                if saved.mealKit != nil || !draft.mealKitAmount.isEmpty {
                    Button("Clear Meal Kit Comparison", role: .destructive) {
                        focus = nil
                        settings.edit(\.mealKitAmount, to: "")
                    }
                }
            } else {
                LabeledContent(
                    "Meal Kit", value: saved.mealKit.map { WeekCostText.mealKit($0) } ?? String(localized: "Not set"))
            }
        } header: {
            Text("Meal Kit Comparison")
        } footer: {
            Text(
                "Shop compares your grocery cost per meal with a week of meal kits, for example $130 for 5 meals. Leave it empty to turn the comparison off."
            )
        }
    }

    // MARK: - Pieces

    /// A control's value, saved `delay` after the member's last change to it.
    private func binding<Value: Equatable>(
        _ field: WritableKeyPath<HouseholdSettingsDraft, Value>,
        delay: Duration = HouseholdSettingsAutosave.controlDelay
    ) -> Binding<Value> {
        Binding(
            get: { settings.draft[keyPath: field] },
            set: { settings.edit(field, to: $0, delay: delay) })
    }

    /// Picking the week start the server already has just takes it back; any other day is
    /// confirmed first.
    private var weekStartBinding: Binding<PlanDay> {
        Binding(
            get: { settings.draft.weekStartsOn },
            set: { day in
                if day == saved.weekStartsOn {
                    settings.edit(\.weekStartsOn, to: day, delay: .zero)
                } else {
                    proposedWeekStart = day
                }
            })
    }
}

/// Quiet on purpose: nothing while a change waits for the member to pause, a spinner while
/// it's out, "Saved" for a moment after. A failure also gets its own row, with a retry.
private struct AutosaveStatusLabel: View {
    let settings: HouseholdSettingsAutosave

    @State private var showsSaved = false

    var body: some View {
        Group {
            switch settings.status {
            case .saving:
                HStack(spacing: 4) {
                    ProgressView()
                        .controlSize(.mini)
                    Text("Saving…")
                }
                .accessibilityElement(children: .combine)
            case .saved where showsSaved && !settings.draft.hasValidationError:
                Label("Saved", systemImage: "checkmark")
                    .labelStyle(.titleAndIcon)
            case .failed:
                Label("Not Saved", systemImage: "exclamationmark.circle")
                    .foregroundStyle(.red)
            default:
                EmptyView()
            }
        }
        .task(id: settings.saveCount) {
            guard settings.saveCount > 0 else { return }
            showsSaved = true
            try? await Task.sleep(for: .seconds(2))
            showsSaved = false
        }
    }
}

/// What the settings need from the screen that shows them, attached once to that screen's
/// list rather than to the sections (where it would run once per section).
private struct HouseholdSettingsAutosaveModifier: ViewModifier {
    let settings: HouseholdSettingsAutosave?

    /// Optional so previews needn't supply one.
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            // Leaving the screen or the app sends what's waiting now.
            .onChange(of: scenePhase) { _, phase in
                if phase != .active { settings?.flush() }
            }
            .onDisappear { settings?.flush() }
            .onChange(of: settings?.saveCount) {
                // Picking an order day is asking to be reminded, so it's the moment to ask
                // whether the reminder may reach a locked phone. Only asked once per device.
                if settings?.lastSaved?.orderDay?.isEmpty == false {
                    Task { await push?.requestAuthorizationIfNeeded() }
                }
            }
    }
}

extension View {
    /// Flushes pending settings when the screen or app goes away, and asks for notification
    /// permission after an order day is saved.
    func householdSettingsAutosave(_ settings: HouseholdSettingsAutosave?) -> some View {
        modifier(HouseholdSettingsAutosaveModifier(settings: settings))
    }
}

/// The settings on their own screen, for the Shop tab's "Compare with a Meal Kit". Changes
/// save as they're made, so the only button closes it.
struct HouseholdSettingsForm: View {
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            if let settings = households.settings {
                HouseholdSettingsSections(settings: settings)
            }
        }
        .householdSettingsAutosave(households.settings)
        .navigationTitle("Household Settings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Done") { dismiss() }
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        HouseholdSettingsForm()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
}
