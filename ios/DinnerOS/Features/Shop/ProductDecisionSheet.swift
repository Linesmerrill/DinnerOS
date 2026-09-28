import SwiftUI

/// "Before Opening Walmart": the products this phone found gone from Walmart, or couldn't check,
/// just before the hand-off. A cart link with a dead item adds nothing for it and says nothing,
/// so Walmart stays closed until each line is decided
/// (docs/shopping-providers.md#checking-saved-products).
struct ProductDecisionSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    @State private var choice: ProductChoice?
    @State private var errorMessage: String?

    private var review: ProductCheckReview? { shopping.productReview }

    var body: some View {
        NavigationStack {
            List {
                if let message = errorMessage ?? shopping.linkError {
                    FormErrorLabel(message: message)
                }
                if let review {
                    content(review)
                }
            }
            .navigationTitle("Before Opening Walmart")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        shopping.dismissProductReview()
                        dismiss()
                    }
                }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                openBar
            }
            .sheet(
                item: $choice,
                onDismiss: {
                    Task { await shopping.productWasRechosen() }
                },
                content: { choice in
                    ChooseProductSheet(choice: choice)
                })
        }
    }

    @ViewBuilder
    private func content(_ review: ProductCheckReview) -> some View {
        let gone = review.gone
        if !gone.isEmpty {
            Section {
                ForEach(gone) { line in
                    ProductDecisionRow(
                        line: line, decision: review.decision(for: line), canChoose: shopping.canEdit,
                        rechoose: { choice = ProductChoice(rechoosing: line) },
                        decide: { decide($0, line) }, undo: { undo(line) })
                }
            } header: {
                Text("No Longer on Walmart")
            } footer: {
                Text("These can't go in your cart. Choose a new product or leave them out.")
            }
        }
        let unknown = review.unknown
        if !unknown.isEmpty {
            Section {
                ForEach(unknown) { line in
                    ProductDecisionRow(
                        line: line, decision: review.decision(for: line), canChoose: shopping.canEdit,
                        rechoose: { choice = ProductChoice(rechoosing: line) },
                        decide: { decide($0, line) }, undo: { undo(line) },
                        look: line.productURL.map { url in { openURL(url) } })
                }
                Button {
                    Task { await shopping.checkAgain(Set(unknown.map(\.ingredientKey))) }
                } label: {
                    if shopping.isCheckingProducts {
                        ProgressView()
                    } else {
                        Label("Check Again", systemImage: "arrow.clockwise")
                    }
                }
                .disabled(shopping.isCheckingProducts)
            } header: {
                Text("Couldn't Check")
            } footer: {
                Text("Walmart didn't answer for these. Use Send Anyway only if you've seen it on Walmart.")
            }
        }
        let unavailable = review.unavailable
        if !unavailable.isEmpty {
            Section {
                ForEach(unavailable) { line in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.name)
                        Text(line.productName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text("Out of Stock")
            } footer: {
                Text("These still go in your cart. They may come back.")
            }
        }
    }

    private var openBar: some View {
        VStack(spacing: 8) {
            let remaining = review?.needsDecision.count ?? 0
            Button(action: open) {
                Group {
                    if shopping.isCreatingHandoff {
                        ProgressView()
                    } else {
                        Label("Open Walmart", systemImage: "cart")
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(remaining > 0 || shopping.isCheckingProducts || shopping.isCreatingHandoff)
            if remaining > 0 {
                Text(
                    remaining == 1
                        ? String(localized: "Decide on 1 item first.")
                        : String(localized: "Decide on \(remaining) items first.")
                )
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
        }
        .padding()
        .frame(maxWidth: .infinity)
        .background(.bar)
    }

    private func decide(_ decision: ProductCheckDecision, _ line: ProductCheckLine) {
        Task { await shopping.decide(decision, for: line.ingredientKey) }
    }

    private func undo(_ line: ProductCheckLine) {
        Task { await shopping.undecide(line.ingredientKey) }
    }

    private func open() {
        errorMessage = nil
        Task {
            do {
                try await shopping.openReviewedInWalmart()
                if !shopping.isReviewingProducts {
                    dismiss()
                }
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// One gone or unchecked line, with the ways to decide it, or the decision and a way to undo it.
private struct ProductDecisionRow: View {
    let line: ProductCheckLine
    let decision: ProductCheckDecision?
    let canChoose: Bool
    let rechoose: () -> Void
    let decide: (ProductCheckDecision) -> Void
    let undo: () -> Void
    /// Opens the product on Walmart, for an unchecked one.
    var look: (() -> Void)? = nil

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isGone: Bool { line.result.status == .gone }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(line.name)
                    .font(.headline)
                Text(line.productName)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .strikethrough(isGone)
                if let decision {
                    Label(
                        decision == .leaveOut ? "Left out of this cart" : "Sending anyway",
                        systemImage: decision == .leaveOut ? "minus.circle" : "checkmark.circle"
                    )
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            if canChoose {
                if decision != nil {
                    Button("Undo", action: undo)
                        .buttonStyle(.borderless)
                        .accessibilityLabel("Undo the choice for \(line.name)")
                } else {
                    buttons
                }
            }
        }
        .padding(.vertical, 2)
    }

    private var buttons: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(spacing: 12))
        return VStack(alignment: .leading, spacing: 8) {
            layout {
                Button("Re-choose", action: rechoose)
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Re-choose the product for \(line.name)")
                Button("Leave Out") { decide(.leaveOut) }
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Leave \(line.name) out of this cart")
                if !isGone {
                    Button("Send Anyway") { decide(.sendAnyway) }
                        .buttonStyle(.bordered)
                        .accessibilityLabel("Send \(line.name) without a check")
                }
            }
            if let look {
                Button("See It on Walmart", action: look)
                    .buttonStyle(.borderless)
                    .font(.footnote)
            }
        }
    }
}
