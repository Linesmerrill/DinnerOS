import SwiftUI

/// One pantry item: name, amount, staple badge, expiry, and status.
struct PantryItemRow: View {
    let item: PantryItem

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.displayName)
                        .foregroundStyle(.primary)
                    if item.isStaple {
                        StapleBadge()
                    }
                }
                if let amount = PantryFormat.amount(item) {
                    Text(amount)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let expiry = PantryExpiry(expiresOn: item.expiresOn) {
                    Label(expiry.text(), systemImage: expiry.isExpired ? "exclamationmark.circle" : "calendar")
                        .font(.footnote)
                        .foregroundStyle(expiryColor(expiry))
                }
            }
            Spacer(minLength: 8)
            PantryStatusPill(status: item.status)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    private func expiryColor(_ expiry: PantryExpiry) -> Color {
        if expiry.isExpired { return .red }
        return expiry.isSoon ? .orange : .secondary
    }
}

/// A colored capsule naming a status.
struct PantryStatusPill: View {
    let status: PantryStatus

    var body: some View {
        Text(status.title)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
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
