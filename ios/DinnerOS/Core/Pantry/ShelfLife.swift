import Foundation

/// A recommended best-by date from the server's shelf-life library
/// (`GET .../pantry/shelf-life`): the USDA FoodKeeper times, or a typical one for a food it
/// doesn't know.
nonisolated struct ShelfLifeSuggestion: Decodable, Equatable, Sendable {
    /// `YYYY-MM-DD`.
    let bestBy: String
    let storedOn: String
    let storage: PantryStorage
    /// "2–3 weeks".
    let text: String
    /// The library food the time is for ("Carrots, parsnips"); `nil` for an estimate.
    var matched: String? = nil
    /// The library didn't know the food; the time is typical for its kind.
    var estimate = false
    let source: String
    /// Where the food is usually kept, for the form's starting choice.
    var usualStorage: PantryStorage? = nil

    private enum CodingKeys: String, CodingKey {
        case bestBy, storedOn, storage, text, matched, estimate, source, usualStorage
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        bestBy = try c.decode(String.self, forKey: .bestBy)
        storedOn = try c.decode(String.self, forKey: .storedOn)
        storage = try c.decode(PantryStorage.self, forKey: .storage)
        text = try c.decode(String.self, forKey: .text)
        matched = c.decodeLenient(String.self, forKey: .matched)
        estimate = c.decodeLenientBool(forKey: .estimate) ?? false
        source = (try? c.decode(String.self, forKey: .source)) ?? ""
        usualStorage = c.decodeLenient(PantryStorage.self, forKey: .usualStorage)
    }

    /// "2–3 weeks in the fridge", with where the time comes from.
    var explanation: String {
        let place = storage == .fridge ? "the fridge" : storage == .freezer ? "the freezer" : "the pantry"
        if estimate {
            return String(localized: "About \(text) in \(place), typical for foods like this.")
        }
        return String(localized: "\(text) in \(place), from USDA FoodKeeper.")
    }
}

nonisolated extension PantryAPI {
    func shelfLife(
        householdID: String, name: String, category: String?, storage: PantryStorage, storedOn: String,
        accessToken: String
    ) async throws -> ShelfLifeSuggestion {
        var request = APIRequest.get(Self.path(householdID) + "/shelf-life")
        var items = [
            URLQueryItem(name: "name", value: name), URLQueryItem(name: "storage", value: storage.rawValue),
            URLQueryItem(name: "storedOn", value: storedOn),
        ]
        if let category, !category.isEmpty { items.append(URLQueryItem(name: "category", value: category)) }
        request.queryItems = items
        return try await client.send(request.authorized(with: accessToken))
    }
}

extension PantryStore {
    /// The recommended best-by date for `name` kept in `storage` from `storedOn`; `nil` when it
    /// can't be looked up (the form then leaves the date to the member).
    func shelfLife(name: String, category: String?, storage: PantryStorage, storedOn: String) async
        -> ShelfLifeSuggestion?
    {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return try? await shelfLifeLookup(name: trimmed, category: category, storage: storage, storedOn: storedOn)
    }
}
