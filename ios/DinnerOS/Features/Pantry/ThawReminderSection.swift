import SwiftUI

/// "Take it out to thaw": the frozen items today's meals need, with how long a portion
/// takes in the fridge and when to move it over (docs/pantry-usage.md#thaw-reminders).
///
/// The push notification is the reminder that reaches a closed app; this is the same thing
/// for somebody already looking at the pantry, so it says the same sentence.
struct ThawReminderSection: View {
    let items: [ThawItem]
    let dismiss: (ThawItem) -> Void

    var body: some View {
        if !items.isEmpty {
            Section {
                ForEach(items) { item in
                    ThawReminderRow(item: item)
                        .swipeActions(edge: .trailing) {
                            Button("Done") { dismiss(item) }
                                .tint(.green)
                        }
                }
            } header: {
                Label("Take Out to Thaw", systemImage: "snowflake")
            } footer: {
                Text("Swipe a reminder away once it's in the fridge.")
            }
        }
    }
}

private struct ThawReminderRow: View {
    let item: ThawItem

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(item.name)
                .foregroundStyle(Color.primary)
            if !item.recipes.isEmpty {
                Text(recipesText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Label(timingText, systemImage: item.overnight ? "exclamationmark.circle" : "clock")
                .font(.footnote)
                .foregroundStyle(item.overnight ? .orange : .secondary)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(item.summary))
    }

    private var recipesText: String {
        String(localized: "For \(ListFormatter.localizedString(byJoining: item.recipes)) tonight")
    }

    /// The honest version of the advice: once the move-by time has passed, naming a
    /// deadline that is already behind the reader helps nobody.
    private var timingText: String {
        let thaw = String(localized: "\(hoursText) in the fridge")
        if item.overnight {
            return thaw + " · " + String(localized: "move it over this morning")
        }
        return thaw + " · " + String(localized: "by \(item.moveByText)")
    }

    private var hoursText: String {
        if item.hours == 1 { return String(localized: "About an hour") }
        if item.hours < 24 { return String(localized: "About \(item.hours) hours") }
        return String(localized: "About a day")
    }
}

#Preview("Thaw reminders") {
    List {
        ThawReminderSection(
            items: [
                ThawItem(
                    itemID: "1", name: "Pork Loin", recipes: ["Sheet-Pan Pork"], hours: 5, measured: true,
                    moveBy: "13:00", overnight: false,
                    summary: "Pork Loin is for Sheet-Pan Pork tonight."),
                ThawItem(
                    itemID: "2", name: "Chicken Thighs", recipes: ["Chicken Curry"], hours: 20, measured: true,
                    moveBy: "22:00", overnight: true,
                    summary: "Chicken Thighs is for Chicken Curry tonight."),
            ],
            dismiss: { _ in })
    }
}
