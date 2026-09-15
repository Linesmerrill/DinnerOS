import SwiftUI

/// Reviews Autopilot's suggestions for a week before anything reaches the plan: switch
/// meals off, swap them, then add the rest, plan again, or dismiss.
struct ProposalReviewView: View {
    let week: ISOWeek
    let canEdit: Bool
    /// The week is finalized; the Week tab offers to reopen it.
    let onPlanFinalized: () -> Void
    let onAccepted: (AutopilotAcceptResult) -> Void

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(\.dismiss) private var dismiss

    @State private var actionError: String?
    @State private var isConfirmingDismiss = false

    private var proposal: AutopilotProposal? {
        guard let proposal = autopilot.pendingProposal, proposal.week == week.description else { return nil }
        return proposal
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Autopilot's Picks")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
                .alert("Couldn't Change the Suggestions", isPresented: Binding(presenting: $actionError)) {
                    Button("OK", role: .cancel) {}
                } message: {
                    Text(actionError ?? "")
                }
                .confirmationDialog(
                    "Dismiss these suggestions?", isPresented: $isConfirmingDismiss, titleVisibility: .visible
                ) {
                    Button("Dismiss Suggestions", role: .destructive, action: dismissProposal)
                } message: {
                    Text("Your week stays as it is. You can plan with Autopilot again anytime.")
                }
        }
        .interactiveDismissDisabled(autopilot.isSaving)
    }

    @ViewBuilder
    private var content: some View {
        if autopilot.isGenerating {
            ProgressView("Planning your week…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let proposal {
            list(proposal)
                .safeAreaInset(edge: .bottom) {
                    if canEdit {
                        acceptBar
                    }
                }
        } else {
            ContentUnavailableView {
                Label("No Suggestions", systemImage: "sparkles")
            } description: {
                if let notice = autopilot.notice {
                    Text(notice)
                } else {
                    Text("These suggestions were added or dismissed.")
                }
            } actions: {
                Button("Done") { dismiss() }
                    .buttonStyle(.borderedProminent)
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Close") { dismiss() }
        }
        if canEdit, proposal != nil {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("Plan Again", systemImage: "arrow.clockwise", action: regenerate)
                    Button("Dismiss Suggestions", systemImage: "xmark", role: .destructive) {
                        isConfirmingDismiss = true
                    }
                } label: {
                    Label("More", systemImage: "ellipsis.circle")
                }
                .disabled(autopilot.isSaving)
            }
        }
    }

    private func list(_ proposal: AutopilotProposal) -> some View {
        List {
            if let notice = autopilot.notice {
                Section {
                    Label(notice, systemImage: "arrow.triangle.2.circlepath")
                        .foregroundStyle(.secondary)
                }
            }
            if !proposal.messages.isEmpty {
                Section {
                    ForEach(proposal.messages, id: \.self) { message in
                        Label(message.text, systemImage: "info.circle")
                    }
                }
            }
            Section {
                ForEach(proposal.rows) { row in
                    switch row {
                    case .slot(let slot):
                        ProposalSlotRow(
                            slot: slot, isIncluded: !autopilot.excludedSlotIDs.contains(slot.id),
                            isSwapping: autopilot.swappingSlotIDs.contains(slot.id), canEdit: canEdit,
                            isBusy: autopilot.isSaving,
                            setIncluded: { autopilot.setSlot(slot.id, included: $0) },
                            swap: { swap(slot) })
                    case .unfilled(let unfilled):
                        UnfilledDayRow(unfilled: unfilled)
                    }
                }
            } header: {
                Text(week.weekOf())
            } footer: {
                if canEdit {
                    Text("Switch off meals you don't want. Nothing is added to your week until you add them.")
                } else {
                    Text("Someone who can plan meals can add these to the week.")
                }
            }
        }
    }

    private var acceptBar: some View {
        let count = autopilot.includedSlots.count
        return VStack(spacing: 6) {
            Button(action: accept) {
                Group {
                    if autopilot.isSaving && autopilot.swappingSlotIDs.isEmpty {
                        ProgressView()
                    } else if count == 0 {
                        Text("Choose at Least One Meal")
                    } else {
                        Text(count == 1 ? "Add 1 Meal to Week" : "Add \(count) Meals to Week")
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(count == 0 || autopilot.isSaving)
        }
        .padding()
        .background(.bar)
    }

    // MARK: - Actions

    private func swap(_ slot: AutopilotSlot) {
        perform { try await autopilot.swap(slotID: slot.id) }
    }

    private func regenerate() {
        perform { try await autopilot.generate(week: week) }
    }

    private func accept() {
        perform {
            guard let result = try await autopilot.accept() else { return }
            dismiss()
            onAccepted(result)
        }
    }

    private func dismissProposal() {
        perform {
            try await autopilot.dismissProposal()
            dismiss()
        }
    }

    private func perform(_ change: @escaping () async throws -> Void) {
        Task {
            do {
                try await change()
            } catch is CancellationError {
                return
            } catch {
                if AutopilotConflict(error) == .planFinalized {
                    dismiss()
                    onPlanFinalized()
                } else {
                    actionError = HouseholdStore.message(for: error)
                }
            }
        }
    }
}

/// A proposed meal: day, photo, name, cook time and band, reasons, and its controls.
struct ProposalSlotRow: View {
    let slot: AutopilotSlot
    let isIncluded: Bool
    let isSwapping: Bool
    let canEdit: Bool
    let isBusy: Bool
    let setIncluded: (Bool) -> Void
    let swap: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 64.0

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            if canEdit {
                Button {
                    setIncluded(!isIncluded)
                } label: {
                    Image(systemName: isIncluded ? "checkmark.circle.fill" : "circle")
                        .font(.title2)
                        .foregroundStyle(isIncluded ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.secondary))
                }
                .buttonStyle(.plain)
                .accessibilityHidden(true)
            }
            if !dynamicTypeSize.isAccessibilitySize {
                RecipeImage(url: slot.recipe.imageURL)
                    .frame(width: min(thumbnailWidth, 96))
                    .clipShape(.rect(cornerRadius: 6))
            }
            VStack(alignment: .leading, spacing: 4) {
                Text(AutopilotFormat.dayTitle(date: slot.date, day: slot.day))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.secondary)
                Text(slot.recipe.name)
                    .font(.headline)
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 6) { facts }
                    VStack(alignment: .leading, spacing: 4) { facts }
                }
                if !slot.reasonText.isEmpty {
                    Text(slot.reasonText)
                        .font(.subheadline)
                        .foregroundStyle(Color.secondary)
                }
                if canEdit {
                    Button(action: swap) {
                        if isSwapping {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Label("Swap", systemImage: "arrow.triangle.2.circlepath")
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .disabled(isBusy)
                    .padding(.top, 2)
                    .accessibilityHidden(true)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .opacity(isIncluded ? 1 : 0.55)
        // A concrete color: hierarchical styles inside list rows with buttons resolve to the tint.
        .foregroundStyle(Color.primary)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityValue(isIncluded ? Text("Included") : Text("Left out"))
        .accessibilityActions {
            if canEdit {
                Button(isIncluded ? "Leave Out" : "Include") { setIncluded(!isIncluded) }
                Button("Swap", action: swap)
            }
        }
    }

    @ViewBuilder
    private var facts: some View {
        Label(AutopilotFormat.cookTime(slot.cookMinutes), systemImage: "clock")
            .font(.caption)
            .foregroundStyle(Color.secondary)
        TimeBandBadge(band: slot.timeBand)
        Text("\(slot.servings) servings")
            .font(.caption)
            .foregroundStyle(Color.secondary)
    }

    private var accessibilityLabel: String {
        [
            AutopilotFormat.dayTitle(date: slot.date, day: slot.day), slot.recipe.name,
            AutopilotFormat.cookTime(slot.cookMinutes), slot.timeBand.title, slot.reasonText,
        ]
        .filter { !$0.isEmpty }
        .joined(separator: ", ")
    }
}

/// A day Autopilot wanted to plan but couldn't.
private struct UnfilledDayRow: View {
    let unfilled: AutopilotUnfilled

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(AutopilotFormat.dayTitle(date: unfilled.date, day: unfilled.day))
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
            Label(unfilled.text, systemImage: "moon.zzz")
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    ProposalReviewView(week: AutopilotPreviewData.week, canEdit: true, onPlanFinalized: {}, onAccepted: { _ in })
        .environment(AutopilotPreviewData.store(session: session))
}
