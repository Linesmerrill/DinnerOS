import SwiftUI

/// A free month from an invite (decision 755). Ready: what redeeming starts, then Redeem,
/// which copies the code and opens Apple's redeem sheet. On the way: a line saying so.
struct FreeMonthCard: View {
    let month: FreeMonth
    /// Called before Apple's sheet opens, so a card floating over the app can get out of the
    /// way.
    var onRedeem: (() -> Void)? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label {
                VStack(alignment: .leading, spacing: 4) {
                    Text(month.title)
                        .font(.headline)
                    Text(month.message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            } icon: {
                Image(systemName: month.isReady ? "gift.fill" : "hourglass")
                    .foregroundStyle(.tint)
            }
            if month.isReady {
                FreeMonthRedeemControls(month: month, onRedeem: onRedeem)
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("FreeMonthCard")
    }
}

/// What redeeming starts, said plainly, then Redeem and what it last said.
struct FreeMonthRedeemControls: View {
    let month: FreeMonth
    var onRedeem: (() -> Void)? = nil

    @Environment(PlusStore.self) private var plus

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(month.termsText)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("FreeMonthTerms")
            Button {
                onRedeem?()
                Task { await plus.redeemFreeMonth(month) }
            } label: {
                Group {
                    if plus.redeemingMonthID == month.id {
                        ProgressView()
                    } else {
                        Text(month.buttonTitle)
                    }
                }
                .font(.headline)
                .frame(maxWidth: .infinity, minHeight: 32)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(plus.redeemingMonthID != nil)
            .accessibilityIdentifier("RedeemFreeMonth")
            if let notice = plus.freeMonthNotice, notice.monthID == month.id {
                if notice.isError {
                    FormErrorLabel(message: notice.text)
                } else {
                    Label(notice.text, systemImage: "doc.on.clipboard")
                        .font(.subheadline.weight(.semibold))
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("FreeMonthCopied")
                }
            }
        }
    }
}

/// Give a Free Month's section for this household's own free month, ready or on the way.
struct FreeMonthSection: View {
    let month: FreeMonth

    var body: some View {
        Section {
            FreeMonthCard(month: month)
        } header: {
            Text("Your Free Month")
        }
    }
}
