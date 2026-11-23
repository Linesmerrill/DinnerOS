import SwiftUI

/// One pantry item: name, amount, staple badge, expiry, and status.
struct PantryItemRow: View {
    let item: PantryItem

    var body: some View {
        HStack(alignment: .center, spacing: DS.Space.m) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: DS.Space.xs) {
                    // A concrete color: `.primary` inside a list Button resolves to the tint.
                    Text(item.displayName)
                        .foregroundStyle(Color.primary)
                    if item.isStaple {
                        StapleBadge()
                    }
                    if item.isFrozen {
                        FrozenBadge()
                    }
                }
                let amount = PantryFormat.amount(item)
                if let amount {
                    Text(amount)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let estimate = item.estimate {
                    PantryEstimateLabel(estimate: estimate, shownAmount: amount)
                }
                if let frozen = item.frozen {
                    // The thaw estimate is per portion, which is the number that
                    // decides when somebody has to go to the freezer.
                    Text(PantryFormat.thawText(frozen))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if let expiry = item.shownExpiry() {
                    PantryRowIconLine {
                        Image(systemName: expiry.isExpired ? "exclamationmark.circle" : "calendar")
                    } text: {
                        Text(expiry.text(frozen: item.isFrozen || item.storedOn != nil))
                    }
                    .font(.footnote)
                    .foregroundStyle(expiryColor(expiry))
                }
            }
            Spacer(minLength: DS.Space.s)
            PantryStatusPill(status: item.status, isEstimated: item.isEstimatedLow)
        }
        // Inside a list Button, hierarchical styles like `.secondary` resolve against the tint;
        // anchoring them to the primary color keeps amounts and estimates gray.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    private func expiryColor(_ expiry: PantryExpiry) -> Color {
        if expiry.isExpired { return .red }
        return expiry.isSoon ? .orange : .secondary
    }
}

/// A colored capsule naming a status. A status the usage estimate set is outlined and
/// dashed with a trend icon, so it reads differently from one a person chose.
struct PantryStatusPill: View {
    let status: PantryStatus
    var isEstimated = false

    var body: some View {
        Group {
            if isEstimated {
                Label("Estimated Low", systemImage: "chart.line.downtrend.xyaxis")
                    .labelStyle(.titleAndIcon)
            } else {
                Text(status.title)
            }
        }
        .font(.caption.weight(.semibold))
        .padding(.horizontal, DS.Space.s)
        .padding(.vertical, 3)
        .foregroundStyle(color)
        .background {
            if isEstimated {
                Capsule().strokeBorder(color, style: StrokeStyle(lineWidth: 1, dash: [3, 2]))
            } else {
                Capsule().fill(color.opacity(0.15))
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(isEstimated ? Text("Estimated low") : Text(status.title))
    }

    private var color: Color {
        switch status {
        case .inStock: DS.Colors.success
        case .low: .orange
        case .out: .red
        }
    }
}

private struct StapleBadge: View {
    var body: some View {
        Text("Staple")
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, DS.Space.xs)
            .padding(.vertical, DS.Space.hairline)
            .foregroundStyle(.secondary)
            .background(.fill.tertiary, in: .capsule)
    }
}

#Preview {
    List(PantryPreviewData.items) { item in
        PantryItemRow(item: item)
    }
}

/// Marks an item kept in the freezer.
struct FrozenBadge: View {
    var body: some View {
        HStack(spacing: DS.Space.xxs) {
            Image(systemName: "snowflake")
            Text("Freezer")
        }
        .font(.caption2.weight(.semibold))
        .padding(.horizontal, DS.Space.xs)
        .padding(.vertical, DS.Space.hairline)
        // The Staple badge's quiet gray, from the design system: a blue of its own showed
        // nowhere else in the app (Hark's build 122 round 5). The snowflake says cold.
        .foregroundStyle(DS.Colors.secondaryText)
        .background(DS.Colors.subtleFill, in: .capsule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("In the freezer"))
    }
}
