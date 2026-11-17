import SwiftUI

/// "What is DinnerOS Plus?" at the top of the free month checklist: a title and a few plain
/// lines from the API, with bundled ones for an older API (decision 708).
struct PlusExplainer: View {
    let progress: ReferralProgress

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label {
                Text(progress.explainerTitle)
                    .font(.headline)
                    .accessibilityAddTraits(.isHeader)
            } icon: {
                Image(systemName: "sparkles")
                    .foregroundStyle(.tint)
            }
            ForEach(Array(progress.explainerLines.enumerated()), id: \.offset) { _, line in
                Text(line)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("PlusExplainer")
    }
}
