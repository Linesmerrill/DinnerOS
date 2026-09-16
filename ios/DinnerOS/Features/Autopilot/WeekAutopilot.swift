import SwiftUI

/// The Week tab's Autopilot state: which sheet or alert is showing, and the actions that
/// open them. Views read the stores from the environment and pass them in.
@Observable
final class WeekAutopilotFlow {
    enum Sheet: String, Identifiable {
        case onboarding, review, context

        var id: String { rawValue }
    }

    enum Alert: Identifiable {
        /// The week is finalized; offer to reopen it and plan.
        case finalized
        /// Week context changed; offer to plan again.
        case contextChanged(hasSuggestions: Bool)
        /// Some meals weren't added.
        case accepted(AutopilotAcceptResult)
        case error(String)

        var id: String {
            switch self {
            case .finalized: "finalized"
            case .contextChanged: "contextChanged"
            case .accepted(let result): "accepted-\(result.proposal.id)"
            case .error(let message): "error-\(message)"
            }
        }
    }

    var sheet: Sheet?
    var alert: Alert?
    var isShowingPreferences = false

    /// Plans the shown week, or asks the setup questions first when Autopilot isn't set up.
    func plan(autopilot: AutopilotStore, plans: PlanStore) {
        guard autopilot.isConfigured else {
            sheet = .onboarding
            return
        }
        generate(week: plans.week, autopilot: autopilot)
    }

    /// Onboarding's "Plan My Week": this week, even when another week is shown.
    func planCurrentWeek(autopilot: AutopilotStore, plans: PlanStore) {
        Task {
            await plans.showCurrentWeek()
            generate(week: plans.currentWeek, autopilot: autopilot)
        }
    }

    func generate(week: ISOWeek, autopilot: AutopilotStore) {
        Task {
            do {
                try await autopilot.generate(week: week)
                sheet = .review
            } catch is CancellationError {
                return
            } catch {
                handle(error)
            }
        }
    }

    /// Reopens the shown week with the existing status change, then plans it.
    func reopenAndPlan(autopilot: AutopilotStore, plans: PlanStore) {
        Task {
            do {
                try await plans.setStatus(.draft)
                try await autopilot.generate(week: plans.week)
                sheet = .review
            } catch is CancellationError {
                return
            } catch {
                handle(error)
            }
        }
    }

    /// Shows the summary when something needs saying: a meal was skipped, or the add-ons
    /// that came with the meals were added or left out.
    func accepted(_ result: AutopilotAcceptResult) {
        if !result.skipped.isEmpty || !result.pairingsAdded.isEmpty || !result.pairingsSkipped.isEmpty {
            alert = .accepted(result)
        }
    }

    func handle(_ error: any Error) {
        alert = AutopilotConflict(error) == .planFinalized ? .finalized : .error(HouseholdStore.message(for: error))
    }
}

/// Autopilot's rows at the top of the Week list: pending suggestions or "Plan with
/// Autopilot", and the week's context.
struct WeekAutopilotSection: View {
    let flow: WeekAutopilotFlow
    let canEdit: Bool

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(PlanStore.self) private var plans

    /// A skipped week generates an empty proposal, which is nothing to review.
    private var proposal: AutopilotProposal? {
        guard let proposal = autopilot.pendingProposal, proposal.week == plans.week.description,
            !proposal.slots.isEmpty
        else { return nil }
        return proposal
    }

    private var isSkipping: Bool {
        autopilot.week == plans.week && autopilot.context?.skip == true
    }

    private var contextSummary: String? {
        guard autopilot.week == plans.week else { return nil }
        return AutopilotFormat.contextSummary(autopilot.context)
    }

    var body: some View {
        // A skipped week is deliberate, so it says so instead of offering to plan. A week whose
        // context failed to load isn't skipping — `isSkipping` needs the context — so the error
        // below still gets its say.
        if isSkipping {
            Section {
                skippingRow
                contextRow
            }
        } else if autopilot.weekError != nil || proposal != nil || (canEdit && plans.isDraft)
            || contextSummary != nil
        {
            Section {
                // The week's suggestions and context failed to load. Without this the rows below
                // simply read as "nothing suggested", which is a different thing entirely.
                if let weekError = autopilot.weekError {
                    VStack(alignment: .leading, spacing: 8) {
                        FormErrorLabel(message: weekError)
                        Button("Try Again") {
                            Task { await autopilot.reloadWeek() }
                        }
                        .buttonStyle(.bordered)
                    }
                }
                if let proposal {
                    suggestionsRow(proposal)
                } else if canEdit && plans.isDraft {
                    planRow
                }
                if canEdit || contextSummary != nil {
                    contextRow
                }
            }
        }
    }

