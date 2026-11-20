import SwiftUI
import UIKit

// MARK: - Where the moments show

extension View {
    /// "Step 2 done" and "Your free month is ready" (decisions 729 and 755), in a window of their own
    /// over the app: they show above any sheet, and touches outside the card go to the app
    /// underneath, so nobody's flow stops. Attach once, inside the Plus environment.
    func freeMonthMoments() -> some View {
        background(FreeMonthMomentsHost())
    }
}

/// The card's frame on screen, so the window takes touches there and nowhere else.
@MainActor
final class MomentHitArea {
    var frame: CGRect = .null
}

private struct FreeMonthMomentsHost: View {
    @Environment(PlusStore.self) private var plus

    var body: some View {
        FreeMonthMomentsInstaller(plus: plus, showing: plus.freeMonthOn != nil || plus.stepDone != nil)
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
    }
}

/// Puts the moments in a window above the app's own, shown only while one is up.
private struct FreeMonthMomentsInstaller: UIViewRepresentable {
    let plus: PlusStore
    let showing: Bool

    func makeUIView(context: Context) -> InstallerView { InstallerView(plus: plus) }

    func updateUIView(_ uiView: InstallerView, context: Context) {
        uiView.setShowing(showing)
    }

    final class InstallerView: UIView {
        private let plus: PlusStore
        private var momentWindow: MomentWindow?
        private var showing = false

        init(plus: PlusStore) {
            self.plus = plus
            super.init(frame: .zero)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

        func setShowing(_ showing: Bool) {
            self.showing = showing
            momentWindow?.isHidden = !showing
        }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            guard let scene = window?.windowScene else {
                // Signed out or the tabs were rebuilt: the next root brings its own.
                momentWindow?.isHidden = true
                momentWindow = nil
                return
            }
            guard momentWindow == nil else { return }
            let hitArea = MomentHitArea()
            let window = MomentWindow(windowScene: scene)
            window.hitArea = hitArea
            // Under the Copied HUD, over sheets and alerts.
            window.windowLevel = .alert
            let host = UIHostingController(rootView: AnyView(FreeMonthMomentsView(hitArea: hitArea).environment(plus)))
            host.view.backgroundColor = .clear
            window.rootViewController = host
            window.isHidden = !showing
            momentWindow = window
        }
    }
}

/// Takes touches on the card only; everything else goes to the app underneath.
final class MomentWindow: UIWindow {
    var hitArea: MomentHitArea?

    override func hitTest(_ point: CGPoint, with event: UIEvent?) -> UIView? {
        guard let frame = hitArea?.frame, frame.contains(point) else { return nil }
        return super.hitTest(point, with: event)
    }
}

/// Whichever moment is up. The free month card wins over a step's.
private struct FreeMonthMomentsView: View {
    let hitArea: MomentHitArea
    @Environment(PlusStore.self) private var plus

