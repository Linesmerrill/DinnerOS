import SwiftUI

/// One pantry item: name, amount, staple badge, expiry, and status.
struct PantryItemRow: View {
    let item: PantryItem

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    // A concrete color: `.primary` inside a list Button resolves to the tint.
                    Text(item.displayName)
                        .foregroundStyle(Color.primary)
                    if item.isStaple {
                        StapleBadge()
                    }
                }
                if let amount = PantryFormat.amount(item) {
                    Text(amount)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let estimate = item.estimate {
                    PantryEstimateLabel(estimate: estimate)
                }
                if let expiry = PantryExpiry(expiresOn: item.expiresOn) {
                    Label(expiry.text(), systemImage: expiry.isExpired ? "exclamationmark.circle" : "calendar")
                        .font(.footnote)
                        .foregroundStyle(expiryColor(expiry))
                }
            }
            Spacer(minLength: 8)
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
        .padding(.horizontal, 8)
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
        case .inStock: .green
        case .low: .orange
        case .out: .red
        }
    }
}

private struct StapleBadge: View {
    var body: some View {
        Text("Staple")
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(.secondary)
            .background(.fill.tertiary, in: .capsule)
    }
}

#Preview {
    List(PantryPreviewData.items) { item in
        PantryItemRow(item: item)
    }
}