    private var skippingRow: some View {
        row(
            systemImage: "beach.umbrella", title: String(localized: "Skipping this week"),
            subtitle: String(localized: "Autopilot won't plan or suggest anything."))
    }

    private func suggestionsRow(_ proposal: AutopilotProposal) -> some View {
        Button {
            flow.sheet = .review
        } label: {
            row(
                systemImage: "sparkles",
                title: proposal.slots.count == 1
                    ? String(localized: "Autopilot suggested 1 meal")
                    : String(localized: "Autopilot suggested \(proposal.slots.count) meals"),
                subtitle: String(localized: "Review, swap, and add them to your week."),
                trailing: String(localized: "Review"))
        }
        .accessibilityHint("Opens the suggestions")
    }

    private var planRow: some View {
        Button {
            flow.plan(autopilot: autopilot, plans: plans)
        } label: {
            row(
                systemImage: "sparkles", title: String(localized: "Plan with Autopilot"),
                subtitle: autopilot.isConfigured
                    ? String(localized: "Suggests dinners for this week's open days. You review them first.")
                    : String(localized: "Answer a few questions, then Autopilot suggests this week's dinners."),
                isWorking: autopilot.isGenerating)
        }
        .disabled(autopilot.isGenerating)
    }

    private var contextRow: some View {
        Button {
            flow.sheet = .context
        } label: {
            row(
                systemImage: "calendar.badge.clock", title: String(localized: "This Week's Plans…"),
                subtitle: contextSummary ?? String(localized: "Vacation, a busy week, or guests"))
        }
    }

