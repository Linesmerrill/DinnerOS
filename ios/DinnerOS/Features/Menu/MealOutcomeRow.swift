import SwiftUI

/// "Did you make it?" with Cooked and Skip on a planned meal.
///
/// Marking a meal cooked or skipped already existed, in a `MealCard` long-press menu and a
/// `WeekView` swipe, and nothing on screen said so: the household has three cooked events and no
/// skips ever. Both feed the planner — cooked and skipped are the `conversion` term in Autopilot
/// scoring, and cooked is what deducts the pantry — so an unrecorded night is a real loss, not a
/// tidiness problem.
///
/// This asks on the card the meal is already showing on, beside the stars that ask how it was, on
/// any day: a household often cooks Thursday's meal on Tuesday. Nothing here infers an answer: a
/// week going by never marks a meal cooked, and neither does rating it. The row simply disappears once it has been answered — the photo already carries
/// the badge — so it can't become a thing that nags.
struct MealOutcomeRow: View {
    let entry: PlanEntry

    @Environment(PlanStore.self) private var plans
    @Environment(EventReporter.self) private var events
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications

    /// Shown instead of the buttons while the answer can still be taken back.
    @State private var undoable: EventReporter.EntryOutcome?
    /// The delayed send, cancelled by Undo.
    @State private var send: Task<Void, Never>?

    private var outcome: EventReporter.EntryOutcome? { events.outcomes[entry.id] }

    var body: some View {
        Group {
            if let undoable {
                undoRow(undoable)
            } else if outcome == nil {
                askRow
            }
        }
        .font(.subheadline)
        .onDisappear {
            // The card scrolled away; the event still sends on the queue's own schedule.
            send = nil
        }
    }

    /// Gives up space before it breaks words: the full row, then the buttons as icons, then the
    /// question on its own line above full buttons.
    private var askRow: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                question
                Spacer(minLength: 0)
                buttons(iconOnly: false)
            }
            HStack(spacing: 8) {
                question
                Spacer(minLength: 0)
                buttons(iconOnly: true)
            }
            VStack(alignment: .leading, spacing: 6) {
                question
                HStack(spacing: 8) { buttons(iconOnly: false) }
            }
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
    }

    private var question: some View {
        Text("Did you make it?")
            .foregroundStyle(Color.secondary)
            .lineLimit(1)
            .fixedSize()
    }

    @ViewBuilder
    private func buttons(iconOnly: Bool) -> some View {
        Button {
            answer(.cooked) { events.recipeCooked(entry, week: plans.week) }
        } label: {
            Label("Cooked", systemImage: "checkmark")
                .labelStyle(OutcomeLabelStyle(iconOnly: iconOnly))
        }
        .accessibilityLabel("Cooked")
        Menu {
            ForEach(SkipReason.allCases) { reason in
                Button(reason.title) {
                    answer(.skipped(reason)) { events.recipeSkipped(entry, week: plans.week, reason: reason) }
                }
            }
            Button("Skip Without a Reason") {
                answer(.skipped(nil)) { events.recipeSkipped(entry, week: plans.week, reason: nil) }
            }
        } label: {
            Label("Skip", systemImage: "forward")
                .labelStyle(OutcomeLabelStyle(iconOnly: iconOnly))
        }
        .accessibilityLabel("Skip")
    }

    /// Undo is the whole reason marking cooked can be a plain button: the event is held for
    /// `EventReporter.undoWindow` before it is sent, so taking it back costs the API nothing and
    /// the pantry is never deducted for a meal nobody made.
    private func undoRow(_ outcome: EventReporter.EntryOutcome) -> some View {
        HStack(spacing: 8) {
            Label(title, systemImage: outcome == .cooked ? "checkmark.circle.fill" : "forward.fill")
                .foregroundStyle(Color.secondary)
            Spacer(minLength: 0)
            Button("Undo") {
                send?.cancel()
                send = nil
                events.undoOutcome(entryID: entry.id)
                undoable = nil
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
    }

    private var title: String {
        switch undoable {
        case .cooked: String(localized: "Marked cooked")
        case .skipped: String(localized: "Marked skipped")
        case nil: ""
        }
    }

    /// Records the answer, then waits out the undo window before sending it. Cooking deducts the
    /// pantry when the API stores the event, so the pantry and the unread count are refreshed
    /// after that send rather than waiting for the next batch.
    private func answer(_ recorded: EventReporter.EntryOutcome, record: () -> Void) {
        record()
        undoable = recorded
        send?.cancel()
        send = Task {
            try? await Task.sleep(for: .seconds(EventReporter.undoWindow))
            guard !Task.isCancelled else { return }
            undoable = nil
            await events.flush()
            if recorded == .cooked {
                await pantry.refresh()
                await notifications.refreshUnreadCount()
            }
        }
    }
}

#Preview("Unanswered") {
    MealOutcomeRow(entry: PlanPreviewData.plan.entries[0])
        .padding()
        .menuPreviewEnvironment()
}

/// Title and icon on one line that never wraps, or the icon alone when space is short.
private struct OutcomeLabelStyle: LabelStyle {
    let iconOnly: Bool

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.icon
            if !iconOnly {
                configuration.title.lineLimit(1)
            }
        }
        .fixedSize()
    }
}
