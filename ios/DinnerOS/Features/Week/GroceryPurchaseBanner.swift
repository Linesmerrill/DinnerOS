import SwiftUI

/// The card at the bottom of a grocery list: "Add to pantry?" for the line just checked
/// off, a purchase that failed, or a brief confirmation. It never covers the list with a
/// modal, so a shopper can keep checking lines off.
struct GroceryPurchaseBanner: View {
    let model: GroceryListModel

    var body: some View {
        Group {
            if let prompt = model.purchasePrompt {
                GroceryPurchasePromptCard(model: model, prompt: prompt)
                    .id(prompt.id)
            } else if let failure = model.purchaseFailure {
                failureCard(failure)
            } else if model.purchasesInFlight > 0 {
                BannerCard {
                    HStack(spacing: DS.Space.s) {
                        ProgressView()
                        Text("Adding to pantry…")
                            .font(.subheadline)
                        Spacer(minLength: 0)
                    }
                    .accessibilityElement(children: .combine)
                }
            } else if let recorded = model.lastRecordedPurchase {
                recordedCard(recorded) { model.dismissRecordedPurchase(id: $0) }
            } else if let batch = model.lastRecordedBatch {
                // "Made It" on a house-made batch.
                recordedCard(batch) { model.dismissRecordedBatch(id: $0) }
            }
        }
        .transition(.move(edge: .bottom).combined(with: .opacity))
        .animation(.snappy, value: model.purchasePrompt?.id)
        .animation(.snappy, value: model.purchaseFailure?.id)
        .animation(.snappy, value: model.lastRecordedPurchase?.id)
        .animation(.snappy, value: model.lastRecordedBatch?.id)
    }

    private func failureCard(_ failure: GroceryListModel.PurchaseFailure) -> some View {
        BannerCard {
            VStack(alignment: .leading, spacing: DS.Space.s) {
                Label {
                    VStack(alignment: .leading, spacing: DS.Space.hairline) {
                        Text("Couldn't add \(failure.prompt.item.name) to the pantry")
                            .font(.subheadline.weight(.semibold))
                        Text(failure.message)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .symbolRenderingMode(.multicolor)
                }
                HStack {
                    Spacer()
                    Button("Dismiss") { model.dismissPurchaseFailure() }
                        .buttonStyle(.dsSecondary(.small))
                    if !failure.isForbidden {
                        Button("Try Again") {
                            Task { await model.retryPurchase() }
                        }
                        .buttonStyle(.dsPrimary(.small))
                    }
                }
            }
            .onAppear {
                AccessibilityNotification.Announcement(
                    String(localized: "Couldn't add \(failure.prompt.item.name) to the pantry")
                ).post()
            }
        }
    }

    private func recordedCard(
        _ recorded: GroceryListModel.RecordedPurchase, dismiss: @escaping (String) -> Void
    ) -> some View {
        BannerCard {
            HStack {
                Label("Added \(recorded.name) to the pantry", systemImage: "checkmark.circle.fill")
                    .font(.subheadline)
                    .foregroundStyle(.tint)
                Spacer(minLength: 0)
            }
        }
        .onAppear {
            AccessibilityNotification.Announcement(String(localized: "Added \(recorded.name) to the pantry")).post()
        }
        .task(id: recorded.id) {
            do {
                try await Task.sleep(for: .seconds(3))
                dismiss(recorded.id)
            } catch {
                // Replaced or dismissed.
            }
        }
    }
}

/// "Add Yellow Onion to pantry?" with the amount and unit editable.
private struct GroceryPurchasePromptCard: View {
    @Bindable var model: GroceryListModel
    let prompt: GroceryListModel.PurchasePrompt

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @FocusState private var isAmountFocused: Bool

    private var isLarge: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        BannerCard {
            VStack(alignment: .leading, spacing: DS.Space.sm) {
                HStack(alignment: .center) {
                    Label {
                        Text("Add \(prompt.item.name) to pantry?")
                            .font(.subheadline.weight(.semibold))
                    } icon: {
                        Image(systemName: "cabinet")
                            .foregroundStyle(.tint)
                    }
                    Spacer(minLength: 8)
                    optionsMenu
                }
                let layout =
                    isLarge
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: DS.Space.s))
                    : AnyLayout(HStackLayout(spacing: DS.Space.s))
                layout {
                    HStack(spacing: DS.Space.s) {
                        TextField("Amount", text: $model.purchaseDraft.quantityText, prompt: Text("Amount"))
                            .keyboardType(.numbersAndPunctuation)
                            .autocorrectionDisabled()
                            .textFieldStyle(.roundedBorder)
                            .frame(maxWidth: isLarge ? .infinity : 90)
                            .focused($isAmountFocused)
                        Picker("Unit", selection: $model.purchaseDraft.unit) {
                            ForEach(PantryUnit.options(including: model.purchaseDraft.unit), id: \.self) { code in
                                Text(PantryUnit.pickerLabel(code)).tag(code)
                            }
                        }
                        .pickerStyle(.menu)
                        .fixedSize()
                    }
                    if !isLarge {
                        Spacer(minLength: 0)
                    }
                    HStack(spacing: DS.Space.s) {
                        Button("Skip") {
                            isAmountFocused = false
                            model.skipPurchase()
                        }
                        .buttonStyle(.dsSecondary(.small))
                        .accessibilityHint("Doesn't add it to the pantry.")
                        Button("Add") {
                            isAmountFocused = false
                            Task { await model.confirmPurchase() }
                        }
                        .buttonStyle(.dsPrimary(.small))
                        .disabled(!model.purchaseDraft.isValid)
                        .accessibilityHint("Adds this amount to the pantry.")
                    }
                }
                if let error = model.purchaseDraft.quantityError {
                    FormErrorLabel(message: error)
                        .font(.footnote)
                }
            }
        }
        .onAppear {
            AccessibilityNotification.Announcement(String(localized: "Add \(prompt.item.name) to pantry?")).post()
        }
    }

    private var optionsMenu: some View {
        Menu {
            if prompt.item.amounts.count > 1 {
                Section("Use Amount") {
                    ForEach(Array(prompt.item.amounts.enumerated()), id: \.offset) { _, amount in
                        Button(amount.text) {
                            model.purchaseDraft.use(amount)
                        }
                    }
                }
            }
            Button("Don't Ask During This Trip", systemImage: "bell.slash") {
                isAmountFocused = false
                model.stopAskingThisTrip()
            }
        } label: {
            Image(systemName: "ellipsis.circle")
                .imageScale(.large)
                .frame(minWidth: 44, minHeight: 44)
        }
        .accessibilityLabel("More Options")
    }
}

private struct BannerCard<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(DS.Space.ml)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial, in: .rect(cornerRadius: DS.Radius.large))
            .shadow(color: .black.opacity(0.12), radius: 8, y: 2)
            .padding(.horizontal)
            .padding(.bottom, DS.Space.s)
    }
}
