import SwiftUI

/// What households asked DinnerOS to add, most asked for first, for the people who run it.
/// Typed names that matched no known store are marked, to match up by hand.
struct StoreRequestsReportView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    @State var summary: StoreRequestSummary
    @State private var errorMessage: String?

    var body: some View {
        List {
            Section {
                LabeledContent("Requests", value: summary.households.formatted())
                LabeledContent("Stores", value: summary.items.count.formatted())
                LabeledContent("Typed, Not Matched", value: summary.custom.formatted())
            } footer: {
                if let errorMessage {
                    Text(errorMessage)
                } else {
                    Text("Only people who run DinnerOS see this.")
                }
            }
            ForEach(summary.items) { tally in
                Section {
                    StoreTallyRow(tally: tally)
                }
            }
        }
        .navigationTitle("Store Requests")
        .refreshable { await refresh() }
    }

    private func refresh() async {
        do {
            if let fresh = try await shopping.storeRequestSummary() {
                summary = fresh
                errorMessage = nil
            }
        } catch {
            errorMessage = ShopErrors.message(for: error, households: households)
        }
    }
}

/// One store: how many households asked, how they found it, where, and what they said.
private struct StoreTallyRow: View {
    let tally: StoreRequestTally

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(tally.name)
                    .font(.headline)
                Spacer()
                Text(tally.households == 1 ? "1 household" : "\(tally.households) households")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            HStack(spacing: 6) {
                if tally.custom {
                    tag("Typed, not matched", color: .orange)
                } else if tally.status == .researched {
                    tag("Looking into it", color: .blue)
                } else if tally.status == .available {
                    tag("Available now", color: .green)
                }
                if !tally.sourceSummary.isEmpty {
                    Text(tally.sourceSummary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            ForEach(tally.places, id: \.self) { place in
                Label {
                    Text([place.name, place.address].filter { !$0.isEmpty }.joined(separator: ", "))
                } icon: {
                    Image(systemName: "mappin.and.ellipse")
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            ForEach(tally.notes, id: \.self) { note in
                Text("“\(note)”")
                    .font(.footnote)
                    .italic()
            }
            Text("Last asked \(tally.lastRequestedAt.formatted(.relative(presentation: .named)))")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private func tag(_ text: LocalizedStringKey, color: Color) -> some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
    }
}
