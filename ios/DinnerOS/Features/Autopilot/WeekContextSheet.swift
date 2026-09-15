import SwiftUI

/// "This Week's Plans": what's special about one week (vacation, a busy week, guests),
/// edited as a whole and saved with one `PUT`, or cleared.
struct WeekContextSheet: View {
    let week: ISOWeek
    /// Called after saving or clearing, so the Week tab can offer to plan again.
    var onChange: (() -> Void)?

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var original: AutopilotWeekContext?
    @State private var draft = AutopilotWeekContextDraft()
    @State private var loadError: String?
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var isConfirmingClear = false

    private var limits: AutopilotLimits { autopilot.limits }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var hasChanges: Bool {
        guard let original else { return false }
        return draft != original.draft
    }

    private var noteTooLong: Bool {
        draft.trimmedNote.count > limits.maxNoteLength
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("This Week's Plans")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    if canEdit {
                        ToolbarItem(placement: .confirmationAction) {
                            if isSaving {
                                ProgressView()
                            } else {
                                Button("Save", action: save)
                                    .disabled(!hasChanges || noteTooLong)
                            }
                        }
                    }
                }
                .confirmationDialog(
                    "Clear this week's plans?", isPresented: $isConfirmingClear, titleVisibility: .visible
                ) {
                    Button("Clear", role: .destructive, action: clear)
                } message: {
                    Text("Autopilot goes back to your usual preferences for this week.")
                }
        }
        .interactiveDismissDisabled(isSaving || hasChanges)
        .task { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if original != nil {
            form
        } else if let loadError {
            ContentUnavailableView {
                Label("Couldn't Load This Week", systemImage: "exclamationmark.triangle")
            } description: {
                Text(loadError)
            } actions: {
                Button("Try Again") {
                    Task { await load() }
                }
                .buttonStyle(.borderedProminent)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var form: some View {
        Form {
            Section {
                Toggle("Skip This Week", isOn: $draft.skip)
            } header: {
                Text(week.weekOf())
            } footer: {
                Text("Away or eating out? Autopilot won't plan anything this week.")
            }
            if !draft.skip {
                Section {
                    Toggle("Busy Week", isOn: $draft.busy)
                } footer: {
                    Text("Favors quick meals and skips long cooks, without a strict limit.")
                }
                Section {
                    OptionalNumberRow(
                        title: String(localized: "Strict Time Limit"), value: $draft.maxMinutes,
                        range: limits.minCookMinutes...limits.maxCookMinutes, step: 5, defaultValue: 20,
                        format: { String(localized: "Up to \(RecipeFormat.minutes($0))") })
                } footer: {
                    Text("Only recipes known to fit are suggested, so some nights may stay open.")
                }
                Section {
                    OptionalNumberRow(
                        title: String(localized: "Servings This Week"), value: $draft.servings,
                        range: 1...limits.maxServings,
                        defaultValue: min((households.current?.household.defaultServings ?? 2) + 2, limits.maxServings),
                        format: { String(localized: "\($0) servings") })
                    OptionalNumberRow(
                        title: String(localized: "Meals This Week"), value: $draft.mealsPerWeek, range: 1...7,
                        defaultValue: autopilot.profile?.schedule.mealsPerWeek ?? 4,
                        format: { $0 == 1 ? String(localized: "1 meal") : String(localized: "\($0) meals") })
                } header: {
                    Text("Guests and Extra Meals")
                }
                Section {
                    ForEach(PlanDay.allCases) { day in
                        NavigationLink {
                            DayOverrideForm(title: day.title(in: week), limits: limits, override: $draft[day])
                                .disabled(!canEdit)
                        } label: {
                            LabeledContent(day.title(in: week), value: Self.summary(draft[day]))
                        }
                    }
                } header: {
                    Text("Days")
                } footer: {
                    Text("Skip a night, set a limit, or add guests for one day.")
                }
            }
            Section {
                TextField(
                    "Note", text: $draft.note, prompt: Text("Optional, like “Grandparents visiting Friday”"),
                    axis: .vertical
                )
                .lineLimit(1...4)
            } header: {
                Text("Note")
            } footer: {
                if noteTooLong {
                    Text("Notes can be up to \(limits.maxNoteLength) characters.")
                        .foregroundStyle(.red)
                } else {
                    Text("Kept for your household. Autopilot doesn't read notes yet.")
                }
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            if canEdit, original?.configured == true {
                Section {
                    Button("Clear This Week's Plans", role: .destructive) {
                        isConfirmingClear = true
                    }
                } footer: {
                    attribution
                }
            } else {
                Section {
                } footer: {
                    attribution
                }
            }
        }
        .disabled(!canEdit || isSaving)
    }

    @ViewBuilder
    private var attribution: some View {
        if let original, let userID = original.updatedBy, let date = original.updatedAt {
            SectionAttributionText(change: AutopilotSectionChange(updatedBy: userID, updatedAt: date))
        }
    }

    static func summary(_ override: AutopilotDayOverride) -> String {
        if override.skip {
            return String(localized: "Skip")
        }
        var parts: [String] = []
        if let maxMinutes = override.maxMinutes {
            parts.append(String(localized: "Up to \(RecipeFormat.minutes(maxMinutes))"))
        }
        if let servings = override.servings {
            parts.append(String(localized: "Serves \(servings)"))
        }
        return parts.isEmpty ? String(localized: "As usual") : parts.joined(separator: " · ")
    }

    // MARK: - Actions

    private func load() async {
        loadError = nil
        do {
            let loaded = try await autopilot.refreshContext(week: week)
            if original == nil || !hasChanges {
                draft = loaded.draft
            }
            original = loaded
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func save() {
        run { try await autopilot.saveContext(draft, week: week) }
    }

    private func clear() {
        run { try await autopilot.clearContext(week: week) }
    }

    private func run(_ change: @escaping () async throws -> Void) {
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                try await change()
                dismiss()
                onChange?()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// One day's skip, time limit, and servings.
private struct DayOverrideForm: View {
    let title: String
    let limits: AutopilotLimits
    @Binding var override: AutopilotDayOverride

    var body: some View {
        Form {
            Section {
                Toggle("Skip This Day", isOn: $override.skip)
            } footer: {
                Text("Autopilot won't plan a meal this day.")
            }
            if !override.skip {
                Section {
                    OptionalNumberRow(
                        title: String(localized: "Strict Time Limit"), value: $override.maxMinutes,
                        range: limits.minCookMinutes...limits.maxCookMinutes, step: 5, defaultValue: 20,
                        format: { String(localized: "Up to \(RecipeFormat.minutes($0))") })
                    OptionalNumberRow(
                        title: String(localized: "Servings"), value: $override.servings, range: 1...limits.maxServings,
                        defaultValue: 6, format: { String(localized: "\($0) servings") })
                } footer: {
                    Text("Having guests? Set the servings for just this day.")
                }
            }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    WeekContextSheet(week: AutopilotPreviewData.week)
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(AutopilotPreviewData.store(session: session))
}
