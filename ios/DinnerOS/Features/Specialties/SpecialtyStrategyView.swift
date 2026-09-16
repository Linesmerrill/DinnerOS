import SwiftUI

/// The household's standing answer for the specialty ingredients nobody has chosen an option for.
///
/// Every label and description is the server's (`SpecialtySettings.options`), so the trade-off is
/// worded in one place and this screen never hardcodes it. Choosing saves at once: nothing is
/// stored per ingredient, so the setup list reloads and every unchosen ingredient resolves again.
///
/// Changing is hidden without `pantry.edit`. That's a convenience only: the API checks every
/// change and answers `403` if a hidden rule applies.
struct SpecialtyStrategyView: View {
    @Environment(SpecialtyStore.self) private var specialties
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    /// The strategy being saved, so only its row shows progress.
    @State private var saving: SpecialtyStrategy?
    @State private var loadError: String?
    @State private var errorMessage: String?

    private var canEdit: Bool {
        households.access?.can(.pantryEdit) == true
    }

    private var settings: SpecialtySettings? {
        specialties.settings
    }

    var body: some View {
        List {
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            content
        }
        .navigationTitle("Specialty Default")
        .navigationBarTitleDisplayMode(.inline)
        .disabled(saving != nil)
        .task { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if let settings {
            Section {
                ForEach(settings.options) { option in
                    row(option, settings: settings)
                }
            } header: {
                Text("When Nobody Has Chosen")
            } footer: {
                footer(settings)
            }
        } else if let loadError {
            Section {
                FormErrorLabel(message: loadError)
                Button("Try Again") {
                    Task { await load() }
                }
            }
        } else {
            Section {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Loading…")
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    @ViewBuilder
    private func row(_ option: SpecialtyStrategyOption, settings: SpecialtySettings) -> some View {
        let isCurrent = option.value == settings.strategy
        if canEdit {
            Button {
                choose(option.value)
            } label: {
                SpecialtyStrategyRow(option: option, isCurrent: isCurrent, isSaving: saving == option.value)
            }
            .accessibilityHint("Uses this for every specialty ingredient nobody has chosen.")
        } else {
            SpecialtyStrategyRow(option: option, isCurrent: isCurrent, isSaving: false)
        }
    }

    @ViewBuilder
    private func footer(_ settings: SpecialtySettings) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if let attribution = SpecialtyFormat.strategyAttribution(
                settings, members: households.current?.members, currentUserID: session.currentUser?.id)
            {
                Text(attribution)
            } else {
                Text("Nobody has changed this yet.")
            }
            if !canEdit {
                Text("Your role in this household can see this but not change it.")
            }
        }
    }

    // MARK: - Actions

    private func load() async {
        loadError = nil
        do {
            try await specialties.loadSettings()
        } catch is CancellationError {
            // The next appearance loads again.
        } catch {
            guard session.currentUser != nil else { return }
            if settings == nil {
                loadError = HouseholdStore.message(for: error)
            }
        }
    }

    private func choose(_ strategy: SpecialtyStrategy) {
        guard strategy != settings?.strategy else { return }
        Task {
            saving = strategy
            defer { saving = nil }
            errorMessage = nil
            do {
                try await specialties.updateSettings(strategy: strategy)
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                // After a sign-out the root view replaces this screen, so no error is shown.
                guard session.currentUser != nil else { return }
                errorMessage = HouseholdStore.message(for: error)
                if (error as? APIError)?.status == 403 {
                    // The role changed elsewhere; reload it so the screen matches.
                    await households.load()
                }
            }
        }
    }
}

/// One strategy: the server's label and description, with a checkmark on the current one.
struct SpecialtyStrategyRow: View {
    let option: SpecialtyStrategyOption
    let isCurrent: Bool
    let isSaving: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: option.value.systemImage)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(minWidth: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(option.label)
                    .font(.headline)
                Text(option.description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 4)
            if isSaving {
                ProgressView()
            } else if isCurrent {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.tint)
                    .imageScale(.large)
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, 4)
        // Inside a list Button, hierarchical styles resolve against the tint.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isCurrent ? .isSelected : [])
    }
}

/// The current strategy, as a row that opens the screen above.
struct SpecialtyStrategySummaryRow: View {
    let settings: SpecialtySettings?

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text("When Nobody Has Chosen")
                    .foregroundStyle(Color.primary)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: settings?.strategy.systemImage ?? "sparkles")
                .foregroundStyle(.tint)
        }
        .accessibilityElement(children: .combine)
    }

    private var detail: String {
        guard let settings else { return String(localized: "Loading…") }
        return settings.currentOption?.label ?? settings.strategy.rawValue
    }
}

#Preview("Buy something similar") {
    SpecialtyStrategyPreview(strategy: .similar)
}

#Preview("Make it as close as possible") {
    SpecialtyStrategyPreview(strategy: .closest)
}

#Preview("Ask every time") {
    SpecialtyStrategyPreview(strategy: .ask)
}

#Preview("Never set") {
    SpecialtyStrategyPreview(strategy: .similar, wasSet: false)
}

#Preview("Read-only") {
    SpecialtyStrategyPreview(strategy: .closest, canEdit: false)
}

/// Sample data for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are type-checked
/// in Release builds too.
private struct SpecialtyStrategyPreview: View {
    let strategy: SpecialtyStrategy
    var wasSet = true
    var canEdit = true

    var body: some View {
        let session = HouseholdPreviewData.session()
        NavigationStack {
            SpecialtyStrategyView()
        }
        .environment(session)
        .environment(SpecialtyPreviewData.householdStore(session: session, canEdit: canEdit))
        .environment(
            SpecialtyPreviewData.store(
                session: session,
                settings: SpecialtyPreviewData.settings(strategy: strategy, wasSet: wasSet)))
    }
}
