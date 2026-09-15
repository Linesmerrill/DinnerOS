import SwiftUI

/// The Shop tab's first run: what the handoff does, and saving Walmart as the household's store.
struct ShopSetupView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var storeNumber = ""
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var canSave: Bool {
        shopping.canEdit && shopping.walmart != nil && ShoppingStoreNumber.error(storeNumber) == nil && !isSaving
    }

    var body: some View {
        Form {
            Section {
                VStack(spacing: 12) {
                    Image(systemName: "cart.badge.plus")
                        .font(.largeTitle)
                        .imageScale(.large)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text("Send this week's list to Walmart")
                        .font(.title2.bold())
                    Text(
                        "Choose a Walmart product for each ingredient once. Then open the week's list in your Walmart cart, check out there, and tell \(configuration.displayName) what you ordered so the pantry stays current."
                    )
                    .foregroundStyle(.secondary)
                }
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)
                .accessibilityElement(children: .combine)
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            ShopStoreFields(storeNumber: $storeNumber, isEditable: shopping.canEdit)
            Section {
                if shopping.canEdit {
                    Button(action: save) {
                        Group {
                            if isSaving {
                                ProgressView()
                            } else {
                                Text("Use Walmart")
                            }
                        }
                        .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(!canSave)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
                } else {
                    Text("Ask a household admin or member to set up shopping.")
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                try await shopping.saveSettings(storeNumber: storeNumber)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// The store sections shared by the first run and Store Settings: providers and the
/// optional store number.
struct ShopStoreFields: View {
    @Binding var storeNumber: String
    let isEditable: Bool

    @Environment(ShoppingStore.self) private var shopping
    @Environment(\.appConfiguration) private var configuration

    var body: some View {
        Section {
            ForEach(shopping.providers) { provider in
                HStack {
                    Text(provider.name)
                    Spacer()
                    if provider.isSupported {
                        Image(systemName: "checkmark")
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                    } else {
                        Text("Coming Soon")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(provider.isSupported ? .isSelected : [])
            }
            if shopping.providers.isEmpty {
                Text("No stores are available right now. Try again later.")
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Store")
        } footer: {
            if shopping.walmart?.affiliateTracked == true {
                Text("\(configuration.displayName) may earn a commission on Walmart purchases.")
            }
        }
        Section {
            TextField("Store Number", text: $storeNumber, prompt: Text("Optional, for example 1234"))
                .keyboardType(.numberPad)
                .disabled(!isEditable)
            if let error = ShoppingStoreNumber.error(storeNumber) {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Store Number")
        } footer: {
            Text(
                "Optional. Cart links use this store for pickup and delivery. It's printed near the top of a Walmart receipt (ST# 1234) and is the number in your store's web address, walmart.com/store/1234. Leave it empty to let Walmart choose."
            )
        }
    }
}

/// Changes the household's store number.
struct ShopStoreSettingsSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var storeNumber = ""
    @State private var didStart = false
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var hasChanges: Bool {
        ShoppingStoreNumber.normalized(storeNumber) != shopping.settings?.storeID
    }

    private var canSave: Bool {
        shopping.canEdit && hasChanges && ShoppingStoreNumber.error(storeNumber) == nil && !isSaving
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                ShopStoreFields(storeNumber: $storeNumber, isEditable: shopping.canEdit)
            }
            .navigationTitle("Store Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save", action: save)
                        .disabled(!canSave)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
            .onAppear {
                guard !didStart else { return }
                didStart = true
                storeNumber = shopping.settings?.storeID ?? ""
            }
        }
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                try await shopping.saveSettings(storeNumber: storeNumber)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

#Preview("Store Settings") {
    let session = HouseholdPreviewData.session()
    ShopStoreSettingsSheet()
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}
