import SwiftUI

/// Switches the tab shell to Shop and shows a week, for example from a grocery list.
struct OpenShopAction {
    let action: (ISOWeek) -> Void

    func callAsFunction(_ week: ISOWeek) {
        action(week)
    }
}

extension EnvironmentValues {
    /// `nil` outside the tab shell, where there's no Shop tab to switch to.
    @Entry var openShop: OpenShopAction? = nil
}

enum ShopErrors {
    /// The message to show for a failed shopping change. A `403` means the role changed
    /// elsewhere, so the household reloads and hidden controls match it again.
    static func message(for error: any Error, households: HouseholdStore) -> String {
        if (error as? APIError)?.status == 403 {
            Task { await households.load() }
        }
        return ShoppingStore.message(for: error)
    }
}

/// Synthetic shopping data for SwiftUI previews. Item IDs are made up. Not DEBUG-only because
/// `#Preview` bodies are type-checked in Release builds too.
enum ShopPreviewData {
    static let walmart = ShoppingProvider(
        key: ShoppingProviderKey.walmart, name: "Walmart", affiliateTracked: true,
        capabilities: ShoppingCapabilities(handoff: "cart_link", pasteProductLink: true, storeID: true))

    static let settings = ShoppingSettings(
        provider: ShoppingProviderKey.walmart, storeID: "1234", updatedBy: nil, updatedAt: nil)

    static let proposal = ShoppingProposal(
        provider: ShoppingProviderKey.walmart, week: "2026-W38", storeID: "1234",
        lines: [
            line(
                "l1", key: "beef", name: "Ground Beef", product: "Sample 80/20 ground beef", size: amount("16", "oz"),
                packages: 3, coverage: "3 × 16 oz covers 36 oz"),
            line(
                "l2", key: "garlic", name: "Garlic", product: "Sample garlic, 3 ct", size: amount("3", "count", "3 ct"),
                packages: 1, coverage: "1 × 3 ct", reason: .unitNotConvertible,
                reasonText: "Check amount: 4 cloves doesn't convert to a 3 ct package"),
        ],
        excluded: [
            excluded("name:flour tortillas", "Flour Tortillas", reason: .noProduct, text: "Choose a Walmart product"),
            excluded("salt", "Salt", reason: .pantryHint, text: "Probably at home"),
        ],
        cartLinks: [
            ShoppingCartLink(
                urlString: "https://www.walmart.com/sc/cart/addToCart?items=100000001_3,100000002",
                lineIDs: ["l1", "l2"], itemCount: 2)
        ],
        affiliateTracked: true)

    static let handoff = ShoppingHandoff(
        id: "handoff-preview", status: .open, createdBy: HouseholdPreviewData.user.id,
        createdAt: .now.addingTimeInterval(-3_600), updatedAt: .now, proposal: proposal)

    static func store(session: AuthSession, configured: Bool = true) -> ShoppingStore {
        .preview(
            session: session,
            settings: configured
                ? settings : ShoppingSettings(provider: nil, storeID: nil, updatedBy: nil, updatedAt: nil),
            providers: [walmart], proposal: configured ? proposal : nil, openHandoff: configured ? handoff : nil)
    }

    private static func amount(_ quantity: String, _ unit: String, _ text: String? = nil) -> ShoppingAmount {
        ShoppingAmount(
            quantity: quantity, quantityValue: Double(quantity) ?? 0, unit: unit, text: text ?? "\(quantity) \(unit)")
    }

    private static func line(
        _ id: String, key: String, name: String, product: String, size: ShoppingAmount?, packages: Int,
        coverage: String, reason: ShoppingCheckReason? = nil, reasonText: String? = nil
    ) -> ShoppingHandoffLine {
        ShoppingHandoffLine(
            id: id, ingredientKey: key, ingredientID: nil, name: name, category: "produce", amounts: [],
            quantityText: "", unquantified: false, groceryStatus: .toBuy,
            product: ShoppingLineProduct(
                productID: "100000001", displayName: product, productURLString: "https://www.walmart.com/ip/100000001",
                packageSize: size),
            computedPackages: packages, packages: packages, packagesOverridden: false, checkAmount: reason != nil,
            reason: reason, reasonText: reasonText, coverageText: coverage,
            confirmation: ShoppingLineConfirmation(
                status: .pending, packages: nil, purchaseID: nil, confirmedAt: nil, skippedAt: nil))
    }

    private static func excluded(
        _ key: String, _ name: String, reason: ShoppingExclusionReason, text: String
    ) -> ShoppingExcludedLine {
        ShoppingExcludedLine(
            ingredientKey: key, ingredientID: nil, name: name, category: "bakery", amounts: [], quantityText: "6",
            unquantified: false, groceryStatus: .toBuy, reason: reason, text: text)
    }
}