    var body: some View {
        ZStack {
            if let progress = plus.freeMonthOn {
                FreeMonthUnlockedCard(hitArea: hitArea) {
                    plus.freeMonthOn = nil
                    plus.freeMonthShown()
                }
                .id(progress.id)
            } else if let moment = plus.stepDone {
                StepDoneCard(moment: moment, hitArea: hitArea) {
                    if plus.stepDone == moment { plus.stepDone = nil }
                }
                .id(moment.id)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Step done

/// "Step 2 done", "2 of 3 done" and what's left, near the bottom of the screen, with a check
/// mark and a little confetti. Fades on its own after a few seconds; a tap closes it sooner.
struct StepDoneCard: View {
    let moment: ReferralStepMoment
    let hitArea: MomentHitArea
    let close: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.horizontalSizeClass) private var sizeClass
    @ScaledMetric(relativeTo: .title) private var markSize: CGFloat = 34
    @State private var shown = false
    @State private var closing = false

    var body: some View {
        VStack {
            Spacer()
            Button(action: dismiss) { card }
                .buttonStyle(.plain)
                .accessibilityHint("Closes this")
                .accessibilityIdentifier("StepDoneCard")
                .onGeometryChange(for: CGRect.self) {
                    $0.frame(in: .global)
                } action: {
                    hitArea.frame = $0
                }
                .opacity(shown ? 1 : 0)
                .offset(y: shown || reduceMotion ? 0 : 40)
                .scaleEffect(shown || reduceMotion ? 1 : 0.95)
        }
        // On a phone the tab bar is at the bottom: stay above it. On an iPad it's at the top.
        .padding(.bottom, sizeClass == .compact ? 72 : 28)
        .padding(.horizontal, DS.Space.l)
        .task { await run() }
        .onDisappear { hitArea.frame = .null }
    }

    private var card: some View {
        HStack(alignment: .center, spacing: DS.Space.ml) {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: markSize))
                .symbolRenderingMode(.palette)
                .foregroundStyle(DS.Colors.onAccent, DS.Colors.success)
                .symbolEffect(.bounce, value: shown && !reduceMotion)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: DS.Space.xxs) {
                Text(moment.title)
                    .font(.headline)
                HStack(spacing: DS.Space.s) {
                    StepDots(done: moment.done, total: moment.total)
                    Text(moment.countText)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
                Text(moment.message)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(DS.Space.l)
        .frame(maxWidth: 520)
        .background(DS.Colors.raisedSurface, in: .rect(cornerRadius: DS.Radius.xLarge))
        .overlay {
            RoundedRectangle(cornerRadius: DS.Radius.xLarge).strokeBorder(DS.Colors.success.opacity(0.35), lineWidth: 1)
        }
        .shadow(color: .black.opacity(0.18), radius: 16, y: 6)
        .overlay {
            if !reduceMotion {
                ConfettiBurst(count: 28, duration: 1.4)
                    .frame(width: 340, height: 260)
            }
        }
        .contentShape(.rect)
    }

    private func run() async {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
        AccessibilityNotification.Announcement(moment.announcement).post()
        withAnimation(reduceMotion ? .easeOut(duration: 0.25) : .spring(duration: 0.4, bounce: 0.35)) {
            shown = true
        }
        let seconds = ReferralMoments.seconds(
            ReferralMoments.stepSeconds, voiceOver: UIAccessibility.isVoiceOverRunning)
        try? await Task.sleep(for: .seconds(seconds))
        guard !Task.isCancelled else { return }
        dismiss()
    }

    private func dismiss() {
        guard !closing else { return }
        closing = true
        hitArea.frame = .null
        withAnimation(.easeIn(duration: 0.25)) {
            shown = false
        } completion: {
            close()
        }
    }
}

/// A dot for each step, filled in for the ones done.
private struct StepDots: View {
    let done: Int
    let total: Int
    @ScaledMetric(relativeTo: .subheadline) private var size: CGFloat = 8

    var body: some View {
        HStack(spacing: DS.Space.xxs) {
            ForEach(0..<max(total, 0), id: \.self) { index in
                Circle()
                    .fill(index < done ? DS.Colors.success : DS.Colors.secondaryText.opacity(0.3))
                    .frame(width: size, height: size)
            }
        }
        .accessibilityHidden(true)
    }
}

// MARK: - The free month is ready

/// The last step's bigger moment, in the middle of the screen with confetti falling over
/// everything (decision 729). The free month comes as an Apple offer code (decision 755), so a
/// ready one says what redeeming starts and has Redeem; it stays until it's closed. One still
/// on the way says so and fades on its own.
struct FreeMonthUnlockedCard: View {
    let hitArea: MomentHitArea
    let close: () -> Void

    @Environment(PlusStore.self) private var plus
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 60
    @State private var shown = false
    @State private var closing = false
    /// The month as it was when the card opened, so Redeem's refresh doesn't swap it out.
    @State private var pinned: FreeMonth?

    private var month: FreeMonth? { pinned ?? plus.openFreeMonth }

    private var title: String {
        if let month { return month.title }
        // An API from before offer codes, with a free month in its own database.
        return String(localized: "Your free month is unlocked")
    }

    var body: some View {
        ZStack {
            if !reduceMotion {
                ConfettiView()
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                    .ignoresSafeArea()
            }
            card
                .onGeometryChange(for: CGRect.self) {
                    $0.frame(in: .global)
                } action: {
                    hitArea.frame = $0
                }
                .opacity(shown ? 1 : 0)
                .scaleEffect(shown || reduceMotion ? 1 : 0.85)
                .padding(DS.Space.xxl)
        }
        .task { await run() }
        .onDisappear { hitArea.frame = .null }
        .onChange(of: plus.freeMonthNotice) { _, notice in
            // The code is copied: get out of the way of Apple's sheet, which opens next.
            guard let notice, let month, notice.monthID == month.id, !notice.isError else { return }
            Task {
                try? await Task.sleep(for: .seconds(1))
                dismiss()
            }
        }
    }

    private var card: some View {
        VStack(spacing: DS.Space.ml) {
            Image(systemName: "gift.fill")
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
                .overlay(alignment: .bottomTrailing) {
                    Image(systemName: month?.isOwed == true ? "hourglass.circle.fill" : "checkmark.circle.fill")
                        .font(.system(size: markSize * 0.42))
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(
                            DS.Colors.onAccent, month?.isOwed == true ? DS.Colors.warning : DS.Colors.success
                        )
                        .offset(x: markSize * 0.18, y: markSize * 0.12)
                }
                .symbolEffect(.bounce, value: shown && !reduceMotion)
                .padding(.bottom, DS.Space.xxs)
                .accessibilityHidden(true)
            Text(title)
                .font(.title2.bold())
                .multilineTextAlignment(.center)
                .accessibilityAddTraits(.isHeader)
            if let month {
                Text(month.message)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                if month.isReady {
                    FreeMonthRedeemControls(month: month)
                        .multilineTextAlignment(.leading)
                        .padding(.top, DS.Space.xxs)
                    Button("Not Now", action: dismiss)
                        .buttonStyle(.dsTextLink)
                } else {
                    doneButton
                }
            } else {
                if let until = plus.status?.freeUntil {
                    Text("Plus is free until \(PlusText.day(until)).")
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                doneButton
            }
        }
        .padding(28)
        .frame(maxWidth: 420)
        // White in light; a raised gray in dark, so it stands off the app behind it.
        .background(DS.Colors.raisedSurface, in: .rect(cornerRadius: 28))
        .shadow(color: .black.opacity(0.25), radius: 28, y: 10)
        .contentShape(.rect)
        .onTapGesture {
            // A ready month waits for Redeem or Not Now; a tap elsewhere shouldn't lose it.
            if month?.isReady != true { dismiss() }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("FreeMonthUnlocked")
    }

    private var doneButton: some View {
        Button(action: dismiss) {
            Text("Done")
        }
        .buttonStyle(.dsPrimary(fullWidth: true))
        .padding(.top, DS.Space.xxs)
    }

    private func run() async {
        pinned = plus.openFreeMonth
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        AccessibilityNotification.Announcement(title).post()
        withAnimation(reduceMotion ? .easeOut(duration: 0.25) : .spring(duration: 0.5, bounce: 0.4)) {
            shown = true
        }
        // Redeem needs reading first, so a ready month stays until it's closed.
        guard month?.isReady != true else { return }
        let seconds = ReferralMoments.seconds(
            ReferralMoments.unlockedSeconds, voiceOver: UIAccessibility.isVoiceOverRunning)
        try? await Task.sleep(for: .seconds(seconds))
        guard !Task.isCancelled else { return }
        dismiss()
    }

    private func dismiss() {
        guard !closing else { return }
        closing = true
        hitArea.frame = .null
        withAnimation(.easeIn(duration: 0.3)) {
            shown = false
        } completion: {
            close()
        }
    }
}

// MARK: - Confetti

/// A small burst of confetti drawn with Canvas: pieces fly up and out from the middle, then
/// fall and fade. Draws nothing once it's done, and never takes a touch. The free month card
/// uses the purchase thank-you's falling `ConfettiView` instead.
struct ConfettiBurst: View {
    let count: Int
    let duration: Double

    @State private var pieces: [BurstPiece] = []
    @State private var start = Date.now
    @State private var finished = false

    var body: some View {
        TimelineView(.animation(paused: finished)) { context in
            Canvas { canvas, size in
                let elapsed = context.date.timeIntervalSince(start)
                guard elapsed < duration else { return }
                for piece in pieces {
                    draw(piece, at: elapsed, in: &canvas, size: size)
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
        .task {
            var generator = SystemRandomNumberGenerator()
            pieces = (0..<count).map { _ in BurstPiece.random(using: &generator) }
            start = .now
            try? await Task.sleep(for: .seconds(duration))
            finished = true
        }
    }

    private func draw(_ piece: BurstPiece, at t: Double, in canvas: inout GraphicsContext, size: CGSize) {
        let gravity = 520.0
        let x = size.width / 2 + piece.velocity.dx * t + sin(t * piece.wobble) * 6
        let y = size.height * 0.55 + piece.velocity.dy * t + 0.5 * gravity * t * t
        var context = canvas
        context.opacity = max(0, 1 - pow(t / duration, 3))
        context.translateBy(x: x, y: y)
        context.rotate(by: .radians(piece.spin * t))
        // Turning over: the piece narrows and widens as it falls.
        let turn = max(abs(cos(t * piece.flip)), 0.15)
        let rect = CGRect(
            x: -piece.size.width / 2 * turn, y: -piece.size.height / 2,
            width: piece.size.width * turn, height: piece.size.height)
        let shape = piece.round ? Path(ellipseIn: rect) : Path(roundedRect: rect, cornerRadius: 1.5)
        context.fill(shape, with: .color(piece.color))
    }
}

/// One piece of the burst: how it moves and how it looks.
private struct BurstPiece {
    /// Points per second.
    var velocity: CGVector
    var spin: Double
    var flip: Double
    var wobble: Double
    var size: CGSize
    var round: Bool
    var color: Color

    static let colors: [Color] = [.pink, .orange, .yellow, .green, .teal, .blue, .purple]

    static func random(using generator: inout some RandomNumberGenerator) -> BurstPiece {
        // Up and out, mostly upward.
        let angle = Double.random(in: (-.pi * 0.95)...(-.pi * 0.05), using: &generator)
        let speed = Double.random(in: 160...340, using: &generator)
        let width = Double.random(in: 6...10, using: &generator)
        return BurstPiece(
            velocity: CGVector(dx: cos(angle) * speed, dy: sin(angle) * speed),
            spin: Double.random(in: -8...8, using: &generator),
            flip: Double.random(in: 4...10, using: &generator),
            wobble: Double.random(in: 2...6, using: &generator),
            size: CGSize(width: width, height: width * Double.random(in: 1.2...1.8, using: &generator)),
            round: Double.random(in: 0...1, using: &generator) < 0.25,
            color: colors.randomElement(using: &generator) ?? .pink)
    }
}

#if DEBUG
    #Preview("Step done") {
        StepDoneCard(
            moment: ReferralStepMoment(
                stepID: "recipes", number: 2, done: 2, total: 3, message: "Plan a dinner to unlock your free month"),
            hitArea: MomentHitArea()
        ) {}
    }
#endif
