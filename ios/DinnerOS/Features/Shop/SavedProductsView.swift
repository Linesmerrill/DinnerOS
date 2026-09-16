import SwiftUI

/// The household's saved Walmart products, to change or remove.
struct SavedProductsView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households

    @State private var editing: ProductChoice?
    @State private var actionError: String?

    var body: some View {
        content
            .navigationTitle("Saved Products")
            .navigationBarTitleDisplayMode(.inline)
            .task {
                await shopping.loadPreferences()
            }
            .sheet(item: $editing) { choice in
                ChooseProductSheet(choice: choice)
            }
            .alert("Couldn't Remove the Product", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        switch shopping.preferencesPhase {
        case .idle, .loading:
            ProgressView("Loading saved products…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load Saved Products", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await shopping.loadPreferences() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            if shopping.preferences.isEmpty {
                ContentUnavailableView(
                    "No Saved Products", systemImage: "bookmark",
                    description: Text(
                        "Choose a product for an ingredient on the Shop tab, and it's saved here for every week.")
                )
            } else {
                list
            }
        }
    }

    private var list: some View {
        List {
            if let refreshError = shopping.preferencesRefreshError {
                FormErrorLabel(message: refreshError)
            }
            let groups = SavedProductGroups(shopping.preferences)
            if !groups.needsPackageSize.isEmpty {
                Section {
                    ForEach(groups.needsPackageSize) { preference in
                        row(preference)
                    }
                } header: {
                    Text("Needs Package Size (\(groups.needsPackageSize.count))")
                } footer: {
                    Text(
                        shopping.canEdit
                            ? "Tap one to add its size so we know how many to buy. Fresh food works without one: we buy 1 for the week."
                            : "Without a size we buy 1. Fresh food works without one."
                    )
                }
            }
            if !groups.complete.isEmpty {
                Section {
                    ForEach(groups.complete) { preference in
                        row(preference)
                    }
                } header: {
                    if !groups.needsPackageSize.isEmpty {
                        Text("Ready")
                    }
                } footer: {
                    if shopping.canEdit {
                        Text("Tap a product to change it, or swipe to remove it.")
                    }
                }
            }
        }
        .refreshable {
            await shopping.loadPreferences()
        }
    }

    @ViewBuilder
    private func row(_ preference: ShoppingPreference) -> some View {
        if shopping.canEdit {
            Button {
                editing = ProductChoice(preference: preference)
            } label: {
                SavedProductRow(preference: preference)
            }
            .accessibilityHint(preference.packageSize == nil ? "Adds its package size" : "Changes the product")
            .swipeActions(edge: .trailing) {
                Button("Remove", systemImage: "trash", role: .destructive) {
                    remove(preference)
                }
            }
        } else {
            SavedProductRow(preference: preference)
        }
    }

    private func remove(_ preference: ShoppingPreference) {
        Task {
            do {
                try await shopping.deletePreference(ingredientKey: preference.ingredientKey)
            } catch is CancellationError {
                return
            } catch {
                actionError = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

private struct SavedProductRow: View {
    let preference: ShoppingPreference

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(preference.ingredientName)
                Text(preference.displayName)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if let size = preference.packageSize {
                    Text(size.text)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    Label("No package size", systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.orange)
                }
            }
            Spacer(minLength: 0)
        }
        // Inside a list Button, hierarchical styles resolve against the tint; anchoring them to
        // the primary color keeps the details gray.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        SavedProductsView()
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(ShopPreviewData.store(session: session))
}
