import SwiftUI

/// Referrals across every household, for the people who run DinnerOS.
struct ReferralsReportView: View {
    let summary: PlusReferralSummary

    var body: some View {
        List {
            Section {
                LabeledContent("Codes used", value: summary.codesUsed.formatted())
                LabeledContent("Joined in the last 7 days", value: summary.joinedThisWeek.formatted())
                LabeledContent("Joined in all", value: summary.joinedAllTime.formatted())
            } footer: {
                Text("Codes used counts households whose code someone used.")
            }
            Section {
                if summary.topReferrers.isEmpty {
                    Text("No one has used a code yet.")
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(Array(summary.topReferrers.enumerated()), id: \.offset) { _, referrer in
                        LabeledContent(referrer.name, value: referrer.count.formatted())
                    }
                }
            } header: {
                Text("Most Shared")
            }
        }
        .navigationTitle("Referrals")
        .navigationBarTitleDisplayMode(.inline)
    }
}
