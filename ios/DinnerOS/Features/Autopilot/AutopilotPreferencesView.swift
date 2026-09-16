import SwiftUI

/// Every Autopilot preference, one screen per section, with who changed it last and a
/// change history. Reachable from the Household tab and the Week tab's menu.
struct AutopilotPreferencesView: View {
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households

    @State private var isOnboarding = false

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    var body: some View {
        content
            .navigationTitle("Autopilot Preferences")
            .navigationBarTitleDisplayMode(.inline)
            .task(id: households.current?.household.id) {
                guard let householdID = households.current?.household.id else { return }
                await autopilot.activate(householdID: householdID)
            }
            .sheet(isPresented: $isOnboarding) {
                AutopilotOnboardingView()
            }
    }

    @ViewBuilder
    private var content: some View {
        switch autopilot.phase {
        case .idle, .loading:
            ProgressView("Loading preferences…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Preferences", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await autopilot.reloadProfile() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            if let profile = autopilot.profile {
                list(profile)
            }
        }
    }

    private func list(_ profile: AutopilotProfile) -> some View {
        List {
            if !profile.configured {
                Section {
                    VStack(alignment: .leading, spacing: 10) {
                        Label("Autopilot isn't set up yet", systemImage: "sparkles")
                            .font(.headline)
                        Text("Until it is, Autopilot uses the usual settings below.")
                            .foregroundStyle(.secondary)
                        if canEdit {
                            Button("Set Up Autopilot") { isOnboarding = true }
                                .buttonStyle(.borderedProminent)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
            // Setup's three questions first, then everything it no longer asks — which is
            // where the cook-time mix, equipment, weekday rules, and pairings now live.
            Section {
                ForEach(AutopilotSection.allCases.filter(\.isInSetup)) { section in
                    row(section, profile: profile)
                }
            } header: {
                Text("From Setup")
            }
            Section {
                ForEach(AutopilotSection.allCases.filter { !$0.isInSetup }) { section in
                    row(section, profile: profile)
                }
            } header: {
                Text("Fine-tune Autopilot")
            } footer: {
                Text(
                    "Setup doesn't ask about these; Autopilot uses sensible defaults until you change them. Every change records who made it and when."
                )
            }
            Section {
                NavigationLink {
                    AutopilotHistoryView()
                } label: {
                    Label("Change History", systemImage: "clock.arrow.circlepath")
                }
            }
            if profile.configured, canEdit {
                Section {
                    Button("Run Setup Again", systemImage: "wand.and.stars") { isOnboarding = true }
                } footer: {
                    Text("Asks the three setup questions again, starting from your current answers.")
                }
            }
        }
        .refreshable { await autopilot.reloadProfile() }
    }

    private func row(_ section: AutopilotSection, profile: AutopilotProfile) -> some View {
        NavigationLink {
            // Pairing rules are a list of rules, not one form, so they have their own screen.
            if section == .pairings {
                PairingRulesView()
            } else {
                AutopilotSectionEditor(section: section)
            }
        } label: {
            SectionRow(section: section, profile: profile, vocabulary: autopilot.vocabulary)
        }
    }
}

private struct SectionRow: View {
    let section: AutopilotSection
    let profile: AutopilotProfile
    let vocabulary: AutopilotVocabulary?

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(section.title)
                Text(section.detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(AutopilotFormat.sectionSummary(section, settings: profile.settings, vocabulary: vocabulary))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                SectionAttributionText(change: profile.sections[section])
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: section.systemImage)
                .foregroundStyle(.tint)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Edits one section and saves it whole with `PATCH`.
struct AutopilotSectionEditor: View {
    let section: AutopilotSection

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var settings: AutopilotSettings?
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    private var changedSections: Set<AutopilotSection> {
        guard let settings, let original = autopilot.profile?.settings else { return [] }
        return settings.sectionsDiffering(from: original)
    }

    var body: some View {
        Form {
            if let vocabulary = autopilot.vocabulary, let binding = Binding($settings) {
                AutopilotSectionForm(
                    section: section, settings: binding, vocabulary: vocabulary,
                    householdServings: households.current?.household.defaultServings)
            }
            Section {
                if let errorMessage {
                    FormErrorLabel(message: errorMessage)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    SectionAttributionText(change: autopilot.profile?.sections[section])
                    if !canEdit {
                        Text("Your role can see Autopilot preferences but not change them.")
                    }
                }
            }
        }
        .disabled(!canEdit || isSaving)
        .navigationTitle(section.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if canEdit {
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save", action: save)
                            .disabled(!changedSections.contains(section))
                    }
                }
            }
        }
        .onAppear {
            if settings == nil {
                settings = autopilot.profile?.settings
            }
        }
    }

    private func save() {
        guard let settings else { return }
        if let message = settings.validationMessage {
            errorMessage = message
            return
        }
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                try await autopilot.saveSections([section], from: settings)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// Preference, week context, and recipe override changes, newest first.
struct AutopilotHistoryView: View {
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session
    @Environment(RecipeLibrary.self) private var library

    @State private var items: [AutopilotHistoryItem]?
    @State private var loadError: String?

    var body: some View {
        Group {
            if let items {
                if items.isEmpty {
                    ContentUnavailableView(
                        "No Changes Yet", systemImage: "clock",
                        description: Text("Changes to preferences, week plans, and recipe settings appear here."))
                } else {
                    List(Array(items.enumerated()), id: \.offset) { _, item in
                        row(item)
                    }
                }
            } else if let loadError {
                ContentUnavailableView {
                    Label("Couldn't Load History", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(loadError)
                } actions: {
                    Button("Try Again") {
                        Task { await load() }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else {
                ProgressView("Loading history…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Change History")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func row(_ item: AutopilotHistoryItem) -> some View {
        let recipeName = item.recipeID.flatMap { id in
            library.cachedRecipe(id: id)?.name ?? library.items.first { $0.id == id }?.name
        }
        let who = AutopilotFormat.memberName(
            item.userID, members: households.current?.members, currentUserID: session.currentUser?.id)
        return VStack(alignment: .leading, spacing: 4) {
            Text(AutopilotFormat.historyTitle(item, vocabulary: autopilot.vocabulary, recipeName: recipeName))
                .font(.headline)
            ForEach(AutopilotFormat.historyLines(item, vocabulary: autopilot.vocabulary), id: \.self) { line in
                Text(line)
                    .font(.subheadline)
            }
            Text("\(who) · \(item.occurredAt.formatted(date: .abbreviated, time: .shortened))")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private func load() async {
        do {
            items = try await autopilot.history()
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        AutopilotPreferencesView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(RecipePreviewData.library(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}
