import Foundation

/// The Choose Product form: a pasted link, the member's name for the product, and an
/// optional package size.
nonisolated struct SavedProductDraft: Equatable, Sendable {
    static let defaultUnit = "oz"

    var linkText = ""
    var displayName = ""
    var hasPackageSize = false
    var packageQuantityText = ""
    var packageUnit = SavedProductDraft.defaultUnit

    init() {}

    /// Starts from a line's current product, to change it.
    init(product: ShoppingLineProduct) {
        self.init(url: product.productURLString, name: product.displayName, size: product.packageSize)
    }

    init(preference: ShoppingPreference) {
        self.init(url: preference.productURLString, name: preference.displayName, size: preference.packageSize)
    }

    private init(url: String, name: String, size: ShoppingAmount?) {
        linkText = url
        displayName = name
        if let size {
            hasPackageSize = true
            packageQuantityText = PantryQuantity.editingText(size.quantity)
            packageUnit = size.unit
        }
    }

    /// The link or item ID found in `linkText`.
    var product: ShoppingProductReference? {
        ProductLink.reference(from: linkText)
    }

    /// The Walmart item the link names, for a confirmation under the field.
    var itemID: String? {
        switch product {
        case .itemID(let id): id
        case .url(let url): ProductLink.walmartItemID(inURL: url)
        case nil: nil
        }
    }

    var trimmedName: String {
        displayName.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var linkError: String? {
        guard !linkText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, product == nil else { return nil }
        return String(localized: "Paste a Walmart product link, such as walmart.com/ip/…, or the item number.")
    }

    var nameError: String? {
        trimmedName.count > ShoppingLimits.maxDisplayNameLength
            ? String(localized: "A product name can be at most \(ShoppingLimits.maxDisplayNameLength) characters.")
            : nil
    }

    var quantityError: String? {
        guard hasPackageSize else { return nil }
        do {
            _ = try PantryQuantity.parse(packageQuantityText)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    /// The package size to send, or `nil` when it's off or not a valid amount.
    private var packageSize: ShoppingPackageSizeInput? {
        guard hasPackageSize, let quantity = try? PantryQuantity.parse(packageQuantityText) else { return nil }
        return ShoppingPackageSizeInput(quantity: quantity, unit: packageUnit)
    }

    var isValid: Bool {
        product != nil && !trimmedName.isEmpty && nameError == nil && (!hasPackageSize || packageSize != nil)
    }

    /// The request body, or `nil` while the form isn't valid.
    func request(ingredientName: String?) -> ShoppingPreferenceRequest? {
        guard isValid, let product else { return nil }
        let name = String(
            (ingredientName ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                .prefix(ShoppingLimits.maxDisplayNameLength))
        return ShoppingPreferenceRequest(
            product: product, displayName: trimmedName, packageSize: packageSize,
            ingredientName: name.isEmpty ? nil : name)
    }
}

/// "Did you order these?": which handed-off lines were ordered, and how many packages.
nonisolated struct OrderConfirmationDraft: Equatable, Sendable {
    let handoffID: String
    /// Lines not confirmed yet, in aisle order.
    let lines: [ShoppingHandoffLine]
    private(set) var selected: Set<String>
    private var packageCounts: [String: Int]

    /// Every unconfirmed line starts checked, at the count sent to the cart.
    init(handoff: ShoppingHandoff) {
        handoffID = handoff.id
        lines = handoff.lines.filter { $0.confirmation?.status != .confirmed }
        selected = Set(lines.map(\.id))
        packageCounts = Dictionary(lines.map { ($0.id, $0.packages) }, uniquingKeysWith: { first, _ in first })
    }

    var selectedCount: Int { selected.count }

    func isSelected(_ line: ShoppingHandoffLine) -> Bool {
        selected.contains(line.id)
    }

    mutating func toggle(_ line: ShoppingHandoffLine) {
        if selected.contains(line.id) {
            selected.remove(line.id)
        } else {
            selected.insert(line.id)
        }
    }

    func packages(for line: ShoppingHandoffLine) -> Int {
        packageCounts[line.id] ?? line.packages
    }

    mutating func setPackages(_ count: Int, for line: ShoppingHandoffLine) {
        packageCounts[line.id] = min(max(count, ShoppingLimits.packages.lowerBound), ShoppingLimits.packages.upperBound)
    }

    /// "Ordered everything": `{all: true}`, unless a count changed or a line was skipped
    /// before, which `all` wouldn't cover; then every line is named.
    var everythingRequest: ConfirmShoppingOrderRequest {
        let isChanged = lines.contains { packages(for: $0) != $0.packages }
        let hasSkipped = lines.contains { $0.confirmation?.status == .skipped }
        guard isChanged || hasSkipped else { return .all }
        return .lines(lines.map(orderLine), skipRest: true)
    }

    /// Only the checked lines; the rest are marked not ordered. `nil` when nothing is checked.
    var selectedRequest: ConfirmShoppingOrderRequest? {
        let chosen = lines.filter { selected.contains($0.id) }
        guard !chosen.isEmpty else { return nil }
        return .lines(chosen.map(orderLine), skipRest: true)
    }

    private func orderLine(_ line: ShoppingHandoffLine) -> ConfirmedOrderLine {
        let count = packages(for: line)
        return ConfirmedOrderLine(lineID: line.id, packages: count == line.packages ? nil : count)
    }
}
