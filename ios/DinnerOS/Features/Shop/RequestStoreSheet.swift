import SwiftUI

/// "Don't see your store?": the grocery stores near the household from Apple Maps, closest
/// first, then every store we know about. Picking one asks us to add that chain and remembers
/// the location; a store that already works opens store setup instead. Typing a name the
/// member can't find anywhere is the last resort.
struct RequestStoreSheet: View {
    /// Walmart and Kroger's stores are usable today, so their rows set up the store instead.
    let openStoreSetup: () -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var query = ""
    @State private var note = ""
    /// Rows with a request or undo in flight, so a row can't be tapped twice.
    @State private var working: Set<String> = []
    @State private var errorMessage: String?
    @State private var nearby: [NearbyGroceryStore] = []
    @State private var nearbyState = NearbyState.idle
    @State private var isChangingZIP = false
    @State private var zipFinder = StoreFinderState()
    @State private var locator = CoreLocationZIPLocator()
    @State private var mapSearch = MapKitNearbyGroceries()

    private enum NearbyState: Equatable {
        case idle, loading, loaded, failed
    }

    private var zip: String? { households.storeFinderZIP }

    /// The typed name, for the last-resort row.
    private var typedName: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Catalog stores matching the search that aren't already in the nearby list.
    private var catalogMatches: [ShoppingCatalogItem] {
        let shown = Set(nearby.compactMap { catalogItem(for: $0)?.key })
        return shopping.catalogWithRequests.filter { $0.matches(query) && !shown.contains($0.key) }
    }

