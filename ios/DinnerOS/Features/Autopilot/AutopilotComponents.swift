import SwiftUI

/// Lays out subviews in rows, wrapping when a row is full. Chips grow with Dynamic Type
/// and simply wrap onto more rows.
struct ChipFlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxWidth = proposal.width ?? .infinity
        var origin = CGPoint.zero
        var rowHeight: CGFloat = 0
        var usedWidth: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(ProposedViewSize(width: maxWidth, height: nil))
            if origin.x > 0, origin.x + size.width > maxWidth {
                origin.x = 0
                origin.y += rowHeight + spacing
                rowHeight = 0
            }
            usedWidth = max(usedWidth, min(origin.x + size.width, maxWidth))
            origin.x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return CGSize(width: proposal.width ?? usedWidth, height: origin.y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var origin = CGPoint(x: bounds.minX, y: bounds.minY)
        var rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(ProposedViewSize(width: bounds.width, height: nil))
            if origin.x > bounds.minX, origin.x + size.width > bounds.maxX {
                origin.x = bounds.minX
                origin.y += rowHeight + spacing
                rowHeight = 0
            }
            subview.place(
                at: origin, proposal: ProposedViewSize(width: min(size.width, bounds.width), height: size.height))
            origin.x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}

/// A tappable capsule for a choice.
struct ChoiceChip: View {
    enum State {
        case off, on, negative
    }

    let title: String
    var count: Int?
    let state: State
    /// Read by VoiceOver in place of on/off, such as "Liked".
    var accessibilityValue: String?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if let icon {
                    Image(systemName: icon)
                        .imageScale(.small)
                }
                Text(title)
                if let count, count > 0 {
                    Text(count.formatted())
                        .foregroundStyle(state == .off ? Color.secondary : foreground.opacity(0.8))
                        .monospacedDigit()
                }
            }
            .font(.subheadline)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            // A concrete color: `.primary` inside a list Button resolves to the tint.
            .foregroundStyle(foreground)
            .background(background, in: .capsule)
            .overlay {
                Capsule().strokeBorder(state == .off ? Color.secondary.opacity(0.4) : .clear, lineWidth: 1)
            }
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(accessibilityValue ?? (state == .off ? "" : String(localized: "Selected")))
        .accessibilityAddTraits(state == .off ? [] : .isSelected)
    }

    private var icon: String? {
        switch state {
        case .off: nil
        case .on: "checkmark"
        case .negative: "hand.thumbsdown.fill"
        }
    }

    private var foreground: Color {
        switch state {
        case .off: .primary
        case .on: .white
        case .negative: .red
        }
    }

    private var background: AnyShapeStyle {
        switch state {
        case .off: AnyShapeStyle(Color.clear)
        case .on: AnyShapeStyle(Color.accentColor)
        case .negative: AnyShapeStyle(Color.red.opacity(0.15))
        }
    }
}

/// Chips for a list of values: options from the vocabulary, plus custom values when
/// `maxLength` is set. Enforces `maxCount` and shows why a value can't be added.
struct ValueChipGroup: View {
    let options: [AutopilotOption]
    @Binding var selection: [String]
    let maxCount: Int
    /// Allows typing custom values up to this length; `nil` for fixed lists.
    var maxLength: Int?
    /// Keeps fixed values in this order; free text keeps the order added.
    var order: [String]?
    var addPrompt: LocalizedStringKey = "Add another"
    var initialCount = 16

    @State private var showsAll = false
    @State private var customValue = ""
    @State private var message: String?

    private var values: [(value: String, label: String, count: Int?)] {
        let known = options.map { ($0.value, $0.label, $0.recipeCount) }
        let custom = selection.filter { value in !options.contains { $0.value == value } }
            .map { ($0, $0.capitalized, Int?.none) }
        return custom + known
    }

