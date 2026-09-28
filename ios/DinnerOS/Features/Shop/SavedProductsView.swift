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
            if !groups.needsRechoosing.isEmpty {
                Section {
                    ForEach(groups.needsRechoosing) { preference in
                        row(preference)
                    }
                } header: {
                    Text("Needs Re-choosing (\(groups.needsRechoosing.count))")
                } footer: {
                    Text(
                        shopping.canEdit
                            ? "Walmart no longer lists these products, so they can't go in your cart. Tap one to choose the product Walmart sells now."
                            : "Walmart no longer lists these products, so they can't go in your cart."
                    )
                }
            }
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
                    if !groups.needsPackageSize.isEmpty || !groups.needsRechoosing.isEmpty {
                        Text("Ready")
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        if shopping.canEdit {
                            Text("Tap a product to change it, or swipe to remove it.")
                        }
                        Text(
                            "We check your saved products on Walmart about once a day and before each order. Stock is read at Walmart's default store, which may not be yours."
                        )
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
            .accessibilityHint(
                preference.needsRechoosing
                    ? "Re-chooses the product"
                    : preference.packageSize == nil ? "Adds its package size" : "Changes the product"
            )
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
                    .strikethrough(preference.needsRechoosing)
                if preference.needsRechoosing {
                    Label("Needs re-choosing: no longer on Walmart", systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.orange)
                } else if let size = preference.packageSize {
                    Text(priceText.map { "\(size.text) · \($0)" } ?? size.text)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    Label("No package size", systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.orange)
                }
                switch preference.health {
                case .unavailable?:
                    Label(ShoppingText.mayBeUnavailable, systemImage: "exclamationmark.circle")
                        .font(.footnote)
                        .foregroundStyle(Color.orange)
                case .unverified?:
                    Label("Couldn't confirm it's still on Walmart lately", systemImage: "questionmark.circle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                default:
                    EmptyView()
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

    /// "$0.85", marked when it's Walmart's listed price rather than one the household entered.
    private var priceText: String? {
        guard let cents = preference.priceCents else { return nil }
        let price = MoneyText.format(cents)
        return preference.priceSource == .provider ? String(localized: "\(price) on Walmart") : price
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
