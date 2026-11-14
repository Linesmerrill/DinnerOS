import SwiftUI

/// A recipe time, like "Prep time" or "Cook time": "Not set" until it's picked, then "4 hr 30 min".
/// Tapping the row opens quick picks and an hours and minutes wheel.
struct RecipeTimeField: View {
    let title: LocalizedStringKey
    @Binding var minutes: Int
    @Binding var isOpen: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Button {
                withAnimation(.snappy) { isOpen.toggle() }
            } label: {
                HStack {
                    Text(title)
                        .foregroundStyle(.primary)
                    Spacer()
                    Text(minutes == 0 ? String(localized: "Not set") : RecipeFormat.minutes(minutes))
                        .foregroundStyle(isOpen ? Color.accentColor : .secondary)
                        .monospacedDigit()
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text(title))
            .accessibilityValue(minutes == 0 ? String(localized: "Not set") : RecipeFormat.minutes(minutes))
            .accessibilityHint(isOpen ? "Closes the time picker" : "Opens the time picker")
            if isOpen {
                RecipeTimePicker(minutes: $minutes)
            }
        }
    }
}

/// Quick picks, then hours and minutes wheels, then Clear.
struct RecipeTimePicker: View {
    @Binding var minutes: Int

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 8) {
                ForEach(RecipeTimes.presets, id: \.self) { preset in
                    Button {
                        minutes = preset
                    } label: {
                        Text(RecipeTimes.short(preset))
                            .font(.subheadline.weight(.medium))
                            .frame(maxWidth: .infinity, minHeight: 36)
                    }
                    .buttonStyle(.bordered)
                    .tint(minutes == preset ? .accentColor : .secondary)
                    .accessibilityLabel(RecipeFormat.minutes(preset))
                }
            }
            HStack(spacing: 0) {
                Picker("Hours", selection: hours) {
                    ForEach(0...RecipeTimes.maxHours, id: \.self) { h in
                        Text("\(h) hr").tag(h)
                    }
                }
                Picker("Minutes", selection: mins) {
                    ForEach(RecipeTimes.minuteSteps, id: \.self) { m in
                        Text("\(m) min").tag(m)
                    }
                }
            }
            .pickerStyle(.wheel)
            .frame(height: 140)
            if minutes > 0 {
                Button("Clear", role: .destructive) { minutes = 0 }
                    .buttonStyle(.borderless)
                    .frame(maxWidth: .infinity, minHeight: 36)
            }
        }
        .padding(.bottom, 4)
    }

    private var hours: Binding<Int> {
        Binding(
            get: { minutes / 60 },
            set: { minutes = RecipeTimes.minutes(hours: $0, minutes: minutes % 60) })
    }

    private var mins: Binding<Int> {
        Binding(
            get: { RecipeTimes.nearestStep(minutes % 60) },
            set: { minutes = RecipeTimes.minutes(hours: minutes / 60, minutes: $0) })
    }
}

nonisolated enum RecipeTimes {
    static let maxHours = 24
    static let presets = [15, 30, 60, 120, 240]
    static let minuteSteps = Array(stride(from: 0, through: 55, by: 5))

    /// Hours and minutes as a time, no more than a day.
    static func minutes(hours: Int, minutes: Int) -> Int {
        min(max(hours, 0) * 60 + max(minutes, 0), maxHours * 60)
    }

    /// The wheel's minute stop for a time that isn't on one, like 7 from an imported recipe.
    static func nearestStep(_ minutes: Int) -> Int {
        min((minutes + 2) / 5 * 5, 55)
    }

    /// A quick pick's label: "15m", "1h".
    static func short(_ minutes: Int) -> String {
        minutes < 60 ? String(localized: "\(minutes)m") : String(localized: "\(minutes / 60)h")
    }

    /// The cook time stored as a total: what's left after prep. A recipe with only one time
    /// shows it as cook.
    static func cook(prep: Int, total: Int) -> Int {
        max(total - prep, 0)
    }
}

/// A recipe whose prep and cook times are being set. Times are kept as prep and total, the way
/// the server stores them; cook is what's left after prep.
nonisolated protocol RecipeTimesEditing {
    var prepMinutes: Int? { get set }
    var totalMinutes: Int? { get set }
}

extension RecipeDraft: RecipeTimesEditing {}
extension RecipeEditDraft: RecipeTimesEditing {}

nonisolated extension RecipeTimesEditing {
    /// Prep time in minutes; 0 when not set. Changing it keeps the cook time.
    var prepTime: Int {
        get { prepMinutes ?? 0 }
        set {
            let cook = cookTime
            prepMinutes = newValue == 0 ? nil : newValue
            setTotal(newValue + cook)
        }
    }

    /// Cook time in minutes; 0 when not set. Saved as the total, prep plus cook.
    var cookTime: Int {
        get { RecipeTimes.cook(prep: prepTime, total: totalMinutes ?? 0) }
        set { setTotal(prepTime + newValue) }
    }

    /// Prep plus cook, for the line under the times.
    var totalTime: Int { prepTime + cookTime }

    private mutating func setTotal(_ total: Int) {
        totalMinutes = total == 0 ? nil : total
    }
}