    private func row(
        systemImage: String, title: String, subtitle: String, trailing: String? = nil, isWorking: Bool = false
    ) -> some View {
        HStack(spacing: 12) {
            Image(systemName: systemImage)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(minWidth: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                Text(subtitle)
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
            Spacer(minLength: 8)
            if isWorking {
                ProgressView()
            } else if let trailing {
                Text(trailing)
                    .foregroundStyle(.tint)
            }
        }
        // Keeps text black and gray inside the list button.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

/// Menu items for the Week tab's toolbar menu.
struct WeekAutopilotMenuItems: View {
    let flow: WeekAutopilotFlow
    let canEdit: Bool

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(PlanStore.self) private var plans

    private var isSkipping: Bool {
        autopilot.week == plans.week && autopilot.context?.skip == true
    }

    var body: some View {
        Section("Autopilot") {
            if canEdit {
                if autopilot.pendingProposal?.week == plans.week.description,
                    autopilot.pendingProposal?.slots.isEmpty == false
                {
                    Button("Review Suggestions", systemImage: "sparkles") { flow.sheet = .review }
                } else {
                    Button("Plan with Autopilot", systemImage: "sparkles") {
                        flow.plan(autopilot: autopilot, plans: plans)
                    }
                    // Nothing to plan on a week the household is away for.
                    .disabled(!plans.isDraft || autopilot.isGenerating || isSkipping)
                }
                Button("This Week's Plans…", systemImage: "calendar.badge.clock") { flow.sheet = .context }
                if autopilot.phase == .loaded {
                    Button(
                        autopilot.isConfigured
                            ? String(localized: "Run Setup Again") : String(localized: "Set Up Autopilot"),
                        systemImage: "wand.and.stars"
                    ) {
                        flow.sheet = .onboarding
                    }
                }
            }
            // The cook-time mix, equipment, weekday rules, and pairings setup no longer asks
            // about all live here (#330).
            Button("Fine-tune Autopilot", systemImage: "slider.horizontal.3") { flow.isShowingPreferences = true }
        }
    }
}

/// Loads Autopilot for the Week tab, offers onboarding on the first visit, and presents
/// Autopilot's sheets and alerts.
struct WeekAutopilotModifier: ViewModifier {
    let flow: WeekAutopilotFlow
    let canEdit: Bool

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(PlanStore.self) private var plans
    @Environment(HouseholdStore.self) private var households

    private struct WeekKey: Equatable {
        let householdID: String?
        let week: ISOWeek
    }

    func body(content: Content) -> some View {
        @Bindable var flow = flow
        return
            content
            .task(id: households.current?.household.id) {
                guard let householdID = households.current?.household.id else { return }
                await autopilot.activate(householdID: householdID)
                // First visit for this household: offer setup once, to members who can plan.
                if canEdit, autopilot.shouldOfferOnboarding, flow.sheet == nil {
                    autopilot.markOnboardingOffered()
                    flow.sheet = .onboarding
                }
            }
            .task(id: WeekKey(householdID: households.current?.household.id, week: plans.week)) {
                guard let householdID = households.current?.household.id else { return }
                await autopilot.showWeek(plans.week, householdID: householdID)
            }
            .navigationDestination(isPresented: $flow.isShowingPreferences) {
                AutopilotPreferencesView()
            }
            .sheet(item: $flow.sheet) { sheet in
                switch sheet {
                case .onboarding:
                    AutopilotOnboardingView(onPlanWeek: { flow.planCurrentWeek(autopilot: autopilot, plans: plans) })
                case .review:
                    ProposalReviewView(
                        week: plans.week, canEdit: canEdit, onPlanFinalized: { flow.alert = .finalized },
                        onAccepted: { flow.accepted($0) })
                case .context:
                    WeekContextSheet(week: plans.week) {
                        flow.alert = .contextChanged(hasSuggestions: autopilot.pendingProposal != nil)
                    }
                }
            }
            .alert(title(flow.alert), isPresented: Binding(presenting: $flow.alert), presenting: flow.alert) { alert in
                buttons(alert)
            } message: { alert in
                Text(message(alert))
            }
    }

    private func title(_ alert: WeekAutopilotFlow.Alert?) -> String {
        switch alert {
        case .finalized: String(localized: "This Week Is Finalized")
        case .contextChanged(let hasSuggestions):
            hasSuggestions ? String(localized: "Plan Again?") : String(localized: "Plan with Autopilot?")
        case .accepted(let result):
            result.added.count == 1
                ? String(localized: "Added 1 Meal") : String(localized: "Added \(result.added.count) Meals")
        case .error: String(localized: "Autopilot Couldn't Do That")
        case nil: ""
        }
    }

    @ViewBuilder
    private func buttons(_ alert: WeekAutopilotFlow.Alert) -> some View {
        switch alert {
        case .finalized:
            if canEdit {
                Button("Reopen and Plan") { flow.reopenAndPlan(autopilot: autopilot, plans: plans) }
            }
            Button("Cancel", role: .cancel) {}
        case .contextChanged(let hasSuggestions):
            if canEdit && plans.isDraft {
                Button(hasSuggestions ? "Regenerate" : "Plan with Autopilot") {
                    flow.plan(autopilot: autopilot, plans: plans)
                }
            }
            Button("Not Now", role: .cancel) {}
        case .accepted, .error:
            Button("OK", role: .cancel) {}
        }
    }

    private func message(_ alert: WeekAutopilotFlow.Alert) -> String {
        switch alert {
        case .finalized:
            String(
                localized:
                    "Its recipes are locked so the grocery list stays put. Reopen the week to plan with Autopilot.")
        case .contextChanged(let hasSuggestions):
            hasSuggestions
                ? String(localized: "Autopilot can suggest this week's meals again with your changes.")
                : String(localized: "Autopilot can suggest this week's meals with your changes.")
        case .accepted(let result):
            [
                result.skipped.isEmpty
                    ? nil
                    : result.skipped.map(\.explanation).joined(separator: " ") + " "
                        + String(localized: "Those meals weren't added, and nothing was replaced."),
                PairingFormat.acceptSummary(result),
            ]
            .compactMap { $0 }
            .joined(separator: " ")
        case .error(let message):
            message
        }
    }
}
