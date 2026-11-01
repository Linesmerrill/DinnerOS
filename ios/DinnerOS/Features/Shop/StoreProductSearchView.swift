import SwiftUI

/// Choose products from a store DinnerOS can search (Fry's, through Kroger): the list items one
/// at a time, each searched in the store's own catalog at the household's store, with its price
/// and stock. Tapping a product saves it and moves on. Nothing is pasted.
struct StoreProductSearchView: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var chooser: WalmartChooser
    @State private var query = ""
    @State private var results: [ShoppingSearchProduct] = []
    @State private var phase: SearchPhase = .idle
    @State private var savingID: String?
    @State private var errorMessage: String?

    private enum SearchPhase: Equatable {
        case idle
        case searching
        case done
        case failed(String)
    }

    init(items: [WalmartChooser.Item]) {
        _chooser = State(initialValue: WalmartChooser(items: items))
    }

    var body: some View {
        NavigationStack {
            Group {
                if let item = chooser.current {
                    list(for: item)
                } else {
                    finished
                }
            }
            .navigationTitle("Choose in \(shopping.providerName)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
                if chooser.current != nil {
                    ToolbarItem(placement: .primaryAction) {
                        Text(chooser.remaining == 1 ? "1 left" : "\(chooser.remaining) left")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                }
            }
        }
        // Choosing one product (a re-choose) goes straight back once it's saved.
        .onChange(of: chooser.isFinished) { _, finished in
            if finished, chooser.items.count == 1, !chooser.chosen.isEmpty { dismiss() }
        }
        .task(id: chooser.current?.id) {
            guard let item = chooser.current else { return }
            query = item.searchQuery.isEmpty ? item.name : item.searchQuery
            await search()
        }
        .alert(
            "Couldn't Save That",
            isPresented: Binding(get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(errorMessage ?? "")
        }
    }

    private func list(for item: WalmartChooser.Item) -> some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.name)
                        .font(.headline)
                    if !item.amountText.isEmpty {
                        Text("Need \(item.amountText)")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                HStack(spacing: 12) {
                    Button("Back", systemImage: "chevron.left") { chooser.back() }
                        .disabled(!chooser.canGoBack || savingID != nil)
                    Spacer()
                    Button("Skip") { chooser.skip() }
                        .disabled(savingID != nil)
                }
                .buttonStyle(.borderless)
            }
            Section {
                switch phase {
                case .idle, .searching:
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Searching \(shopping.providerName)…")
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                case .failed(let message):
                    FormErrorLabel(message: message)
                    Button("Try Again") { Task { await search() } }
                case .done where results.isEmpty:
                    Text("Nothing found. Try other words.")
                        .foregroundStyle(.secondary)
                case .done:
                    ForEach(results) { product in
                        Button {
                            use(product, for: item)
                        } label: {
                            StoreProductRow(product: product, isSaving: savingID == product.productID)
                        }
                        .disabled(savingID != nil)
                    }
                }
            } footer: {
                if phase == .done, !results.isEmpty {
                    Text("Prices and stock are from your store and can change.")
                }
            }
        }
        .searchable(
            text: $query, placement: .navigationBarDrawer(displayMode: .always),
            prompt: "Search \(shopping.providerName)"
        )
        .onSubmit(of: .search) { Task { await search() } }
    }

    private var finished: some View {
        ContentUnavailableView {
            Label("All Set", systemImage: "checkmark.circle")
        } description: {
            Text(
                chooser.chosen.isEmpty
                    ? "No products chosen."
                    : chooser.chosen.count == 1
                        ? "Saved 1 product. It goes in the cart next time."
                        : "Saved \(chooser.chosen.count) products. They go in the cart next time."
            )
        } actions: {
            Button("Done") { dismiss() }
                .buttonStyle(.borderedProminent)
        }
    }

    private func search() async {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.count >= 3 else {
            results = []
            phase = .failed(String(localized: "Search for at least 3 letters."))
            return
        }
        phase = .searching
        do {
            let found = try await shopping.searchProducts(text)
            guard text == query.trimmingCharacters(in: .whitespacesAndNewlines) else { return }
            results = found
            phase = .done
        } catch is CancellationError {
            return
        } catch {
            phase = .failed(ShopErrors.message(for: error, households: households))
        }
    }

    private func use(_ product: ShoppingSearchProduct, for item: WalmartChooser.Item) {
        Task {
            savingID = product.productID
            defer { savingID = nil }
            do {
                _ = try await shopping.savePreference(
                    ingredientKey: item.ingredientKey, request: product.preferenceRequest(ingredientName: item.name))
                chooser.didChoose()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// One search result: photo, name, size, price, and stock.
struct StoreProductRow: View {
    let product: ShoppingSearchProduct
    let isSaving: Bool
    var hint: LocalizedStringKey = "Saves it for this item."

    var body: some View {
        HStack(spacing: 12) {
            AsyncImage(url: product.imageURL) { image in
                image.resizable().scaledToFit()
            } placeholder: {
                Image(systemName: "photo")
                    .foregroundStyle(.tertiary)
            }
            .frame(width: 56, height: 56)
            .background(Color.white, in: RoundedRectangle(cornerRadius: 8))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(product.name)
                    .font(.subheadline)
                    .foregroundStyle(Color.primary)
                    .multilineTextAlignment(.leading)
                Text(detail)
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
                if product.isOutOfStock {
                    Text("Out of stock at your store")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                } else if product.stock == "low" {
                    Text("Low stock")
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                }
            }
            Spacer(minLength: 0)
            if isSaving {
                ProgressView()
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
        .accessibilityHint(hint)
    }

    private var detail: String {
        var parts: [String] = []
        if let brand = product.brand, !brand.isEmpty { parts.append(brand) }
        if let size = product.size, !size.isEmpty { parts.append(size) }
        if let cents = product.priceCents {
            parts.append((Double(cents) / 100).formatted(.currency(code: "USD")))
        }
        return parts.joined(separator: " · ")
    }
}
