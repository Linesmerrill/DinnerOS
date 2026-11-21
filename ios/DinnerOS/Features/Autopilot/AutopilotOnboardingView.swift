import SwiftUI

/// A short, skippable setup for Autopilot: a welcome, three one-screen questions, and a
/// finish that saves everything with one `PUT` and offers to plan this week.
///
/// Everything else Autopilot knows — the cook-time mix, equipment, weekday rules, novelty,
/// and pairings — lives in Autopilot Preferences instead, reachable from the summary's
/// **Fine-Tune Autopilot** row (#330).
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
        case step(AutopilotOnboardingStep)
        case finished
    }

    /// Setup opens on the first question: a welcome screen was one more tap before anything
    /// happened. "Set Up Later" is the one way out (#336); a question left alone keeps the
    /// usual settings, so Next is all a skip needs (build 121).
    @State private var stage = Stage.step(.taste)
    @State private var settings: AutopilotSettings?
    @State private var tiles = CuisineTileLoader()
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var explaining: AutopilotOnboardingStep?

    var body: some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
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
            case .step(let step):
                self.step(step, settings: binding, vocabulary: vocabulary)
            case .finished:
                finished
            }
        } else if case .failed(let message) = autopilot.phase {
            ContentUnavailableView {
                Label("Couldn’t Load Autopilot", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await autopilot.reloadProfile() }
                }
                .buttonStyle(.dsPrimary(.small))
            }
        } else {
            ProgressView("Getting Autopilot ready…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: - Stages

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
                AutopilotWeekStep(settings: settings)
            }
        }
        .dsReadableWidth()
        .dsBottomBar {
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
                    .font(.footnote)
            }
            HStack(spacing: DS.Space.m) {
                if step.index > 0 {
                    Button("Back") { move(to: step.index - 1) }
                        .buttonStyle(.dsTextLink)
                        .disabled(isSaving)
                } else {
                    // Setup opens here, so leaving it entirely belongs on this step.
                    Button("Set Up Later") { dismiss() }
                        .buttonStyle(.dsTextLink)
                        .disabled(isSaving)
                }
                Spacer()
                if isSaving {
                    ProgressView()
                        .padding(.horizontal)
                } else {
                    Button(step.index == Self.steps.count - 1 ? "Finish" : "Next") { move(to: step.index + 1) }
                        .buttonStyle(.dsPrimary(.small))
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
        VStack(alignment: .leading, spacing: DS.Space.s) {
            ProgressView(value: Double(step.index + 1), total: Double(Self.steps.count))
                .accessibilityLabel("Step \(step.positionText)")
            HStack(alignment: .firstTextBaseline) {
                Text(step.positionText)
                    .font(DS.Typography.subheadline.weight(.semibold))
                    .foregroundStyle(DS.Colors.secondaryText)
                Spacer()
            }
            HStack(alignment: .firstTextBaseline, spacing: DS.Space.xs) {
                Text(step.question)
                    .font(DS.Typography.title)
                Button {
                    explaining = step
                } label: {
                    Image(systemName: "info.circle")
                }
                .buttonStyle(.plain)
                .foregroundStyle(DS.Colors.accent)
                .accessibilityLabel("About this question")
            }
            Text(step.subtitle)
                .font(DS.Typography.subheadline)
                .foregroundStyle(DS.Colors.secondaryText)
        }
        .padding(.horizontal, DS.Space.l)
        .padding(.bottom, DS.Space.m)
        .accessibilityElement(children: .contain)
    }

    private var finished: some View {
        VStack(spacing: DS.Space.xl) {
            Spacer()
            Image(systemName: "checkmark.seal.fill")
                .font(DS.Typography.largeTitle)
                .imageScale(.large)
                .foregroundStyle(DS.Colors.success)
                .accessibilityHidden(true)
            Text("You’re All Set")
                .font(DS.Typography.largeTitle)
            Text(
                onPlanWeek == nil
                    ? "Autopilot will use these preferences when it plans a week."
                    : "Autopilot can plan this week now. Nothing is added until you review it."
            )
            .font(DS.Typography.subtitle)
            .multilineTextAlignment(.center)
            .foregroundStyle(DS.Colors.secondaryText)
            fineTuneRow
            Spacer()
        }
        .padding(DS.Space.l)
        .dsReadableWidth()
        .dsBottomBar {
            if let onPlanWeek {
                Button {
                    dismiss()
                    onPlanWeek()
                } label: {
                    Label("Plan My Week", systemImage: "sparkles")
                }
                .buttonStyle(.dsPrimary(fullWidth: true))
            }
            Button {
                dismiss()
            } label: {
                Text(onPlanWeek == nil ? "Done" : "Not Now")
            }
            .buttonStyle(.dsSecondary(fullWidth: true))
        }
    }

    /// Quiet, and only here: the cook-time mix, equipment, weekday rules, and pairings are
    /// all a tap away without lengthening setup.
    private var fineTuneRow: some View {
        NavigationLink {
            AutopilotPreferencesView()
        } label: {
            Label("Fine-Tune Autopilot", systemImage: "slider.horizontal.3")
                .font(DS.Typography.subheadline)
        }
        .padding(.top, DS.Space.xxs)
    }

    // MARK: - Actions

    private func prepare() async {
        if let householdID = households.current?.household.id {
            await autopilot.activate(householdID: householdID)
        }
        guard settings == nil, let profile = autopilot.profile else { return }
        settings = profile.settings
    }

    private func move(to index: Int) {
        errorMessage = nil
        guard index >= 0 else { return }
        if index >= Self.steps.count {
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
