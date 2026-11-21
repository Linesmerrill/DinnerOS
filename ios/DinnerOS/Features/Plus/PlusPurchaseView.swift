import SwiftUI

/// The DinnerOS Plus purchase screen (decision 675). Its words come from the API; prices come
/// from the App Store when it answers.
struct PlusPurchaseView: View {
    @Environment(PlusStore.self) private var plus
    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
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
        // A page-sized sheet on iPad, tall enough that every plan, price and term shows at once.
        .presentationSizing(.page)
    }

    /// How much of the screen the offer can take before the purchase controls.
    private enum Density {
        /// The big title, the benefits and a row per plan.
        case full
        /// A smaller title, the benefits in smaller type, and the plans side by side.
        case medium
        /// Only what deciding needs: the title, the plans side by side and the invite link.
        case small
    }

    /// Every plan and price, "Have an Invite Code?", the button, the renewal terms, and the
    /// Restore, Terms and Privacy links all show at once on every screen, an iPhone Duo's cover
    /// screen and an iPad on its side included: the roomiest layout that fits is used (build
    /// 122, Hark's review item 1: a pinned footer covered the plans, and on an iPad in landscape
    /// the terms ran off the sheet). Only at large text sizes, where nothing fits, does the page
    /// scroll, with the purchase controls pinned on a phone.
    private func content(_ offer: PlusOffer) -> some View {
        ViewThatFits(in: .vertical) {
            page(offer, density: .full)
            page(offer, density: .medium)
            page(offer, density: .small)
            ScrollView {
                offerContent(offer, density: .full, includesPurchase: !pinsPurchaseBar)
            }
            .dsBottomBar(isShown: pinsPurchaseBar) { purchaseBar(offer) }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    /// A phone pins the purchase controls when the page has to scroll; an iPad keeps them under
    /// the plans.
    private var pinsPurchaseBar: Bool { horizontalSizeClass != .regular }

    /// The whole offer, purchase controls included, with no scrolling.
    private func page(_ offer: PlusOffer, density: Density) -> some View {
        offerContent(offer, density: density, includesPurchase: true)
    }

    private func offerContent(_ offer: PlusOffer, density: Density, includesPurchase: Bool) -> some View {
        VStack(alignment: .leading, spacing: density == .full ? DS.Space.xxl : DS.Space.l) {
            // A household that entered an invite code sees what's left before its free month.
            if let progress = plus.status?.referralProgress, progress.isPending {
                ReferralProgressContent(progress: progress)
                    .padding(DS.Space.ml)
                    .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
            } else if let month = plus.openFreeMonth, !(plus.status?.active ?? false) {
                // An earned free month: redeeming it is how it starts (decision 755).
                FreeMonthCard(month: month)
                    .padding(DS.Space.ml)
                    .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
            }
            header(offer, density: density)
            if density != .small {
                VStack(alignment: .leading, spacing: density == .full ? DS.Space.m : DS.Space.xs) {
                    ForEach(offer.benefits, id: \.self) { benefit in
                        Label {
                            Text(benefit)
                        } icon: {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(DS.Colors.success)
                        }
                    }
                }
                .font(density == .full ? DS.Typography.body : DS.Typography.subheadline)
            }
            plans(offer, density: density)
            VStack(spacing: density == .full ? DS.Space.xxl : DS.Space.s) {
                if density == .full {
                    Text(offer.freeText)
                        .font(DS.Typography.callout)
                        .foregroundStyle(DS.Colors.secondaryText)
                        .multilineTextAlignment(.center)
                }
                Button("Have an Invite Code?") { showsInvite = true }
                    .buttonStyle(.dsTextLink)
            }
            .frame(maxWidth: .infinity)
            if includesPurchase {
                VStack(spacing: DS.Space.sm) { purchaseBar(offer) }
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(DS.Space.l)
        .dsReadableWidth()
    }

    @ViewBuilder
    private func header(_ offer: PlusOffer, density: Density) -> some View {
        if density == .full {
            VStack(alignment: .leading, spacing: DS.Space.s) {
                Image(systemName: "sparkles")
                    .font(DS.Typography.largeTitle)
                    .foregroundStyle(DS.Colors.accent)
                    .accessibilityHidden(true)
                Text(offer.title)
                    .font(DS.Typography.largeTitle)
                Text(offer.subtitle)
                    .font(DS.Typography.subtitle)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
        } else {
            VStack(alignment: .leading, spacing: DS.Space.xxs) {
                Label {
                    Text(offer.title)
                } icon: {
                    Image(systemName: "sparkles")
                        .foregroundStyle(DS.Colors.accent)
                        .accessibilityHidden(true)
                }
                .font(DS.Typography.title)
                Text(offer.subtitle)
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
        }
    }

    @ViewBuilder
    private func plans(_ offer: PlusOffer, density: Density) -> some View {
        if density == .full {
            VStack(spacing: DS.Space.m) { planRows(offer, compact: false) }
        } else {
            // Side by side when both fit, so a short screen keeps room for the terms.
            ViewThatFits(in: .horizontal) {
                // Both tiles as tall as the taller one.
                HStack(alignment: .top, spacing: DS.Space.s) { planRows(offer, compact: true) }
                    .fixedSize(horizontal: false, vertical: true)
                VStack(spacing: DS.Space.s) { planRows(offer, compact: false) }
            }
        }
    }

    private func planRows(_ offer: PlusOffer, compact: Bool) -> some View {
        ForEach(offer.plans) { plan in
            PlanRow(
                plan: plan, price: PlusText.price(plan, product: plus.products[plan.productID]),
                isSelected: plan.productID == selectedID(offer), compact: compact
            ) {
                selectedPlanID = plan.productID
            }
        }
    }

    @ViewBuilder
    private func purchaseBar(_ offer: PlusOffer) -> some View {
        if let status = plus.status, status.active {
            // Plus came on while this screen was open: an invite code, or a purchase.
            Label(
                PlusText.status(status, channel: BuildChannelStore.shared.channel),
                systemImage: "checkmark.seal.fill"
            )
            .font(DS.Typography.headline)
            Button("Done") { dismiss() }
                .buttonStyle(.dsPrimary(fullWidth: true))
        } else {
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
            }
            .buttonStyle(.dsPrimary(fullWidth: true))
            .disabled(!plus.canPurchase || plus.isPurchasing)
            Group {
                // Apple wants what's charged after the trial right by the button.
                Text(chargeLine(offer))
                    .font(DS.Typography.subheadline.weight(.semibold))
                if !plus.canPurchase {
                    Text("DinnerOS Plus isn’t for sale yet.")
                        .font(DS.Typography.footnote)
                        .foregroundStyle(DS.Colors.secondaryText)
                }
                if let error {
                    FormErrorLabel(message: error)
                }
                if let notice {
                    Text(notice)
                        .font(DS.Typography.footnote)
                }
                Text(offer.terms)
                    .font(DS.Typography.caption)
                    .foregroundStyle(DS.Colors.secondaryText)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .multilineTextAlignment(.center)
        }
        // Restore and the two links on one line: Restore Purchases, Terms of Use, Privacy Policy.
        ViewThatFits(in: .horizontal) {
            HStack(spacing: DS.Space.l) { legalLinks(offer) }
            // Narrow (the Duo's cover screen): Restore, then Terms and Privacy on one line.
            VStack(spacing: DS.Space.xxs) {
                restoreButton(offer)
                HStack(spacing: DS.Space.l) { documentLinks(offer) }
            }
            VStack(spacing: DS.Space.xxs) { legalLinks(offer) }
        }
        .font(DS.Typography.footnote)
    }

    @ViewBuilder
    private func legalLinks(_ offer: PlusOffer) -> some View {
        restoreButton(offer)
        documentLinks(offer)
    }

    private func restoreButton(_ offer: PlusOffer) -> some View {
        Button(offer.restoreText) { restore() }
            .disabled(!plus.canPurchase || plus.isPurchasing)
    }

    private func documentLinks(_ offer: PlusOffer) -> some View {
        ForEach(offer.shownLinks, id: \.self) { link in
            Link(link.title, destination: link.url)
        }
    }

    /// "Free for 14 days, then $39.99 a year." for the selected plan. The renewal itself is in
    /// the terms under it, so it's said once.
    private func chargeLine(_ offer: PlusOffer) -> String {
        guard let plan = offer.plans.first(where: { $0.productID == selectedID(offer) }) ?? offer.plans.first
        else { return "" }
        let product = plus.products[plan.productID]
        return PlusText.chargeLine(
            plan, product: product, trialDays: PlusText.trialDays(offerDays: offer.trialDays, product: product))
    }

    /// The yearly plan unless another was picked.
    private func selectedID(_ offer: PlusOffer) -> String {
        selectedPlanID ?? offer.plans.first?.productID ?? ""
    }

    /// The trial button while the App Store still offers this person a trial, or before it
    /// has answered.
    private func buttonTitle(_ offer: PlusOffer) -> String {
        PlusText.buyTitle(offer, product: plus.products[selectedID(offer)])
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
                    notice = String(localized: "We didn’t find a Plus purchase on this Apple account.")
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
    /// A tile for side-by-side plans: name, price and note stacked, the check in the corner.
    var compact = false
    let select: () -> Void

    var body: some View {
        Button(action: select) {
            Group {
                if compact {
                    VStack(alignment: .leading, spacing: DS.Space.xxs) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(plan.name)
                                .font(DS.Typography.headline)
                            Spacer(minLength: DS.Space.xxs)
                            check
                        }
                        Text(price)
                            .font(DS.Typography.subheadline)
                            .foregroundStyle(DS.Colors.secondaryText)
                            .fixedSize(horizontal: false, vertical: true)
                        if let note = plan.note { badge(note) }
                    }
                    .padding(DS.Space.m)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                } else {
                    HStack {
                        VStack(alignment: .leading, spacing: DS.Space.hairline) {
                            Text(plan.name)
                                .font(DS.Typography.headline)
                            Text(price)
                                .foregroundStyle(DS.Colors.secondaryText)
                        }
                        Spacer()
                        if let note = plan.note { badge(note) }
                        check
                    }
                    .padding(DS.Space.l)
                }
            }
            .contentShape(.rect)
            .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.medium))
            .overlay {
                // An outline on the plan not picked too: on a sheet its fill alone all but
                // vanished (an iPad in light mode, build 122).
                RoundedRectangle(cornerRadius: DS.Radius.medium)
                    .strokeBorder(isSelected ? DS.Colors.accent : DS.Colors.cardStroke, lineWidth: isSelected ? 2 : 1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isSelected ? [.isSelected, .isButton] : .isButton)
    }

    private var check: some View {
        Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
            .font(compact ? DS.Typography.sectionTitle : DS.Typography.title)
            .foregroundStyle(isSelected ? DS.Colors.accent : DS.Colors.secondaryText)
            .accessibilityHidden(true)
    }

    private func badge(_ note: String) -> some View {
        Text(note)
            .font(DS.Typography.caption.bold())
            .padding(.horizontal, DS.Space.s)
            .padding(.vertical, DS.Space.xxs)
            .background(DS.Colors.badge.opacity(0.15), in: .capsule)
    }
}

/// Presents the purchase screen whenever `PlusStore.showsPurchase` is set.
struct PlusPurchasePresenter: ViewModifier {
    @Environment(PlusStore.self) private var plus

    func body(content: Content) -> some View {
        @Bindable var plus = plus
        content
            .dsSheet(isPresented: $plus.showsPurchase, onDismiss: plus.showPendingCelebration) {
                PlusPurchaseView()
            }
            .fullScreenCover(item: $plus.celebration) { celebration in
                PlusThanksView(celebration: celebration)
            }
            .dsSheet(item: $plus.compThanks) { comp in
                PlusCompThanksView(comp: comp)
            }
            // A new household's code is in: what's left for the free month.
            .dsSheet(isPresented: $plus.showsChecklist) {
                ReferralChecklistSheet()
            }
            // "Step 2 done" and "Your free month is unlocked": over everything, sheets
            // included, without stopping what the person is doing.
            .freeMonthMoments()
            .alert(
                "Couldn’t Use Your Invite Code",
                isPresented: Binding(get: { plus.inviteError != nil }, set: { if !$0 { plus.inviteError = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(plus.inviteError ?? "")
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
                "Autopilot plans the week’s dinners for you.", "See what each week costs and how much you save.",
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
            trialDays: 14, trialText: "Try It Free for 14 Days", buyText: "Get DinnerOS Plus",
            terms:
                "Payment goes through your Apple account. Plus renews automatically until you cancel. Cancel in your Apple account settings at least 24 hours before the free trial or the current period ends.",
            restoreText: "Restore Purchases", freeText: "Sending your list to a store is always free.")
        static let free = PlusStatus(
            enabled: true, active: false, trial: false, expiresAt: nil, willRenew: false, billingIssue: false,
            freeUntil: nil, offer: offer)
    }
#endif
