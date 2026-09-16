import SwiftUI

/// A short, skippable setup for Autopilot: a welcome, three one-screen questions, and a
/// finish that saves everything with one `PUT` and offers to plan this week.
///
/// Everything else Autopilot knows — the cook-time mix, equipment, weekday rules, novelty,
/// and pairings — lives in Autopilot Preferences instead, reachable from the summary's
/// **Fine-tune Autopilot** row (#330).
struct AutopilotOnboardingView: View {
    /// Called after the profile is saved, when the member chooses to plan this week.
    /// `nil` hides that offer.
    var onPlanWeek: (() -> Void)?

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households
    @Environment(MenuStore.self) private var menu
    @Environment(\.dismiss) private var dismiss

    static let steps = AutopilotOnboardingStep.allCases

    private enum Stage: Hashable {
        case welcome
        case step(AutopilotOnboardingStep)
        case finished
    }

    @State private var stage = Stage.welcome
    @State private var settings: AutopilotSettings?
    /// What skipping a step restores: the profile as loaded, which is the API's defaults
    /// for a household that never saved one.
    @State private var baseline = AutopilotSettings.defaults
    @State private var tiles = CuisineTileLoader()
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var explaining: AutopilotOnboardingStep?

    var body: some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
                .alert(
                    explaining?.question ?? "", isPresented: Binding(presenting: $explaining), presenting: explaining
                ) { _ in
                    Button("OK", role: .cancel) {}
                } message: { step in
                    Text(step.explanation)
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
            case .step(let step):
                self.step(step, settings: binding, vocabulary: vocabulary)
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

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            if case .step = stage {
                // Every step can be skipped; a skipped one keeps the server's defaults.
                Button("Skip") { skipCurrent() }
                    .disabled(isSaving)
                    .accessibilityHint("Keeps the usual settings for this question")
            } else if stage != .finished {
                Button("Not Now") { dismiss() }
                    .disabled(isSaving)
            }
        }
    }

    // MARK: - Stages

    private var welcome: some View {
        VStack(alignment: .leading, spacing: 16) {
            Spacer()
            Image(systemName: "sparkles")
                .font(.system(size: 44))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text("Let Autopilot Plan Your Week")
                .font(.largeTitle.bold())
            Text("Three quick questions. Skip any of them.")
                .font(.title3)
                .foregroundStyle(.secondary)
            Spacer()
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .safeAreaInset(edge: .bottom) {
            bottomBar {
                Button {
                    withAnimation { stage = .step(Self.steps[0]) }
                } label: {
                    Text("Get Started").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                Button("Set Up Later") { dismiss() }
                    .controlSize(.large)
            }
        }
    }

    private func step(
        _ step: AutopilotOnboardingStep, settings: Binding<AutopilotSettings>, vocabulary: AutopilotVocabulary
    ) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            header(step)
            switch step {
            case .taste:
                AutopilotTasteStep(settings: settings, vocabulary: vocabulary, tiles: tiles.tiles)
            case .avoid:
                AutopilotAvoidStep(settings: settings, vocabulary: vocabulary)
            case .week:
                AutopilotWeekStep(
                    settings: settings, limits: vocabulary.limits,
                    householdServings: households.current?.household.defaultServings)
            }
        }
        .safeAreaInset(edge: .bottom) {
            bottomBar {
                if let errorMessage {
                    FormErrorLabel(message: errorMessage)
                        .font(.footnote)
                }
                HStack(spacing: 12) {
                    if step.index > 0 {
                        Button("Back") { move(to: step.index - 1) }
                            .disabled(isSaving)
                    }
                    Spacer()
                    if isSaving {
                        ProgressView()
                            .padding(.horizontal)
                    } else {
                        Button(step.index == Self.steps.count - 1 ? "Finish" : "Next") { move(to: step.index + 1) }
                            .buttonStyle(.borderedProminent)
                    }
                }
            }
        }
        .task(id: step) {
            guard step == .taste else { return }
            await tiles.load(cuisines: vocabulary.cuisines, menu: menu)
        }
    }

    /// The step count, a slim progress bar, the question, and at most one line under it.
    private func header(_ step: AutopilotOnboardingStep) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            ProgressView(value: Double(step.index + 1), total: Double(Self.steps.count))
                .accessibilityLabel("Step \(step.positionText)")
            HStack(alignment: .firstTextBaseline) {
                Text(step.positionText)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.secondary)
                Spacer()
            }
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(step.question)
                    .font(.title2.bold())
                Button {
                    explaining = step
                } label: {
                    Image(systemName: "info.circle")
                }
                .buttonStyle(.plain)
                .foregroundStyle(.tint)
                .accessibilityLabel("About this question")
            }
            Text(step.subtitle)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal)
        .padding(.bottom, 12)
        .accessibilityElement(children: .contain)
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
            fineTuneRow
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

    /// Quiet, and only here: the cook-time mix, equipment, weekday rules, and pairings are
    /// all a tap away without lengthening setup.
    private var fineTuneRow: some View {
        NavigationLink {
            AutopilotPreferencesView()
        } label: {
            Label("Fine-tune Autopilot", systemImage: "slider.horizontal.3")
                .font(.subheadline)
        }
        .padding(.top, 4)
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

    private func skipCurrent() {
        guard case .step(let step) = stage, let current = settings else { return }
        settings = current.replacing(step.section, from: baseline)
        move(to: step.index + 1)
    }

    private func move(to index: Int) {
        errorMessage = nil
        if index < 0 {
            withAnimation { stage = .welcome }
        } else if index >= Self.steps.count {
            finish()
        } else {
            withAnimation { stage = .step(Self.steps[index]) }
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
}

#Preview("Onboarding") {
    AutopilotOnboardingView(onPlanWeek: {})
        .menuPreviewEnvironment()
}

#Preview("Onboarding, dark") {
    AutopilotOnboardingView(onPlanWeek: {})
        .menuPreviewEnvironment()
        .preferredColorScheme(.dark)
}

#Preview("Onboarding, accessibility size") {
    AutopilotOnboardingView(onPlanWeek: {})
        .menuPreviewEnvironment()
        .environment(\.dynamicTypeSize, .accessibility3)
}
