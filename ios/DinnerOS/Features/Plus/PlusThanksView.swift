import SwiftUI

/// The thank-you after buying Plus: confetti, what it unlocked, and one way on to Your Plus.
struct PlusThanksView: View {
    let celebration: PlusCelebration
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showsYourPlus = false
    @State private var appeared = false

    /// One icon per benefit, in the offer's order.
    static let symbols = ["sparkles", "chart.bar.fill", "person.2.fill", "gift.fill"]

    var body: some View {
        NavigationStack {
            ZStack {
                ScrollView {
                    VStack(spacing: 28) {
                        Image(systemName: "heart.circle.fill")
                            .font(.system(size: 72))
                            .foregroundStyle(.tint)
                            .scaleEffect(appeared || reduceMotion ? 1 : 0.6)
                            .accessibilityHidden(true)
                        VStack(spacing: 10) {
                            Text("Thank you for supporting DinnerOS")
                                .font(.largeTitle.bold())
                                .multilineTextAlignment(.center)
                            Text(subtitle)
                                .font(.title3)
                                .foregroundStyle(.secondary)
                                .multilineTextAlignment(.center)
                        }
                        VStack(alignment: .leading, spacing: 16) {
                            Text("What you just unlocked")
                                .font(.headline)
                            ForEach(Array(celebration.benefits.prefix(4).enumerated()), id: \.offset) {
                                index, benefit in
                                Label {
                                    Text(benefit)
                                } icon: {
                                    Image(systemName: Self.symbols[index % Self.symbols.count])
                                        .foregroundStyle(.tint)
                                        .frame(minWidth: 28)
                                }
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding()
                        .background(.background.secondary, in: .rect(cornerRadius: 16))
                    }
                    .padding(24)
                    .frame(maxWidth: 560)
                    .frame(maxWidth: .infinity)
                }
                .safeAreaInset(edge: .bottom) {
                    Button {
                        showsYourPlus = true
                    } label: {
                        Text("Let's go")
                            .frame(maxWidth: 520)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .padding()
                }
                if !reduceMotion {
                    ConfettiView()
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                        .ignoresSafeArea()
                }
            }
            .navigationDestination(isPresented: $showsYourPlus) {
                YourPlusView()
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") { dismiss() }
                        }
                    }
            }
        }
        .sensoryFeedback(.success, trigger: appeared)
        .onAppear {
            withAnimation(reduceMotion ? nil : .spring(duration: 0.5, bounce: 0.4)) { appeared = true }
        }
    }

    private var subtitle: String {
        if let name = households.current?.household.name, !name.isEmpty {
            return String(localized: "\(name) has DinnerOS Plus now.")
        }
        return String(localized: "Your household has DinnerOS Plus now.")
    }
}

/// A short burst of confetti that falls once and stops. Callers leave it out with Reduce Motion.
struct ConfettiView: View {
    private struct Piece {
        let x: Double
        let delay: Double
        let speed: Double
        let drift: Double
        let spin: Double
        let size: Double
        let color: Color
    }

    private static let colors: [Color] = [.green, .yellow, .orange, .pink, .mint, .teal]
    private static let duration = 3.2

    @State private var start = Date.now
    private let pieces: [Piece] = (0..<70).map { i in
        var rng = SeededGenerator(seed: UInt64(i + 1))
        return Piece(
            x: .random(in: 0...1, using: &rng), delay: .random(in: 0...0.6, using: &rng),
            speed: .random(in: 0.35...0.6, using: &rng), drift: .random(in: -40...40, using: &rng),
            spin: .random(in: 2...6, using: &rng), size: .random(in: 6...11, using: &rng),
            color: colors[i % colors.count])
    }

    var body: some View {
        TimelineView(.animation(paused: Date.now.timeIntervalSince(start) > Self.duration)) { context in
            Canvas { canvas, size in
                let t = context.date.timeIntervalSince(start)
                for piece in pieces {
                    let life = t - piece.delay
                    guard life > 0 else { continue }
                    let y = -20 + life * piece.speed * size.height
                    guard y < size.height + 20 else { continue }
                    let x = piece.x * size.width + sin(life * 3) * piece.drift
                    var copy = canvas
                    copy.translateBy(x: x, y: y)
                    copy.rotate(by: .radians(life * piece.spin))
                    copy.opacity = max(0, 1 - life / Self.duration)
                    copy.fill(
                        Path(CGRect(x: -piece.size / 2, y: -piece.size / 4, width: piece.size, height: piece.size / 2)),
                        with: .color(piece.color))
                }
            }
        }
    }
}

/// Same confetti every time, so the burst looks designed rather than noisy.
private struct SeededGenerator: RandomNumberGenerator {
    var state: UInt64
    init(seed: UInt64) { state = seed &* 0x9E37_79B9_7F4A_7C15 }
    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
        z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
        return z ^ (z >> 31)
    }
}
