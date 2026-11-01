import SwiftUI

/// Starts Fry's from the products the household already buys at Walmart: a Fry's product
/// picked for each, to keep, swap, or skip, then save together.
struct MatchProductsSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var model = MatchProductsModel()

    private var storeName: String { shopping.krogerName }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(
                        "We picked a \(storeName) product for each item you buy at Walmart. Uncheck any you don't want, or tap one to choose another."
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                }
                if model.items.isEmpty {
                    emptyState
                }
                ForEach(model.items) { item in
                    Section {
                        MatchSuggestionRow(item: item, model: model, storeName: storeName)
                    }
                }
                if model.isLoadingMore {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Finding more… \(model.items.count) of \(model.total)")
                            .foregroundStyle(.secondary)
                    }
                }
                if case .failed(let message) = model.phase, !model.items.isEmpty {
                    Section {
                        FormErrorLabel(message: message)
                        Button("Try Again") { Task { await load() } }
                    }
                }
            }
            .navigationTitle("Match Your Products")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .disabled(model.isSaving)
                }
            }
            .safeAreaInset(edge: .bottom) {
                saveBar
            }
            .task { await load() }
        }
        .interactiveDismissDisabled(model.isSaving)
    }

    @ViewBuilder
    private var emptyState: some View {
        switch model.phase {
        case .loading:
            HStack(spacing: 8) {
                ProgressView()
                Text("Finding \(storeName) products…")
                    .foregroundStyle(.secondary)
            }
        case .failed(let message):
            VStack(alignment: .leading, spacing: 8) {
                FormErrorLabel(message: message)
                Button("Try Again") { Task { await load() } }
            }
        case .loaded:
            ContentUnavailableView(
                "All Matched", systemImage: "checkmark.circle",
                description: Text("Every item you buy at Walmart has a \(storeName) product."))
        }
    }

    private var saveBar: some View {
        VStack(spacing: 6) {
            if let error = model.saveError {
                FormErrorLabel(message: error)
            }
            Button(action: save) {
                Group {
                    if model.isSaving {
                        HStack(spacing: 8) {
                            ProgressView()
                            Text("Saving… \(model.savedCount)")
                        }
                    } else {
                        Text(
                            model.selectedCount == 1
                                ? "Save 1 Product" : "Save \(model.selectedCount) Products")
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(model.selectedCount == 0 || model.isSaving)
        }
        .padding()
        .background(.bar)
    }

    private func load() async {
        await model.loadAll(
            fetch: { try await shopping.krogerSuggestions(after: $0) },
            message: { ShopErrors.message(for: $0, households: households) })
    }

    private func save() {
        Task {
            let done = await model.save(
                using: { item, product in
                    try await shopping.savePreference(
                        ingredientKey: item.ingredientKey,
                        request: product.preferenceRequest(ingredientName: item.ingredientName),
                        provider: ShoppingProviderKey.kroger, rematch: false)
                },
                message: { ShopErrors.message(for: $0, households: households) })
            await shopping.rematch()
            if done { dismiss() }
        }
    }
}

/// One item: what the household buys at Walmart, and the Fry's product picked for it.
private struct MatchSuggestionRow: View {
    let item: ShoppingMatchSuggestion
    let model: MatchProductsModel
    let storeName: String

    var body: some View {
        let chosen = model.product(for: item)
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.ingredientName)
                        .font(.headline)
                    Text("Walmart: \(item.from.displayName)")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                Spacer()
                if !model.options(for: item).isEmpty {
                    Button {
                        model.toggle(item)
                    } label: {
                        Image(systemName: chosen == nil ? "circle" : "checkmark.circle.fill")
                            .font(.title2)
                            .foregroundStyle(chosen == nil ? Color.secondary : Color.accentColor)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(chosen == nil ? "Save \(item.ingredientName)" : "Skip \(item.ingredientName)")
                }
            }
            if let shown = chosen ?? item.product {
                Menu {
                    ForEach(model.options(for: item)) { option in
                        Button {
                            model.choose(option, for: item)
                        } label: {
                            Label(
                                optionTitle(option),
                                systemImage: option.productID == chosen?.productID ? "checkmark" : "")
                        }
                        .disabled(option.isOutOfStock)
                    }
                    Divider()
                    Button("Skip This Item", role: .destructive) { model.choose(nil, for: item) }
                } label: {
                    StoreProductRow(product: shown, isSaving: false, hint: "Shows other choices.")
                        .opacity(chosen == nil ? 0.45 : 1)
                }
            } else {
                Text("No clear match at \(storeName). Choose it from your list when you shop.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if !model.options(for: item).isEmpty {
                    Menu("See Other Choices") {
                        ForEach(model.options(for: item)) { option in
                            Button(optionTitle(option)) { model.choose(option, for: item) }
                                .disabled(option.isOutOfStock)
                        }
                    }
                    .font(.footnote)
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func optionTitle(_ product: ShoppingSearchProduct) -> String {
        var parts = [product.name]
        if let size = product.size, !size.isEmpty { parts.append(size) }
        if let cents = product.priceCents { parts.append((Double(cents) / 100).formatted(.currency(code: "USD"))) }
        if product.isOutOfStock { parts.append(String(localized: "Out of stock")) }
        return parts.joined(separator: " · ")
    }
}
