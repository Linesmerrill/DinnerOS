import ActivityKit
import SwiftUI
import WidgetKit

/// The meal-kit import's Live Activity: the Lock Screen banner and the Dynamic Island.
///
/// It shows the service, "N of M recipes", a real progress bar, and one short status line. When
/// the import finishes the whole banner turns the app's green and says how many recipes arrived;
/// when it stops it says so plainly. Everything it knows is in
/// `MealKitImportActivityAttributes` — counts only.
struct MealKitImportLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: MealKitImportActivityAttributes.self) { context in
            ImportLockScreenView(
                serviceName: context.attributes.serviceName, state: context.state, isStale: context.isStale
            )
            .activityBackgroundTint(context.state.phase == .done ? ImportPalette.doneBackground : nil)
            .activitySystemActionForegroundColor(context.state.phase == .done ? .white : ImportPalette.accent)
        } dynamicIsland: { context in
            let state = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label {
                        Text(context.attributes.serviceName)
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                    } icon: {
                        Image(systemName: ImportPalette.symbol(for: state))
                            .foregroundStyle(ImportPalette.tint(for: state))
                    }
                    .padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if !state.isFinal {
                        Text(state.fraction, format: .percent.precision(.fractionLength(0)))
                            .font(.system(.subheadline, design: .rounded).weight(.semibold))
                            .monospacedDigit()
                            .foregroundStyle(ImportPalette.accent)
                            .padding(.trailing, 4)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    ImportProgressBlock(state: state, isStale: context.isStale, onGreen: false)
                        .padding(.horizontal, 4)
                }
            } compactLeading: {
                Image(systemName: ImportPalette.symbol(for: state))
                    .foregroundStyle(ImportPalette.tint(for: state))
            } compactTrailing: {
                Text(state.phase == .done ? "\(state.done)" : state.compactCount)
                    .font(.system(.caption, design: .rounded).weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(ImportPalette.tint(for: state))
                    .contentTransition(.numericText(value: Double(state.done)))
            } minimal: {
                if state.isFinal {
                    Image(systemName: ImportPalette.symbol(for: state))
                        .foregroundStyle(ImportPalette.tint(for: state))
                } else {
                    ProgressView(value: state.fraction)
                        .progressViewStyle(.circular)
                        .tint(ImportPalette.accent)
                }
            }
            .keylineTint(ImportPalette.tint(for: state))
        }
    }
}

/// Colors and symbols, in one place so the island and the banner agree.
enum ImportPalette {
    /// The app's accent green (the extension carries its own copy of `AccentColor`).
    static let accent = Color("AccentColor")
    /// A deeper green behind white text, in light and dark mode alike.
    static let doneBackground = Color("DoneBackground")

    static func tint(for state: MealKitImportActivityAttributes.ContentState) -> Color {
        switch state.phase {
        case .importing, .waiting, .done: accent
        case .failed: .orange
        case .canceled: .secondary
        }
    }

    static func symbol(for state: MealKitImportActivityAttributes.ContentState) -> String {
        switch state.phase {
        case .importing, .waiting: "fork.knife.circle.fill"
        case .done: "checkmark.circle.fill"
        case .failed: "exclamationmark.circle.fill"
        case .canceled: "stop.circle.fill"
        }
    }
}

/// The Lock Screen and banner presentation.
struct ImportLockScreenView: View {
    let serviceName: String
    let state: MealKitImportActivityAttributes.ContentState
    let isStale: Bool

    private var onGreen: Bool { state.phase == .done }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: ImportPalette.symbol(for: state))
                    .foregroundStyle(onGreen ? .white : ImportPalette.tint(for: state))
                Text("\(serviceName) recipes")
                    .foregroundStyle(onGreen ? .white.opacity(0.85) : .secondary)
            }
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
            ImportProgressBlock(state: state, isStale: isStale, onGreen: onGreen)
        }
        .padding(16)
        // Live Activities have a fixed height budget; past this the count would truncate.
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .accessibilityElement(children: .combine)
    }
}

/// The count, the bar, and the status line, shared by the banner and the expanded island.
struct ImportProgressBlock: View {
    let state: MealKitImportActivityAttributes.ContentState
    let isStale: Bool
    let onGreen: Bool

    private var status: String {
        isStale && !state.isFinal ? String(localized: "Waiting for an update") : state.status
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            headline
            if !state.isStopped {
                ProgressView(value: state.fraction)
                    .tint(onGreen ? .white : ImportPalette.accent)
                    .accessibilityHidden(true)
            }
            Text(status)
                .font(.footnote)
                .foregroundStyle(onGreen ? .white.opacity(0.85) : .secondary)
                .lineLimit(1)
        }
    }

    @ViewBuilder private var headline: some View {
        switch state.phase {
        case .importing, .waiting:
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(state.done, format: .number)
                    .font(.system(.title2, design: .rounded).weight(.bold))
                    .contentTransition(.numericText(value: Double(state.done)))
                Text("of \(state.total) recipes")
                    .font(.system(.subheadline, design: .rounded).weight(.medium))
                    .foregroundStyle(.secondary)
            }
            .monospacedDigit()
            .lineLimit(1)
            .minimumScaleFactor(0.8)
        case .done, .failed, .canceled:
            Text(state.headline)
                .font(.system(.title3, design: .rounded).weight(.bold))
                .monospacedDigit()
                .foregroundStyle(onGreen ? .white : .primary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
    }
}

#Preview("Lock Screen", as: .content, using: MealKitImportActivityAttributes.preview) {
    MealKitImportLiveActivity()
} contentStates: {
    MealKitImportActivityAttributes.ContentState(phase: .importing, done: 40, total: 740)
    MealKitImportActivityAttributes.ContentState(phase: .waiting, done: 80, total: 740)
    MealKitImportActivityAttributes.ContentState(phase: .done, done: 737, total: 740, failed: 3)
    MealKitImportActivityAttributes.ContentState(phase: .failed, done: 120, total: 740)
}

extension MealKitImportActivityAttributes {
    fileprivate static let preview = MealKitImportActivityAttributes(
        serviceName: "HelloFresh", service: "hellofresh", householdID: "preview", jobID: "preview")
}
