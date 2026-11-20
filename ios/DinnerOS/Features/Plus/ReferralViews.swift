import SwiftUI

/// "How it works", above the invite code. The steps come from the API, built from the same
/// settings that decide when an invite counts (decision 698).
struct HowItWorksSection: View {
    let howItWorks: PlusReferral.HowItWorks

    var body: some View {
        Section {
            ForEach(Array(howItWorks.steps.enumerated()), id: \.offset) { index, step in
                HStack(alignment: .firstTextBaseline, spacing: DS.Space.m) {
                    Text("\(index + 1)")
                        .font(.subheadline.weight(.bold))
                        .foregroundStyle(DS.Colors.onAccent)
                        .frame(minWidth: DS.Space.xxl, minHeight: DS.Space.xxl)
                        .background(Circle().fill(DS.Colors.accent))
                        .accessibilityHidden(true)
                    Text(step)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.vertical, DS.Space.hairline)
                .accessibilityElement(children: .combine)
                .accessibilityLabel(Text("Step \(index + 1). ") + Text(step))
            }
        } header: {
            Text(howItWorks.title)
        }
    }
}

/// The household that entered an invite code: what's left before the free month, ticked off
/// as it goes ("Plan a dinner", "Save 3 recipes").
struct ReferralProgressSection: View {
    let progress: ReferralProgress

    var body: some View {
        Section {
            ReferralProgressContent(progress: progress)
        } header: {
            Text("Your Free Month")
        }
    }
}

/// The progress itself, for a Form section or a card on the Plus screen.
struct ReferralProgressContent: View {
    let progress: ReferralProgress

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.sm) {
            Label {
                Text(progress.message)
                    .fixedSize(horizontal: false, vertical: true)
            } icon: {
                Image(systemName: icon)
                    .foregroundStyle(progress.state == "expired" ? Color.secondary : Color.accentColor)
            }
            .font(.subheadline.weight(.semibold))
            if progress.isPending {
                ForEach(progress.steps) { step in
                    HStack(spacing: DS.Space.sm) {
                        Image(systemName: step.done ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(step.done ? Color.accentColor : Color.secondary)
                            .imageScale(.large)
                            .accessibilityHidden(true)
                        Text(step.title)
                            .strikethrough(step.done, color: .secondary)
                            .foregroundStyle(step.done ? Color.secondary : Color.primary)
                        Spacer(minLength: DS.Space.s)
                        if let count = step.count, let target = step.target, target > 1, !step.done {
                            Text("\(count) of \(target)")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityValue(step.done ? Text("Done") : Text("Not done yet"))
                }
            }
        }
        .padding(.vertical, DS.Space.xxs)
        .accessibilityIdentifier("ReferralProgress")
    }

    private var icon: String {
        switch progress.state {
        case "qualified": "gift.fill"
        case "expired": "clock.badge.xmark"
        default: "gift"
        }
    }
}

/// Who used this household's code, each with where it stands.
struct YourInvitesList: View {
    let invites: PlusInvites

    var body: some View {
        Section {
            if let pending = invites.pending, let qualified = invites.qualified, let expired = invites.expired {
                LabeledContent("Pending", value: pending.formatted())
                LabeledContent("Free Months Earned", value: invites.capText)
                    .accessibilityHint(Text("\(qualified) invites counted"))
                if expired > 0 {
                    LabeledContent("Expired", value: expired.formatted())
                }
            } else {
                LabeledContent("Households Joined", value: invites.joined.formatted())
                LabeledContent("Free Months Earned", value: invites.capText)
            }
            if let free = invites.freeUntil {
                Label("Plus free until \(PlusText.day(free))", systemImage: "gift.fill")
            }
            if let items = invites.items {
                if items.isEmpty {
                    Text(invites.emptyText)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(items) { item in
                        InviteRow(item: item)
                    }
                }
            } else if invites.joins.isEmpty {
                Text(invites.emptyText)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(invites.joins.enumerated()), id: \.offset) { _, join in
                    LabeledContent {
                        Text(join.status)
                            .foregroundStyle(join.counted ? Color.accentColor : Color.secondary)
                    } label: {
                        Text("A household joined, \(PlusText.day(join.at))")
                    }
                }
            }
        } header: {
            Text("Your Invites")
        }
    }
}

/// "The Lovelace Kitchen / Pending, needs 2 more recipes".
private struct InviteRow: View {
    let item: PlusInviteItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Space.m) {
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(item.householdName)
                if let detail = item.detailLine {
                    Text(detail)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: DS.Space.s)
            Text(item.stateTitle)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(stateColor)
                .multilineTextAlignment(.trailing)
        }
        .accessibilityElement(children: .combine)
    }

    private var stateColor: Color {
        switch item.state {
        case "qualified": .accentColor
        case "expired": .secondary
        default: .orange
        }
    }
}
