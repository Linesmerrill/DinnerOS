import SwiftUI

/// "Did you order these?": records the handed-off lines a member ordered as pantry purchases.
/// Walmart doesn't report orders, so the member says what was bought.
struct OrderConfirmationSheet: View {
    let handoff: ShoppingHandoff

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var draft: OrderConfirmationDraft
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(handoff: ShoppingHandoff) {
        self.handoff = handoff
        _draft = State(initialValue: OrderConfirmationDraft(handoff: handoff))
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(
                        "Walmart doesn't tell \(configuration.displayName) what you ordered. Check what you bought and it's added to your pantry."
                    )
                    .foregroundStyle(.secondary)
                    if let errorMessage {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    ForEach(draft.lines) { line in
                        OrderLineRow(
                            line: line, isSelected: draft.isSelected(line),
                            packages: Binding(
                                get: { draft.packages(for: line) },
                                set: { draft.setPackages($0, for: line) }),
                            toggle: { draft.toggle(line) })
                    }
                } header: {
                    Text("Sent to Walmart \(handoff.createdAt.formatted(.relative(presentation: .named)))")
                }
            }
            .navigationTitle("Did You Order These?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not Yet") {
                        shopping.dismissConfirmation()
                    }
                }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                actions
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
        }
    }

    private var actions: some View {
        VStack(spacing: 8) {
            Button {
                confirm(draft.everythingRequest)
            } label: {
                Group {
                    if isSaving {
                        ProgressView()
                    } else {
                        Text("Ordered Everything")
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(draft.lines.isEmpty)

            Button {
                if let request = draft.selectedRequest {
                    confirm(request)
                }
            } label: {
                Text(selectedTitle)
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
            .disabled(draft.selectedRequest == nil)
            .accessibilityHint("Adds the checked items to the pantry and marks the rest as not ordered.")
        }
        .padding()
        .background(.bar)
    }

    private var selectedTitle: String {
        draft.selectedCount == 1
            ? String(localized: "Ordered Only the 1 Checked Item")
            : String(localized: "Ordered Only the \(draft.selectedCount) Checked Items")
    }

    private func confirm(_ request: ConfirmShoppingOrderRequest) {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                // Success clears the store's prompt, which closes this sheet.
                try await shopping.confirm(request, for: handoff)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

private struct OrderLineRow: View {
    let line: ShoppingHandoffLine
    let isSelected: Bool
    @Binding var packages: Int
    let toggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button(action: toggle) {
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.name)
                        Text(line.product.displayName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        if line.confirmation?.status == .skipped {
                            Text("Marked not ordered")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 0)
                }
                // Inside a list Button, hierarchical styles resolve against the tint.
                .foregroundStyle(Color.primary)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(isSelected ? .isSelected : [])
            .accessibilityHint(isSelected ? "Leaves it out" : "Marks it ordered")

            if isSelected {
                Stepper(value: $packages, in: ShoppingLimits.packages) {
                    Text(ShoppingText.packages(packages))
                        .font(.subheadline)
                }
                .padding(.leading, 36)
                .accessibilityLabel("Packages of \(line.name) ordered")
                .accessibilityValue(ShoppingText.packages(packages))
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    OrderConfirmationSheet(handoff: ShopPreviewData.handoff)
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}
