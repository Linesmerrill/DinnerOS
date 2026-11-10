import SwiftUI

/// "Didn't make it?" on a planned meal whose night is over, and "I Made It" on one marked
/// not made.
///
/// A meal counts as made unless someone says otherwise: the server marks every unanswered meal
/// of a week made when the week ends, and next week's list already counts on it (decision 662).
/// So there's nothing to ask on the night, only a quiet way to say it didn't happen, which also
/// gives back what the meal took from the pantry. Future nights show nothing, in the same space.
struct MealOutcomeRow: View {
    let entry: PlanEntry

    @Environment(PlanStore.self) private var plans
    @Environment(EventReporter.self) private var events
    @Environment(MealPlanner.self) private var planner
    @Environment(PantryStore.self) private var pantry
    @Environment(NotificationStore.self) private var notifications

    /// Shown instead of the buttons while the answer can still be taken back.
    @State private var undoable: EventReporter.EntryOutcome?
    /// The delayed send, cancelled by Undo.
    @State private var send: Task<Void, Never>?

    private var outcome: EventReporter.EntryOutcome? { events.outcome(for: entry) }

    var body: some View {
        Group {
            if let undoable {
                undoRow(undoable)
            } else if case .skipped = outcome {
                HStack {
                    Spacer(minLength: 0)
                    Button("I Made It", systemImage: "checkmark") {
                        answer(.cooked) { events.recipeCooked(entry, week: plans.week) }
                    }
                }
            } else if outcome != nil || MealNight.hasPassed(date: entry.date) {
                HStack {
                    Spacer(minLength: 0)
                    DidntMakeItMenu(entry: entry) { reason in
                        answer(.skipped(reason)) { events.recipeSkipped(entry, week: plans.week, reason: reason) }
                    } label: {
                        Text("Didn't make it?")
                    }
                }
            } else {
                // Holds the row's height, so a card for a night still ahead lines up with its
                // neighbours in the iPad's grid.
                Text("Didn't make it?")
                    .hidden()
                    .accessibilityHidden(true)
            }
        }
        .font(.subheadline)
        .buttonStyle(.borderless)
        .controlSize(.small)
        .onDisappear {
            // The card scrolled away; the event still sends on the queue's own schedule.
            send = nil
        }
    }

    /// Undo is the whole reason an answer can be a single tap: the event is held for
    /// `EventReporter.undoWindow` before it is sent, so taking it back costs the API nothing and
    /// the pantry never changes for a tap made by mistake.
    private func undoRow(_ outcome: EventReporter.EntryOutcome) -> some View {
        HStack(spacing: 8) {
            Label(title, systemImage: outcome == .cooked ? "checkmark.circle.fill" : "xmark.circle.fill")
                .foregroundStyle(Color.secondary)
            Spacer(minLength: 0)
            Button("Undo") {
                send?.cancel()
                send = nil
                events.undoOutcome(entryID: entry.id)
                undoable = nil
            }
            .buttonStyle(.bordered)
        }
    }

    private var title: String {
        switch undoable {
        case .cooked: String(localized: "Marked made")
        case .skipped: String(localized: "Marked not made")
        case nil: ""
        }
    }

    /// Records the answer, then waits out the undo window before sending it. Either answer
    /// changes the pantry when the API stores it — made takes the meal out, not made puts it
    /// back — so the pantry and the unread count are refreshed after that send.
    private func answer(_ recorded: EventReporter.EntryOutcome, record: () -> Void) {
        record()
        undoable = recorded
        send?.cancel()
        send = Task {
            try? await Task.sleep(for: .seconds(EventReporter.undoWindow))
            guard !Task.isCancelled else { return }
            undoable = nil
            await events.flush()
            await pantry.refresh()
            await notifications.refreshUnreadCount()
        }
    }
}

/// "Didn't Make It" with its reasons, and for this week's or a past meal, moving it to a week
/// it can still be made in. Shared by the card's row, its long-press menu, and the By Day list.
struct DidntMakeItMenu<MenuLabel: View>: View {
    let entry: PlanEntry
    let didntMakeIt: (SkipReason?) -> Void
    @ViewBuilder let label: () -> MenuLabel

    @Environment(PlanStore.self) private var plans
    @Environment(MealPlanner.self) private var planner

    var body: some View {
        Menu {
            if let later = laterWeek {
                // Didn't make it, but still will: the meat's in the freezer for next week.
                Section {
                    Button(laterTitle(later), systemImage: "calendar.badge.clock") {
                        Task { await planner.move(entry, toWeek: later) }
                    }
                    MoveToWeekMenu(entry: entry)
                }
            }
            Section("Didn't Make It") {
                ForEach(SkipReason.allCases) { reason in
                    Button(reason.title) { didntMakeIt(reason) }
                }
                Button("No Reason") { didntMakeIt(nil) }
            }
        } label: {
            label()
        }
        .accessibilityLabel("Didn't make \(entry.recipe.name)")
    }

    /// The week to make a meal in when it didn't get made: next week for this week's meal, this
    /// week for a past one. `nil` for a week still ahead, or when the meal can't move.
    private var laterWeek: ISOWeek? {
        guard plans.week <= plans.currentWeek, planner.canMoveToAnotherWeek(entry, isCooked: false) else { return nil }
        return planner.weeksToMoveTo.first
    }

    private func laterTitle(_ week: ISOWeek) -> String {
        week == plans.currentWeek ? String(localized: "Make It This Week") : String(localized: "Make It Next Week")
    }
}

#Preview("Night passed") {
    MealOutcomeRow(entry: PlanPreviewData.plan.entries[0])
        .padding()
        .menuPreviewEnvironment()
}
