import SwiftUI

/// Past weeks, newest first, with what was planned, cooked, and delivered. Picking one shows
/// it on the Menu screen.
struct PastWeeksView: View {
    @Environment(MenuStore.self) private var menu
    @Environment(PlanStore.self) private var plans
    @Environment(\.dismiss) private var dismiss

    private var weeks: [WeekStripItem] {
        menu.stripItems.filter { $0.week < menu.currentWeek }.reversed()
    }

    var body: some View {
        List {
            if let weeksError = menu.weeksError {
                Section {
                    FormErrorLabel(message: weeksError)
                    Text("Weeks are shown without their counts.")
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                }
            }
            Section {
                ForEach(weeks) { item in
                    Button {
                        Task { await plans.show(week: item.week) }
                        dismiss()
                    } label: {
                        row(item)
                    }
                    .buttonStyle(.plain)
                }
                if menu.canLoadEarlierWeeks {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .listRowSeparator(.hidden)
                        .task(id: menu.oldestWeek) {
                            await menu.loadEarlierWeeks()
                        }
                }
            } footer: {
                if !menu.canLoadEarlierWeeks, let earliest = menu.earliestWeek {
                    Text("Your history starts in \(earliest.monthAndYear()).")
                }
            }
        }
        .navigationTitle("Past Weeks")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if weeks.isEmpty {
                ContentUnavailableView {
                    Label("No Past Weeks", systemImage: "clock.arrow.circlepath")
                } description: {
                    Text("Weeks you've planned or had delivered appear here.")
                }
            }
        }
    }

    private func row(_ item: WeekStripItem) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.week.rangeLabel())
                    .font(.headline)
                Text(MenuFormat.weekHistoryDetail(item.summary))
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
            Spacer(minLength: 8)
            if item.summary?.status == .finalized {
                Image(systemName: "lock.fill")
                    .font(.caption)
                    .foregroundStyle(Color.secondary)
                    .accessibilityLabel("Finalized")
            }
            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
        }
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

#Preview("Past weeks") {
    NavigationStack {
        PastWeeksView()
    }
    .menuPreviewEnvironment()
}
