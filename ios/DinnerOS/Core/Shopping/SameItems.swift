import Foundation
import Observation
import os

/// Another name an item on the list goes by, because the household said they're the same thing
/// to buy (decision 661): "Chipotle Powder" on the "Chipotle Chili Powder" line.
nonisolated struct SameItemName: Decodable, Hashable, Sendable {
    /// The link that made them one item, to undo it.
    let linkID: String
    let name: String

    private enum CodingKeys: String, CodingKey {
        case linkID = "linkId"
        case name
    }
}

/// A request to buy one item as another: `variant` becomes `kept` on every list.
nonisolated struct SameItemRequest: Encodable, Hashable, Sendable {
    let variantKey: String
    let variantName: String
    let keptKey: String
    let keptName: String
}

/// A stored link (`/same-items`).
nonisolated struct SameItemLink: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let variantKey: String
    let variantName: String
    let keptKey: String
    let keptName: String
}

/// Something on this week's list, or in the pantry, that another item can be the same as.
nonisolated struct SameItemChoice: Hashable, Sendable, Identifiable {
    let key: String
    let name: String
    /// Where it was found, shown under the name: "On this week's list" or "In your pantry".
    let detail: String

    var id: String { key }

    /// What an item can be linked to: this week's other items first, then pantry items, each
    /// name once, never the item itself or a name it already goes by.
    static func choices(
        for key: String, name: String, alsoCalled: [String] = [],
        listItems: [(key: String, name: String)], pantryItems: [(key: String, name: String)]
    ) -> [SameItemChoice] {
        var seen: Set<String> = [key]
        var names: Set<String> = Set(([name] + alsoCalled).map(Self.folded))
        var out: [SameItemChoice] = []
        func add(_ items: [(key: String, name: String)], detail: String) {
            let sorted = items.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
            for item in sorted where !item.key.isEmpty && !item.name.isEmpty {
                let folded = Self.folded(item.name)
                guard !seen.contains(item.key), !names.contains(folded) else { continue }
                seen.insert(item.key)
                names.insert(folded)
                out.append(SameItemChoice(key: item.key, name: item.name, detail: detail))
            }
        }
        add(listItems, detail: String(localized: "On this week's list"))
        add(pantryItems, detail: String(localized: "In your pantry"))
        return out
    }

    /// Choices whose name contains `query`, ignoring case and accents; all of them when empty.
    static func filter(_ choices: [SameItemChoice], by query: String) -> [SameItemChoice] {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return choices }
        return choices.filter { $0.name.localizedStandardContains(trimmed) }
    }

    private static func folded(_ name: String) -> String {
        name.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: nil)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// Typed wrappers for `/api/v1/households/{householdId}/same-items` (docs/api.md). Linking and
/// unlinking need `plan.edit`. Use them through `AuthSession.authorized`.
nonisolated struct SameItemsAPI: Sendable {
    let client: APIClient

    func link(householdID: String, request body: SameItemRequest, accessToken: String) async throws -> SameItemLink {
        let request = try APIRequest.post(Self.path(householdID), body: body)
        return try await client.send(request.authorized(with: accessToken))
    }

    func unlink(householdID: String, linkID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete(Self.path(householdID) + "/\(linkID)").authorized(with: accessToken))
    }

    static func path(_ householdID: String) -> String {
        "/api/v1/households/\(householdID)/same-items"
    }
}

/// Links and unlinks items for the Shop tab. The list itself comes back merged from the API, so
/// nothing is kept here but the change count that tells the list to re-match.
@Observable
final class SameItemStore {
    /// Incremented after every change, so an open list re-matches.
    private(set) var revision = 0

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: SameItemsAPI?

    private static let logger = Logger(subsystem: "DinnerOS", category: "same-items")

    init(session: AuthSession, api: SameItemsAPI?) {
        self.session = session
        self.api = api
    }

    @discardableResult
    func link(_ request: SameItemRequest, householdID: String) async throws -> SameItemLink {
        guard let api else { throw AuthSessionError.notConfigured }
        let stored = try await session.authorized { token in
            try await api.link(householdID: householdID, request: request, accessToken: token)
        }
        revision += 1
        Self.logger.info("Items linked")
        return stored
    }

    func unlink(linkID: String, householdID: String) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            try await session.authorized { token in
                try await api.unlink(householdID: householdID, linkID: linkID, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            // Someone else undid it. The end state is the one we wanted.
        }
        revision += 1
        Self.logger.info("Items unlinked")
    }
}
