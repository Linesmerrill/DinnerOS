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

    private var storeName: String { choice.isKroger ? shopping.krogerName : "Walmart" }

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
                        VStack(alignment: .leading, spacing: 2) {
                            Text(provider.name)
                                .foregroundStyle(Color.primary)
                            if let summary = provider.bannerSummary {
                                Text(summary)
                                    .font(.footnote)
                                    .foregroundStyle(Color.secondary)
                            }
                        }
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

    private var walmartStoreNumber: some View {
        WalmartStoreNumberSection(storeNumber: $storeNumber, isEditable: isEditable, showsCommission: false)
    }
}

/// The household's stores: which one is the default, each store's details, adding another,
/// and removing one. Saved products stay with each store on the server.
struct ShopStoreSettingsSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var working: String?
    @State private var errorMessage: String?
    @State private var removing: ShoppingStoreChoice?

    /// Offered stores the household hasn't set up.
    private var addable: [ShoppingProvider] {
        shopping.providers.filter { provider in
            provider.isSupported && shopping.settings?.store(for: provider.key) == nil
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                Section {
                    ForEach(shopping.setUpStores) { store in
                        NavigationLink {
                            ShopStoreEditForm(provider: store.provider, isNew: false)
                        } label: {
                            storeRow(store)
                        }
                        .swipeActions {
                            if shopping.canEdit {
                                Button("Remove", role: .destructive) { removing = store }
                                if !store.isDefault {
                                    Button("Make Default") { makeDefault(store) }
                                        .tint(.accentColor)
                                }
                            }
                        }
                        .contextMenu {
                            if shopping.canEdit {
                                if !store.isDefault {
                                    Button("Make Default", systemImage: "star") { makeDefault(store) }
                                }
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = store }
                            }
                        }
                    }
                } header: {
                    Text("Your Stores")
                } footer: {
                    if shopping.canSwitchStores {
                        Text(
                            "Your shopping list goes to your default store. To use another store for one week, change it at the top of Shop."
                        )
                    }
                }
                if shopping.canEdit, !addable.isEmpty {
                    Section {
                        ForEach(addable) { provider in
                            NavigationLink {
                                ShopStoreEditForm(provider: provider.key, isNew: true)
                            } label: {
                                VStack(alignment: .leading, spacing: 2) {
                                    Label("Add \(provider.name)", systemImage: "plus")
                                    if let summary = provider.bannerSummary {
                                        Text(summary)
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    } footer: {
                        Text("Your other stores and their saved products stay as they are.")
                    }
                }
            }
            .navigationTitle("Store Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .disabled(working != nil)
            .confirmationDialog(
                "Remove \(removing.map { shopping.name(of: $0.provider) } ?? "")?",
                isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                titleVisibility: .visible, presenting: removing
            ) { store in
                Button("Remove", role: .destructive) { remove(store) }
            } message: { _ in
                Text("Its saved products are kept. Add the store again to use them.")
            }
        }
    }

    private func storeRow(_ store: ShoppingStoreChoice) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(shopping.name(of: store.provider))
                if let detail = detail(store) {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            if working == store.provider {
                ProgressView()
            } else if store.isDefault {
                Text("Default")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func detail(_ store: ShoppingStoreChoice) -> String? {
        if let name = store.storeName, !name.isEmpty { return name }
        if let id = store.storeID { return String(localized: "Store \(id)") }
        return nil
    }

    private func makeDefault(_ store: ShoppingStoreChoice) {
        run(store) { try await shopping.makeDefault(store.provider) }
    }

    private func remove(_ store: ShoppingStoreChoice) {
        run(store) { try await shopping.removeStore(store.provider) }
    }

    private func run(_ store: ShoppingStoreChoice, _ action: @escaping @MainActor () async throws -> Void) {
        Task {
            working = store.provider
            defer { working = nil }
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

/// Adds one store, or changes one already set up: Walmart's optional store number, or the
/// Kroger sign-in and store.
struct ShopStoreEditForm: View {
    let provider: String
    let isNew: Bool

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.dismiss) private var dismiss

    @State private var storeNumber = ""
    @State private var krogerStore: ShoppingStoreLocation?
    @State private var makeDefault = false
    @State private var didStart = false
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var isKroger: Bool { provider == ShoppingProviderKey.kroger }
    private var saved: ShoppingStoreChoice? { shopping.settings?.store(for: provider) }
    private var name: String { isKroger ? shopping.krogerName : "Walmart" }

    private var hasChanges: Bool {
        if isNew || makeDefault { return true }
        if isKroger { return krogerStore != nil && krogerStore?.storeID != saved?.storeID }
        return ShoppingStoreNumber.normalized(storeNumber) != saved?.storeID
    }

    private var canSave: Bool {
        guard shopping.canEdit, hasChanges, !isSaving else { return false }
        if isKroger { return krogerStore != nil || (!isNew && saved?.storeID != nil) }
        return ShoppingStoreNumber.error(storeNumber) == nil
    }

    var body: some View {
        Form {
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            if isKroger {
                KrogerStoreFields(
                    store: $krogerStore, isEditable: shopping.canEdit,
                    confirm: StoreConfirm(title: isNew ? "Add \(name)" : "Save", isEnabled: canSave, action: save))
            } else {
                walmartStoreNumber
            }
            if shopping.canEdit, saved?.isDefault != true, shopping.settings?.provider != nil {
                Section {
                    Toggle("Make Default", isOn: $makeDefault)
                } footer: {
                    Text("Your shopping list goes to this store unless you choose a different one for a week.")
                }
            }
        }
        .navigationTitle(isNew ? "Add \(name)" : name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(isNew ? "Add" : "Save", action: save)
                    .disabled(!canSave)
            }
        }
        .disabled(isSaving)
        .onAppear {
            guard !didStart else { return }
            didStart = true
            if !isKroger {
                storeNumber = saved?.storeID ?? ""
            }
        }
    }

    private var walmartStoreNumber: some View {
        WalmartStoreNumberSection(
            storeNumber: $storeNumber, isEditable: shopping.canEdit,
            showsCommission: shopping.walmart?.affiliateTracked == true)
    }

    private func save() {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                if isKroger {
                    if let krogerStore {
                        try await shopping.saveStore(provider: provider, store: krogerStore, makeDefault: makeDefault)
                    } else if makeDefault {
                        try await shopping.makeDefault(provider)
                    }
                } else {
                    try await shopping.saveSettings(storeNumber: storeNumber, makeDefault: makeDefault)
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

/// The button a store finder shows under the store just picked, so the next step is plain.
struct StoreConfirm {
    let title: String
    let isEnabled: Bool
    let action: () -> Void
}

/// Fry's: the household's sign-in, and its store from the store finder.
struct KrogerStoreFields: View {
    @Binding var store: ShoppingStoreLocation?
    let isEditable: Bool
    /// Shown under a newly picked store; `nil` when the screen's own button is right below.
    var confirm: StoreConfirm? = nil

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration

    @State private var finder = StoreFinderState()
    @State private var didStart = false
    @State private var signInError: String?
    @State private var signInService = StoreSignInService()
    @State private var locator = CoreLocationZIPLocator()

    private var name: String { shopping.krogerName }
    private var selected: ShoppingStoreLocation? { store ?? currentStore }
    private var pickedNew: Bool { store != nil && store?.storeID != currentStore?.storeID }

    var body: some View {
        Section {
            if let signInError {
                FormErrorLabel(message: signInError)
            }
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
            StoreFinderMessage(finder: finder)
            if let selected, !finder.isExpanded {
                Button {
                    withAnimation { finder.isExpanded = true }
                } label: {
                    StoreLocationRow(location: selected, isSelected: true, showsChange: isEditable)
                }
                .disabled(!isEditable)
                .accessibilityHint("Shows other stores to choose from")
                if pickedNew, let confirm {
                    Button(action: confirm.action) {
                        Text(confirm.title)
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(!confirm.isEnabled)
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                }
            } else {
                StoreFinderZIPRow(finder: finder, isEditable: isEditable, find: find, locate: locate)
                if finder.isBusy {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }
                if let selected, !finder.stores.contains(where: { $0.storeID == selected.storeID }) {
                    Button {
                        withAnimation { finder.didPick() }
                    } label: {
                        StoreLocationRow(location: selected, isSelected: true)
                    }
                    .disabled(!isEditable)
                }
                ForEach(finder.stores) { location in
                    Button {
                        store = location
                        withAnimation { finder.didPick() }
                    } label: {
                        StoreLocationRow(location: location, isSelected: location.storeID == selected?.storeID)
                    }
                    .disabled(!isEditable)
                }
            }
        } header: {
            Text("Your \(name)")
        } footer: {
            if finder.isExpanded, selected == nil || finder.hasSearched {
                Text("Pick the store you shop at. Your list goes to its cart for pickup or delivery.")
            } else {
                Text("Your list goes to this store's cart for pickup or delivery.")
            }
        }
        .onAppear {
            guard !didStart else { return }
            didStart = true
            finder.start(savedZIP: households.storeFinderZIP, hasStore: selected != nil)
            // Coming back to add a store: search the saved ZIP code straight away.
            if selected == nil, finder.isValidZIP, isEditable { find() }
        }
    }

    /// The store already saved, before another is picked.
    private var currentStore: ShoppingStoreLocation? {
        guard let saved = shopping.settings?.store(for: ShoppingProviderKey.kroger), let id = saved.storeID else {
            return nil
        }
        return ShoppingStoreLocation(storeID: id, name: saved.storeName ?? name, address: "")
    }

    private func find() {
        Task {
            await finder.find(
                using: { try await shopping.findStores(zip: $0) },
                remember: { await households.rememberZIP($0) },
                storeName: name,
                message: { ShopErrors.message(for: $0, households: households) })
        }
    }

    private func locate() {
        Task {
            if await finder.fillZIP(with: locator) { find() }
        }
    }

    private func run(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            signInError = nil
            do {
                try await action()
            } catch is CancellationError {
                return
            } catch {
                signInError = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// One store in a finder's list: name, address, and a checkmark on the picked one. Folded
/// down, it also says it can be tapped to change.
struct StoreLocationRow: View {
    let location: ShoppingStoreLocation
    let isSelected: Bool
    var showsChange = false

    var body: some View {
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
            if showsChange {
                Text("Change")
                    .font(.subheadline)
                    .foregroundStyle(.tint)
            }
            if isSelected {
                Image(systemName: "checkmark")
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
            }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}

/// A store finder's error, with a way to Settings when location is off.
struct StoreFinderMessage: View {
    let finder: StoreFinderState
    @Environment(\.openURL) private var openURL

    var body: some View {
        if let message = finder.errorMessage {
            FormErrorLabel(message: message)
            if finder.locationDenied, let url = URL(string: UIApplication.openSettingsURLString) {
                Button("Open Settings") { openURL(url) }
            }
        }
    }
}

/// The ZIP code field: type it or fill it in from where you are, then Find Stores. The number
/// pad has no Return key, so a bar above it has Find Stores and Done.
struct StoreFinderZIPRow: View {
    @Bindable var finder: StoreFinderState
    let isEditable: Bool
    var findTitle: LocalizedStringKey = "Find Stores"
    let find: () -> Void
    let locate: () -> Void

    @FocusState private var isFocused: Bool

    var body: some View {
        HStack(spacing: 12) {
            TextField("ZIP Code", text: $finder.zip, prompt: Text("ZIP code"))
                .keyboardType(.numberPad)
                .textContentType(.postalCode)
                .submitLabel(.search)
                .focused($isFocused)
                .onSubmit(search)
            Button(action: useLocation) {
                if finder.isLocating {
                    ProgressView()
                } else {
                    Image(systemName: "location")
                }
            }
            .buttonStyle(.borderless)
            .disabled(finder.isBusy)
            .accessibilityLabel("Use My Location")
            Button(findTitle, action: search)
                .buttonStyle(.borderless)
                .disabled(!finder.isValidZIP || finder.isBusy)
        }
        .disabled(!isEditable)
        .toolbar {
            if isFocused {
                ToolbarItemGroup(placement: .keyboard) {
                    Button(findTitle, action: search)
                        .disabled(!finder.isValidZIP)
                    Spacer()
                    Button("Done") { isFocused = false }
                }
            }
        }
    }

    private func search() {
        guard finder.isValidZIP else { return }
        isFocused = false
        find()
    }

    private func useLocation() {
        isFocused = false
        locate()
    }
}

/// Walmart's optional store number, and a way to look it up near a ZIP code on walmart.com.
/// Walmart doesn't let apps list its stores, so the number is typed in.
struct WalmartStoreNumberSection: View {
    @Binding var storeNumber: String
    let isEditable: Bool
    let showsCommission: Bool

    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.openURL) private var openURL

    @State private var finder = StoreFinderState()
    @State private var didStart = false
    @State private var locator = CoreLocationZIPLocator()

    var body: some View {
        Section {
            TextField("Store Number", text: $storeNumber, prompt: Text("Optional, for example 1234"))
                .keyboardType(.numberPad)
                .keyboardDoneButton()
                .disabled(!isEditable)
            if let error = ShoppingStoreNumber.error(storeNumber) {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Store Number")
        } footer: {
            VStack(alignment: .leading, spacing: 8) {
                Text(
                    "Optional. Your cart uses this store for pickup and delivery. Leave it empty to let Walmart choose."
                )
                if showsCommission {
                    Text("\(configuration.displayName) may earn a commission on Walmart purchases.")
                }
            }
        }
        if isEditable {
            Section {
                StoreFinderMessage(finder: finder)
                StoreFinderZIPRow(finder: finder, isEditable: isEditable, find: find, locate: locate)
            } header: {
                Text("Find Your Store Number")
            } footer: {
                Text(
                    "Opens Walmart's store finder. The number is on the store's page, in its web address (walmart.com/store/1234), and near the top of your receipt (ST# 1234)."
                )
            }
            .onAppear {
                guard !didStart else { return }
                didStart = true
                finder.start(savedZIP: households.storeFinderZIP, hasStore: false)
            }
        }
    }

    private func find() {
        guard let url = WalmartStoreFinder.url(zip: finder.zip) else { return }
        openURL(url)
        let zip = finder.zip
        Task { await households.rememberZIP(zip) }
    }

    private func locate() {
        Task {
            if await finder.fillZIP(with: locator) { find() }
        }
    }
}

/// walmart.com's store finder, opened at a ZIP code.
enum WalmartStoreFinder {
    static func url(zip: String) -> URL? {
        guard ZIPCode.isValid(zip) else { return nil }
        var components = URLComponents(string: "https://www.walmart.com/store-finder")
        components?.queryItems = [URLQueryItem(name: "location", value: zip)]
        return components?.url
    }
}

#Preview("Store Settings") {
    let session = HouseholdPreviewData.session()
    ShopStoreSettingsSheet()
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}
