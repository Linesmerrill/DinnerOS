import SwiftUI

/// The one-time thank-you for households that tested the beta: Plus is on the house.
struct PlusCompThanksView: View {
    let comp: PlusComp
    @Environment(\.dismiss) private var dismiss
    @Environment(PlusStore.self) private var plus
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var appeared = false

    var body: some View {
        ZStack {
            ScrollView {
                VStack(spacing: 20) {
                    Image(systemName: "gift.fill")
                        .font(.system(size: 56))
                        .foregroundStyle(.tint)
                        .scaleEffect(appeared || reduceMotion ? 1 : 0.6)
                        .accessibilityHidden(true)
                    Text(comp.thanksTitle)
                        .font(.title.bold())
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(comp.thanksText)
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    if let benefits = comp.benefits, !benefits.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            if let title = comp.benefitsTitle {
                                Text(title)
                                    .font(.headline)
                            }
                            ForEach(Array(benefits.enumerated()), id: \.offset) { index, benefit in
                                Label {
                                    Text(benefit)
                                        .fixedSize(horizontal: false, vertical: true)
                                } icon: {
                                    Image(systemName: PlusThanksView.symbols[index % PlusThanksView.symbols.count])
                                        .foregroundStyle(.tint)
                                        .frame(minWidth: 28)
                                }
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding()
                        .background(.background.secondary, in: .rect(cornerRadius: 16))
                    }
                    Label("Founding member", systemImage: "star.fill")
                        .font(.subheadline.bold())
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(.tint.opacity(0.15), in: .capsule)
                    Button {
                        dismiss()
                    } label: {
                        Text("Thanks")
                            .frame(maxWidth: 420)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                }
                .padding(28)
                .frame(maxWidth: 520)
                .frame(maxWidth: .infinity)
            }
            if !reduceMotion {
                ConfettiView()
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                    .ignoresSafeArea()
            }
        }
        .presentationDetents([.large])
        .sensoryFeedback(.success, trigger: appeared)
        .onAppear {
            plus.compThanksShown()
            withAnimation(reduceMotion ? nil : .spring(duration: 0.5, bounce: 0.4)) { appeared = true }
        }
    }
}
