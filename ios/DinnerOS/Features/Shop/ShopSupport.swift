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

    /// The order day has arrived and nobody has marked the week, so the banner shows.
    static let orderReminder = OrderReminder(
        week: "2026-W38", orderDay: "thu", dueOn: "2026-09-17", due: true, remind: true, ordered: false,
        orderedBy: nil, orderedAt: nil)

    static let handoff = ShoppingHandoff(
        id: "handoff-preview", status: .open, createdBy: HouseholdPreviewData.user.id,
        createdAt: .now.addingTimeInterval(-3_600), updatedAt: .now, proposal: proposal)

    /// Sample stores behind "Don't see your store?". Request counts are made up.
    static let catalog: [ShoppingCatalogItem] = [
        ShoppingCatalogItem(
            key: ShoppingProviderKey.walmart, name: "Walmart", kind: .grocer, status: .available,
            aliases: ["wal mart", "wal-mart"]),
        ShoppingCatalogItem(
            key: "kroger", name: "Kroger", kind: .grocer, status: .researched, aliases: ["krogers"],
            note: "Its cart API needs a partner agreement.", requests: 3),
        ShoppingCatalogItem(
            key: "instacart", name: "Instacart", kind: .delivery, status: .researched, aliases: ["insta cart"],
            requests: 2),
        ShoppingCatalogItem(
            key: "frys", name: "Fry's Food Stores", kind: .grocer, status: .unsupported, aliases: ["frys", "fry"],
            requests: 1),
        ShoppingCatalogItem(key: "costco", name: "Costco", kind: .warehouse, status: .unsupported, requests: 4),
    ]

    /// A request this household already sent, so a row shows Requested with an Undo.
    static func request(key: String) -> ShoppingStoreRequest {
        ShoppingStoreRequest(
            id: "request-\(key)", key: key, name: catalog.first { $0.key == key }?.name ?? key,
            status: .researched, note: nil, requestedBy: HouseholdPreviewData.user.id,
            requestedAt: .now.addingTimeInterval(-7_200))
    }

    static func store(
        session: AuthSession, configured: Bool = true, requestedStoreKey: String? = nil
    ) -> ShoppingStore {
        .preview(
            session: session,
            settings: configured
                ? settings : ShoppingSettings(provider: nil, storeID: nil, updatedBy: nil, updatedAt: nil),
            providers: [walmart], proposal: configured ? proposal : nil, openHandoff: configured ? handoff : nil,
            catalog: catalog, storeRequests: requestedStoreKey.map { [request(key: $0)] } ?? [],
            orderReminder: configured ? orderReminder : nil)
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
            reason: reason, reasonText: reasonText, coverageText: coverage, coverage: "per_week", coversWeek: false,
            searchTerms: ShoppingSearchTerms(
                query: "fresh whole \(name)", qualifiers: ["fresh", "whole"], avoid: ["powder", "minced", "dried"],
                why: "Produce: the fresh whole item, not a dried, powdered or prepared form."),
            confirmation: ShoppingLineConfirmation(
                status: .pending, packages: nil, purchaseID: nil, confirmedAt: nil, skippedAt: nil))
    }

    private static func excluded(
        _ key: String, _ name: String, reason: ShoppingExclusionReason, text: String
    ) -> ShoppingExcludedLine {
        ShoppingExcludedLine(
            ingredientKey: key, ingredientID: nil, name: name, category: "bakery", amounts: [], quantityText: "6",
            unquantified: false, groceryStatus: .toBuy, reason: reason, text: text,
            searchTerms: ShoppingSearchTerms.plain(name))
    }
}