    /// Before anyone types: what other households want most.
    private var mostRequested: [ShoppingCatalogItem] {
        shopping.catalogWithRequests
            .filter { $0.requests > 0 && $0.status != .available }
            .sorted { ($0.requests, $1.name) > ($1.requests, $0.name) }
            .prefix(5)
            .map { $0 }
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Request a Store")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Done") { dismiss() }
                    }
                }
                .searchable(
                    text: $query, placement: .navigationBarDrawer(displayMode: .always),
                    prompt: Text("Search stores")
                )
                .task {
                    await shopping.loadCatalog()
                }
                .task(id: NearbySearch(zip: zip, query: typedName)) {
                    await searchNearby()
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch shopping.catalogPhase {
        case .idle, .loading:
            ProgressView("Loading stores…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Stores", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await shopping.loadCatalog() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            if let errorMessage {
                FormErrorLabel(message: errorMessage)
            }
            nearbySection
            if typedName.isEmpty {
                if !mostRequested.isEmpty {
                    Section {
                        ForEach(mostRequested) { item in
                            row(item)
                        }
                    } header: {
                        Text("Most Requested")
                    } footer: {
                        Text("The stores other households asked for most.")
                    }
                }
            } else {
                if !catalogMatches.isEmpty {
                    Section(zip == nil ? "Stores" : "Other Stores") {
                        ForEach(catalogMatches) { item in
                            row(item)
                        }
                    }
                }
                customSection
            }
        }
    }

    // MARK: Nearby

    @ViewBuilder
    private var nearbySection: some View {
        if let zip, !isChangingZIP {
            Section {
                switch nearbyState {
                case .idle,
                    .loading where nearby.isEmpty:
                    ProgressView()
                        .frame(maxWidth: .infinity)
                case .failed where nearby.isEmpty:
                    Text("Couldn't load stores near \(zip). Check your connection.")
                        .foregroundStyle(.secondary)
                default:
                    if nearby.isEmpty {
                        Text(
                            typedName.isEmpty
                                ? "No grocery stores found near \(zip)."
                                : "No stores near \(zip) match “\(typedName)”."
                        )
                        .foregroundStyle(.secondary)
                    }
                    ForEach(nearby) { store in
                        nearbyRow(store)
                    }
                }
            } header: {
                HStack {
                    Text("Near \(zip)")
                    Spacer()
                    Button("Change") {
                        zipFinder.zip = zip
                        isChangingZIP = true
                    }
                    .font(.subheadline)
                    .textCase(nil)
                }
            } footer: {
                Text("Pick your store and we'll ask to add it. Closest first.")
            }
        } else if shopping.canRequestStore {
            Section {
                StoreFinderMessage(finder: zipFinder)
                StoreFinderZIPRow(
                    finder: zipFinder, isEditable: true, findTitle: "Show Stores",
                    find: saveZIP, locate: locateZIP)
            } header: {
                Text("Stores Near You")
            } footer: {
                Text("Enter your ZIP code to see grocery stores nearby.")
            }
        }
    }

    private func nearbyRow(_ store: NearbyGroceryStore) -> some View {
        let item = catalogItem(for: store)
        let request = existingRequest(for: store, item: item)
        return NearbyStoreRow(
            store: store,
            status: item?.status,
            isRequested: request != nil,
            isWorking: working.contains(store.id),
            canRequest: shopping.canRequestStore,
            choose: { choose(store, item: item) },
            undo: { if let request { undo(request, workingKey: store.id) } })
    }

    private func catalogItem(for store: NearbyGroceryStore) -> ShoppingCatalogItem? {
        NearbyGroceries.catalogItem(for: store.name, in: shopping.catalogWithRequests)
    }

    private func existingRequest(for store: NearbyGroceryStore, item: ShoppingCatalogItem?) -> ShoppingStoreRequest? {
        shopping.storeRequest(forKey: item?.key ?? NearbyGroceries.requestKey(for: store.name))
    }

    private func choose(_ store: NearbyGroceryStore, item: ShoppingCatalogItem?) {
        guard !working.contains(store.id) else { return }
        if item?.status == .available {
            dismiss()
            openStoreSetup()
            return
        }
        guard existingRequest(for: store, item: item) == nil else { return }
        run(key: store.id) { _ = try await shopping.requestStore(.nearby(store)) }
    }

    private func searchNearby() async {
        guard let zip else {
            nearby = []
            nearbyState = .idle
            return
        }
        // Wait for a pause in typing before asking Maps.
        if !typedName.isEmpty {
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
        }
        nearbyState = .loading
        do {
            let found = try await mapSearch.stores(near: zip, matching: typedName)
            guard !Task.isCancelled else { return }
            nearby = Array(found.prefix(25))
            nearbyState = .loaded
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled else { return }
            nearby = []
            nearbyState = .failed
        }
    }

    private func saveZIP() {
        let zip = zipFinder.zip
        guard ZIPCode.isValid(zip) else { return }
        Task {
            await households.rememberZIP(zip)
            isChangingZIP = false
        }
    }

    private func locateZIP() {
        Task {
            if await zipFinder.fillZIP(with: locator) { saveZIP() }
        }
    }

    // MARK: Last resort

    /// Nothing nearby or in the catalog is it: ask for exactly what was typed. It's saved as
    /// the member's own words, so we can match it to a store by hand later.
    private var customSection: some View {
        Section {
            Button {
                request(.name(typedName, note: note), key: typedName)
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: "plus.circle.fill")
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text("Can't find it? Add “\(typedName)”")
                        .foregroundStyle(Color.primary)
                    Spacer(minLength: 0)
                    if working.contains(typedName) {
                        ProgressView()
                    }
                }
                .contentShape(.rect)
            }
            .disabled(!shopping.canRequestStore || working.contains(typedName))
            TextField("Note", text: $note, prompt: Text("Anything we should know? (optional)"))
                .textInputAutocapitalization(.sentences)
                .onChange(of: note) { _, text in
                    if text.count > ShoppingRequestLimits.maxNoteLength {
                        note = String(text.prefix(ShoppingRequestLimits.maxNoteLength))
                    }
                }
        } footer: {
            Text("Only if your store isn't listed above.")
        }
    }

    // MARK: Catalog rows

    private func row(_ item: ShoppingCatalogItem) -> some View {
        CatalogRow(
            item: item,
            isWorking: working.contains(item.key),
            canRequest: shopping.canRequestStore,
            choose: { choose(item) },
            undo: {
                if let request = shopping.storeRequest(forKey: item.key) { undo(request, workingKey: item.key) }
            })
    }

    /// An available store needs no request: it opens store setup. Anything else is a request,
    /// and tapping a row that's already requested does nothing (Undo takes it back).
    private func choose(_ item: ShoppingCatalogItem) {
        guard !working.contains(item.key) else { return }
        if item.status == .available {
            dismiss()
            openStoreSetup()
            return
        }
        guard !item.requestedByHousehold else { return }
        request(.key(item.key), key: item.key)
    }

    private func undo(_ request: ShoppingStoreRequest, workingKey: String) {
        guard !working.contains(workingKey) else { return }
        run(key: workingKey) { try await shopping.undoStoreRequest(request) }
    }

    private func request(_ body: CreateShoppingStoreRequest, key: String) {
        run(key: key) {
            _ = try await shopping.requestStore(body)
            if body.source == "typed" {
                // Done; clear the search so the list reads normally again.
                query = ""
                note = ""
            }
        }
    }

    private func run(key: String, _ action: @escaping @MainActor () async throws -> Void) {
        working.insert(key)
        Task {
            defer { working.remove(key) }
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

/// What the nearby list is showing: a new ZIP code or search reloads it.
private struct NearbySearch: Equatable {
    let zip: String?
    let query: String
}

/// One store near the household: name, address, and how far away it is; or, once asked
/// for, Requested with Undo.
private struct NearbyStoreRow: View {
    let store: NearbyGroceryStore
    let status: ShoppingCatalogStatus?
    let isRequested: Bool
    let isWorking: Bool
    let canRequest: Bool
    let choose: () -> Void
    let undo: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Button(action: choose) {
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(store.name)
                        if !store.address.isEmpty {
                            Text(store.address)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        if status == .available {
                            StatusChip(text: "Available now", color: .green)
                        }
                    }
                    .foregroundStyle(Color.primary)
                    Spacer(minLength: 0)
                    if !isRequested, !isWorking, let distance = store.distanceText {
                        Text(distance)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(!canRequest || isWorking || isRequested)
            .accessibilityLabel(accessibilityLabel)
            .accessibilityHint(
                isRequested
                    ? "Already requested" : status == .available ? "Sets up this store" : "Asks us to add this store")
            if isWorking {
                ProgressView()
            } else if isRequested {
                VStack(alignment: .trailing, spacing: 2) {
                    Label("Requested", systemImage: "checkmark.circle.fill")
                        .font(.subheadline)
                        .foregroundStyle(.tint)
                    Button("Undo", action: undo)
                        .font(.footnote)
                        .buttonStyle(.plain)
                        .foregroundStyle(.tint)
                        .accessibilityLabel("Undo the request for \(store.name)")
                }
            }
        }
    }

    private var accessibilityLabel: String {
        var parts = [store.name]
        if !store.address.isEmpty { parts.append(store.address) }
        if let distance = store.distanceText { parts.append(String(localized: "\(distance) away")) }
        if status == .available { parts.append(String(localized: "Available now")) }
        if isRequested { parts.append(String(localized: "Requested")) }
        return parts.joined(separator: ", ")
    }
}

/// A small colored label such as "Available now".
private struct StatusChip: View {
    let text: LocalizedStringKey
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
    }
}

/// Opens the request sheet from the Shop tab and from store setup. It hides once the API has
/// answered that it has no catalog, so an older API shows nothing rather than a dead end.
struct RequestStoreRow: View {
    let open: () -> Void

    @Environment(ShoppingStore.self) private var shopping

    var body: some View {
        if shopping.isCatalogAvailable, shopping.canRequestStore {
            Button(action: open) {
                Label("Don't see your store?", systemImage: "questionmark.circle")
            }
            .accessibilityHint("Asks us to add a store")
        }
    }
}

/// One store: its name, a small kind icon, how far along it is, and whether this household
/// already asked.
private struct CatalogRow: View {
    let item: ShoppingCatalogItem
    let isWorking: Bool
    let canRequest: Bool
    let choose: () -> Void
    let undo: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Button(action: choose) {
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Image(systemName: item.kind.systemImage)
                        .foregroundStyle(.secondary)
                        .frame(width: 22)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(item.name)
                        statusChip
                        if let note = item.note {
                            Text(note)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        if let proof = item.socialProof {
                            Text(proof)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .foregroundStyle(Color.primary)
                    Spacer(minLength: 0)
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(!canRequest || isWorking || item.requestedByHousehold)
            .accessibilityLabel(accessibilityLabel)
            .accessibilityHint(accessibilityHint)
            trailing
        }
    }

    @ViewBuilder
    private var trailing: some View {
        if isWorking {
            ProgressView()
        } else if item.requestedByHousehold {
            VStack(alignment: .trailing, spacing: 2) {
                Label("Requested", systemImage: "checkmark.circle.fill")
                    .font(.subheadline)
                    .foregroundStyle(.tint)
                    .labelStyle(.titleAndIcon)
                Button("Undo", action: undo)
                    .font(.footnote)
                    .buttonStyle(.plain)
                    .foregroundStyle(.tint)
                    .accessibilityLabel("Undo the request for \(item.name)")
            }
        } else if item.status == .available {
            Image(systemName: "chevron.forward")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
    }

    /// "Available now" only for a store that works today; "Looking into it" while it's being
    /// researched. An unsupported or unknown status says nothing, because there's nothing to say.
    @ViewBuilder
    private var statusChip: some View {
        switch item.status {
        case .available:
            chip(text: "Available now", color: .green)
        case .researched:
            chip(text: "Looking into it", color: .orange)
        default:
            EmptyView()
        }
    }

    private func chip(text: LocalizedStringKey, color: Color) -> some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: .capsule)
    }

    private var accessibilityLabel: String {
        var parts = [item.name, item.kind.name]
        switch item.status {
        case .available: parts.append(String(localized: "Available now"))
        case .researched: parts.append(String(localized: "Looking into it"))
        default: break
        }
        if item.requestedByHousehold {
            parts.append(String(localized: "Requested"))
        }
        if let proof = item.socialProof {
            parts.append(proof)
        }
        return parts.joined(separator: ", ")
    }

    private var accessibilityHint: String {
        if item.requestedByHousehold {
            return String(localized: "Already requested")
        }
        return item.status == .available
            ? String(localized: "Sets up this store") : String(localized: "Asks us to add this store")
    }
}

#Preview("Empty") {
    let session = HouseholdPreviewData.session()
    RequestStoreSheet(openStoreSetup: {})
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}

#Preview("Searching") {
    let session = HouseholdPreviewData.session()
    RequestStoreSheet(openStoreSetup: {})
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}

#Preview("Requested") {
    let session = HouseholdPreviewData.session()
    RequestStoreSheet(openStoreSetup: {})
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session, requestedStoreKey: "kroger"))
}
