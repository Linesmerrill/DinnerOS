import SwiftUI

/// "Don't see your store?": search every store DinnerOS knows about, ask for one, and take
/// the request back. Demand is what decides which store gets built next, so a request is the
/// point of this screen — except for a store that already works, which opens store setup.
struct RequestStoreSheet: View {
    /// Walmart is usable today, so its row sets the household's store instead of asking.
    let openStoreSetup: () -> Void

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var query = ""
    @State private var note = ""
    /// Catalog keys with a request or undo in flight, so a row can't be tapped twice.
    @State private var working: Set<String> = []
    @State private var errorMessage: String?

    /// The typed name, for the "Request '…'" row.
    private var typedName: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var matches: [ShoppingCatalogItem] {
        shopping.catalogWithRequests.filter { $0.matches(query) }
    }

    /// Before anyone types: what other households want most, then what's being looked into.
    private var mostRequested: [ShoppingCatalogItem] {
        shopping.catalogWithRequests
            .filter { $0.requests > 0 && $0.status != .available }
            .sorted { ($0.requests, $1.name) > ($1.requests, $0.name) }
            .prefix(5)
            .map { $0 }
    }

    private var researched: [ShoppingCatalogItem] {
        shopping.catalogWithRequests.filter { $0.status == .researched }
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
                .searchable(text: $query, prompt: Text("Search stores"))
                .task {
                    await shopping.loadCatalog()
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
            if typedName.isEmpty {
                emptyStateSections
            } else if matches.isEmpty {
                freeTextSection
            } else {
                Section {
                    ForEach(matches) { item in
                        row(item)
                    }
                } footer: {
                    Text("Asking tells us where to go next. We'll add the most-asked-for stores first.")
                }
            }
        }
    }

    @ViewBuilder
    private var emptyStateSections: some View {
        if mostRequested.isEmpty && researched.isEmpty {
            ContentUnavailableView(
                "Search for Your Store", systemImage: "magnifyingglass",
                description: Text("Type a store's name to ask us to add it.")
            )
            .listRowBackground(Color.clear)
        }
        if !mostRequested.isEmpty {
            Section {
                ForEach(mostRequested) { item in
                    row(item)
                }
            } header: {
                Text("Most Requested")
            } footer: {
                Text("What other households asked for most.")
            }
        }
        if !researched.isEmpty {
            Section("Looking Into It") {
                ForEach(researched) { item in
                    row(item)
                }
            }
        }
    }

    /// Nothing matched, so the member can ask for exactly what they typed.
    private var freeTextSection: some View {
        Section {
            Button {
                request(.name(typedName, note: note))
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: "plus.circle.fill")
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    Text("Request “\(typedName)”")
                        .foregroundStyle(Color.primary)
                    Spacer(minLength: 0)
                    if working.contains(typedName) {
                        ProgressView()
                    }
                }
                .contentShape(.rect)
            }
            .disabled(!shopping.canRequestStore || working.contains(typedName))
            TextField("Note", text: $note, prompt: Text("What would help?"))
                .textInputAutocapitalization(.sentences)
                .onChange(of: note) { _, text in
                    if text.count > ShoppingRequestLimits.maxNoteLength {
                        note = String(text.prefix(ShoppingRequestLimits.maxNoteLength))
                    }
                }
        } header: {
            Text("No Match")
        } footer: {
            Text("Optional. One line about how you'd shop there helps us pick what to build.")
        }
    }

    private func row(_ item: ShoppingCatalogItem) -> some View {
        CatalogRow(
            item: item,
            isWorking: working.contains(item.key),
            canRequest: shopping.canRequestStore,
            choose: { choose(item) },
            undo: { undo(item) })
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

    private func undo(_ item: ShoppingCatalogItem) {
        guard let request = shopping.storeRequest(forKey: item.key), !working.contains(item.key) else { return }
        run(key: item.key) { try await shopping.undoStoreRequest(request) }
    }

    private func request(_ body: CreateShoppingStoreRequest, key: String? = nil) {
        let workingKey = key ?? typedName
        run(key: workingKey) {
            _ = try await shopping.requestStore(body)
            if key == nil {
                // A free-text request is done; clear the search so the list reads normally again.
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
