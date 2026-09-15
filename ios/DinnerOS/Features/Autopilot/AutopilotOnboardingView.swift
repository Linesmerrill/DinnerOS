import SwiftUI

/// A short, skippable setup for Autopilot: a welcome, one step per profile section, and a
/// finish that saves everything with one `PUT` and offers to plan this week.
struct AutopilotOnboardingView: View {
    /// Called after the profile is saved, when the member chooses to plan this week.
    /// `nil` hides that offer.
    var onPlanWeek: (() -> Void)?

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    /// Steps in order: taste first, weekday rules after the equipment they can use.
    static let steps: [AutopilotSection] = [
        .taste, .restrictions, .schedule, .cookTime, .equipment, .weekdayRules, .novelty,
    ]

    private enum Stage: Hashable {
        case welcome
        case step(Int)
        case finished
    }

    @State private var stage = Stage.welcome
    @State private var settings: AutopilotSettings?
    /// What skipping a step restores: the profile as loaded, which is the API's defaults
    /// for a household that never saved one.
    @State private var baseline = AutopilotSettings.defaults
    @State private var isSaving = false
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        if stage != .finished {
                            Button("Not Now") { dismiss() }
                                .disabled(isSaving)
                        }
                    }
                }
        }
        .interactiveDismissDisabled(isSaving)
        .task { await prepare() }
    }

    @ViewBuilder
    private var content: some View {
        if let vocabulary = autopilot.vocabulary, let binding = Binding($settings) {
            switch stage {
            case .welcome:
                welcome
            case .step(let index):
                step(index, settings: binding, vocabulary: vocabulary)
            case .finished:
                finished
            }
        } else if case .failed(let message) = autopilot.phase {
            ContentUnavailableView {
                Label("Couldn't Load Autopilot", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await autopilot.reloadProfile() }
                }
                .buttonStyle(.borderedProminent)
            }
        } else {
            ProgressView("Getting Autopilot ready…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: - Stages

    private var welcome: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                Image(systemName: "sparkles")
                    .font(.system(size: 44))
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                Text("Let Autopilot Plan Your Week")
                    .font(.largeTitle.bold())
                Text(
                    "Autopilot suggests dinners for the days you cook, from what your household likes and what you've made before."
                )
                .font(.title3)
                .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 14) {
                    Label("A few quick questions. Skip any of them.", systemImage: "checklist")
                    Label("You review every meal before it's added.", systemImage: "hand.tap")
                    Label("Change your answers anytime in Preferences.", systemImage: "slider.horizontal.3")
                }
                .font(.body)
            }
            .padding()
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .safeAreaInset(edge: .bottom) {
            bottomBar {
                Button {
                    withAnimation { stage = .step(0) }
                } label: {
                    Text("Get Started").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
            }
        }
    }

    private func step(
        _ index: Int, settings: Binding<AutopilotSettings>, vocabulary: AutopilotVocabulary
    ) -> some View {
        let section = Self.steps[index]
        return Form {
            Section {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Step \(index + 1) of \(Self.steps.count)")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                    Text(Self.question(for: section))
                        .font(.title2.bold())
                    Text(Self.explanation(for: section))
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 8, leading: 4, bottom: 8, trailing: 4))
            }
            AutopilotSectionForm(
                section: section, settings: settings, vocabulary: vocabulary,
                householdServings: households.current?.household.defaultServings)
        }
        .navigationTitle(section.title)
        .safeAreaInset(edge: .bottom) {
            bottomBar {
                if let errorMessage {
                    FormErrorLabel(message: errorMessage)
                        .font(.footnote)
                }
                HStack(spacing: 12) {
                    Button("Back") { move(to: index - 1) }
                        .disabled(isSaving)
                    Spacer()
                    Button("Skip") { skip(index) }
                        .disabled(isSaving)
                        .accessibilityHint("Uses the usual settings for this step")
                    if isSaving {
                        ProgressView()
                            .padding(.horizontal)
                    } else {
                        Button(index == Self.steps.count - 1 ? "Finish" : "Next") { move(to: index + 1) }
                            .buttonStyle(.borderedProminent)
                    }
                }
            }
        }
        .id(section)
    }

    private var finished: some View {
        VStack(spacing: 20) {
            Spacer()
            Image(systemName: "checkmark.seal.fill")
                .font(.system(size: 56))
                .foregroundStyle(.green)
                .accessibilityHidden(true)
            Text("You're All Set")
                .font(.largeTitle.bold())
            Text(
                onPlanWeek == nil
                    ? "Autopilot will use these preferences when it plans a week."
                    : "Autopilot can plan this week now. Nothing is added until you review it."
            )
            .font(.title3)
            .multilineTextAlignment(.center)
            .foregroundStyle(.secondary)
            Spacer()
        }
        .padding()
        .frame(maxWidth: .infinity)
        .safeAreaInset(edge: .bottom) {
            bottomBar {
                if let onPlanWeek {
                    Button {
                        dismiss()
                        onPlanWeek()
                    } label: {
                        Label("Plan My Week", systemImage: "sparkles").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                }
                Button {
                    dismiss()
                } label: {
                    Text(onPlanWeek == nil ? "Done" : "Not Now").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
            }
        }
    }

    private func bottomBar(@ViewBuilder content: () -> some View) -> some View {
        VStack(spacing: 10) {
            content()
        }
        .padding()
        .background(.bar)
    }

    // MARK: - Actions

    private func prepare() async {
        if let householdID = households.current?.household.id {
            await autopilot.activate(householdID: householdID)
        }
        guard settings == nil, let profile = autopilot.profile else { return }
        baseline = profile.settings
        settings = profile.settings
    }

    private func skip(_ index: Int) {
        guard let current = settings else { return }
        var restored = current.replacing(Self.steps[index], from: baseline)
        if Self.steps[index] == .equipment {
            // Rules may no longer have the equipment they asked for.
            for method in current.equipment where !restored.equipment.contains(method) {
                restored.setEquipment(method, owned: false, order: [])
            }
        }
        settings = restored
        move(to: index + 1)
    }

    private func move(to index: Int) {
        errorMessage = nil
        if index < 0 {
            withAnimation { stage = .welcome }
        } else if index >= Self.steps.count {
            finish()
        } else {
            withAnimation { stage = .step(index) }
        }
    }

    private func finish() {
        guard let settings else { return }
        if let message = settings.validationMessage {
            errorMessage = message
            return
        }
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                try await autopilot.saveProfile(settings)
                withAnimation { stage = .finished }
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }

    // MARK: - Copy

    private static func question(for section: AutopilotSection) -> String {
        switch section {
        case .taste: String(localized: "What does your household like?")
        case .restrictions: String(localized: "Anything to avoid?")
        case .schedule: String(localized: "When do you cook?")
        case .cookTime: String(localized: "How long can dinner take?")
        case .equipment: String(localized: "What do you cook with?")
        case .weekdayRules: String(localized: "Any weekly habits?")
        case .novelty: String(localized: "Favorites or something new?")
        }
    }

    private static func explanation(for section: AutopilotSection) -> String {
        switch section {
        case .taste:
            String(localized: "Pick what you love and what's not for you. Autopilot leans toward your likes.")
        case .restrictions:
            String(localized: "These are strict: Autopilot never suggests a recipe that breaks one.")
        case .schedule:
            String(localized: "Choose the nights Autopilot should plan and how many meals you want.")
        case .cookTime:
            String(localized: "Mix quick and longer meals so you're not cooking 40-minute recipes every night.")
        case .equipment:
            String(localized: "Some meals suit a smoker, grill, or air fryer. Autopilot only asks for what you have.")
        case .weekdayRules:
            String(localized: "For example, “Sunday: smoker night, chicken or pork, long cook OK.”")
        case .novelty:
            String(localized: "Autopilot can stick to meals you know or mix in new ones.")
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    AutopilotOnboardingView(onPlanWeek: {})
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(AutopilotPreviewData.store(session: session))
}
