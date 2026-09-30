import SwiftUI

/// The running timers, stacked in the corner of the cooking screen like dials on a stove: a
/// ring that drains, the time left, and what it's for. A tap opens its controls.
struct CookTimerDock: View {
    let timers: CookTimers
    /// iPad stacks them in the bottom corner; a phone lays them in a row along the bottom.
    let stacked: Bool

    var body: some View {
        if !timers.timers.isEmpty {
            if stacked {
                VStack(alignment: .trailing, spacing: 10) {
                    ForEach(timers.timers) { timer in
                        CookTimerChip(timer: timer, timers: timers)
                            .reorderable(timer, in: timers)
                    }
                }
                .padding(20)
            } else {
                ScrollView(.horizontal) {
                    HStack(spacing: 10) {
                        ForEach(timers.timers) { timer in
                            CookTimerChip(timer: timer, timers: timers)
                                .reorderable(timer, in: timers)
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                }
                .scrollIndicators(.hidden)
            }
        }
    }
}

/// One timer. Collapsed it's a pill; tapped, it shows pause, a minute more, and stop. Finished,
/// it fills green and asks to be stopped.
struct CookTimerChip: View {
    let timer: CookTimer
    let timers: CookTimers

    @State private var isOpen = false
    @State private var pulse = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var isFinished: Bool { timer.state == .finished }
    private var isPaused: Bool { if case .paused = timer.state { true } else { false } }

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let remaining = timer.remaining(at: context.date)
            let urgent = !isFinished && !isPaused && remaining <= 60
            VStack(alignment: .leading, spacing: 10) {
                Button {
                    withAnimation(.snappy(duration: 0.25)) {
                        if isFinished { timers.remove(timer.id) } else { isOpen.toggle() }
                    }
                } label: {
                    HStack(spacing: 10) {
                        ring(progress: timer.progress(at: context.date), urgent: urgent)
                        VStack(alignment: .leading, spacing: 0) {
                            Text(isFinished ? String(localized: "Done") : CookTimer.clock(remaining))
                                .font(.system(.title2, design: .rounded, weight: .heavy).monospacedDigit())
                                .contentTransition(.numericText(countsDown: true))
                                .foregroundStyle(clockColor(urgent: urgent))
                            // "Zucchini  6 min timer": what it's for, and how long it was set for.
                            (Text(timer.label).fontWeight(.semibold)
                                + Text(verbatim: "  ")
                                + Text("\(CookDuration.words(Int(timer.total))) timer"))
                                .font(.caption)
                                .foregroundStyle(isFinished ? Color.white.opacity(0.9) : Color.secondary)
                                .lineLimit(1)
                        }
                        .frame(minWidth: 86, alignment: .leading)
                    }
                }
                .buttonStyle(.plain)
                .accessibilityLabel(Text(spoken(remaining: remaining)))
                .accessibilityHint(Text(isFinished ? "Stops the timer" : "Shows the timer's controls"))
                if isOpen || isFinished {
                    controls
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
            .padding(.leading, 8)
            .padding(.trailing, 16)
            .padding(.vertical, 8)
            .background(background)
            .overlay(
                RoundedRectangle(cornerRadius: 30, style: .continuous)
                    .strokeBorder(isFinished ? Color.clear : Color.primary.opacity(0.08))
            )
            .shadow(color: .black.opacity(0.12), radius: 12, y: 4)
            .scaleEffect(isFinished && pulse && !reduceMotion ? 1.04 : 1)
        }
        .onChange(of: isFinished, initial: true) { _, finished in
            guard finished, !reduceMotion else { return }
            withAnimation(.easeInOut(duration: 0.7).repeatForever(autoreverses: true)) { pulse = true }
        }
    }

    private var background: some View {
        RoundedRectangle(cornerRadius: 30, style: .continuous)
            .fill(isFinished ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.regularMaterial))
    }

    private func clockColor(urgent: Bool) -> Color {
        if isFinished { return .white }
        if isPaused { return .secondary }
        return urgent ? CookTimerStyle.urgent : .primary
    }

    /// The stove dial: a track, the time left draining clockwise, and a glyph for the state.
    private func ring(progress: Double, urgent: Bool) -> some View {
        let tint: Color = isFinished ? .white : isPaused ? .secondary : urgent ? CookTimerStyle.urgent : .accentColor
        return ZStack {
            Circle()
                .stroke(tint.opacity(0.22), lineWidth: 4)
            Circle()
                .trim(from: 0, to: isFinished ? 1 : max(0.001, 1 - progress))
                .stroke(tint, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                .rotationEffect(.degrees(-90))
                .animation(.linear(duration: 1), value: progress)
            Image(systemName: isFinished ? "bell.fill" : isPaused ? "pause.fill" : "timer")
                .font(.system(size: 14, weight: .bold))
                .foregroundStyle(tint)
        }
        .frame(width: 40, height: 40)
        .accessibilityHidden(true)
    }

    private var controls: some View {
        HStack(spacing: 8) {
            if !isFinished {
                controlButton(isPaused ? "Resume" : "Pause", systemImage: isPaused ? "play.fill" : "pause.fill") {
                    if isPaused { timers.resume(timer.id) } else { timers.pause(timer.id) }
                }
            }
            controlButton("1 Min", systemImage: "plus") {
                timers.addMinute(timer.id)
            }
            controlButton(isFinished ? "Stop" : "Cancel", systemImage: "xmark") {
                withAnimation(.snappy(duration: 0.25)) { timers.remove(timer.id) }
            }
        }
        .padding(.leading, 8)
    }

    private func controlButton(_ title: LocalizedStringKey, systemImage: String, action: @escaping () -> Void)
        -> some View
    {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .font(.subheadline.weight(.semibold))
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(
                    Capsule().fill(isFinished ? Color.white.opacity(0.22) : Color.primary.opacity(0.07))
                )
                .foregroundStyle(isFinished ? Color.white : Color.primary)
        }
        .buttonStyle(.plain)
    }

    private func spoken(remaining: TimeInterval) -> String {
        if isFinished { return String(localized: "\(timer.label) timer done") }
        let left = CookDuration.words(Int(remaining.rounded(.up)))
        return isPaused
            ? String(localized: "\(timer.label) timer paused, \(left) left")
            : String(localized: "\(timer.label) timer, \(left) left")
    }
}

enum CookTimerStyle {
    /// The last minute: warm, like a burner turning orange. Paired with the ring and numbers,
    /// never the only signal.
    static let urgent = Color(.systemOrange)
}

/// The quick adjust for a tapped time: starts at the high end of the recipe's range ("2-3
/// minutes" is 3), a minute more or less at a tap, then Start.
struct CookTimerSetup: View {
    let request: CookTimerRequest
    let label: String
    let start: (Int) -> Void

