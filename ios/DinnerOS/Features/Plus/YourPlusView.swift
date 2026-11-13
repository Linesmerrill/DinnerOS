import StoreKit
import SwiftUI

/// Your Plus (decision 680): the plan and status, what Plus did for the household, invites,
/// and, for the member who started it, Apple's manage, restore and refund sheets and the
/// billing history. Everyone else sees status only.
struct YourPlusView: View {
    @Environment(PlusStore.self) private var plus
    @State private var loadError: String?
    @State private var showsManage = false
    @State private var refundTransactionID: UInt64?
    @State private var showsRefund = false
    @State private var isRestoring = false
    @State private var restoreMessage: String?
    @State private var taps = 0

    var body: some View {
        List {
            if let details = plus.details {
                Section {
                    PlusHeaderCard(details: details, price: price(details))
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                }
                if !details.stats.isEmpty {
                    Section {
                        PlusStatsGrid(stats: details.stats)
                            .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                            .listRowBackground(Color.clear)
                    } header: {
                        Text(details.statsTitle)
                    }
                }
                Section {
                    NavigationLink {
                        PlusInviteView()
                    } label: {
                        GiftBoxes(earned: details.monthsEarned, cap: details.monthsCap)
                    }
                } header: {
                    Text("Free Months From Invites")
                } footer: {
                    Text("Invite another household and you both get a free month.")
                }
                if details.canManage {
                    manageSection(details)
                    historySection(details)
                } else if let note = details.manageNote {
                    Section {
                        Text(note)
                            .foregroundStyle(.secondary)
                    }
                }
            } else if let loadError {
                FormErrorLabel(message: loadError)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
            }
        }
        .navigationTitle("Your Plus")
        .navigationBarTitleDisplayMode(.inline)
        .manageSubscriptionsSheet(isPresented: $showsManage)
        .refundRequestSheet(for: refundTransactionID ?? 0, isPresented: $showsRefund) { _ in
            Task { await load() }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: taps)
        .task { await load() }
        .refreshable { await load() }
        .onChange(of: showsManage) { _, showing in
            if !showing { Task { await load() } }
        }
    }

    private func price(_ details: PlusDetails) -> String? {
        guard let plan = details.plan else { return nil }
        let line = PlusPlan(productID: plan.productID, name: plan.name, priceText: plan.priceText, note: nil)
        return PlusText.price(line, product: plus.products[plan.productID])
    }

    private func manageSection(_ details: PlusDetails) -> some View {
        Section {
            if details.plan != nil {
                Button {
                    taps += 1
                    showsManage = true
                } label: {
                    Label("Manage Subscription", systemImage: "creditcard")
                }
            }
            Button {
                taps += 1
                restore()
            } label: {
                Label("Restore Purchases", systemImage: "arrow.clockwise")
            }
            .disabled(isRestoring)
            if refundTransactionID != nil {
                Button {
                    taps += 1
                    showsRefund = true
                } label: {
                    Label("Request a Refund", systemImage: "arrow.uturn.backward.circle")
                }
            }
            if let restoreMessage {
                Text(restoreMessage)
                    .foregroundStyle(.secondary)
            }
        } footer: {
            Text("Change your plan or cancel in Apple's own menu. Refunds are up to Apple.")
        }
    }

    private func historySection(_ details: PlusDetails) -> some View {
        Section {
            if details.history.isEmpty {
                Text("Nothing here yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(Array(details.history.enumerated()), id: \.offset) { _, item in
                if item.isInvite {
                    NavigationLink {
                        PlusInviteView()
                    } label: {
                        HistoryRow(item: item)
                    }
                } else {
                    HistoryRow(item: item)
                }
            }
        } header: {
            Text("History")
        } footer: {
            Text(details.historyNote)
        }
    }

    private func load() async {
        loadError = nil
        do {
            try await plus.loadDetails()
            refundTransactionID = await Self.latestTransactionID(for: plus.details?.plan?.productID)
        } catch is CancellationError {
        } catch {
            if plus.details == nil { loadError = HouseholdStore.message(for: error) }
        }
    }

    private func restore() {
        isRestoring = true
        restoreMessage = nil
        Task {
            defer { isRestoring = false }
            do {
                restoreMessage =
                    try await plus.restore()
                    ? String(localized: "Your purchases are restored.")
                    : String(localized: "We didn't find a Plus purchase on this Apple account.")
                await load()
            } catch {
                restoreMessage = error.localizedDescription
            }
        }
    }

    /// The App Store transaction a refund would be for: the current one for this plan on this
    /// Apple account, if any.
    static func latestTransactionID(for productID: String?) async -> UInt64? {
        guard let productID, let result = await Transaction.latest(for: productID),
            case .verified(let transaction) = result, transaction.revocationDate == nil
        else { return nil }
        return transaction.id
    }
}

/// The top of Your Plus: plan, status, date, badge and who started it, on a softly glowing card.
private struct PlusHeaderCard: View {
    let details: PlusDetails
    let price: String?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var glow = false

