import SwiftUI
import UIKit

/// The grocery line a product is being chosen or changed for.
struct ProductChoice: Identifiable {
    let ingredientKey: String
    let ingredientName: String
    let amountText: String?
    let draft: SavedProductDraft
    /// What to search Walmart for, from the API: an ingredient's bare name is the wrong
    /// search, because "garlic" ranks powder and snacks above the bulb.
    let searchTerms: ShoppingSearchTerms
    /// A product is already saved for the ingredient, so it can be removed.
    let isSaved: Bool
    /// Opened to fix a package size: the size comes first, switched on, with its field focused.
    var packageSizeFix: ShoppingPackageSizeFix?

    var id: String { ingredientKey }

    init(excluded line: ShoppingExcludedLine) {
        ingredientKey = line.ingredientKey
        ingredientName = line.name
        amountText = line.quantityText.isEmpty ? nil : line.quantityText
        draft = SavedProductDraft(ingredientName: line.name)
        searchTerms = line.searchTerms
        isSaved = false
    }

    init(line: ShoppingHandoffLine, packageSizeFix: ShoppingPackageSizeFix? = nil) {
        ingredientKey = line.ingredientKey
        ingredientName = line.name
        amountText = line.quantityText.isEmpty ? nil : line.quantityText
        var draft = SavedProductDraft(product: line.product)
        if packageSizeFix != nil {
            draft.hasPackageSize = true
        }
        self.draft = draft
        searchTerms = line.searchTerms
        isSaved = true
        self.packageSizeFix = packageSizeFix
    }

    init(preference: ShoppingPreference) {
        ingredientKey = preference.ingredientKey
        ingredientName = preference.ingredientName
        amountText = nil
        var draft = SavedProductDraft(preference: preference)
        // A product without a size is opened to add one.
        if preference.packageSize == nil {
            draft.hasPackageSize = true
            packageSizeFix = .add
        }
        self.draft = draft
        // Saved Products lists products, not this week's lines, so the API sends no terms.
        searchTerms = .plain(preference.ingredientName)
        isSaved = true
    }
}

