import SwiftUI

/// "Before Opening Walmart": the lines whose saved product Walmart no longer lists, or that
/// DinnerOS couldn't confirm lately. A cart link with a dead item fails in Walmart without
/// saying which one, so nothing opens until each line is re-chosen or knowingly left out
/// (docs/shopping-providers.md#checking-saved-products).
struct ProductDecisionSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var choice: ProductChoice?
    @State private var errorMessage: String?

    private var prompt: ShoppingProposal? { shopping.decisionPrompt }

    var body: some View {
        NavigationStack {
            List {
                if let message = errorMessage ?? shopping.linkError {
                    FormErrorLabel(message: message)
                }
                if let prompt {
                    content(prompt)
                }
            }
            .navigationTitle("Before Opening Walmart")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        shopping.dismissDecision()
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
                    Task { await shopping.recheckDecision() }
                },
                content: { choice in
                    ChooseProductSheet(choice: choice)
                })
        }
    }

    @ViewBuilder
    private func content(_ prompt: ShoppingProposal) -> some View {
        let open = prompt.needsDecision
        if !open.isEmpty {
            Section {
                ForEach(open) { line in
                    NeedsDecisionRow(
                        line: line, canChoose: shopping.canEdit,
                        rechoose: { choice = ProductChoice(rechoosing: line) },
                        leaveOut: { Task { await shopping.leaveOutOfCart(line.ingredientKey) } })
                }
            } header: {
                Text("Needs a Decision")
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text(
                        "A cart link with an item Walmart no longer sells can fail without saying which one. Choose a new product, or leave the item out of this cart."
                    )
                    if open.contains(where: { $0.reason == .productUnverified }) {
                        Text(
                            prompt.checksPaused == true
                                ? "Walmart isn't answering our checks right now, so these can't be confirmed. Re-choose or leave them out, or try again later."
                                : "Items we couldn't confirm may be fine. Check Again tries once more."
                        )
                    }
                }
            }
            if open.contains(where: { $0.reason == .productUnverified }) {
                Section {
                    Button {
                        Task { await shopping.recheckDecision() }
                    } label: {
                        if shopping.isCheckingProducts {
                            ProgressView()
                        } else {
                            Label("Check Again", systemImage: "arrow.clockwise")
                        }
                    }
                    .disabled(shopping.isCheckingProducts)
                }
            }
        }
        let leftOut = prompt.excluded.filter { $0.reason == .leftOutGone || $0.reason == .leftOutUnverified }
        if !leftOut.isEmpty {
            Section {
                ForEach(leftOut) { line in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(line.name)
                            Text(line.text)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                        .accessibilityElement(children: .combine)
                        Spacer(minLength: 8)
                        Button("Put Back") {
                            Task { await shopping.putBackInCart(line.ingredientKey) }
                        }
                        .buttonStyle(.borderless)
                        .accessibilityLabel("Put \(line.name) back")
                    }
                }
            } header: {
                Text("Left Out of This Cart")
            }
        }
        let unavailable = prompt.mayBeUnavailable
        if !unavailable.isEmpty {
            Section {
                ForEach(unavailable) { line in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.name)
                        Text(line.product.healthText ?? ShoppingText.mayBeUnavailable)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text("May Be Out of Stock")
            } footer: {
                Text("These still go in your cart. Walmart may offer a substitute or leave them out at checkout.")
            }
        }
    }

    private var openBar: some View {
        VStack(spacing: 8) {
            let remaining = prompt?.needsDecision.count ?? 0
            Button {
                open()
            } label: {
                Label("Open Walmart", systemImage: "cart")
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

    private func open() {
        errorMessage = nil
        shopping.dismissDecision()
        dismiss()
        Task {
            do {
                try await shopping.openInWalmart()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// A line whose saved product needs re-choosing, with the two ways to resolve it.
struct NeedsDecisionRow: View {
    let line: ShoppingExcludedLine
    let canChoose: Bool
    var mealNote: String? = nil
    var remove: ShopRemoveOptions? = nil
    let rechoose: () -> Void
    let leaveOut: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var title: LocalizedStringKey {
        line.reason == .productGone ? "Needs Re-choosing" : "Couldn't Confirm"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(line.name)
                    .font(.headline)
                if let product = line.product {
                    Text(product.displayName)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .strikethrough(line.reason == .productGone)
                }
                Label(title, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.orange)
                Text(line.product?.healthText ?? line.text)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if let mealNote {
                    Text(mealNote)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            if canChoose {
                let layout =
                    dynamicTypeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                    : AnyLayout(HStackLayout(spacing: 12))
                layout {
                    Button("Re-choose", action: rechoose)
                        .buttonStyle(.bordered)
                        .accessibilityLabel("Re-choose the product for \(line.name)")
                    Button("Leave Out of Cart", action: leaveOut)
                        .buttonStyle(.borderless)
                        .accessibilityLabel("Leave \(line.name) out of this cart")
                    if let remove {
                        Spacer(minLength: 0)
                        ShopRemoveMenu(options: remove)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }
}
