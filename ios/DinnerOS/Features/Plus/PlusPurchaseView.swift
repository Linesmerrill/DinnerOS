import SwiftUI

/// The DinnerOS Plus purchase screen (decision 675). Its words come from the API; prices come
/// from the App Store when it answers.
struct PlusPurchaseView: View {
    @Environment(PlusStore.self) private var plus
    @Environment(\.dismiss) private var dismiss
    @State private var selectedPlanID: String?
    @State private var error: String?
    @State private var notice: String?
    @State private var showsInvite = false

    var body: some View {
        NavigationStack {
            Group {
                if let offer = plus.status?.offer {
                    content(offer)
                } else {
                    ContentUnavailableView(
                        "DinnerOS Plus", systemImage: "sparkles",
                        description: Text("Connect to the internet to see DinnerOS Plus."))
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not Now") { dismiss() }
                }
            }
            .navigationDestination(isPresented: $showsInvite) {
                PlusInviteView()
            }
        }
    }

    private func content(_ offer: PlusOffer) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 8) {
                    Image(systemName: "sparkles")
                        .font(.largeTitle)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text(offer.title)
                        .font(.largeTitle.bold())
                    Text(offer.subtitle)
                        .font(.title3)
                        .foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(offer.benefits, id: \.self) { benefit in
                        Label {
                            Text(benefit)
                        } icon: {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                        }
                    }
                }
                VStack(spacing: 12) {
                    ForEach(offer.plans) { plan in
                        PlanRow(
                            plan: plan, price: PlusText.price(plan, product: plus.products[plan.productID]),
                            isSelected: plan.productID == selectedID(offer)
                        ) {
                            selectedPlanID = plan.productID
                        }
                    }
                }
                if let status = plus.status, status.active {
                    // Plus came on while this screen was open: an invite code, or a purchase.
                    VStack(spacing: 12) {
                        Label(PlusText.status(status), systemImage: "checkmark.seal.fill")
                            .font(.headline)
                        Button("Done") { dismiss() }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.large)
                    }
                    .frame(maxWidth: .infinity)
                } else {
                    VStack(spacing: 12) {
                        Button {
                            buy(selectedID(offer))
                        } label: {
                            Group {
                                if plus.isPurchasing {
                                    ProgressView()
                                } else {
                                    Text(buttonTitle(offer))
                                }
                            }
                            .frame(maxWidth: .infinity, minHeight: 32)
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.large)
                        .disabled(!plus.canPurchase || plus.isPurchasing)
                        // Apple wants what's charged after the trial right by the button.
                        Text(chargeLine(offer))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        if !plus.canPurchase {
                            Text("DinnerOS Plus isn't for sale yet.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        if let error {
                            FormErrorLabel(message: error)
                        }
                        if let notice {
                            Text(notice)
                                .font(.callout)
                        }
                        Text(offer.freeText)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .multilineTextAlignment(.center)
                }
                VStack(spacing: 8) {
                    Text(offer.terms)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    HStack(spacing: 24) {
                        Button(offer.restoreText) { restore() }
                            .disabled(!plus.canPurchase || plus.isPurchasing)
                        Button("Have an invite code?") { showsInvite = true }
                    }
                    .font(.footnote)
                    HStack(spacing: 24) {
                        ForEach(offer.shownLinks, id: \.self) { link in
                            Link(link.title, destination: link.url)
                        }
                    }
                    .font(.footnote)
                }
                .frame(maxWidth: .infinity)
            }
            .padding()
            // Readable on iPad instead of stretching across the screen.
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
        }
    }

    /// "Free for 14 days, then $39.99 a year." or "$39.99 a year." for the selected plan.
    private func chargeLine(_ offer: PlusOffer) -> String {
        guard let plan = offer.plans.first(where: { $0.productID == selectedID(offer) }) ?? offer.plans.first
        else { return "" }
        let product = plus.products[plan.productID]
        let trial = offer.trialDays > 0 && (product?.trialEligible ?? true)
        return PlusText.chargeLine(plan, product: product, trialDays: trial ? offer.trialDays : 0)
    }

    /// The yearly plan unless another was picked.
    private func selectedID(_ offer: PlusOffer) -> String {
        selectedPlanID ?? offer.plans.first?.productID ?? ""
    }

    /// The trial button while the App Store still offers this person a trial, or before it
    /// has answered.
    private func buttonTitle(_ offer: PlusOffer) -> String {
        let product = plus.products[selectedID(offer)]
        let trial = offer.trialDays > 0 && (product?.trialEligible ?? true)
        return trial ? offer.trialText : offer.buyText
    }

    private func buy(_ productID: String) {
        error = nil
        notice = nil
        Task {
            do {
                if try await plus.purchase(productID: productID) {
                    dismiss()
                } else if !plus.isPurchasing {
                    notice = String(localized: "If you finished buying, Plus turns on in a minute.")
                }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func restore() {
        error = nil
        notice = nil
        Task {
            do {
                if try await plus.restore() {
                    dismiss()
                } else {
                    notice = String(localized: "We didn't find a Plus purchase on this Apple account.")
                }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

private struct PlanRow: View {
    let plan: PlusPlan
    let price: String
    let isSelected: Bool
    let select: () -> Void

    var body: some View {
        Button(action: select) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(plan.name)
                        .font(.headline)
                    Text(price)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if let note = plan.note {
                    Text(note)
                        .font(.caption.bold())
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.tint.opacity(0.15), in: .capsule)
                }
                Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                    .font(.title2)
                    .foregroundStyle(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    .accessibilityHidden(true)
            }
            .padding()
            .contentShape(.rect)
            .background(.background.secondary, in: .rect(cornerRadius: 12))
            .overlay {
                RoundedRectangle(cornerRadius: 12)
                    .strokeBorder(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.clear), lineWidth: 2)
            }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isSelected ? [.isSelected, .isButton] : .isButton)
    }
}

/// Presents the purchase screen whenever `PlusStore.showsPurchase` is set.
struct PlusPurchasePresenter: ViewModifier {
    @Environment(PlusStore.self) private var plus

    func body(content: Content) -> some View {
        @Bindable var plus = plus
        content
            .sheet(isPresented: $plus.showsPurchase, onDismiss: plus.showPendingCelebration) {
                PlusPurchaseView()
            }
            .fullScreenCover(item: $plus.celebration) { celebration in
                PlusThanksView(celebration: celebration)
            }
            .sheet(item: $plus.compThanks) { comp in
                PlusCompThanksView(comp: comp)
            }
            .sheet(item: $plus.linkedCode) { linked in
                NavigationStack {
                    PlusInviteView(code: linked.code)
                        .toolbar {
                            ToolbarItem(placement: .cancellationAction) {
                                Button("Close") { plus.linkedCode = nil }
                            }
                        }
                }
            }
    }
}

extension View {
    /// Shows the Plus purchase screen when a tapped feature is part of Plus.
    func plusPurchaseSheet() -> some View {
        modifier(PlusPurchasePresenter())
    }
}

#if DEBUG
    #Preview("Purchase") {
        let plus = PlusStore(session: .preview(.signedOut), api: nil, purchaser: StoreKitPlusPurchaser())
        plus.preview(status: PlusPreview.free)
        return PlusPurchaseView()
            .environment(plus)
    }

    enum PlusPreview {
        static let offer = PlusOffer(
            title: "DinnerOS Plus", subtitle: "Let DinnerOS plan your week and track what you spend.",
            benefits: [
                "Autopilot plans the week's dinners for you.", "See what each week costs and how much you save.",
                "One plan covers everyone in your household.",
            ],
            plans: [
                PlusPlan(
                    productID: "com.linesmerrill.dinneros.plus.yearly", name: "Yearly", priceText: "$39.99 a year",
                    note: "Save 33%"),
                PlusPlan(
                    productID: "com.linesmerrill.dinneros.plus.monthly", name: "Monthly", priceText: "$4.99 a month",
                    note: nil),
            ],
            trialDays: 14, trialText: "Try it free for 14 days", buyText: "Get DinnerOS Plus",
            terms:
                "Plus renews automatically until you cancel. Cancel anytime in your Apple account settings at least a day before it renews.",
            restoreText: "Restore purchases", freeText: "Sending your list to a store is always free.")
        static let free = PlusStatus(
            enabled: true, active: false, trial: false, expiresAt: nil, willRenew: false, billingIssue: false,
            freeUntil: nil, offer: offer)
    }
#endif