/// Saves the Walmart product the household buys for an ingredient, from a link the member
/// copies in Walmart. Nothing here reads Walmart's pages: the search button is a plain link,
/// and the API takes the item ID from the pasted link.
struct ChooseProductSheet: View {
    let choice: ProductChoice

    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.dismiss) private var dismiss

    @State private var draft: SavedProductDraft
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var isConfirmingRemoval = false
    @FocusState private var isSizeFocused: Bool

    private var title: String {
        if let fix = choice.packageSizeFix {
            return fix.title
        }
        return choice.isSaved ? String(localized: "Change Product") : String(localized: "Choose Product")
    }

    init(choice: ProductChoice) {
        self.choice = choice
        _draft = State(initialValue: choice.draft)
    }

    var body: some View {
        NavigationStack {
            Form {
                if let errorMessage {
                    Section {
                        FormErrorLabel(message: errorMessage)
                    }
                }
                if choice.packageSizeFix != nil {
                    // Fixing a size: that's the whole job, so it comes first.
                    sizeSection
                    instructions
                    linkSection
                    nameSection
                } else {
                    instructions
                    linkSection
                    nameSection
                    sizeSection
                }
                priceSection
                if choice.isSaved {
                    Section {
                        Button("Remove Saved Product", role: .destructive) {
                            isConfirmingRemoval = true
                        }
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .task {
                // A line doesn't carry its product's price; the saved product does, when loaded.
                if choice.isSaved {
                    draft.startPrice(
                        shopping.preferences.first { $0.ingredientKey == choice.ingredientKey }?.priceCents)
                }
                if choice.packageSizeFix != nil {
                    isSizeFocused = true
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save", action: save)
                        .disabled(!draft.isValid || isSaving)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving)
            .confirmationDialog(
                Text("Remove the saved product for \(choice.ingredientName)?"),
                isPresented: $isConfirmingRemoval,
                titleVisibility: .visible
            ) {
                Button("Remove", role: .destructive, action: remove)
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("You'll be asked to choose a product for it again the next time it's on your list.")
            }
        }
    }

    /// The first few wrong forms, as "powder, minced, and dried".
    private var avoidHint: String {
        ListFormatter.localizedString(byJoining: Array(choice.searchTerms.avoid.prefix(3)))
    }

    private var instructions: some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                StepLabel(number: 1, text: "Open Walmart and find the product you buy.")
                StepLabel(number: 2, text: "Tap Share, then Copy Link.")
                StepLabel(number: 3, text: "Come back here and paste the link.")
            }
            .padding(.vertical, 4)
            if let url = ProductLink.walmartSearchURL(for: choice.searchTerms.query) {
                Link(destination: url) {
                    Label("Search on Walmart for “\(choice.searchTerms.query)”", systemImage: "magnifyingglass")
                }
            }
            if !choice.searchTerms.why.isEmpty {
                Text(choice.searchTerms.why)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if !choice.searchTerms.avoid.isEmpty {
                // The app can't filter these out — there's no product search API — so the
                // member is told what to skip.
                Text("Skip the \(avoidHint) versions.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text(choice.amountText.map { "\(choice.ingredientName), \($0)" } ?? choice.ingredientName)
        } footer: {
            Text("Opens the Walmart app or Safari. \(configuration.displayName) doesn't read Walmart's pages.")
        }
    }

    private var linkSection: some View {
        Section {
            TextField(
                "Product Link", text: $draft.linkText, prompt: Text("https://www.walmart.com/ip/…"), axis: .vertical
            )
            .lineLimit(1...4)
            .keyboardType(.URL)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            // A pasted link names the product, so the name fills itself in. Typing a link by
            // hand fills it too; an edited name is never overwritten.
            .onChange(of: draft.linkText) { draft.fillNameFromLink() }
            // A plain button, not PasteButton: Walmart's share sheet copies a URL, which a
            // String-only PasteButton treats as nothing to paste and shows disabled. Reading
            // the pasteboard here shows iOS's one-time "Allow Paste" prompt instead.
            Button("Paste", systemImage: "doc.on.clipboard") {
                let board = UIPasteboard.general
                paste([board.url?.absoluteString, board.string].compactMap { $0 })
            }
            if let error = draft.linkError {
                FormErrorLabel(message: error)
            } else if let itemID = draft.itemID {
                Label("Walmart item \(itemID)", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Product Link")
        } footer: {
            Text("Pasting the whole shared message works too.")
        }
    }

    private var nameSection: some View {
        Section {
            TextField(
                "Product Name", text: $draft.displayName, prompt: Text("For example, Great Value 80/20 ground beef")
            )
            .textInputAutocapitalization(.sentences)
            if let error = draft.nameError {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Product Name")
        } footer: {
            Text(
                "Filled in for you. Change it to note the brand or size; everyone in your household sees this name."
            )
        }
    }

    private var sizeSection: some View {
        Section {
            Toggle("Package Size", isOn: $draft.hasPackageSize.animation())
            if draft.hasPackageSize {
                TextField("Amount", text: $draft.packageQuantityText, prompt: Text("For example, 16"))
                    .keyboardType(.numbersAndPunctuation)
                    .autocorrectionDisabled()
                    .focused($isSizeFocused)
                Picker("Unit", selection: $draft.packageUnit) {
                    ForEach(PantryUnit.options(including: draft.packageUnit), id: \.self) { code in
                        Text(PantryUnit.pickerLabel(code)).tag(code)
                    }
                }
                if let error = draft.quantityError {
                    FormErrorLabel(message: error)
                }
            }
        } header: {
            // First in the form when fixing a size, so it names what's being fixed.
            if choice.packageSizeFix != nil {
                Text(choice.amountText.map { "\(choice.ingredientName), \($0)" } ?? choice.ingredientName)
            }
        } footer: {
            Text(
                "How much one package holds, from the product page, so we know how many to buy. Fresh food works without it: we buy 1 for the week."
            )
        }
    }

    private var priceSection: some View {
        Section {
            TextField("Price", text: $draft.priceText, prompt: Text("Optional, for example 4.98"))
                .keyboardType(.decimalPad)
                .autocorrectionDisabled()
            if let error = draft.priceError {
                FormErrorLabel(message: error)
            }
        } header: {
            Text("Price per Package")
        } footer: {
            Text("What one package costs at Walmart. It's used to work out your cost per meal.")
        }
    }

    private func paste(_ strings: [String]) {
        guard let text = strings.first else { return }
        draft.linkText = ProductLink.firstURL(in: text) ?? text.trimmingCharacters(in: .whitespacesAndNewlines)
        draft.fillNameFromLink()
    }

    private func save() {
        guard let request = draft.request(ingredientName: choice.ingredientName) else { return }
        let key = choice.ingredientKey
        run { try await shopping.savePreference(ingredientKey: key, request: request) }
    }

    private func remove() {
        let key = choice.ingredientKey
        run { try await shopping.deletePreference(ingredientKey: key) }
    }

    private func run(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                try await action()
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

private struct StepLabel: View {
    let number: Int
    let text: LocalizedStringKey

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text("\(number).")
                .font(.body.monospacedDigit().weight(.semibold))
                .foregroundStyle(.tint)
            Text(text)
        }
        .accessibilityElement(children: .combine)
    }
}

#Preview("Choose") {
    let session = HouseholdPreviewData.session()
    ChooseProductSheet(
        choice: ProductChoice(excluded: ShopPreviewData.proposal.needsProduct[0])
    )
    .environment(HouseholdPreviewData.store(session: session))
    .environment(ShopPreviewData.store(session: session))
}

#Preview("Change") {
    let session = HouseholdPreviewData.session()
    ChooseProductSheet(choice: ProductChoice(line: ShopPreviewData.proposal.lines[0]))
        .environment(HouseholdPreviewData.store(session: session))
        .environment(ShopPreviewData.store(session: session))
}
