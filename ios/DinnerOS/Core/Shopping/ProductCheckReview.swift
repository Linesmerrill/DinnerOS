import Foundation

/// What the member decided about a line whose product is gone or couldn't be checked.
nonisolated enum ProductCheckDecision: String, Codable, Hashable, Sendable {
    /// Sent even though the check couldn't confirm it: the member saw it on Walmart themselves.
    /// Never allowed for a gone product.
    case sendAnyway = "send_anyway"
    /// Knowingly left out of this cart.
    case leaveOut = "leave_out"
}

/// One line's check, reported with a match or hand-off request (`productChecks`).
nonisolated struct ShoppingProductCheckReport: Encodable, Equatable, Sendable {
    let ingredientKey: String
    let productID: String
    let status: WalmartProductStatus
    let checkedAt: Date
    var name: String? = nil
    var priceCents: Int? = nil
    var decision: ProductCheckDecision? = nil

    private enum CodingKeys: String, CodingKey {
        case ingredientKey
        case productID = "productId"
        case status, checkedAt, name, priceCents, decision
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(ingredientKey, forKey: .ingredientKey)
        try container.encode(productID, forKey: .productID)
        try container.encode(status, forKey: .status)
        try container.encode(checkedAt, forKey: .checkedAt)
        try container.encodeIfPresent(name, forKey: .name)
        try container.encodeIfPresent(priceCents, forKey: .priceCents)
        try container.encodeIfPresent(decision, forKey: .decision)
    }
}

/// A line the cart would send, with its saved product and what the phone's check found.
nonisolated struct ProductCheckLine: Hashable, Sendable, Identifiable {
    let ingredientKey: String
    let name: String
    let productID: String
    let productName: String
    let productURL: URL?
    var result: WalmartProductResult

    var id: String { ingredientKey }
}

/// "Before Opening Walmart": the checks of the products a cart will send, and the member's
/// decisions. Pure; `ShoppingStore` holds one per shown week.
///
/// The rule it enforces (docs/shopping-providers.md#checking-saved-products): a gone product is
/// never sent, so it must be re-chosen or left out. Walmart stays closed until every gone line is
/// decided. Every saved product is one the household picked on Walmart, so a page the check
/// couldn't read (blocked, out of time, renumbered) goes in like a found one (decision 639).
/// Unavailable goes in with a note.
nonisolated struct ProductCheckReview: Hashable, Sendable {
    private(set) var lines: [ProductCheckLine]
    private(set) var decisions: [String: ProductCheckDecision] = [:]

    init(lines: [ProductCheckLine]) {
        self.lines = lines
    }

    /// The decision that counts for a line: Send Anyway on a gone product doesn't.
    func decision(for line: ProductCheckLine) -> ProductCheckDecision? {
        guard let decision = decisions[line.ingredientKey] else { return nil }
        switch (line.result.status, decision) {
        case (.gone, .leaveOut), (.unknown, _): return decision
        case (.gone, .sendAnyway): return nil
        case (.found, .leaveOut), (.unavailable, .leaveOut): return .leaveOut
        case (.found, .sendAnyway), (.unavailable, .sendAnyway): return nil
        }
    }

    /// Gone lines: never sent.
    var gone: [ProductCheckLine] { lines.filter { $0.result.status == .gone } }
    /// Lines the check couldn't read.
    var unknown: [ProductCheckLine] { lines.filter { $0.result.status == .unknown } }
    /// Lines that go in with "it may come back".
    var unavailable: [ProductCheckLine] {
        lines.filter { $0.result.status == .unavailable && decision(for: $0) == nil }
    }

    /// Gone lines without a decision. Walmart doesn't open until this is empty.
    var needsDecision: [ProductCheckLine] {
        lines.filter { $0.result.status == .gone && decision(for: $0) == nil }
    }

    var canOpen: Bool { needsDecision.isEmpty }

    /// Anything left to send once the decisions are applied.
    var sendsAnything: Bool {
        lines.contains { line in
            switch line.result.status {
            case .found, .unavailable: decision(for: line) != .leaveOut
            case .unknown: decision(for: line) != .leaveOut
            case .gone: false
            }
        }
    }

    /// Records a decision. Send Anyway on a gone product is refused and returns `false`.
    @discardableResult
    mutating func decide(_ decision: ProductCheckDecision, for ingredientKey: String) -> Bool {
        guard let line = lines.first(where: { $0.ingredientKey == ingredientKey }) else { return false }
        if decision == .sendAnyway, line.result.status != .unknown { return false }
        decisions[ingredientKey] = decision
        return true
    }

    /// Takes a decision back ("Put Back").
    mutating func undecide(_ ingredientKey: String) {
        decisions[ingredientKey] = nil
    }

    /// A new result for a line (Check Again). A changed status drops the old decision: it was
    /// made about a different answer.
    mutating func update(_ result: WalmartProductResult, for ingredientKey: String) {
        guard let index = lines.firstIndex(where: { $0.ingredientKey == ingredientKey }) else { return }
        if lines[index].result.status != result.status {
            decisions[ingredientKey] = nil
        }
        lines[index].result = result
    }

    /// Rebuilds the review for a new set of lines (a product was re-chosen, or the week changed):
    /// a line whose product is unchanged keeps its result and decision; a new product needs a
    /// result from `results`, or it's unknown.
    func replacingLines(_ newLines: [ProductCheckLine]) -> ProductCheckReview {
        var next = ProductCheckReview(lines: newLines)
        for line in newLines {
            guard
                let old = lines.first(where: {
                    $0.ingredientKey == line.ingredientKey && $0.productID == line.productID
                })
            else { continue }
            next.decisions[line.ingredientKey] = decisions[line.ingredientKey]
            if let index = next.lines.firstIndex(where: { $0.ingredientKey == line.ingredientKey }) {
                next.lines[index].result = old.result
            }
        }
        return next
    }

    /// Drops a line, for a product re-chosen elsewhere: the old check says nothing about it.
    mutating func remove(_ ingredientKey: String) {
        lines.removeAll { $0.ingredientKey == ingredientKey }
        decisions[ingredientKey] = nil
    }

    /// What the hand-off request reports: every line's result, with its decision.
    var reports: [ShoppingProductCheckReport] {
        lines.map { line in
            ShoppingProductCheckReport(
                ingredientKey: line.ingredientKey, productID: line.productID, status: line.result.status,
                checkedAt: line.result.checkedAt, name: line.result.name, priceCents: line.result.priceCents,
                decision: decision(for: line))
        }
    }
}
