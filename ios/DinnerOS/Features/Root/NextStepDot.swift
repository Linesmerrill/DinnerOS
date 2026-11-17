import SwiftUI

extension View {
    /// A plain red dot at the end of a row while a next step goes through it (decisions
    /// 736-739). VoiceOver reads "Needs attention" after the row's name; UI tests find it as
    /// `NextStepDot-<row>`.
    func nextStepDot(_ row: NextStepRow) -> some View {
        modifier(NextStepDotModifier(row: row))
    }

    /// Keeps the next steps current: loads them for the household, again when the app comes
    /// back, and after a save that may finish one, clearing that step's dots at once.
    func nextStepsSync() -> some View {
        modifier(NextStepsSync())
    }
}

private struct NextStepDotModifier: ViewModifier {
    let row: NextStepRow
    /// Optional so previews needn't supply one.
    @Environment(NextStepsStore.self) private var nextSteps: NextStepsStore?

    func body(content: Content) -> some View {
        if nextSteps?.shows(row) == true {
            HStack {
                content
                Spacer(minLength: 8)
                NextStepDotMark()
            }
            .accessibilityValue(Text("Needs attention"))
            .accessibilityIdentifier("NextStepDot-\(row.rawValue)")
        } else {
            content
        }
    }
}

/// The dot itself: system red, no number.
struct NextStepDotMark: View {
    var body: some View {
        Circle()
            .fill(Color(uiColor: .systemRed))
            .frame(width: 10, height: 10)
            .accessibilityHidden(true)
    }
}

private struct NextStepsSync: ViewModifier {
    @Environment(NextStepsStore.self) private var nextSteps
    @Environment(HouseholdStore.self) private var households
    @Environment(AutopilotStore.self) private var autopilot
    @Environment(ImportReviewStore.self) private var importReviews
    @Environment(PlusStore.self) private var plus
    @Environment(\.scenePhase) private var scenePhase

    private var householdID: String? { households.current?.household.id }

    func body(content: Content) -> some View {
        content
            .task(id: householdID) {
                guard let householdID else { return }
                nextSteps.activate(householdID: householdID)
                await nextSteps.load()
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { Task { await nextSteps.load() } }
            }
            // Setup saved, or any preference: Autopilot is set up.
            .onChange(of: autopilot.isConfigured) { _, configured in
                guard configured, autopilot.householdID == nextSteps.householdID else { return }
                nextSteps.finish(NextStep.autopilotSetup)
                Task { await nextSteps.load() }
            }
            .onChange(of: importReviewState) { _, state in
                guard let state, state.householdID == nextSteps.householdID else { return }
                if state.open == 0 {
                    nextSteps.finish(NextStep.importReview)
                } else {
                    nextSteps.reopen(NextStep.importReview)
                }
                Task { await nextSteps.load() }
            }
            .onChange(of: plus.status?.referralProgress) { _, progress in
                guard plus.status != nil, plus.currentHouseholdID == nextSteps.householdID else { return }
                if progress?.isPending == true, progress?.steps.contains(where: { !$0.done }) == true {
                    nextSteps.reopen(NextStep.freeMonthSteps)
                } else {
                    nextSteps.finish(NextStep.freeMonthSteps)
                }
                Task { await nextSteps.load() }
            }
    }

    /// Import Review's open count once it has loaded for a household.
    private var importReviewState: ImportReviewState? {
        guard importReviews.phase == .loaded, importReviews.isAvailable, let id = importReviews.householdID else {
            return nil
        }
        return ImportReviewState(householdID: id, open: importReviews.digest.openCount)
    }

    private struct ImportReviewState: Equatable {
        let householdID: String
        let open: Int
    }
}

extension AppTab {
    /// The row key a next step uses for this tab, if any path starts here.
    var nextStepRow: NextStepRow? {
        switch self {
        case .household: .household
        case .menu, .shop, .pantry: nil
        }
    }
}

extension NextStepsStore {
    /// Whether `row` carries a dot; never for a tab no step can start on.
    func shows(tab row: NextStepRow?) -> Bool {
        row.map { shows($0) } ?? false
    }
}
