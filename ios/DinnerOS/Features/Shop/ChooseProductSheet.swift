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

    var id: String { ingredientKey }

    init(excluded line: ShoppingExcludedLine) {
        ingredientKey = line.ingredientKey
        ingredientName = line.name
        amountText = line.quantityText.isEmpty ? nil : line.quantityText
        draft = SavedProductDraft(ingredientName: line.name)
        searchTerms = line.searchTerms
        isSaved = false
    }

    init(line: ShoppingHandoffLine) {
        ingredientKey = line.ingredientKey
        ingredientName = line.name
        amountText = line.quantityText.isEmpty ? nil : line.quantityText
        draft = SavedProductDraft(product: line.product)
        searchTerms = line.searchTerms
        isSaved = true
    }

    init(preference: ShoppingPreference) {
        ingredientKey = preference.ingredientKey
        ingredientName = preference.ingredientName
        amountText = nil
        draft = SavedProductDraft(preference: preference)
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
                instructions
                linkSection
                nameSection
                sizeSection
                if choice.isSaved {
                    Section {
                        Button("Remove Saved Product", role: .destructive) {
                            isConfirmingRemoval = true
                        }
                    }
                }
            }
            .navigationTitle(choice.isSaved ? "Change Product" : "Choose Product")
            .navigationBarTitleDisplayMode(.inline)
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
                Text("It goes back to Needs a Product until someone chooses another.")
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
            Text("Pasting the whole shared message works too; the link is picked out of it.")
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
                "Starts as the ingredient's name, so you usually don't type anything. Edit it if you want the brand or size; it's shown on the Shop tab so everyone knows what's being bought."
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
                Picker("Unit", selection: $draft.packageUnit) {
                    ForEach(PantryUnit.options(including: draft.packageUnit), id: \.self) { code in
                        Text(PantryUnit.pickerLabel(code)).tag(code)
                    }
                }
                if let error = draft.quantityError {
                    FormErrorLabel(message: error)
                }
            }
        } footer: {
            Text(
                "How much one package holds, from the product page. It sets how many packages to add. Without it, 1 package is added and the line is flagged to check."
            )
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