    private var title: some View {
        Label("DinnerOS Plus", systemImage: "sparkles")
            .font(.headline)
            .lineLimit(1)
    }

    @ViewBuilder private var badge: some View {
        if let badge = details.badge {
            Label(badge, systemImage: "star.fill")
                .labelStyle(.titleAndIcon)
                .font(.caption.bold())
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(.white.opacity(0.25), in: .capsule)
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 8) {
                title
                badge
            }
            Text(details.stateText)
                .font(.largeTitle.bold())
            if let plan = details.plan {
                Text([plan.name, price].compactMap { $0 }.joined(separator: ", "))
                    .font(.title3)
            }
            VStack(alignment: .leading, spacing: 4) {
                if let label = details.dateLabel, let date = details.date {
                    Text("\(label) \(PlusText.day(date))")
                }
                if let since = details.since {
                    Text("\(details.sinceText) \(PlusText.day(since))")
                }
                if let startedBy = details.startedBy {
                    Text("Started by \(startedBy)")
                }
            }
            .font(.subheadline)
            .opacity(0.9)
        }
        .foregroundStyle(.white)
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            LinearGradient(
                colors: [Color.accentColor, Color.accentColor.mix(with: .teal, by: 0.45)],
                startPoint: .topLeading, endPoint: .bottomTrailing),
            in: .rect(cornerRadius: 22)
        )
        .shadow(color: Color.accentColor.opacity(glow ? 0.45 : 0.2), radius: glow ? 18 : 8, y: 4)
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeInOut(duration: 2.4).repeatForever(autoreverses: true)) { glow = true }
        }
    }
}

/// "What Plus did for you": a grid that reflows for large text.
private struct PlusStatsGrid: View {
    let stats: [PlusDetails.Stat]
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        let columns = [GridItem(.adaptive(minimum: typeSize.isAccessibilitySize ? 260 : 150), spacing: 12)]
        LazyVGrid(columns: columns, spacing: 12) {
            ForEach(Array(stats.enumerated()), id: \.offset) { _, stat in
                VStack(alignment: .leading, spacing: 6) {
                    Image(systemName: stat.symbol)
                        .font(.title3)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text(stat.valueText)
                        .font(.title.bold())
                        .contentTransition(.numericText())
                    Text(stat.label)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: typeSize.isAccessibilitySize ? nil : 118, alignment: .topLeading)
                .padding(14)
                .background(.background, in: .rect(cornerRadius: 16))
                .accessibilityElement(children: .combine)
            }
        }
    }
}

/// Free months from invites as gift boxes, filled as they're earned.
struct GiftBoxes: View {
    let earned: Int
    let cap: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 6), count: 6), spacing: 8) {
                ForEach(0..<max(cap, 1), id: \.self) { i in
                    Image(systemName: i < earned ? "gift.fill" : "gift")
                        .font(.title3)
                        .foregroundStyle(i < earned ? AnyShapeStyle(.tint) : AnyShapeStyle(.tertiary))
                }
            }
            Text("\(earned) of \(cap) free months earned")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(earned) of \(cap) free months earned")
        .accessibilityHint("Shows your invites.")
    }
}

private struct HistoryRow: View {
    let item: PlusDetails.HistoryItem

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title)
                Text(PlusText.day(item.at))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if let amount = item.amountText {
                Text(amount)
                    .monospacedDigit()
            }
        }
        .accessibilityElement(children: .combine)
    }
}