    var body: some View {
        let all = values
        let shown = showsAll ? all : Array(all.prefix(max(initialCount, selection.count)))
        VStack(alignment: .leading, spacing: 10) {
            ChipFlowLayout {
                ForEach(shown, id: \.value) { item in
                    let isSelected = selection.contains(item.value)
                    ChoiceChip(title: item.label, count: item.count, state: isSelected ? .on : .off) {
                        toggle(item.value)
                    }
                }
            }
            if all.count > shown.count {
                Button("Show All \(all.count)") { showsAll = true }
                    .buttonStyle(.borderless)
                    .font(.subheadline)
            }
            if let maxLength {
                HStack {
                    TextField(addPrompt, text: $customValue)
                        .textInputAutocapitalization(.never)
                        .submitLabel(.done)
                        .onSubmit { addCustom(maxLength: maxLength) }
                    Button("Add") { addCustom(maxLength: maxLength) }
                        .buttonStyle(.borderless)
                        .disabled(AutopilotInput.normalized(customValue).isEmpty)
                }
            }
            if let message {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
        }
        .padding(.vertical, 4)
    }

    private func toggle(_ value: String) {
        message = nil
        if selection.contains(value) {
            selection.removeAll { $0 == value }
        } else if selection.count >= maxCount {
            message = String(localized: "You can choose up to \(maxCount).")
        } else if let order {
            AutopilotInput.set(value, included: true, in: &selection, order: order)
        } else {
            selection.append(value)
        }
    }

    private func addCustom(maxLength: Int) {
        var list = selection
        switch AutopilotInput.add(customValue, to: &list, maxCount: maxCount, maxLength: maxLength) {
        case .added, .duplicate:
            selection = list
            customValue = ""
            message = nil
        case .empty:
            break
        case .tooLong:
            message = String(localized: "Keep it to \(maxLength) characters.")
        case .full:
            message = String(localized: "You can choose up to \(maxCount).")
        }
    }
}

/// Chips for the seven days, Monday first.
struct DayChipRow: View {
    let selection: [PlanDay]
    let toggle: (PlanDay) -> Void

    var body: some View {
        ChipFlowLayout(spacing: 6) {
            ForEach(PlanDay.allCases) { day in
                let isSelected = selection.contains(day)
                ChoiceChip(title: Self.shortName(day), state: isSelected ? .on : .off) { toggle(day) }
                    .accessibilityLabel(day.name())
            }
        }
        .padding(.vertical, 4)
    }

    static func shortName(_ day: PlanDay, locale: Locale = .autoupdatingCurrent) -> String {
        let monday = ISOWeek("2026-W01")?.startDate ?? .distantPast
        return monday.addingTimeInterval(TimeInterval(day.offset) * 86_400)
            .formatted(Date.FormatStyle(locale: locale, timeZone: .gmt).weekday(.abbreviated))
    }
}

/// A toggle that reveals a stepper, for a number that can be unset ("No limit").
struct OptionalNumberRow: View {
    let title: String
    @Binding var value: Int?
    let range: ClosedRange<Int>
    var step = 1
    let defaultValue: Int
    let format: (Int) -> String

    var body: some View {
        Toggle(
            title,
            isOn: Binding(
                get: { value != nil },
                set: { isOn in value = isOn ? (value ?? defaultValue) : nil }))
        if let current = value {
            Stepper(
                value: Binding(get: { current }, set: { value = min(max($0, range.lowerBound), range.upperBound) }),
                in: range, step: step
            ) {
                Text(format(current))
                    .monospacedDigit()
            }
            .accessibilityLabel(title)
            .accessibilityValue(format(current))
        }
    }
}

/// Quick, medium, or long, as a small colored capsule.
struct TimeBandBadge: View {
    let band: AutopilotTimeBand

    var body: some View {
        Text(band.title)
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
            .accessibilityLabel(Text("\(band.title) cook"))
    }

    private var color: Color {
        switch band {
        case .quick: .green
        case .medium: .blue
        case .long: .orange
        default: .gray
        }
    }
}

/// Marks a plan entry Autopilot added.
struct AutopilotEntryBadge: View {
    var body: some View {
        Image(systemName: "sparkles")
            .font(.caption)
            .foregroundStyle(Color.secondary)
            .accessibilityLabel("Added by Autopilot")
    }
}

/// "Changed by Ada · Sep 14, 2026" for a section, resolving the member's name.
struct SectionAttributionText: View {
    let change: AutopilotSectionChange?

    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    var body: some View {
        if let text = AutopilotFormat.attribution(
            change, members: households.current?.members, currentUserID: session.currentUser?.id)
        {
            Text(text)
        }
    }
}