    @State private var seconds: Int

    init(request: CookTimerRequest, label: String, start: @escaping (Int) -> Void) {
        self.request = request
        self.label = label
        self.start = start
        _seconds = State(initialValue: request.highSeconds)
    }

    /// Short times move by 15 seconds; everything else by a minute.
    private var increment: Int { request.highSeconds < 120 ? 15 : 60 }

    private var recipeText: String {
        CookDuration(
            range: "".startIndex..<"".endIndex, lowSeconds: request.lowSeconds, highSeconds: request.highSeconds
        )
        .text
    }

    var body: some View {
        VStack(spacing: 14) {
            Text(label)
                .font(.headline)
                .foregroundStyle(.secondary)
            HStack(spacing: 18) {
                adjust(by: -increment, systemImage: "minus", label: "Less time")
                    .disabled(seconds <= increment)
                Text(CookTimer.clock(TimeInterval(seconds)))
                    .font(.system(size: 52, weight: .heavy, design: .rounded).monospacedDigit())
                    .contentTransition(.numericText())
                    .frame(minWidth: 150)
                    .accessibilityLabel(Text(CookDuration.words(seconds)))
                adjust(by: increment, systemImage: "plus", label: "More time")
                    .disabled(seconds >= 12 * 3600)
            }
            Text("The recipe says \(recipeText).")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button {
                start(seconds)
            } label: {
                Label("Start Timer", systemImage: "timer")
                    .font(.headline)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 6)
            }
            .buttonStyle(.borderedProminent)
        }
        .padding(20)
        .frame(minWidth: 300)
    }

    private func adjust(by delta: Int, systemImage: String, label: LocalizedStringKey) -> some View {
        Button {
            withAnimation(.snappy(duration: 0.2)) { seconds = max(increment, seconds + delta) }
        } label: {
            Image(systemName: systemImage)
                .font(.title3.weight(.bold))
                .frame(width: 48, height: 48)
                .background(Circle().fill(Color.accentColor.opacity(0.14)))
                .foregroundStyle(Color.accentColor)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

extension View {
    /// Press and hold a timer to drag it onto another one's place.
    func reorderable(_ timer: CookTimer, in timers: CookTimers) -> some View {
        draggable(timer.id.uuidString) {
            Text(timer.label)
                .font(.headline)
                .padding(10)
                .background(.regularMaterial, in: Capsule())
        }
        .dropDestination(for: String.self) { items, _ in
            guard let id = items.first.flatMap(UUID.init(uuidString:)) else { return false }
            withAnimation(.snappy(duration: 0.25)) { timers.move(id, to: timer.id) }
            return true
        }
    }
}
