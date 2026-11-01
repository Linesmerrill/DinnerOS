import SwiftUI

/// The Shop tab's first run: what the handoff does, and saving the household's store (Walmart,
/// or Fry's when the API offers it).
struct ShopSetupView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var storeNumber = ""
    @State private var choice = ShopStoreChoice()
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var isRequestingStore = false

    private var canSave: Bool {
        guard shopping.canEdit, !isSaving else { return false }
        if choice.isKroger { return shopping.kroger != nil && choice.store != nil }
        return shopping.walmart != nil && ShoppingStoreNumber.error(storeNumber) == nil
    }

    private var storeName: String { choice.isKroger ? shopping.kroger?.name ?? "Fry's" : "Walmart" }

    var body: some View {
        Form {
            Section {
                VStack(spacing: 12) {
                    Image(systemName: "cart.badge.plus")
                        .font(.largeTitle)
                        .imageScale(.large)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text("Send this week's list to \(storeName)")
                        .font(.title2.bold())
                    Text(
                        "Choose a \(storeName) product for each ingredient once. Then send the week's list to your \(storeName) cart, check out there, and tell \(configuration.displayName) what you ordered so the pantry stays current."
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
            ShopStoreFields(storeNumber: $storeNumber, choice: $choice, isEditable: shopping.canEdit)
            Section {
                if shopping.canEdit {
                    Button(action: save) {
                        Group {
                            if isSaving {
                                ProgressView()
                            } else {
                                Text("Use \(storeName)")
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
            Section {
                RequestStoreRow { isRequestingStore = true }
            } footer: {
                Text(
                    shopping.kroger == nil
                        ? "Walmart is the only store we can send a list to today. Tell us where else you shop."
                        : "Tell us where else you shop.")
            }
        }
        .sheet(isPresented: $isRequestingStore) {
            // Already on the setup screen, so an available store just closes the sheet.
            RequestStoreSheet(openStoreSetup: {})
        }
        .task {
            await shopping.loadCatalog()
        }
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                if choice.isKroger, let store = choice.store {
                    try await shopping.saveStore(provider: ShoppingProviderKey.kroger, store: store)
                } else {
                    try await shopping.saveSettings(storeNumber: storeNumber)
                }
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// Which store the setup screens are choosing, and the Kroger store picked from the finder.
struct ShopStoreChoice: Equatable {
    var provider = ShoppingProviderKey.walmart
    var store: ShoppingStoreLocation?

    var isKroger: Bool { provider == ShoppingProviderKey.kroger }
}

/// The store sections shared by the first run and Store Settings: the store to use, then
/// Walmart's optional store number or Fry's sign-in and store finder.
struct ShopStoreFields: View {
    @Binding var storeNumber: String
    @Binding var choice: ShopStoreChoice
    let isEditable: Bool

    @Environment(ShoppingStore.self) private var shopping
    @Environment(\.appConfiguration) private var configuration

    /// Stores the member can pick between: Walmart, and the Kroger store when offered.
    private var choosable: [ShoppingProvider] { shopping.providers.filter(\.isSupported) }

    var body: some View {
        Section {
            ForEach(shopping.providers) { provider in
                let selected = provider.isSupported && provider.key == choice.provider
                Button {
                    guard provider.isSupported, choice.provider != provider.key else { return }
                    choice = ShopStoreChoice(provider: provider.key)
                } label: {
                    HStack {
                        Text(provider.name)
                            .foregroundStyle(Color.primary)
                        Spacer()
                        if selected {
                            Image(systemName: "checkmark")
                                .foregroundStyle(.tint)
                                .accessibilityHidden(true)
                        } else if !provider.isSupported {
                            Text("Coming Soon")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .disabled(!isEditable || !provider.isSupported || choosable.count < 2)
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
            if shopping.providers.isEmpty {
                Text("No stores are available right now. Try again later.")
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Store")
        } footer: {
            if !choice.isKroger, shopping.walmart?.affiliateTracked == true {
                Text("\(configuration.displayName) may earn a commission on Walmart purchases.")
            }
        }
        if choice.isKroger {
            KrogerStoreFields(store: $choice.store, isEditable: isEditable)
        } else {
            walmartStoreNumber
        }
    }

    @ViewBuilder
    private var walmartStoreNumber: some View {
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
    @State private var choice = ShopStoreChoice()
    @State private var didStart = false
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var hasChanges: Bool {
        if choice.provider != shopping.provider { return true }
        if choice.isKroger { return choice.store != nil && choice.store?.storeID != shopping.settings?.storeID }
        return ShoppingStoreNumber.normalized(storeNumber) != shopping.settings?.storeID
    }

    private var canSave: Bool {
        guard shopping.canEdit, hasChanges, !isSaving else { return false }
        if choice.isKroger { return choice.store != nil }
        return ShoppingStoreNumber.error(storeNumber) == nil
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                ShopStoreFields(storeNumber: $storeNumber, choice: $choice, isEditable: shopping.canEdit)
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
                choice = ShopStoreChoice(provider: shopping.provider)
                if !choice.isKroger {
                    storeNumber = shopping.settings?.storeID ?? ""
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
                if choice.isKroger, let store = choice.store {
                    try await shopping.saveStore(provider: ShoppingProviderKey.kroger, store: store)
                } else {
                    try await shopping.saveSettings(storeNumber: storeNumber)
                }
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// Fry's: the household's sign-in, and its store from the store finder.
struct KrogerStoreFields: View {
    @Binding var store: ShoppingStoreLocation?
    let isEditable: Bool

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var zip = ""
    @FocusState private var zipFocused: Bool
    @State private var stores: [ShoppingStoreLocation] = []
    @State private var isSearching = false
    @State private var errorMessage: String?
    @State private var signInService = StoreSignInService()

    private var name: String { shopping.kroger?.name ?? "Fry's" }
    private var isValidZip: Bool { zip.count == 5 && zip.allSatisfy { $0.isASCII && $0.isNumber } }

    var body: some View {
        Section {
            if shopping.isSignedIn {
                Label("Signed In", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.tint)
                if isEditable {
                    Button("Sign Out", role: .destructive) { run { try await shopping.signOut() } }
                }
            } else if isEditable {
                Button {
                    run { try await shopping.signIn(using: signInService) }
                } label: {
                    if shopping.isConnecting {
                        ProgressView()
                    } else {
                        Label("Sign In to \(name)", systemImage: "person.crop.circle")
                    }
                }
                .disabled(shopping.isConnecting)
            }
        } header: {
            Text("\(name) Account")
        } footer: {
            Text(
                "Sign in with your \(name) account so \(configuration.displayName) can add your list to your cart. \(configuration.displayName) never sees your password."
            )
        }
        Section {
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
            }
            if let current = store ?? currentStore {
                storeRow(current, selected: true)
            }
            HStack {
                TextField("ZIP Code", text: $zip, prompt: Text("ZIP code"))
                    .keyboardType(.numberPad)
                    .textContentType(.postalCode)
                    .focused($zipFocused)
                    .disabled(!isEditable)
                Button("Find Stores", action: find)
                    .disabled(!isEditable || !isValidZip || isSearching)
            }
            if isSearching {
                ProgressView()
            }
            ForEach(stores.filter { $0.storeID != (store ?? currentStore)?.storeID }) { location in
                Button {
                    store = location
                } label: {
                    storeRow(location, selected: false)
                }
                .disabled(!isEditable)
            }
        } header: {
            Text("Your \(name)")
        } footer: {
            Text("Your list goes to this store's cart for pickup or delivery.")
        }
    }

    /// The store already saved, before another is picked.
    private var currentStore: ShoppingStoreLocation? {
        guard shopping.settings?.provider == ShoppingProviderKey.kroger, let id = shopping.settings?.storeID else {
            return nil
        }
        return ShoppingStoreLocation(storeID: id, name: shopping.settings?.storeName ?? name, address: "")
    }

    private func storeRow(_ location: ShoppingStoreLocation, selected: Bool) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(location.name)
                    .foregroundStyle(Color.primary)
                if !location.address.isEmpty {
                    Text(location.address)
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                }
            }
            Spacer()
            if selected {
                Image(systemName: "checkmark")
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func find() {
        zipFocused = false
        Task {
            isSearching = true
            defer { isSearching = false }
            errorMessage = nil
            do {
                stores = try await shopping.findStores(zip: zip)
                if stores.isEmpty {
                    errorMessage = String(localized: "No \(name) stores near that ZIP code.")
                }
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }

    private func run(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            errorMessage = nil
            do {
                try await action()
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
